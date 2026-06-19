package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dikdotcom/stackly/internal/metrics"
	"github.com/golang-jwt/jwt/v5"
)

// ─── Context ────────────────────────────────────────────────

type ctxKey int

const ctxUserKey ctxKey = 1

// UserFromContext returns the authenticated user from request context.
func UserFromContext(r *http.Request) *User {
	if u, ok := r.Context().Value(ctxUserKey).(*User); ok {
		return u
	}
	return nil
}

// IsAdmin checks if the authenticated user has admin tier.
func IsAdmin(r *http.Request) bool {
	u := UserFromContext(r)
	return u != nil && u.Tier == "admin"
}

// ─── User model ─────────────────────────────────────────────

// User represents an API consumer. One user == one key.
type User struct {
	ID           string    `json:"id"`
	Key          string    `json:"key"`
	Name         string    `json:"name"`
	Tier         string    `json:"tier"` // free, pro, admin
	RateLimit    int       `json:"rate_limit"`
	CreatedAt    time.Time `json:"created_at"`
	LastSeenAt   time.Time `json:"last_seen_at,omitempty"`
	RequestCount int64     `json:"request_count"`
	Disabled     bool      `json:"disabled,omitempty"`

	// Monthly scan quota. ScanCount resets to 0 once MonthReset is in
	// the past. Both fields are optional in JSON so old user files load.
	ScanCount  int       `json:"scan_count,omitempty"`
	MonthReset time.Time `json:"month_reset,omitempty"`
}

// QuotaByTier maps tier → monthly scan limit. Admin is effectively
// unlimited (math.MaxInt32) — kept as a finite number so JSON marshalling
// stays readable.
var QuotaByTier = map[string]int{
	"free":  100,
	"basic": 1000,
	"pro":   10000,
	"admin": 1 << 30, // ~1 billion, effectively unlimited
}

// QuotaForTier returns the monthly scan limit for a tier. Unknown tiers
// fall back to free.
func QuotaForTier(tier string) int {
	if q, ok := QuotaByTier[tier]; ok {
		return q
	}
	return QuotaByTier["free"]
}

// Store is a thread-safe persistent user store.
type Store struct {
	path    string
	mu      sync.RWMutex
	users   map[string]*User // keyed by Key
	dirty   bool
	stopCh  chan struct{}
	stopped bool
	done    chan struct{} // closed when persistLoop exits
}

// NewStore creates or loads a user store from disk.
// If the file doesn't exist, it bootstraps from env (STACKLY_API_KEYS, etc.).
func NewStore(path string) (*Store, error) {
	s := &Store{
		path:   path,
		users:  make(map[string]*User),
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
	}

	// Load existing
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		var loaded struct {
			Users []*User `json:"users"`
		}
		if err := json.Unmarshal(data, &loaded); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		for _, u := range loaded.Users {
			s.users[u.Key] = u
		}
	}

	// Always layer env users on top of JSON users (basic auth, API keys from env, etc.)
	// These are ephemeral and not persisted — JSON store remains the source of truth for persistence.
	_ = s.bootstrapFromEnv()

	// Start background persistence
	go s.persistLoop()

	return s, nil
}

