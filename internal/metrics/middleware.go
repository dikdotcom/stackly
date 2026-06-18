package metrics

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// statusRecorder wraps http.ResponseWriter to capture the status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// HTTPMiddleware records request count, duration, and in-flight gauge.
//
// `pathFn` resolves the route pattern (e.g. "/api/results/:id") for low-cardinality labels.
// If nil, the raw URL path is used (high cardinality — not recommended).
func HTTPMiddleware(pathFn func(r *http.Request) string, next http.Handler) http.Handler {
	if pathFn == nil {
		pathFn = func(r *http.Request) string { return r.URL.Path }
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// WebSocket upgrades hijack the connection. Skip metrics recording
		// so we don't try to call WriteHeader on a hijacked ResponseWriter.
		if isWebSocketUpgrade(r) {
			next.ServeHTTP(w, r)
			return
		}

		HTTPInFlight.Inc()
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		HTTPInFlight.Dec()

		path := pathFn(r)
		status := strconv.Itoa(rec.status)
		HTTPDuration.WithLabelValues(r.Method, path).Observe(time.Since(start).Seconds())
		HTTPRequests.WithLabelValues(r.Method, path, status).Inc()
	})
}

// isWebSocketUpgrade returns true if the request is a WebSocket handshake.
func isWebSocketUpgrade(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	return strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// Handler returns the Prometheus scrape handler.
func Handler() http.Handler {
	return promhttp.Handler()
}