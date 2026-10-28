// Package telemetry provides bounded, student-isolated server-sent events.
package telemetry

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Event contains safe client-facing data, never private orchestrator logs/tokens.
type Event struct {
	Type string
	Data any
}
type frame struct {
	id   uint64
	kind string
	data []byte
}
type subscriber struct {
	student  string
	messages chan frame
}
type Hub struct {
	mu                 sync.Mutex
	clients            map[*subscriber]struct{}
	sequence           uint64
	closed             bool
	buffer, maxClients int
	heartbeat          time.Duration
}

func NewHub() *Hub {
	return &Hub{clients: make(map[*subscriber]struct{}), buffer: 32, maxClients: 1000, heartbeat: 20 * time.Second}
}

func (h *Hub) PublishStudent(student string, event Event) error {
	if student == "" {
		return errors.New("student required")
	}
	return h.publish(student, event)
}

// Cohort events only announce rank invalidation or a public announcement. Student
// updates must use PublishStudent to avoid cross-student activity disclosure.
func (h *Hub) PublishCohort(event Event) error {
	if event.Type != "rank" && event.Type != "announcement" {
		return errors.New("invalid cohort event")
	}
	return h.publish("", event)
}
func (h *Hub) publish(student string, event Event) error {
	switch event.Type {
	case "activity", "progress", "rank", "announcement":
	default:
		return errors.New("invalid event type")
	}
	raw, err := json.Marshal(event.Data)
	if err != nil || len(raw) > 8192 {
		return errors.New("invalid event data")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return errors.New("hub closed")
	}
	h.sequence++
	f := frame{h.sequence, event.Type, raw}
	for c := range h.clients {
		if student != "" && student != c.student {
			continue
		}
		select {
		case c.messages <- f:
		default:
			delete(h.clients, c)
			close(c.messages)
		}
	}
	return nil
}
func (h *Hub) subscribe(student string) (*subscriber, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || len(h.clients) >= h.maxClients {
		return nil, errors.New("stream unavailable")
	}
	count := 0
	for c := range h.clients {
		if c.student == student {
			count++
		}
	}
	if count >= 8 {
		return nil, errors.New("too many student streams")
	}
	c := &subscriber{student: student, messages: make(chan frame, h.buffer)}
	h.clients[c] = struct{}{}
	return c, nil
}
func (h *Hub) unsubscribe(c *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.messages)
	}
}
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for c := range h.clients {
		delete(h.clients, c)
		close(c.messages)
	}
}

// Serve must receive a student ID resolved by trusted session middleware.
// Streams are invalidation notifications, not a durable event archive: clients
// fetch a fresh REST snapshot when connecting/reconnecting (including event gaps).
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, student string) {
	if student == "" {
		http.Error(w, "authentication required", 401)
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "streaming unavailable", 500)
		return
	}
	c, err := h.subscribe(student)
	if err != nil {
		http.Error(w, "stream unavailable", 503)
		return
	}
	defer h.unsubscribe(c)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	controller := http.NewResponseController(w)
	write := func(text string) error {
		_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err := fmt.Fprint(w, text); err != nil {
			return err
		}
		return controller.Flush()
	}
	defer controller.SetWriteDeadline(time.Time{})
	if write("retry: 3000\n: connected\n\n") != nil {
		return
	}
	ticker := time.NewTicker(h.heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case f, ok := <-c.messages:
			if !ok {
				return
			}
			if write(fmt.Sprintf("id: %d\nevent: %s\ndata: %s\n\n", f.id, f.kind, f.data)) != nil {
				return
			}
		case <-ticker.C:
			if write(": heartbeat\n\n") != nil {
				return
			}
		}
	}
}