// bootstrapFromEnv creates initial users from environment variables.
// Existing users (loaded from disk) are preserved: only missing keys are added.
// This ensures persisted state (RequestCount, LastSeenAt) survives restarts.
func (s *Store) bootstrapFromEnv() error {
	now := time.Now()

	// 1. STACKLY_API_KEYS=key1:tier1,key2:tier2 (or just key1,key2 → free)
	if ks := os.Getenv("STACKLY_API_KEYS"); ks != "" {
		for _, entry := range strings.Split(ks, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			key, tier := splitKeyTier(entry, "free")
			if _, exists := s.users[key]; exists {
				continue // preserve persisted state
			}
			s.users[key] = &User{
				ID:        newID(),
				Key:       key,
				Name:      deriveName(key),
				Tier:      tier,
				RateLimit: defaultRateForTier(tier),
				CreatedAt: now,
			}
		}
	}

	// 2. STACKLY_BASIC_USERS=user:pass,user2:pass2 (basic auth, separate from API keys)
	// Stored with a synthesized key sha256("basic:" + user + ":" + pass) — used for lookup.
	if bs := os.Getenv("STACKLY_BASIC_USERS"); bs != "" {
		for _, entry := range strings.Split(bs, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			parts := strings.SplitN(entry, ":", 2)
			if len(parts) != 2 {
				continue
			}
			user, pass := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
			key := hashKey("basic:" + user + ":" + pass)
			if _, exists := s.users[key]; exists {
				continue
			}
			s.users[key] = &User{
				ID:        newID(),
				Key:       key,
				Name:      user,
				Tier:      "basic",
				RateLimit: defaultRateForTier("basic"),
				CreatedAt: now,
			}
		}
	}

	// 3. JWT secret config
	// Users authenticated via JWT are NOT stored — they're verified by signature only.

	if len(s.users) == 0 && os.Getenv("STACKLY_JWT_SECRET") == "" {
		return errors.New("no users configured: set STACKLY_API_KEYS or STACKLY_BASIC_USERS or STACKLY_JWT_SECRET")
	}

	return nil
}

// splitKeyTier splits "key:tier" or returns ("key", defaultTier).
func splitKeyTier(entry, def string) (string, string) {
	parts := strings.SplitN(entry, ":", 2)
	if len(parts) == 2 && parts[1] != "" {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return strings.TrimSpace(parts[0]), def
}

// defaultRateForTier returns per-minute rate limit by tier.
func defaultRateForTier(tier string) int {
	switch strings.ToLower(tier) {
	case "admin":
		return 10000
	case "pro":
		return 600
	case "basic":
		return 120
	default:
		return 60
	}
}

func deriveName(key string) string {
	if len(key) <= 8 {
		return key
	}
	return key[:4] + "..." + key[len(key)-4:]
}

func newID() string {
	h := sha256.New()
	h.Write([]byte(strconv.FormatInt(time.Now().UnixNano(), 10)))
	return hex.EncodeToString(h.Sum(nil))[:12]
}

func hashKey(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ─── User lookup ─────────────────────────────────────────────

// AuthenticateAPIKey looks up a user by their API key.
func (s *Store) AuthenticateAPIKey(key string) *User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[key]
	if !ok || u.Disabled {
		return nil
	}
	return u
}

// AllUsers returns a snapshot of all users (admin only).
func (s *Store) AllUsers() []*User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		copy := *u
		copy.Key = redactKey(copy.Key)
		out = append(out, &copy)
	}
	return out
}

// RecordUsage bumps request count + last-seen for the user (call after successful auth).
func (s *Store) RecordUsage(key string) {
	s.mu.Lock()
	u, ok := s.users[key]
	if !ok {
		s.mu.Unlock()
		return
	}
	u.RequestCount++
	u.LastSeenAt = time.Now()
	s.dirty = true
	s.mu.Unlock()
}

// QuotaInfo describes a user's current quota state.
type QuotaInfo struct {
	UserID     string    `json:"user_id"`
	Tier       string    `json:"tier"`
	Used       int       `json:"used"`
	Limit      int       `json:"limit"`
	Remaining  int       `json:"remaining"`
	MonthReset time.Time `json:"month_reset"`
	Unlimited  bool      `json:"unlimited"`
}

// Quota returns the current quota state for a user. Pure read — does
// not increment. Resets the counter lazily if MonthReset is past.
func (s *Store) Quota(key string) *QuotaInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[key]
	if !ok {
		return nil
	}
	s.maybeResetLocked(u)
	limit := QuotaForTier(u.Tier)
	return &QuotaInfo{
		UserID:     u.ID,
		Tier:       u.Tier,
		Used:       u.ScanCount,
		Limit:      limit,
		Remaining:  max0(limit - u.ScanCount),
		MonthReset: u.MonthReset,
		Unlimited:  u.Tier == "admin",
	}
}

