package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/dikdotcom/stackly/internal/cache"
	"github.com/dikdotcom/stackly/internal/metrics"
	"github.com/dikdotcom/stackly/internal/scanner"
	"github.com/dikdotcom/stackly/internal/ws"
)

// JobStatus represents the state of a scan job
type JobStatus string

const (
	StatusPending   JobStatus = "pending"
	StatusRunning   JobStatus = "running"
	StatusCompleted JobStatus = "completed"
	StatusFailed    JobStatus = "failed"
)

// Job represents a scan job
type Job struct {
	ID         string                  `json:"id"`
	URL        string                  `json:"url"`
	Status     JobStatus               `json:"status"`
	CreatedAt  time.Time               `json:"created_at"`
	StartedAt  *time.Time              `json:"started_at,omitempty"`
	FinishedAt *time.Time              `json:"finished_at,omitempty"`
	Duration   string                  `json:"duration,omitempty"`
	Error      string                  `json:"error,omitempty"`
	Result     *scanner.ScanResult     `json:"result,omitempty"`
	FromCache  bool                    `json:"from_cache,omitempty"`
	UA         string                  `json:"user_agent,omitempty"`
}

// JobQueue is an in-memory job queue with worker pool
type JobQueue struct {
	mu        sync.RWMutex
	jobs      map[string]*Job
	pending   chan *Job
	scanner   *scanner.Scanner
	cache     *cache.Cache
	hub       *ws.Hub
	workers   int
	maxJobs   int
	resultTTL time.Duration
	cacheTTL  time.Duration
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

// NewJobQueue creates a new job queue
func NewJobQueue(s *scanner.Scanner, c *cache.Cache, hub *ws.Hub, workers int) *JobQueue {
	if workers <= 0 {
		workers = 2
	}
	return &JobQueue{
		jobs:      make(map[string]*Job),
		pending:   make(chan *Job, 100),
		scanner:   s,
		cache:     c,
		hub:       hub,
		workers:   workers,
		maxJobs:   1000,
		resultTTL: 24 * time.Hour,
		cacheTTL:  24 * time.Hour,
		stopCh:    make(chan struct{}),
	}
}

// Start starts the worker pool
func (q *JobQueue) Start() {
	for i := 0; i < q.workers; i++ {
		q.wg.Add(1)
		go q.worker()
	}

	// Cleanup goroutine
	q.wg.Add(1)
	go q.cleanup()
}

// Stop stops all workers
func (q *JobQueue) Stop() {
	close(q.stopCh)
	close(q.pending)
	q.wg.Wait()
}

// Enqueue adds a new scan job
func (q *JobQueue) Enqueue(url string) (*Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.jobs) >= q.maxJobs {
		// Cleanup old jobs first
		q.cleanupLocked()
		if len(q.jobs) >= q.maxJobs {
			return nil, ErrQueueFull
		}
	}

	id := generateID()
	job := &Job{
		ID:        id,
		URL:       url,
		Status:    StatusPending,
		CreatedAt: time.Now(),
	}
	q.jobs[id] = job

	// Non-blocking send
	select {
	case q.pending <- job:
	default:
		// Queue full, leave as pending
	}

	metrics.QueueDepth.Set(float64(len(q.pending)))

	q.hub.Publish(id, ws.Event{
		Type:    "progress",
		Status:  string(StatusPending),
		Progress: 0,
		Message: "Queued for scan",
	})

	return job, nil
}

// GetJob returns a job by ID
func (q *JobQueue) GetJob(id string) *Job {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.jobs[id]
}

// ListJobs returns recent jobs (last N)
func (q *JobQueue) ListJobs(limit int) []*Job {
	q.mu.RLock()
	defer q.mu.RUnlock()

	jobs := make([]*Job, 0, len(q.jobs))
	for _, j := range q.jobs {
		jobs = append(jobs, j)
	}

	// Sort by created_at desc
	for i := 0; i < len(jobs); i++ {
		for j := i + 1; j < len(jobs); j++ {
			if jobs[i].CreatedAt.Before(jobs[j].CreatedAt) {
				jobs[i], jobs[j] = jobs[j], jobs[i]
			}
		}
	}

	if limit > 0 && limit < len(jobs) {
		jobs = jobs[:limit]
	}

	return jobs
}

// Stats returns queue statistics
func (q *JobQueue) Stats() Stats {
	q.mu.RLock()
	defer q.mu.RUnlock()

	stats := Stats{
		Total: len(q.jobs),
	}

	for _, j := range q.jobs {
		switch j.Status {
		case StatusPending:
			stats.Pending++
		case StatusRunning:
			stats.Running++
		case StatusCompleted:
			stats.Completed++
		case StatusFailed:
			stats.Failed++
		}
	}

	return stats
}

