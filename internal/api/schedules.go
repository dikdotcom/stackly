package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dikdotcom/stackly/internal/auth"
	"github.com/dikdotcom/stackly/internal/scheduler"
)

// requireSchedStore returns the scheduler store or writes 503 if missing.
// All schedule endpoints depend on it being injected at startup.
func (s *Server) requireSchedStore(w http.ResponseWriter) (*scheduler.Store, bool) {
	if s.schedStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "scheduler not initialized",
		})
		return nil, false
	}
	return s.schedStore, true
}

// scheduleOwner returns the user ID from the auth context, or "default"
// when auth is disabled (dev mode). Schedules are scoped to the owner
// so multi-tenant users only see their own.
func scheduleOwner(r *http.Request) string {
	if u := auth.UserFromContext(r); u != nil {
		return u.ID
	}
	return "default"
}

// handleSchedules handles the collection endpoint /api/schedules.
// GET → list (filtered by owner)
// POST → create
func (s *Server) handleSchedules(w http.ResponseWriter, r *http.Request) {
	store, ok := s.requireSchedStore(w)
	if !ok {
		return
	}
	owner := scheduleOwner(r)

	switch r.Method {
	case http.MethodGet:
		out := store.ListSchedulesByOwner(owner)
		// Redact webhook secret in responses.
		for _, sch := range out {
			redactSchedule(sch)
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"schedules": out,
		})

	case http.MethodPost:
		var body struct {
			URL           string `json:"url"`
			Interval      string `json:"interval"`
			WebhookURL    string `json:"webhook_url,omitempty"`
			WebhookSecret string `json:"webhook_secret,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		if body.URL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url is required"})
			return
		}

		// Validate scan target URL (blocks internal IPs unless overridden via env).
		if err := scheduler.ValidateScanURL(body.URL); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid url: " + err.Error(),
			})
			return
		}

		// Validate webhook URL if provided (always blocks internal IPs).
		if body.WebhookURL != "" {
			if err := scheduler.ValidateWebhookURL(body.WebhookURL); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": "invalid webhook_url: " + err.Error(),
				})
				return
			}
		}

		if body.Interval == "" {
			body.Interval = scheduler.IntervalDaily
		}
		// Validate interval
		switch body.Interval {
		case scheduler.IntervalHourly, scheduler.IntervalDaily, scheduler.IntervalWeekly:
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "interval must be one of: hourly, daily, weekly",
			})
			return
		}

		// Per-tier quota check.
		owner := scheduleOwner(r)
		tier := "free"
		hasAuth := false
		if u := auth.UserFromContext(r); u != nil {
			tier = u.Tier
			hasAuth = true
		}
		quota := scheduler.QuotaForOwner(tier, hasAuth)
		existing := len(store.ListSchedulesByOwner(owner))
		if existing >= quota {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"error": fmt.Sprintf("schedule quota exceeded (%d/%d for tier %q)", existing, quota, tier),
			})
			return
		}

		sch := &scheduler.Schedule{
			URL:           body.URL,
			Interval:      body.Interval,
			Owner:         owner,
			Enabled:       true,
			WebhookURL:    body.WebhookURL,
			WebhookSecret: body.WebhookSecret,
		}
		sch.HasSecret = body.WebhookSecret != ""

		id := store.AddSchedule(sch)
		sch.ID = id
		redactSchedule(sch)

		writeJSON(w, http.StatusCreated, sch)

	default:
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// handleScheduleSubpath dispatches /api/schedules/{id} and /api/schedules/{id}/run
func (s *Server) handleScheduleSubpath(w http.ResponseWriter, r *http.Request) {
	store, ok := s.requireSchedStore(w)
	if !ok {
		return
	}

	// Path: /api/schedules/{id}/run
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/schedules/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id := parts[0]

	// Ownership check
	sch := store.GetSchedule(id)
	if sch == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "schedule not found"})
		return
	}
	owner := scheduleOwner(r)
	if sch.Owner != owner {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "schedule not found"})
		return
	}

	if len(parts) == 2 && parts[1] == "run" {
		s.handleScheduleRun(w, r, sch)
		return
	}
	if len(parts) > 1 {
		http.NotFound(w, r)
		return
	}

	// /api/schedules/{id}
	switch r.Method {
	case http.MethodGet:
		redactSchedule(sch)
		writeJSON(w, http.StatusOK, sch)
	case http.MethodPatch:
		var body struct {
			Enabled       *bool   `json:"enabled,omitempty"`
			Interval      *string `json:"interval,omitempty"`
			WebhookURL    *string `json:"webhook_url,omitempty"`
			WebhookSecret *string `json:"webhook_secret,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}

		// Validate webhook URL when updating. Same rule as POST — always
		// reject internal targets.
		if body.WebhookURL != nil && *body.WebhookURL != "" {
			if err := scheduler.ValidateWebhookURL(*body.WebhookURL); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": "invalid webhook_url: " + err.Error(),
				})
				return
			}
		}

		updated := store.UpdateSchedule(id, func(sch *scheduler.Schedule) {
			if body.Enabled != nil {
				sch.Enabled = *body.Enabled
			}
			if body.Interval != nil {
				switch *body.Interval {
				case scheduler.IntervalHourly, scheduler.IntervalDaily, scheduler.IntervalWeekly:
					sch.Interval = *body.Interval
				}
			}
			if body.WebhookURL != nil {
				sch.WebhookURL = *body.WebhookURL
			}
			if body.WebhookSecret != nil {
				sch.WebhookSecret = *body.WebhookSecret
				sch.HasSecret = *body.WebhookSecret != ""
			}
		})
		if updated == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "schedule not found"})
			return
		}
		redactSchedule(updated)
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		if !store.DeleteSchedule(id) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "schedule not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// handleScheduleRun triggers a one-off execution. Used by /api/schedules/{id}/run.
// In v1 this just returns success — actual execution is handled by the scheduler
// loop on next tick. To force immediate execution, we'd need to expose the
// scheduler. For now, this endpoint exists to satisfy the API contract and
// can be wired to a direct execution path in v2.
func (s *Server) handleScheduleRun(w http.ResponseWriter, r *http.Request, sch *scheduler.Schedule) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	// Mark as due now so the next tick fires immediately (within 30s).
	store := s.schedStore
	store.UpdateSchedule(sch.ID, func(s *scheduler.Schedule) {
		s.NextRun = time.Now()
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "queued",
		"message":     "schedule will run on next scheduler tick (within 30s)",
		"schedule_id": sch.ID,
	})
}

