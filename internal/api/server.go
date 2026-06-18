package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/dikdotcom/stackly/internal/auth"
	"github.com/dikdotcom/stackly/internal/cache"
	"github.com/dikdotcom/stackly/internal/fingerprint"
	"github.com/dikdotcom/stackly/internal/metrics"
	"github.com/dikdotcom/stackly/internal/queue"
	"github.com/dikdotcom/stackly/internal/scanner"
	"github.com/dikdotcom/stackly/internal/scheduler"
	"github.com/dikdotcom/stackly/internal/ws"
)

// Server is the HTTP API server
type Server struct {
	queue       *queue.JobQueue
	cache       *cache.Cache
	auth        *auth.Auth
	userStore   *auth.Store
	mux         *http.ServeMux
	webDir      string
	hub         *ws.Hub
	fingerprints *fingerprint.Database
	schedStore  *scheduler.Store
}

// NewServer creates a new API server
func NewServer(q *queue.JobQueue, c *cache.Cache, webDir string, a *auth.Auth, store *auth.Store, hub *ws.Hub, db *fingerprint.Database) *Server {
	s := &Server{
		queue:       q,
		cache:       c,
		auth:        a,
		userStore:   store,
		mux:         http.NewServeMux(),
		webDir:      webDir,
		hub:         hub,
		fingerprints: db,
	}
	s.routes()
	return s
}

// SetSchedulerStore injects the schedule store for the /api/schedules
// endpoints. Optional — server runs fine without it (returns 503).
func (s *Server) SetSchedulerStore(st *scheduler.Store) {
	s.schedStore = st
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/", s.handleAPI)
	s.mux.HandleFunc("/api/ws/scan/", s.handleScanWS)
	s.mux.HandleFunc("/static/", s.handleStatic)
	s.mux.HandleFunc("/extension/", s.handleExtension)
	s.mux.HandleFunc("/metrics", s.handleMetrics)
	s.mux.HandleFunc("/", s.handleRoot)
}

// openAPIPath is the filesystem location of the OpenAPI spec.
const openAPIPath = "docs/openapi.yaml"

// swaggerUIHTML is a minimal Swagger UI that loads the spec from /api/docs.
const swaggerUIHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8" />
<title>Stackly API Docs</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
  window.onload = () => {
    window.ui = SwaggerUIBundle({
      url: "/api/docs",
      dom_id: "#swagger-ui",
      deepLinking: true,
      presets: [SwaggerUIBundle.presets.apis],
    });
  };