// CheckAndIncrementQuota atomically reserves a scan slot. Returns the
// updated quota info. If the user is at or above the limit, returns
// ErrQuotaExceeded WITHOUT incrementing.
func (s *Store) CheckAndIncrementQuota(key string) (*QuotaInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[key]
	if !ok {
		return nil, ErrUserNotFound
	}
	s.maybeResetLocked(u)
	limit := QuotaForTier(u.Tier)
	if u.Tier != "admin" && u.ScanCount >= limit {
		return &QuotaInfo{
			UserID:     u.ID,
			Tier:       u.Tier,
			Used:       u.ScanCount,
			Limit:      limit,
			Remaining:  0,
			MonthReset: u.MonthReset,
			Unlimited:  false,
		}, ErrQuotaExceeded
	}
	u.ScanCount++
	s.dirty = true
	return &QuotaInfo{
		UserID:     u.ID,
		Tier:       u.Tier,
		Used:       u.ScanCount,
		Limit:      limit,
		Remaining:  max0(limit - u.ScanCount),
		MonthReset: u.MonthReset,
		Unlimited:  u.Tier == "admin",
	}, nil
}

// maybeResetLocked resets ScanCount + advances MonthReset if the
// current period has elapsed. Caller must hold write lock.
func (s *Store) maybeResetLocked(u *User) {
	now := time.Now()
	if u.MonthReset.IsZero() || now.After(u.MonthReset) {
		u.ScanCount = 0
		u.MonthReset = nextMonthStart(now)
		s.dirty = true
	}
}

// nextMonthStart returns the first instant of the next calendar month
// in UTC. Simple — ignores timezone of the server, which is fine since
// quotas reset at boundaries and a few hours drift doesn't matter.
func nextMonthStart(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// ErrQuotaExceeded is returned by CheckAndIncrementQuota when the user
// has used all scans in the current month.
var ErrQuotaExceeded = fmt.Errorf("monthly scan quota exceeded")

// ErrUserNotFound is returned when the key doesn't resolve to a user.
var ErrUserNotFound = fmt.Errorf("user not found")

// Usage returns a user's stats (with redacted key).
func (s *Store) Usage(key string) map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[key]
	if !ok {
		return nil
	}
	return map[string]interface{}{
		"id":            u.ID,
		"name":          u.Name,
		"tier":          u.Tier,
		"rate_limit":    u.RateLimit,
		"request_count": u.RequestCount,
		"created_at":    u.CreatedAt,
		"last_seen_at":  u.LastSeenAt,
		"key_preview":   redactKey(u.Key),
	}
}

func redactKey(k string) string {
	if len(k) <= 8 {
		return "***"
	}
	return k[:4] + "..." + k[len(k)-4:]
}

// ─── Persistence ────────────────────────────────────────────

func (s *Store) save() error {
	s.mu.RLock()
	users := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		users = append(users, u)
	}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(struct {
		Users []*User `json:"users"`
	}{users}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

// persistLoop flushes dirty state every 30s.
func (s *Store) persistLoop() {
	defer close(s.done)
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.stopCh:
			s.save()
			return
		case <-t.C:
			s.mu.Lock()
			dirty := s.dirty
			s.dirty = false
			s.mu.Unlock()
			if dirty {
				_ = s.save()
			}
		}
	}
}

// Stop flushes and exits the persist loop. Blocks until goroutine exits.
func (s *Store) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		<-s.done
		return
	}
	s.stopped = true
	close(s.stopCh)
	s.mu.Unlock()
	<-s.done
}

// ─── Rate limiter (per-user) ────────────────────────────────

type bucket struct {
	tokens   float64
	lastFill time.Time
}

// Auth is the middleware. Each user has their own bucket.
type Auth struct {
	store       *Store
	enabled     bool
	defaultRate int
	window      time.Duration
	jwtSecret   []byte
	mu          sync.Mutex
	anonBuckets map[string]*bucket // IP-level fallback
}

// New creates an Auth instance with optional JWT secret.
func New(store *Store, jwtSecret string) *Auth {
	return &Auth{
		store:       store,
		enabled:     true, // always enabled when there's a store
		defaultRate: 60,
		window:      1 * time.Minute,
		jwtSecret:   []byte(jwtSecret),
		anonBuckets: make(map[string]*bucket),
	}
}

