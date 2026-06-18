// Package ws implements a per-job WebSocket pub/sub for live scan progress.
//
// Clients connect to /api/ws/scan/:id and receive events for that specific job
// until the job reaches a terminal state (completed/failed) or the client
// disconnects. There is no broadcast to other clients — one job, one stream.
//
// The hub keeps a short replay buffer per job so clients that connect a moment
// after the scan starts still see the early progress events.
//
// Event schema (JSON):
//
//	{
//	  "type":     "progress" | "completed" | "failed",
//	  "job_id":   "abc123",
//	  "status":   "pending|running|fetching|parsing|detecting|completed|failed",
//	  "progress": 0-100,
//	  "message":  "human-readable update",
//	  "techs":    [...DetectedTech] (optional, only on detecting/completed),
//	  "error":    "..." (optional, only on failed),
//	  "ts":       "RFC3339 timestamp"
//	}
package ws

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Event is a single message sent over the WebSocket.
type Event struct {
	Type     string         `json:"type"`               // progress | completed | failed
	JobID    string         `json:"job_id"`             // job this event belongs to
	Status   string         `json:"status"`             // coarse-grained status
	Progress int            `json:"progress,omitempty"` // 0-100
	Message  string         `json:"message,omitempty"`  // human-readable
	Techs    []DetectedTech `json:"techs,omitempty"`    // partial result while detecting
	Error    string         `json:"error,omitempty"`    // present only on failed
	TS       string         `json:"ts"`                 // RFC3339
}

// DetectedTech is a slim tech notification emitted during the detecting phase.
type DetectedTech struct {
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Category   string `json:"category"`
	Confidence int    `json:"confidence"`
}

// Hub manages per-job subscribers and a small replay buffer for late joiners.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[string]map[*Subscriber]struct{}
	replay      map[string][]Event
	replaySize  int
}

// Subscriber is a single WebSocket connection listening to one job.
type Subscriber struct {
	Conn    *websocket.Conn
	JobID   string
	Send    chan Event
	closeMu sync.Mutex
	closed  bool
}

// NewHub creates an empty hub with a 32-event replay buffer per job.
func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[string]map[*Subscriber]struct{}),
		replay:      make(map[string][]Event),
		replaySize:  32,
	}
}

// Subscribe registers a new subscriber for a job. If replay is true,
// any buffered events for this job (from before the client connected) are
// queued to the subscriber's channel so it can catch up.
func (h *Hub) Subscribe(jobID string, conn *websocket.Conn, replay bool) *Subscriber {
	sub := &Subscriber{
		Conn:  conn,
		JobID: jobID,
		Send:  make(chan Event, 32),
	}
	h.mu.Lock()
	if h.subscribers[jobID] == nil {
		h.subscribers[jobID] = make(map[*Subscriber]struct{})
	}
	h.subscribers[jobID][sub] = struct{}{}
	if replay {
		for _, ev := range h.replay[jobID] {
			select {
			case sub.Send <- ev:
			default:
				break
			}
		}
	}
	h.mu.Unlock()
	return sub
}

// Unsubscribe removes the subscriber, closes its send channel, and (if no
// other subscribers remain) drops the replay buffer for the job.
func (h *Hub) Unsubscribe(sub *Subscriber) {
	h.mu.Lock()
	if subs, ok := h.subscribers[sub.JobID]; ok {
		delete(subs, sub)
		if len(subs) == 0 {
			delete(h.subscribers, sub.JobID)
			delete(h.replay, sub.JobID)
		}
	}
	h.mu.Unlock()
	sub.closeOnce()
}

// Publish sends an event to all current subscribers and records it in the
// replay buffer for late joiners. Non-blocking: drops the event if a
// subscriber's send channel is full.
func (h *Hub) Publish(jobID string, ev Event) {
	ev.JobID = jobID
	if ev.TS == "" {
		ev.TS = time.Now().UTC().Format(time.RFC3339)
	}
	h.mu.Lock()
	buf := h.replay[jobID]
	buf = append(buf, ev)
	if len(buf) > h.replaySize {
		buf = buf[len(buf)-h.replaySize:]
	}
	h.replay[jobID] = buf
	subs := h.subscribers[jobID]
	recipients := make([]*Subscriber, 0, len(subs))
	for s := range subs {
		recipients = append(recipients, s)
	}
	h.mu.Unlock()

	for _, s := range recipients {
		select {
		case s.Send <- ev:
		default:
		}
	}
}

// SubscriberCount returns the number of live subscribers for a job.
func (h *Hub) SubscriberCount(jobID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers[jobID])
}

func (s *Subscriber) closeOnce() {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if !s.closed {
		close(s.Send)
		s.closed = true
	}
}

// WriteJSON marshals and sends an event with a write deadline.
func (s *Subscriber) WriteJSON(ev Event, deadline time.Time) error {
	_ = s.Conn.SetWriteDeadline(deadline)
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return s.Conn.WriteMessage(websocket.TextMessage, data)
}