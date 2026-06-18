package scheduler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/dikdotcom/stackly/internal/scanner"
)

// ScannerFunc is the signature for triggering a scan. We accept an
// interface (not the concrete *scanner.Scanner) so tests can inject
// a fake without spinning up chromedp.
type ScannerFunc func(ctx context.Context, jobID, url string) *scanner.ScanResult

// Scheduler ticks every `interval` and runs due schedules. It also
// dispatches webhooks on scan completion.
type Scheduler struct {
	store   *Store
	scan    ScannerFunc
	stopCh  chan struct{}
	doneCh  chan struct{}
	tickDur time.Duration

	// inflight tracks schedules currently being processed so a slow
	// scan doesn't get double-fired on the next tick.
	inflight sync.Map // map[scheduleID]bool

	// dispatchSem caps concurrent outbound webhook deliveries. Without
	// this, a burst of due schedules could spawn 100s of parallel HTTP
	// requests, exhausting sockets and getting our IP throttled by
	// receivers. Tuned conservatively; raise via dispatchSem multiplier
	// if observed.
	dispatchSem chan struct{}
}

// MaxConcurrentDispatches caps parallel webhook deliveries. Each slot
// covers one in-flight POST + its retries (3 attempts × up to 9s backoff
// between = up to ~30s per slot). With 10 slots that's ~3 dispatches
// per second sustained outbound rate.
const MaxConcurrentDispatches = 10

// New creates a Scheduler. The ticker fires every 30 seconds by default.
// Caller must call Start to begin ticking.
func New(store *Store, scan ScannerFunc) *Scheduler {
	return &Scheduler{
		store:       store,
		scan:        scan,
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
		tickDur:     30 * time.Second,
		dispatchSem: make(chan struct{}, MaxConcurrentDispatches),
	}
}

// Start kicks off the background ticker loop. Non-blocking.
func (s *Scheduler) Start() {
	go s.run()
}

// Stop signals the ticker to exit and waits for it.
func (s *Scheduler) Stop() {
	close(s.stopCh)
	<-s.doneCh
}

func (s *Scheduler) run() {
	defer close(s.doneCh)

	// Fire one tick immediately so schedules created just before Start
	// don't wait up to tickDur for their first check.
	s.tick()

	t := time.NewTicker(s.tickDur)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			s.tick()
		case <-s.stopCh:
			return
		}
	}
}

func (s *Scheduler) tick() {
	now := time.Now()
	for _, sch := range s.store.DueSchedules(now) {
		// Skip if already running.
		if _, busy := s.inflight.Load(sch.ID); busy {
			continue
		}
		s.inflight.Store(sch.ID, true)
		go s.runOne(sch.ID)
	}
}

// runOne executes a single schedule. Errors are recorded on the schedule
// itself, not propagated.
func (s *Scheduler) runOne(id string) {
	defer s.inflight.Delete(id)

	sch := s.store.GetSchedule(id)
	if sch == nil {
		return
	}

	// Mark as running (advance NextRun) so concurrent ticks skip it.
	now := time.Now()
	nextNext := now.Add(IntervalDur(sch.Interval))
	s.store.UpdateSchedule(id, func(s *Schedule) {
		s.NextRun = nextNext
		s.RunCount++
	})

	// 30-second scan budget per scheduled run.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	jobID := fmt.Sprintf("sched-%s-%d", id, now.Unix())
	result := s.scan(ctx, jobID, sch.URL)

	// Update last-run metadata.
	status := "success"
	errStr := ""
	if result == nil || result.Error != nil {
		status = "failed"
		if result != nil && result.Error != nil {
			errStr = result.Error.Error()
		} else {
			errStr = "scan returned nil"
		}
	}
	s.store.UpdateSchedule(id, func(sch *Schedule) {
		sch.LastRun = now
		sch.LastStatus = status
		sch.LastError = errStr
	})

	// Dispatch webhook if configured. Acquire semaphore slot first so a
	// burst of due schedules doesn't spawn unbounded goroutines.
	if sch.WebhookURL != "" {
		s.dispatchSem <- struct{}{}
		go func() {
			defer func() { <-s.dispatchSem }()
			s.dispatchWebhook(sch, result, status, errStr)
		}()
	}
}

// WebhookPayload is the JSON body POSTed to the user's webhook URL.
// Versioned so consumers can pin a known schema.
//
// Note: Result.HTML is truncated to MaxPayloadHTMLBytes (4KB) to prevent
// DoS amplification — a scan against a multi-MB page would otherwise
// produce an equally large outbound POST. Consumers wanting the full
// HTML can call /api/results/:id with their own credentials.
type WebhookPayload struct {
	Version    string                `json:"version"` // "v1"
	Event      string                `json:"event"`   // "scan.completed" | "scan.failed"
	ScheduleID string                `json:"schedule_id"`
	URL        string                `json:"url"`
	Status     string                `json:"status"`
	Error      string                `json:"error,omitempty"`
	Timestamp  time.Time             `json:"timestamp"`
	Result     *scanner.ScanResult   `json:"result"`
}