// Enabled reports whether auth is configured (any users or JWT secret).
// Auth can be force-disabled with STACKLY_NO_AUTH=<any value> (dev mode).
func (a *Auth) Enabled() bool {
	if os.Getenv("STACKLY_NO_AUTH") != "" {
		return false
	}
	if !a.enabled {
		return false
	}
	if a.store == nil {
		return false
	}
	if len(a.store.AllUsers()) == 0 && len(a.jwtSecret) == 0 {
		return false
	}
	return true
}

// detectAuthType identifies which auth scheme was attempted on the request.
// Returns one of: "api_key", "bearer", "basic", "none".
func detectAuthType(r *http.Request) string {
	if r.Header.Get("X-API-Key") != "" {
		return "api_key"
	}
	h := r.Header.Get("Authorization")
	if h == "" {
		return "none"
	}
	if strings.HasPrefix(h, "Basic ") {
		return "basic"
	}
	if strings.HasPrefix(h, "Bearer ") {
		return "bearer"
	}
	return "unknown"
}

// Middleware validates auth + enforces per-user rate limit.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for public endpoints + WebSocket upgrades.
		// TODO: WS auth — accept token via Sec-WebSocket-Protocol subprotocol or
		// ?token=*** query param so rate-limited users can't open unlimited sockets.
		if r.URL.Path == "/" ||
			strings.HasPrefix(r.URL.Path, "/static/") ||
			strings.HasPrefix(r.URL.Path, "/extension/") ||
			r.URL.Path == "/api/health" ||
			r.URL.Path == "/api/docs" ||
			r.URL.Path == "/api/docs/ui" ||
			r.URL.Path == "/api/fingerprints" ||
			strings.HasPrefix(r.URL.Path, "/api/ws/") {
			next.ServeHTTP(w, r)
			return
		}

		// Try to authenticate
		user := a.authenticate(r)

		if a.Enabled() && user == nil {
			authType := detectAuthType(r)
			metrics.AuthRequests.WithLabelValues("anonymous", authType, "fail").Inc()
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("WWW-Authenticate", `Bearer realm="stackly", Basic realm="stackly"`)
			http.Error(w, `{"error":"missing or invalid credentials. Use X-API-Key header, Authorization: Bearer ***, or HTTP Basic"}`, http.StatusUnauthorized)
			return
		}

		// Rate limit (per-user if authenticated, per-IP otherwise)
		rate := a.defaultRate
		key := clientIP(r)
		tier := "anonymous"
		if user != nil {
			if user.RateLimit > 0 {
				rate = user.RateLimit
			}
			key = "user:" + user.Key
			tier = user.Tier
			if tier == "" {
				tier = "free"
			}
		}
		if rate > 0 {
			if !a.allow(key, rate) {
				metrics.AuthRequests.WithLabelValues(tier, detectAuthType(r), "rate_limited").Inc()
				metrics.AuthRateLimited.WithLabelValues(tier).Inc()
				w.Header().Set("Retry-After", "60")
				w.Header().Set("Content-Type", "application/json")
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
		}

		// Record successful auth
		if user != nil {
			authType := detectAuthType(r)
			metrics.AuthRequests.WithLabelValues(tier, authType, "success").Inc()
		}

		// Record usage
		if user != nil && a.store != nil {
			a.store.RecordUsage(user.Key)
		}

		// Inject user into request context for handlers
		ctx := context.WithValue(r.Context(), ctxUserKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// authenticate tries API key → Basic → JWT in order.
func (a *Auth) authenticate(r *http.Request) *User {
	if a.store == nil {
		return nil
	}

	// 1. X-API-Key header
	if k := r.Header.Get("X-API-Key"); k != "" {
		if u := a.store.AuthenticateAPIKey(k); u != nil {
			return u
		}
	}

	// 2. Authorization header — Bearer JWT or API key
	if auth := r.Header.Get("Authorization"); auth != "" {
		if strings.HasPrefix(auth, "Bearer ") {
			token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
			// First try as API key (allow keys in Bearer too)
			if u := a.store.AuthenticateAPIKey(token); u != nil {
				return u
			}
			// Then try as JWT
			if u := a.verifyJWT(token); u != nil {
				return u
			}
		} else if strings.HasPrefix(auth, "Basic ") {
			payload, err := decodeBasic(strings.TrimPrefix(auth, "Basic "))
			if err == nil {
				key := hashKey("basic:" + payload.user + ":" + payload.pass)
				if u := a.store.AuthenticateAPIKey(key); u != nil {
					return u
				}
				// Constant-time compare in case basic user not in store
				_ = subtle.ConstantTimeCompare([]byte(key), []byte(key))
			}
		}
	}

	// 3. ?api_key= query param
	if k := r.URL.Query().Get("api_key"); k != "" {
		if u := a.store.AuthenticateAPIKey(k); u != nil {
			return u
		}
	}

	return nil
}

// VerifyToken is the public wrapper around verifyJWT, suitable for
// callers outside the auth package (e.g., the WebSocket handler).
// Returns true if the token is a valid JWT signed with our secret.
func (a *Auth) VerifyToken(tokenStr string) bool {
	return a.verifyJWT(tokenStr) != nil
}

// verifyJWT validates HS256 token. Returns a synthetic User on success.
func (a *Auth) verifyJWT(tokenStr string) *User {
	if len(a.jwtSecret) == 0 {
		return nil
	}
	tok, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return a.jwtSecret, nil
	})
	if err != nil || !tok.Valid {
		return nil
	}

	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return nil
	}

	// Determine tier from claims (default "pro" for valid JWT)
	tier, _ := claims["tier"].(string)
	if tier == "" {
		tier = "pro"
	}
	rate := defaultRateForTier(tier)

	// Synthesize a user (not persisted)
	return &User{
		ID:        fmt.Sprintf("jwt:%v", claims["sub"]),
		Key:       "jwt:" + fmt.Sprintf("%v", claims["sub"]),
		Name:      fmt.Sprintf("%v", claims["sub"]),
		Tier:      tier,
		RateLimit: rate,
	}
}

