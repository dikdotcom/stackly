package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dikdotcom/stackly/internal/report"
	"github.com/dikdotcom/stackly/internal/scanner"
)

// handleReports dispatches /api/report/{id} and /api/report/{id}.pdf
//
// Auth follows the standard /api/* middleware. Ownership is not enforced
// here — anyone with a job ID can fetch its report. The queue is global
// (no per-user partitioning yet); if you need it, store owner_id on Job
// and check it here.
func (s *Server) handleReports(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/report/")
	if trimmed == "" || strings.Contains(trimmed, "/") {
		http.NotFound(w, r)
		return
	}

	// Detect .pdf suffix before stripping it from the ID.
	id := trimmed
	isPDF := false
	if strings.HasSuffix(id, ".pdf") {
		isPDF = true
		id = strings.TrimSuffix(id, ".pdf")
	}

	job := s.queue.GetJob(id)
	if job == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}
	if job.Status != "completed" {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error":  "scan not complete",
			"status": string(job.Status),
		})
		return
	}
	if job.Result == nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error": "scan completed but no result captured",
		})
		return
	}

	data := report.Data{
		Result:    job.Result,
		JobID:     id,
		Generated: time.Now(),
		ServerURL: serverBaseURL(r),
	}

	if isPDF {
		s.serveReportPDF(w, r, data)
		return
	}
	s.serveReportHTML(w, r, data)
}

func (s *Server) serveReportHTML(w http.ResponseWriter, r *http.Request, data report.Data) {
	html := report.Render(data)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`inline; filename="stackly-%s.html"`, data.JobID))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}

func (s *Server) serveReportPDF(w http.ResponseWriter, r *http.Request, data report.Data) {
	html := report.Render(data)
	pdfBytes, err := report.RenderPDF(html)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "pdf render failed: " + err.Error(),
		})
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="stackly-%s.pdf"`, data.JobID))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(pdfBytes)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdfBytes)
}

// serverBaseURL returns the public-facing base URL for footer links.
// We rebuild from the request so this works behind reverse proxies and
// on any port. Falls back to a placeholder if Host header is missing.
func serverBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	host := r.Host
	if host == "" {
		host = "stackly.local"
	}
	return scheme + "://" + host + "/"
}

// Compile-time guard that we use scanner types indirectly.
var _ = (*scanner.ScanResult)(nil)