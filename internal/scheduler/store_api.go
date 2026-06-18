package scheduler

import (
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

// Store wraps the persistence layer with public accessors. It also keeps
// a debounced async save loop so callers don't pay for JSON marshalling
// on every mutation.
type Store struct {
	store     *store
	dirtyCh   chan struct{}
	stopCh    chan struct{}
	doneCh    chan struct{}
	closeOnce sync.Once
}

// NewStore returns a Store backed by the given JSON file. The file is
// loaded eagerly; missing file is fine (empty store).
func NewStore(path string) (*Store, error) {
	st := newStore(path)
	if err := st.load(); err != nil {
		return nil, err
	}
	s := &Store{
		store:   st,
		dirtyCh: make(chan struct{}, 1),
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
	go s.saveLoop()
	return s, nil
}

// Close stops the background save loop and flushes pending writes.
func (s *Store) Close() error {
	s.closeOnce.Do(func() {
		close(s.stopCh)
	})
	<-s.doneCh
	return s.store.save()
}

// saveLoop coalesces burst writes: multiple mark-dirty calls within the
// same tick result in one disk flush.
func (s *Store) saveLoop() {
	defer close(s.doneCh)
	for {
		select {
		case <-s.dirtyCh:
			// Drain any pending additional marks, then save once.
			for {
				select {
				case <-s.dirtyCh:
				default:
					goto save
				}
			}
		save:
			if err := s.store.save(); err != nil {
				// Persist errors are not fatal — log on caller side.
				// We don't have a logger here; just retry on next tick.
				_ = err
			}
		case <-s.stopCh:
			return
		}
	}
}

// markDirty signals the save loop. Non-blocking.
func (s *Store) markDirty() {
	select {
	case s.dirtyCh <- struct{}{}:
	default:
	}
}

// ─── Schedule CRUD ─────────────────────────────────────────────

// AddSchedule inserts a new schedule. Returns the assigned ID.
func (s *Store) AddSchedule(sch *Schedule) string {
	if sch.ID == "" {
		sch.ID = genID()
	}
	if sch.CreatedAt.IsZero() {
		sch.CreatedAt = time.Now()
	}
	if sch.NextRun.IsZero() {
		sch.NextRun = time.Now().Add(IntervalDur(sch.Interval))
	}
	s.store.mu.Lock()
	s.store.schedules[sch.ID] = sch
	s.store.mu.Unlock()
	s.markDirty()
	return sch.ID
}

// GetSchedule returns a schedule by ID, or nil if missing.
func (s *Store) GetSchedule(id string) *Schedule {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	return s.store.schedules[id]
}

// UpdateSchedule applies a mutator function to a schedule under write
// lock. Returns the updated schedule, or nil if missing.
func (s *Store) UpdateSchedule(id string, mut func(*Schedule)) *Schedule {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	sch, ok := s.store.schedules[id]
	if !ok {
		return nil
	}
	mut(sch)
	s.store.schedules[id] = sch
	return sch
}

// DeleteSchedule removes a schedule. Returns true if it existed.
func (s *Store) DeleteSchedule(id string) bool {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	_, ok := s.store.schedules[id]
	if ok {
		delete(s.store.schedules, id)
	}
	return ok
}

// ListSchedulesByOwner returns schedules filtered by owner, sorted by
// NextRun ascending (soonest first).
func (s *Store) ListSchedulesByOwner(owner string) []*Schedule {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	out := make([]*Schedule, 0)
	for _, sch := range s.store.schedules {
		if sch.Owner == owner {
			out = append(out, sch)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].NextRun.Before(out[j].NextRun)
	})
	return out
}

// ListAllSchedules returns every schedule regardless of owner. Used by
// the scheduler loop. Sorted by NextRun ascending.
func (s *Store) ListAllSchedules() []*Schedule {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	out := make([]*Schedule, 0, len(s.store.schedules))
	for _, sch := range s.store.schedules {
		out = append(out, sch)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].NextRun.Before(out[j].NextRun)
	})
	return out
}

// DueSchedules returns schedules whose NextRun is at or before `now`.
func (s *Store) DueSchedules(now time.Time) []*Schedule {
	all := s.ListAllSchedules()
	due := make([]*Schedule, 0)
	for _, sch := range all {
		if sch.Enabled && !sch.NextRun.After(now) {
			due = append(due, sch)
		}
	}
	return due
}

// ─── Delivery log ─────────────────────────────────────────────

// LogDelivery appends a delivery record and trims the log to maxDeliveryLog.
func (s *Store) LogDelivery(d *WebhookDelivery) {
	if d.ID == "" {
		d.ID = genID()
	}
	if d.Timestamp.IsZero() {
		d.Timestamp = time.Now()
	}
	s.store.mu.Lock()
	s.store.log = append(s.store.log, d)
	if len(s.store.log) > maxDeliveryLog {
		s.store.log = s.store.log[len(s.store.log)-maxDeliveryLog:]
	}
	s.store.mu.Unlock()
	s.markDirty()
}

// RecentDeliveries returns the last N delivery records (newest first).
func (s *Store) RecentDeliveries(n int) []*WebhookDelivery {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	if n <= 0 || n > len(s.store.log) {
		n = len(s.store.log)
	}
	out := make([]*WebhookDelivery, 0, n)
	for i := len(s.store.log) - 1; i >= 0 && len(out) < n; i-- {
		out = append(out, s.store.log[i])
	}
	return out
}

// genID returns a 16-hex-char unique ID. Sufficient collision resistance
// for the in-process scope.
func genID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}