// handleWebhookSubpath dispatches /api/webhooks/log and /api/webhooks/test
func (s *Server) handleWebhookSubpath(w http.ResponseWriter, r *http.Request) {
	store, ok := s.requireSchedStore(w)
	if !ok {
		return
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/webhooks/")

	switch trimmed {
	case "log":
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		owner := scheduleOwner(r)
		// Filter delivery log to schedules owned by this user
		all := store.RecentDeliveries(100)
		out := make([]*scheduler.WebhookDelivery, 0, len(all))
		for _, d := range all {
			sch := store.GetSchedule(d.ScheduleID)
			if sch != nil && sch.Owner == owner {
				out = append(out, d)
			}
		}
		// Cap at 50 for response size
		if len(out) > 50 {
			out = out[:50]
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"deliveries": out})

	case "test":
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			URL    string `json:"url"`
			Secret string `json:"secret,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if body.URL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url is required"})
			return
		}
		// Validate before attempting — blocks SSRF targets (internal IPs,
		// non-http schemes) at the API layer.
		if err := scheduler.ValidateWebhookURL(body.URL); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid url: " + err.Error(),
			})
			return
		}
		// Send a tiny ping payload
		payload := map[string]interface{}{
			"event":     "ping",
			"timestamp": time.Now(),
			"message":   "Stackly webhook test",
		}
		bodyBytes, _ := json.Marshal(payload)
		// Inline call to dispatcher logic — reuse HMAC sign from scheduler.
		// For brevity we re-implement the test POST here using the same
		// signing convention (sha256=<hex>) so users can validate their
		// endpoint works before configuring it on a schedule.
		code, err := testWebhookDelivery(body.URL, bodyBytes, body.Secret)
		result := map[string]interface{}{
			"http_code": code,
			"success":   err == nil && code >= 200 && code < 300,
		}
		if err != nil {
			result["error"] = err.Error()
		}
		writeJSON(w, http.StatusOK, result)

	default:
		http.NotFound(w, r)
	}
}

// redactSchedule hides the webhook secret before returning to clients.
// Sets HasSecret=true when a secret is configured but value is replaced
// with empty string. Prevents secret leakage via GET /api/schedules.
func redactSchedule(sch *scheduler.Schedule) {
	if sch.WebhookSecret != "" {
		sch.HasSecret = true
	}
	sch.WebhookSecret = ""
}

// randomID is a small helper for test/diagnostic purposes.
func randomID(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
