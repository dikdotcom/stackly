// Package metrics provides Prometheus instrumentation for Stackly.
//
// Exposed metrics:
//
//   # HTTP
//   - stackly_http_requests_total{method,path,status}
//   - stackly_http_request_duration_seconds{method,path}
//   - stackly_http_requests_in_flight
//
//   # Scanner
//   - stackly_scans_total{status,cache}
//   - stackly_scan_duration_seconds
//   - stackly_scan_detections_total{category}
//
//   # Queue + Cache
//   - stackly_queue_depth
//   - stackly_cache_size
//   - stackly_cache_hits_total
//   - stackly_cache_misses_total
//
//   # Auth
//   - stackly_auth_requests_total{tier,auth_type,result}
//   - stackly_auth_active_users
//   - stackly_auth_rate_limited_total{tier}
//
// Plus default Go runtime + process metrics via promhttp.Handler().
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTP
	HTTPRequests = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "stackly",
			Name:      "http_requests_total",
			Help:      "Total HTTP requests by method, path, and status code.",
		},
		[]string{"method", "path", "status"},
	)
	HTTPDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "stackly",
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request latency in seconds.",
			Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60},
		},
		[]string{"method", "path"},
	)
	HTTPInFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "stackly",
			Name:      "http_requests_in_flight",
			Help:      "Number of HTTP requests currently being served.",
		},
	)

	// Scanner
	ScansTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "stackly",
			Name:      "scans_total",
			Help:      "Total scans by terminal status and cache hit/miss.",
		},
		[]string{"status", "cache"},
	)
	ScanDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "stackly",
			Name:      "scan_duration_seconds",
			Help:      "End-to-end scan duration in seconds (excludes cached responses).",
			Buckets:   []float64{.25, .5, 1, 2.5, 5, 10, 20, 30, 60, 120},
		},
	)
	ScanDetections = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "stackly",
			Name:      "scan_detections_total",
			Help:      "Total technology detections by category.",
		},
		[]string{"category"},
	)
	ScanTechMatches = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "stackly",
			Name:      "scan_tech_matches_total",
			Help:      "Total individual technology matches by technology slug.",
		},
		[]string{"slug"},
	)

	// Queue + Cache
	QueueDepth = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "stackly",
			Name:      "queue_depth",
			Help:      "Number of jobs currently in the scan queue (pending + running).",
		},
	)
	QueueCompleted = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "stackly",
			Name:      "queue_completed_total",
			Help:      "Total jobs completed since process start.",
		},
	)
	QueueFailed = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "stackly",
			Name:      "queue_failed_total",
			Help:      "Total jobs that failed since process start.",
		},
	)

	CacheSize = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "stackly",
			Name:      "cache_size",
			Help:      "Number of entries currently in the scan result cache.",
		},
	)
	CacheHits = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "stackly",
			Name:      "cache_hits_total",
			Help:      "Total cache hits since process start.",
		},
	)
	CacheMisses = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "stackly",
			Name:      "cache_misses_total",
			Help:      "Total cache misses since process start.",
		},
	)
	CacheInvalidations = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "stackly",
			Name:      "cache_invalidations_total",
			Help:      "Total cache entries invalidated since process start.",
		},
	)

	// Auth
	AuthRequests = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "stackly",
			Name:      "auth_requests_total",
			Help:      "Authentication attempts by tier, scheme, and outcome.",
		},
		[]string{"tier", "auth_type", "result"},
	)
	AuthActiveUsers = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "stackly",
			Name:      "auth_active_users",
			Help:      "Number of distinct users seen in the rate-limit window.",
		},
	)
	AuthRateLimited = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "stackly",
			Name:      "auth_rate_limited_total",
			Help:      "Total requests rejected by rate limiter, by tier.",
		},
		[]string{"tier"},
	)
	AuthEnabled = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "stackly",
			Name:      "auth_enabled",
			Help:      "Whether API authentication is currently enforced (1) or disabled (0).",
		},
	)

	// Build info
	BuildInfo = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "stackly",
			Name:      "build_info",
			Help:      "Build metadata (always 1; labels carry the values).",
		},
		[]string{"version", "go_version"},
	)
)