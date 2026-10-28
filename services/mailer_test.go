package services

import (
	"context"
	"sync"
	"testing"
)

func TestMailerCapture(t *testing.T) {
	var m DryRunMailer
	message := Message{To: "learner@example.com", Subject: "Verify", Text: "secret", UnsubscribeURL: "https://example.com/unsubscribe"}
	if err := m.Send(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	got := m.Messages()
	if len(got) != 1 || got[0] != message {
		t.Fatalf("messages: %v", got)
	}
	got[0].Text = "changed"
	if m.Messages()[0].Text != "secret" {
		t.Fatal("capture buffer exposed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if m.Send(ctx, message) == nil {
		t.Fatal("cancellation ignored")
	}
	if m.Send(context.Background(), Message{}) == nil {
		t.Fatal("invalid message accepted")
	}
	if len(m.Messages()) != 1 {
		t.Fatal("failed sends captured")
	}
}
func TestMailerConcurrent(t *testing.T) {
	var m DryRunMailer
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := m.Send(context.Background(), Message{To: "a@b.co", Subject: "s", Text: "t"}); err != nil {
				t.Error(err)
			}
			_ = m.Messages()
		}()
	}
	wg.Wait()
	if len(m.Messages()) != 40 {
		t.Fatal("messages lost")
	}
}