// Stats contains queue statistics
type Stats struct {
	Total     int `json:"total"`
	Pending   int `json:"pending"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

// worker processes jobs from the queue
func (q *JobQueue) worker() {
	defer q.wg.Done()

	for {
		select {
		case <-q.stopCh:
			return
		case job, ok := <-q.pending:
			if !ok {
				return
			}
			q.processJob(job)
		}
	}
}

// processJob runs the actual scan
func (q *JobQueue) processJob(job *Job) {
	q.mu.Lock()
	now := time.Now()
	job.Status = StatusRunning
	job.StartedAt = &now
	q.mu.Unlock()

	q.hub.Publish(job.ID, ws.Event{
		Type:    "progress",
		Status:  string(StatusRunning),
		Progress: 10,
		Message: "Worker picked up job",
	})

	// Check cache first
	if q.cache != nil {
		if cached := q.cache.Get(job.URL); cached != nil {
			metrics.CacheHits.Inc()
			q.mu.Lock()
			finished := time.Now()
			job.FinishedAt = &finished
			job.Duration = "0s (cached)"
			job.Status = StatusCompleted
			job.Result = cached.Result
			job.FromCache = true
			job.UA = cached.UserAgent
			q.mu.Unlock()
			metrics.ScansTotal.WithLabelValues("completed", "hit").Inc()
			metrics.QueueCompleted.Inc()
			recordScanDetections(job.Result)
			return
		}
		metrics.CacheMisses.Inc()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result := q.scanner.ScanURL(ctx, job.ID, job.URL)

	// Tell subscribers the scan finished (success or fail). Hub.Publish is no-op
	// if nobody is listening, so this is safe even without subscribers.
	if result.Error != nil {
		q.hub.Publish(job.ID, ws.Event{
			Type:   "failed",
			Status: string(StatusFailed),
			Error:  result.Error.Error(),
		})
	} else {
		q.hub.Publish(job.ID, ws.Event{
			Type:     "completed",
			Status:   string(StatusCompleted),
			Progress: 100,
			Message:  fmt.Sprintf("Detected %d technologies", len(result.Results)),
			Techs:    toDetectedTechs(result.Results),
		})
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	finished := time.Now()
	job.FinishedAt = &finished
	job.Duration = finished.Sub(*job.StartedAt).Round(time.Millisecond).String()
	job.UA = q.scanner.GetLastUserAgent()

	if result.Error != nil {
		job.Status = StatusFailed
		job.Error = result.Error.Error()
		metrics.ScansTotal.WithLabelValues("failed", "miss").Inc()
		metrics.QueueFailed.Inc()
		metrics.ScanDuration.Observe(finished.Sub(*job.StartedAt).Seconds())
		return
	}

	job.Status = StatusCompleted
	job.Result = result

	metrics.ScansTotal.WithLabelValues("completed", "miss").Inc()
	metrics.QueueCompleted.Inc()
	metrics.ScanDuration.Observe(finished.Sub(*job.StartedAt).Seconds())
	recordScanDetections(result)

	// Store in cache for future scans
	if q.cache != nil {
		q.cache.Set(job.URL, result, job.UA)
	}
}

// toDetectedTechs converts scanner.Result into the slim ws.DetectedTech shape.
func toDetectedTechs(results []scanner.Result) []ws.DetectedTech {
	out := make([]ws.DetectedTech, 0, len(results))
	for _, r := range results {
		out = append(out, ws.DetectedTech{
			Slug:       r.Technology.Slug,
			Name:       r.Technology.Name,
			Category:   r.Technology.Category,
			Confidence: r.Confidence,
		})
	}
	return out
}

// recordScanDetections increments per-category and per-tech counters.
func recordScanDetections(result *scanner.ScanResult) {
	if result == nil {
		return
	}
	for _, r := range result.Results {
		if r.Technology.Category != "" {
			metrics.ScanDetections.WithLabelValues(r.Technology.Category).Inc()
		}
		if r.Technology.Slug != "" {
			metrics.ScanTechMatches.WithLabelValues(r.Technology.Slug).Inc()
		}
	}
}

// cleanup removes old completed jobs
func (q *JobQueue) cleanup() {
	defer q.wg.Done()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-q.stopCh:
			return
		case <-ticker.C:
			q.mu.Lock()
			q.cleanupLocked()
			q.mu.Unlock()
		}
	}
}

// cleanupLocked removes expired jobs (must be called with lock held)
func (q *JobQueue) cleanupLocked() {
	cutoff := time.Now().Add(-q.resultTTL)
	for id, job := range q.jobs {
		if job.Status == StatusCompleted || job.Status == StatusFailed {
			if job.CreatedAt.Before(cutoff) {
				delete(q.jobs, id)
			}
		}
	}

	// If still over max, remove oldest completed
	if len(q.jobs) >= q.maxJobs {
		var oldest *Job
		var oldestID string
		for id, job := range q.jobs {
			if (job.Status == StatusCompleted || job.Status == StatusFailed) {
				if oldest == nil || job.CreatedAt.Before(oldest.CreatedAt) {
					oldest = job
					oldestID = id
				}
			}
		}
		if oldestID != "" {
			delete(q.jobs, oldestID)
		}
	}
}

// ErrQueueFull is returned when queue is at capacity
var ErrQueueFull = &QueueError{Message: "queue is full"}

type QueueError struct {
	Message string
}

func (e *QueueError) Error() string {
	return e.Message
}

// generateID creates a unique job ID
func generateID() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}