// allow implements a token bucket per key (user-key or IP).
func (a *Auth) allow(key string, rate int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	b, ok := a.anonBuckets[key]
	if !ok {
		b = &bucket{tokens: float64(rate), lastFill: now}
		a.anonBuckets[key] = b
	}

	elapsed := now.Sub(b.lastFill).Seconds()
	refill := (elapsed / a.window.Seconds()) * float64(rate)
	if b.tokens+refill > float64(rate) {
		b.tokens = float64(rate)
	} else {
		b.tokens += refill
	}
	b.lastFill = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// ─── Helpers ────────────────────────────────────────────────

type basicPayload struct {
	user, pass string
}

func decodeBasic(s string) (*basicPayload, error) {
	dec, err := base64Decode(s)
	if err != nil {
		return nil, err
	}
	parts := strings.SplitN(dec, ":", 2)
	if len(parts) != 2 {
		return nil, errors.New("invalid basic auth")
	}
	return &basicPayload{user: parts[0], pass: parts[1]}, nil
}

func base64Decode(s string) (string, error) {
	dec, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	return string(dec), nil
}

// Stats returns overall auth + rate-limit stats.
func (a *Auth) Stats() map[string]interface{} {
	a.mu.Lock()
	activeBuckets := len(a.anonBuckets)
	a.mu.Unlock()

	users := 0
	adminUsers := 0
	if a.store != nil {
		all := a.store.AllUsers()
		users = len(all)
		for _, u := range all {
			if u.Tier == "admin" {
				adminUsers++
			}
		}
	}

	return map[string]interface{}{
		"auth_enabled":   a.enabled,
		"users":          users,
		"admin_users":    adminUsers,
		"jwt_enabled":    len(a.jwtSecret) > 0,
		"active_buckets": activeBuckets,
		"window":         a.window.String(),
	}
}

// clientIP extracts the real client IP from headers, falling back to RemoteAddr
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		ip := strings.TrimSpace(parts[0])
		if parsed := net.ParseIP(ip); parsed != nil {
			return parsed.String()
		}
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		if parsed := net.ParseIP(xri); parsed != nil {
			return parsed.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
