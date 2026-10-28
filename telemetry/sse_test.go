package telemetry

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSSEAudienceAndSlowConsumer(t *testing.T) {
	h := NewHub()
	h.buffer = 1
	defer h.Close()
	a, _ := h.subscribe("a")
	b, _ := h.subscribe("b")
	if err := h.PublishStudent("a", Event{Type: "activity", Data: map[string]string{"title": "Private update\nevent: forged"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-b.messages:
		t.Fatal("other student received private event")
	default:
	}
	f := <-a.messages
	if strings.Contains(string(f.data), "\nevent:") {
		t.Fatal("SSE newline injection")
	}
	var data map[string]string
	if json.Unmarshal(f.data, &data) != nil {
		t.Fatal("invalid JSON frame")
	}
	if err := h.PublishCohort(Event{Type: "activity", Data: "private"}); err == nil {
		t.Fatal("unsafe cohort type accepted")
	}
	if err := h.PublishStudent("", Event{Type: "activity", Data: "private"}); err == nil {
		t.Fatal("empty audience accepted")
	}
	if err := h.PublishStudent("a", Event{Type: "bad\nevent", Data: "x"}); err == nil {
		t.Fatal("invalid event accepted")
	}
	if err := h.PublishStudent("a", Event{Type: "activity", Data: strings.Repeat("x", 9000)}); err == nil {
		t.Fatal("oversized event accepted")
	}
	if err := h.PublishCohort(Event{Type: "rank", Data: map[string]bool{"refresh": true}}); err != nil {
		t.Fatal(err)
	}
	<-a.messages // leave b full; next publish evicts b without blocking a
	if err := h.PublishCohort(Event{Type: "rank", Data: nil}); err != nil {
		t.Fatal(err)
	}
	<-b.messages
	if _, ok := <-b.messages; ok {
		t.Fatal("slow subscriber not disconnected")
	}
}

type ssePipeWriter struct {
	header http.Header
	writer *io.PipeWriter
}

func (w *ssePipeWriter) Header() http.Header         { return w.header }
func (w *ssePipeWriter) WriteHeader(int)             {}
func (w *ssePipeWriter) Write(p []byte) (int, error) { return w.writer.Write(p) }
func (w *ssePipeWriter) Flush()                      {}
func TestSSEStreamAndCancellation(t *testing.T) {
	h := NewHub()
	h.heartbeat = 10 * time.Millisecond
	defer h.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	req := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	response := &ssePipeWriter{header: make(http.Header), writer: writer}
	done := make(chan struct{})
	go func() { defer close(done); defer writer.Close(); h.Serve(response, req, "student") }()
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		if scanner.Text() == ": heartbeat" {
			break
		}
	}
	if err := h.PublishStudent("student", Event{Type: "progress", Data: map[string]int{"xp": 250}}); err != nil {
		t.Fatal(err)
	}
	found := false
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data: ") {
			found = strings.Contains(scanner.Text(), `"xp":250`)
			break
		}
	}
	if !found {
		t.Fatalf("event not received: %v", scanner.Err())
	}
	cancel()
	reader.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not stop stream")
	}
	if response.Header().Get("Content-Type") != "text/event-stream" || response.Header().Get("X-Accel-Buffering") != "no" {
		t.Fatal("stream headers missing")
	}
	h.Close()
	if h.PublishCohort(Event{Type: "rank"}) == nil {
		t.Fatal("closed hub accepted event")
	}
	if _, err := h.subscribe("student"); err == nil {
		t.Fatal("closed hub accepted client")
	}
}
func TestSSELimitsAndConcurrentLifecycle(t *testing.T) {
	h := NewHub()
	defer h.Close()
	h.maxClients = 2
	a, _ := h.subscribe("a")
	_, _ = h.subscribe("b")
	if _, err := h.subscribe("c"); err == nil {
		t.Fatal("global stream limit ignored")
	}
	h.unsubscribe(a)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				c, err := h.subscribe("a")
				if err == nil {
					_ = h.PublishStudent("a", Event{Type: "progress", Data: j})
					h.unsubscribe(c)
				}
			}
		}()
	}
	wg.Wait()
	h.Close()
	h.Close()
	w := httptest.NewRecorder()
	h.Serve(w, httptest.NewRequest("GET", "/", nil), "")
	if w.Code != 401 {
		t.Fatal("anonymous stream allowed")
	}
}