</script>
</body>
</html>
`

// handleRoot serves the SPA shell
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.webDir, "index.html"))
}

// handleStatic serves static files (JS, CSS)
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/static/")
	fullPath := filepath.Join(s.webDir, "static", path)
	http.ServeFile(w, r, fullPath)
}

// handleExtension serves Chrome extension artifacts (zip + README).
// Files live under webDir/extension/ and are bundled at build time.
func (s *Server) handleExtension(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/extension/")
	fullPath := filepath.Join(s.webDir, "extension", path)
	if path == "" || strings.Contains(path, "..") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, fullPath)
}

// handleMetrics serves Prometheus metrics. Public — scrape-friendly.
// Auth-protected deployments should restrict access at the network layer.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	metrics.Handler().ServeHTTP(w, r)
}

// handleScanWS upgrades to WebSocket and streams live progress for a scan job.
//
// Auth: the WS endpoint sits in the middleware skip list. We pass an
// authenticator closure that validates tokens from the subprotocol header
// or ?token= query param. In dev/no-auth mode we pass nil to allow all.
func (s *Server) handleScanWS(w http.ResponseWriter, r *http.Request) {
	var auth ws.TokenAuthenticator
	if s.auth != nil && s.auth.Enabled() && s.userStore != nil {
		auth = func(token string) bool {
			// Try API key first, then fall back to JWT.
			if s.userStore.AuthenticateAPIKey(token) != nil {
				return true
			}
			return s.auth.VerifyToken(token) // exported wrapper
		}
	}
	s.hub.HandleScanWS(w, r, auth)
}

// handleAPI routes /api/* requests
func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch path {
	case "/api/scan":
		s.handleScan(w, r)
	case "/api/stats":
		s.handleStats(w, r)
	case "/api/health":
		s.handleHealth(w, r)
	case "/api/jobs":
		s.handleListJobs(w, r)
	case "/api/cache/stats":
		s.handleCacheStats(w, r)
	case "/api/cache/clear":
		s.handleCacheClear(w, r)
	case "/api/auth/stats":
		s.handleAuthStats(w, r)
	case "/api/auth/usage":
		s.handleUsage(w, r)
	case "/api/auth/users":
		s.handleUsersList(w, r)
	case "/api/auth/quota":
		s.handleQuota(w, r)
	case "/api/docs":
		s.handleOpenAPI(w, r)
	case "/api/docs/ui":
		s.handleSwaggerUI(w, r)
	case "/api/fingerprints":
		s.handleFingerprints(w, r)
	case "/api/schedules":
		s.handleSchedules(w, r)
	default:
		if strings.HasPrefix(path, "/api/results/") {
			s.handleResults(w, r)
			return
		}
		if strings.HasPrefix(path, "/api/report/") {
			s.handleReports(w, r)
			return
		}
		if strings.HasPrefix(path, "/api/schedules/") {
			s.handleScheduleSubpath(w, r)
			return
		}
		if strings.HasPrefix(path, "/api/webhooks/") {
			s.handleWebhookSubpath(w, r)
			return
		}
		http.NotFound(w, r)
	}
}

// handleCacheStats returns cache statistics
func (s *Server) handleCacheStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.cache == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
		return
	}
	writeJSON(w, http.StatusOK, s.cache.Stats())
}

// handleCacheClear clears all cache entries
func (s *Server) handleCacheClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.cache == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
		return
	}
	s.cache.Clear()
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// Handler returns the HTTP handler with CORS + auth + metrics middleware applied
func (s *Server) Handler() http.Handler {
	// Inner chain: CORS → auth → mux
	inner := corsMiddleware(s.auth.Middleware(s.mux))
	// Outer: metrics middleware records per-request labels using a low-cardinality path resolver.
	return metrics.HTTPMiddleware(routePattern, inner)
}

// routePattern returns a low-cardinality route label for metrics.
// Raw paths like /api/results/<id> collapse to /api/results/:id so we
// don't blow up Prometheus cardinality.
func routePattern(r *http.Request) string {
	p := r.URL.Path
	switch {
	case p == "/metrics":
		return "/metrics"
	case p == "/api/health":
		return "/api/health"
	case p == "/api/scan":
		return "/api/scan"
	case p == "/api/stats":
		return "/api/stats"
	case p == "/api/jobs":
		return "/api/jobs"
	case p == "/api/cache/stats":
		return "/api/cache/stats"
	case p == "/api/cache/clear":
		return "/api/cache/clear"
	case p == "/api/auth/stats":
		return "/api/auth/stats"
	case p == "/api/auth/usage":
		return "/api/auth/usage"
	case p == "/api/auth/users":
		return "/api/auth/users"
	case p == "/api/docs":
		return "/api/docs"
	case p == "/api/docs/ui":
		return "/api/docs/ui"
	case strings.HasPrefix(p, "/api/results/"):
		return "/api/results/:id"
	case strings.HasPrefix(p, "/static/"):
		return "/static/*"
	case strings.HasPrefix(p, "/extension/"):
		return "/extension/*"
	default:
		return "/"
	}
}

// corsMiddleware adds CORS headers
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleAuthStats returns auth + rate-limit stats
func (s *Server) handleAuthStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, s.auth.Stats())
}

// handleUsage returns the calling user's usage stats
func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	user := auth.UserFromContext(r)
	if user == nil {
		http.Error(w, `{"error":"no user in context"}`, http.StatusUnauthorized)
		return
	}

	// JWT users are not persisted; return synthetic usage
	if strings.HasPrefix(user.Key, "jwt:") {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"id":            user.ID,
			"name":          user.Name,
			"tier":          user.Tier,
			"rate_limit":    user.RateLimit,
			"request_count": -1, // not tracked for JWT
			"auth_type":     "jwt",
			"key_preview":   "jwt:" + user.Name,
		})
		return
	}

	usage := s.userStore.Usage(user.Key)
	if usage == nil {
		http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, usage)
}

// handleUsersList returns all users (admin only)
func (s *Server) handleUsersList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if !auth.IsAdmin(r) {
		http.Error(w, `{"error":"admin only"}`, http.StatusForbidden)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"users": s.userStore.AllUsers(),
	})
}

// handleOpenAPI serves the OpenAPI YAML spec.
func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	http.ServeFile(w, r, openAPIPath)
}

// handleSwaggerUI serves a minimal Swagger UI page.
func (s *Server) handleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(swaggerUIHTML))
}

// handleFingerprints returns a minimal slug→{name,website,category} map for all
// known technologies. Used by the frontend to build clickable website links
// (including for implied technologies that weren't directly detected).
func (s *Server) handleFingerprints(w http.ResponseWriter, r *http.Request) {
	type techInfo struct {
		Name     string `json:"name"`
		Website  string `json:"website,omitempty"`
		Category string `json:"category"`
	}
	out := make(map[string]techInfo, len(s.fingerprints.Technologies))
	for _, t := range s.fingerprints.Technologies {
		out[t.Slug] = techInfo{Name: t.Name, Website: t.Website, Category: t.Category}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	writeJSON(w, http.StatusOK, out)
}

// handleScan handles POST /api/scan
func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		URL     string `json:"url"`
		Wait    bool   `json:"wait,omitempty"`
		Timeout int    `json:"timeout,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
		return
	}

	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "URL is required"})
		return
	}

	// Auto-prepend scheme if missing (e.g. user submits "example.com")
	if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
		req.URL = "https://" + req.URL
	}

	// Validate URL — blocks internal targets (SSRF defense). Same rule
	// as schedule creation; consistent surface for users.
	if err := scheduler.ValidateScanURL(req.URL); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "invalid url: " + err.Error(),
		})
		return
	}

	// Quota check. Only enforced when auth is enabled and the user is
	// resolved. In dev/no-auth mode the queue still accepts unlimited
	// scans (we don't want to gate local testing on a quota wall).
	if s.auth != nil && s.auth.Enabled() {
		if u := auth.UserFromContext(r); u != nil {
			info, err := s.userStore.CheckAndIncrementQuota(u.Key)
			if err == auth.ErrQuotaExceeded {
				writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
					"error":       "monthly scan quota exceeded",
					"tier":        info.Tier,
					"used":        info.Used,
					"limit":       info.Limit,
					"month_reset": info.MonthReset,
				})
				return
			}
			if err != nil && err != auth.ErrUserNotFound {
				// Other errors (DB corruption, etc.) — fail open with a
				// warning header. Refusing all scans on a quota error
				// would be worse UX than a transient miss.
				w.Header().Set("X-Quota-Warning", err.Error())
			}
			// Expose quota headers so callers can budget their usage.
			if info != nil {
				w.Header().Set("X-Quota-Used", fmt.Sprintf("%d", info.Used))
				w.Header().Set("X-Quota-Limit", fmt.Sprintf("%d", info.Limit))
				w.Header().Set("X-Quota-Remaining", fmt.Sprintf("%d", info.Remaining))
			}
		}
	}

	job, err := s.queue.Enqueue(req.URL)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}

	// If wait=true, poll until done
	if req.Wait {
		s.waitForJob(job.ID, req.Timeout)
		job = s.queue.GetJob(job.ID)
	}

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"id":     job.ID,
		"url":    job.URL,
		"status": job.Status,
	})
}

