// Package services defines delivery boundaries used by student journey handlers.
package services

import (
	"context"
	"errors"
	"sync"
)

// Message contains a transactional email. Tokens in Text are confidential.
type Message struct {
	To             string
	Subject        string
	Text           string
	UnsubscribeURL string
}

// Mailer implementations must honor cancellation and return delivery failures.
// Production adapters should add List-Unsubscribe and List-Unsubscribe-Post
// headers from UnsubscribeURL when their provider supports them.
type Mailer interface {
	Send(context.Context, Message) error
}

// DryRunMailer captures messages without sending email or logging bearer tokens.
// Its zero value is ready for use and safe for concurrent handlers.
type DryRunMailer struct {
	mu       sync.Mutex
	messages []Message
}

func (m *DryRunMailer) Send(ctx context.Context, message Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if message.To == "" || message.Subject == "" || message.Text == "" {
		return errors.New("incomplete email")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, message)
	return nil
}

// Messages returns a copy so callers cannot mutate the capture buffer.
func (m *DryRunMailer) Messages() []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Message(nil), m.messages...)
}
