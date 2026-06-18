package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/dikdotcom/stackly/internal/api"
	"github.com/dikdotcom/stackly/internal/auth"
	"github.com/dikdotcom/stackly/internal/cache"
	"github.com/dikdotcom/stackly/internal/fingerprint"
	"github.com/dikdotcom/stackly/internal/metrics"
	"github.com/dikdotcom/stackly/internal/queue"
	"github.com/dikdotcom/stackly/internal/scanner"
	"github.com/dikdotcom/stackly/internal/scheduler"
	"github.com/dikdotcom/stackly/internal/ws"
)

func main() {
	var (
		port         = flag.Int("port", 8899, "HTTP port")
		host         = flag.String("host", "0.0.0.0", "HTTP host")
		fpPath       = flag.String("fingerprints", "data/fingerprints.json", "Path to fingerprints JSON")
		webDir       = flag.String("web", "web", "Path to web directory (with index.html and static/)")
		workers      = flag.Int("workers", 2, "Number of scanner workers")
		chromePath   = flag.String("chrome", os.Getenv("CHROME_BIN"), "Chrome/Chromium binary path")
		readTimeout  = flag.Duration("read-timeout", 10*time.Second, "HTTP read timeout")
		writeTimeout = flag.Duration("write-timeout", 120*time.Second, "HTTP write timeout")
		uaRotate     = flag.Bool("rotate-ua", true, "Enable User-Agent rotation per scan")
		proxyURL     = flag.String("proxy", os.Getenv("PROXY_URL"), "Proxy URL (http://host:port or socks5://host:port)")
		cacheTTL     = flag.Duration("cache-ttl", 24*time.Hour, "Cache TTL for scan results")
		cachePath    = flag.String("cache-path", "data/cache.json", "Path to cache persistence file")
		usersPath    = flag.String("users", "data/users.json", "Path to user store JSON file")
		schedPath    = flag.String("schedules", "data/schedules.json", "Path to schedules+webhook log JSON file")
	)
	flag.Parse()

	// Set Chrome path if provided
	if *chromePath != "" {
		os.Setenv("CHROME_BIN", *chromePath)
	}

	// Load fingerprints
	fmt.Printf("→ Loading fingerprints from %s ...\n", *fpPath)
	db, err := fingerprint.Load(*fpPath)
	if err != nil {
		log.Fatalf("Failed to load fingerprints: %v", err)
	}
	fmt.Printf("✓ Loaded %d technologies\n", len(db.Technologies))

	// Create scanner
	s := scanner.NewScanner(db)
	s.SetTimeout(45 * time.Second)
	if *uaRotate {
		s.EnableUARotation()
		fmt.Printf("✓ User-Agent rotation enabled\n")
	}
	if *proxyURL != "" {
		s.SetProxy(*proxyURL)
		fmt.Printf("✓ Proxy configured: %s\n", *proxyURL)
	}

	// Create cache
	c := cache.NewCache(*cacheTTL, *cachePath)
	fmt.Printf("✓ Cache initialized (TTL: %s, persist: %s)\n", *cacheTTL, *cachePath)

	// WebSocket hub for live scan progress
	hub := ws.NewHub()

	// Create queue
	q := queue.NewJobQueue(s, c, hub, *workers)
	q.Start()
	fmt.Printf("✓ Started %d scanner workers\n", *workers)

	// Wire scanner progress events → WS hub
	s.ProgressCallback = func(jobID, phase string, progress int, message string, partial []scanner.Result) {
		techs := make([]ws.DetectedTech, 0, len(partial))
		for _, r := range partial {
			techs = append(techs, ws.DetectedTech{
				Slug:       r.Technology.Slug,
				Name:       r.Technology.Name,
				Category:   r.Technology.Category,
				Confidence: r.Confidence,
			})
		}
		hub.Publish(jobID, ws.Event{
			Type:     "progress",
			Status:   phase,
			Progress: progress,
			Message:  message,
			Techs:    techs,
		})
	}

	// Create user store (multi-user with usage tracking)
	store, err := auth.NewStore(*usersPath)
	if err != nil {
		log.Fatalf("Failed to load user store: %v", err)
	}
	defer store.Stop()
	fmt.Printf("✓ User store loaded (%d users, %s)\n", len(store.AllUsers()), *usersPath)

	// Create auth middleware (with optional JWT secret)
	authHandler := auth.New(store, os.Getenv("STACKLY_JWT_SECRET"))

	// Create API server (with auth + user store + ws hub)
	apiServer := api.NewServer(q, c, *webDir, authHandler, store, hub, db)

	// Initialize scheduler (Phase 2: scheduled re-scans + webhook delivery)
	schedStore, err := scheduler.NewStore(*schedPath)
	if err != nil {
		log.Fatalf("Failed to load scheduler store: %v", err)
	}
	defer schedStore.Close()
	// Wrap scanner.ScanURL as the scheduler's scan function. The scheduler
	// doesn't go through the queue (avoids polluting job history with
	// background scans and lets it run independently of the worker pool).
	scanFn := func(ctx context.Context, jobID, url string) *scanner.ScanResult {
		return s.ScanURL(ctx, jobID, url)
	}
	sched := scheduler.New(schedStore, scanFn)
	sched.Start()
	apiServer.SetSchedulerStore(schedStore)
	fmt.Printf("✓ Scheduler started (%d existing schedules)\n", len(schedStore.ListAllSchedules()))

	if authHandler.Enabled() {
		fmt.Printf("🔒 API auth enabled (%d users", len(store.AllUsers()))
		if os.Getenv("STACKLY_JWT_SECRET") != "" {
			fmt.Printf(" + JWT")
		}
		fmt.Printf(")\n")
		metrics.AuthEnabled.Set(1)
	} else {
		if os.Getenv("STACKLY_NO_AUTH") != "" {
			fmt.Printf("⚠ API auth disabled (STACKLY_NO_AUTH set)\n")
		} else {
			fmt.Printf("⚠ API auth disabled (no users or JWT secret)\n")
		}
		metrics.AuthEnabled.Set(0)
	}

	// Build metadata
	metrics.BuildInfo.WithLabelValues("1.1.0", runtime.Version()).Set(1)

	// Periodic gauge refresh for stats that change outside the request path.
	stopRefresh := make(chan struct{})
	go refreshGauges(q, c, stopRefresh)

	httpServer := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", *host, *port),
		Handler:      apiServer.Handler(),
		ReadTimeout:  *readTimeout,
		WriteTimeout: *writeTimeout,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		fmt.Printf("\n🚀 Stackly — Tech Stack Detector\n")
		fmt.Printf("   Listening on http://%s\n", httpServer.Addr)
		fmt.Printf("   Web UI:    http://%s/\n", httpServer.Addr)
		fmt.Printf("   Endpoints:\n")
		fmt.Printf("     POST /api/scan        Submit URL for scanning\n")
		fmt.Printf("     GET  /api/results/:id Get scan results\n")
		fmt.Printf("     GET  /api/jobs        List recent jobs\n")
		fmt.Printf("     GET  /api/stats       Queue + cache stats\n")
		fmt.Printf("     GET  /api/cache/stats Cache stats\n")
		fmt.Printf("     GET  /api/auth/stats  Auth + rate-limit stats\n")
		fmt.Printf("     GET  /api/auth/usage  Your usage stats\n")
		fmt.Printf("     GET  /api/auth/users  List all users (admin)\n")
		fmt.Printf("     GET  /api/health      Health check (no auth)\n\n")

		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Wait for interrupt
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\n→ Shutting down...")
	close(stopRefresh)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	}

	q.Stop()
	fmt.Println("✓ Stopped")
}

// refreshGauges periodically updates Prometheus gauges that don't have
// natural per-event hooks (queue stats, cache size, etc.).
func refreshGauges(q *queue.JobQueue, c *cache.Cache, stop <-chan struct{}) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	update := func() {
		stats := q.Stats()
		metrics.QueueDepth.Set(float64(stats.Pending + stats.Running))

		if cs := c.Stats(); cs != (cache.Stats{}) {
			metrics.CacheSize.Set(float64(cs.Size))
		}
	}

	update()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			update()
		}
	}
}