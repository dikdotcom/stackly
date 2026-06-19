package api

import (
	"net/http"

	"github.com/dikdotcom/stackly/internal/auth"
)

// handleQuota serves GET /api/auth/quota. Returns the calling user's
// current monthly quota usage.
//
// 404 when auth is disabled (no user context to look up).
// 503 when the quota subsystem is unavailable.
func (s *Server) handleQuota(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if s.auth == nil || !s.auth.Enabled() {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "quota tracking requires auth to be enabled",
		})
		return
	}
	u := auth.UserFromContext(r)
	if u == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "no authenticated user",
		})
		return
	}
	info := s.userStore.Quota(u.Key)
	if info == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	writeJSON(w, http.StatusOK, info)
}
