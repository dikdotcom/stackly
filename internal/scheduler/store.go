package scheduler

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/dikdotcom/stackly/internal/fsutil"
)

// Interval presets supported by the scheduler. Custom cron expressions
// can be added later; for v1 these three cover ~95% of use cases.
const (
	IntervalHourly = "hourly"
	IntervalDaily  = "daily"
	IntervalWeekly = "weekly"
)

// IntervalDur returns the duration between runs for a preset string.
// Unknown strings default to daily.
func IntervalDur(s string) time.Duration {
	switch s {
	case IntervalHourly:
		return 1 * time.Hour
	case IntervalWeekly:
		return 7 * 24 * time.Hour
	case IntervalDaily:
		fallthrough
	default:
		return 24 * time.Hour
	}
}

// Schedule is a single re-scan configuration.
type Schedule struct {
	ID            string    `json:"id"`
	URL           string    `json:"url"`
	Interval      string    `json:"interval"` // hourly | daily | weekly
	Owner         string    `json:"owner"`    // user ID for multi-tenant scoping
	Enabled       bool      `json:"enabled"`
	WebhookURL    string    `json:"webhook_url,omitempty"`
	WebhookSecret string    `json:"webhook_secret,omitempty"` // HMAC secret (kept on server, never returned in GET)
	HasSecret     bool      `json:"has_secret"`               // true when WebhookSecret is set (exposed via API; value redacted)
	LastRun       time.Time `json:"last_run,omitempty"`
	LastStatus    string    `json:"last_status,omitempty"` // success | failed | ""
	LastError     string    `json:"last_error,omitempty"`
	NextRun       time.Time `json:"next_run"`
	RunCount      int       `json:"run_count"`
	CreatedAt     time.Time `json:"created_at"`
}

// WebhookDelivery records one delivery attempt. We persist only the last
// N (default 100) so the log file stays small.
type WebhookDelivery struct {
	ID         string    `json:"id"`
	ScheduleID string    `json:"schedule_id"`
	URL        string    `json:"url"` // webhook target
	Attempt    int       `json:"attempt"`
	Status     string    `json:"status"` // success | failed
	HTTPCode   int       `json:"http_code,omitempty"`
	Error      string    `json:"error,omitempty"`
	DurationMs int64     `json:"duration_ms"`
	Timestamp  time.Time `json:"timestamp"`
}

// store is the JSON-backed persistence for schedules and delivery log.
// All access goes through the mutex; reads are RWMutex-friendly.
type store struct {
	path string

	mu        sync.RWMutex
	schedules map[string]*Schedule
	log       []*WebhookDelivery
}

const maxDeliveryLog = 100

func newStore(path string) *store {
	return &store{
		path:      path,
		schedules: make(map[string]*Schedule),
	}
}

// load reads the JSON file from disk. Missing file is OK (returns empty).
// Corrupt file is logged via the error return — caller decides whether to
// rename and start fresh or surface the failure.
func (s *store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := fsutil.ReadFileBytes(s.path)
	if err != nil {
		if fsutil.IsNotExist(err) {
			return nil
		}
		return err
	}

	var file struct {
		Schedules []*Schedule        `json:"schedules"`
		Log       []*WebhookDelivery `json:"log"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}

	s.schedules = make(map[string]*Schedule, len(file.Schedules))
	for _, sch := range file.Schedules {
		s.schedules[sch.ID] = sch
	}
	s.log = file.Log
	return nil
}

// save writes the current state to disk atomically (tmp + rename).
func (s *store) save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	file := struct {
		Schedules []*Schedule        `json:"schedules"`
		Log       []*WebhookDelivery `json:"log"`
	}{
		Schedules: make([]*Schedule, 0, len(s.schedules)),
		Log:       s.log,
	}
	for _, sch := range s.schedules {
		file.Schedules = append(file.Schedules, sch)
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(s.path, data)
}