// handleResults handles GET /api/results/:id
func (s *Server) handleResults(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/results/")
	if id == "" || strings.Contains(id, "/") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid ID"})
		return
	}

	job := s.queue.GetJob(id)
	if job == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Job not found"})
		return
	}

	// Check if ?wait=true
	if r.URL.Query().Get("wait") == "true" && job.Status == queue.StatusPending {
		s.waitForJob(id, 60)
		job = s.queue.GetJob(id)
	}

	// Compact mode: strip HTML, headers, scripts for smaller payload
	if r.URL.Query().Get("compact") == "true" || r.URL.Query().Get("compact") == "1" {
		compact := makeCompact(job)
		writeJSON(w, http.StatusOK, compact)
		return
	}

	writeJSON(w, http.StatusOK, job)
}

// compactResult is a slim view of a scan result
type compactResult struct {
	ID         string   `json:"id"`
	URL        string   `json:"url"`
	Status     string   `json:"status"`
	Duration   string   `json:"duration,omitempty"`
	Error      string   `json:"error,omitempty"`
	Detected   []string `json:"detected,omitempty"`
	Categories []string `json:"categories,omitempty"`
	Implied    []string `json:"implied,omitempty"`
}

func makeCompact(job *queue.Job) compactResult {
	c := compactResult{
		ID:     job.ID,
		URL:    job.URL,
		Status: string(job.Status),
	}

	if job.Duration != "" {
		c.Duration = job.Duration
	}
	if job.Error != "" {
		c.Error = job.Error
	}

	if job.Result != nil {
		catSet := make(map[string]bool)
		for _, r := range job.Result.Results {
			c.Detected = append(c.Detected, r.Technology.Name)
			if !catSet[r.Technology.Category] {
				catSet[r.Technology.Category] = true
				c.Categories = append(c.Categories, r.Technology.Category)
			}
		}
		c.Implied = scanner.GetImplied(job.Result.Results)
	}

	return c
}

// handleListJobs handles GET /api/jobs
func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	jobs := s.queue.ListJobs(50)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"count": len(jobs),
		"jobs":  jobs,
	})
}

// handleStats handles GET /api/stats
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := s.queue.Stats()

	resp := map[string]interface{}{
		"queue": stats,
	}
	if s.cache != nil {
		resp["cache"] = s.cache.Stats()
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleHealth handles GET /api/health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "ok",
		"time":   time.Now().Format(time.RFC3339),
	})
}

// waitForJob polls for job completion
func (s *Server) waitForJob(id string, maxSeconds int) {
	if maxSeconds <= 0 {
		maxSeconds = 60
	}
	if maxSeconds > 300 {
		maxSeconds = 300
	}

	deadline := time.Now().Add(time.Duration(maxSeconds) * time.Second)
	for time.Now().Before(deadline) {
		job := s.queue.GetJob(id)
		if job != nil && (job.Status == queue.StatusCompleted || job.Status == queue.StatusFailed) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// writeJSON writes a JSON response
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}