// MaxPayloadHTMLBytes caps HTML included in webhook payloads. Pages
// larger than this are truncated with an ellipsis marker. Consumers
// can fetch the full result via the API.
const MaxPayloadHTMLBytes = 4096

// sanitizeForWebhook returns a shallow copy of the result with HTML
// truncated. Pointer fields other than HTML are passed through
// unmodified — they're already small.
func sanitizeForWebhook(r *scanner.ScanResult) *scanner.ScanResult {
	if r == nil {
		return nil
	}
	cp := *r
	if len(cp.HTML) > MaxPayloadHTMLBytes {
		cp.HTML = cp.HTML[:MaxPayloadHTMLBytes] + "...[truncated, fetch full result via /api/results/" + cp.URL + "]"
	}
	return &cp
}

// dispatchWebhook POSTs the payload to the configured URL with HMAC
// signature header. Retries 3x with exponential backoff. URL is
// re-validated at dispatch time to block SSRF even if a schedule was
// created before the validator existed or via a backdoor.
func (s *Scheduler) dispatchWebhook(sch *Schedule, result *scanner.ScanResult, status, errStr string) {
	event := "scan.completed"
	if status != "success" {
		event = "scan.failed"
	}
	payload := &WebhookPayload{
		Version:    "v1",
		Event:      event,
		ScheduleID: sch.ID,
		URL:        sch.URL,
		Status:     status,
		Error:      errStr,
		Timestamp:  time.Now(),
		Result:     sanitizeForWebhook(result), // truncates HTML to 4KB
	}
	body, err := json.Marshal(payload)
	if err != nil {
		s.logDelivery(sch.ID, sch.WebhookURL, 0, "failed", 0, "marshal: "+err.Error(), 0)
		return
	}

	// Re-validate at dispatch time. Defense in depth — even if the API
	// layer is bypassed somehow, the scheduler refuses to POST to an
	// internal target.
	if vErr := ValidateWebhookURL(sch.WebhookURL); vErr != nil {
		s.logDelivery(sch.ID, sch.WebhookURL, 0, "failed", 0, "validation: "+vErr.Error(), 0)
		return
	}

	maxAttempts := 3
	backoff := 1 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		t0 := time.Now()
		code, derr := s.postOnce(sch.WebhookURL, body, sch.WebhookSecret)
		dur := time.Since(t0)

		if derr == nil && code >= 200 && code < 300 {
			s.logDelivery(sch.ID, sch.WebhookURL, attempt, "success", code, "", dur.Milliseconds())
			return
		}

		errMsg := ""
		if derr != nil {
			errMsg = derr.Error()
		} else {
			errMsg = fmt.Sprintf("HTTP %d", code)
		}

		if attempt == maxAttempts {
			s.logDelivery(sch.ID, sch.WebhookURL, attempt, "failed", code, errMsg, dur.Milliseconds())
			return
		}
		time.Sleep(backoff)
		backoff *= 3 // 1s, 3s, 9s
	}
}

// postOnce performs one POST with an HMAC-signed body. 10s timeout.
func (s *Scheduler) postOnce(url string, body []byte, secret string) (int, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequest(http.MethodPost, url, readerOf(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Stackly-Webhook/1.0")
	req.Header.Set("X-Stackly-Event", "scan")

	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		req.Header.Set("X-Stackly-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) // drain so connection can be reused

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp.StatusCode, nil
	}
	return resp.StatusCode, fmt.Errorf("non-2xx response: %d", resp.StatusCode)
}

func (s *Scheduler) logDelivery(scheduleID, url string, attempt int, status string, code int, errMsg string, durMs int64) {
	s.store.LogDelivery(&WebhookDelivery{
		ScheduleID: scheduleID,
		URL:        url,
		Attempt:    attempt,
		Status:     status,
		HTTPCode:   code,
		Error:      errMsg,
		DurationMs: durMs,
	})
}

// readerOf returns an io.Reader that yields body. Used so callers can
// retry with the same payload without re-marshalling.
func readerOf(b []byte) *bytesReader { return &bytesReader{b: b} }

type bytesReader struct {
	b   []byte
	off int
}

func (r *bytesReader) Read(p []byte) (int, error) {
	if r.off >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.off:])
	r.off += n
	return n, nil
}