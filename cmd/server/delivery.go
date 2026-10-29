package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/eeyung1/meuseAI/db"
	"github.com/eeyung1/meuseAI/services"
)

type deliveryKey struct{}

// ResendMailer is an optional production adapter; tests can inject Mailer instead.
type ResendMailer struct {
	key, from string
	client    *http.Client
}

func NewResendMailer(key, from string, client *http.Client) (*ResendMailer, error) {
	if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\r\n") {
		return nil, errors.New("Resend key required")
	}
	if _, err := mail.ParseAddress(from); err != nil || strings.ContainsAny(from, "\r\n") {
		return nil, errors.New("valid sender required")
	}
	c := http.Client{Timeout: 10 * time.Second}
	if client != nil {
		c = *client
		if c.Timeout <= 0 || c.Timeout > 10*time.Second {
			c.Timeout = 10 * time.Second
		}
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &ResendMailer{key, from, &c}, nil
}
func (m *ResendMailer) Send(ctx context.Context, message services.Message) error {
	headers := map[string]string{}
	if message.UnsubscribeURL != "" {
		headers["List-Unsubscribe"] = "<" + message.UnsubscribeURL + ">"
		headers["List-Unsubscribe-Post"] = "List-Unsubscribe=One-Click"
	}
	body, err := json.Marshal(map[string]any{"from": m.from, "to": []string{message.To}, "subject": message.Subject, "text": message.Text, "headers": headers})
	if err != nil {
		return errors.New("email encoding failed")
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.resend.com/emails", bytes.NewReader(body))
	if err != nil {
		return errors.New("email unavailable")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.key)
	if key, ok := ctx.Value(deliveryKey{}).(string); ok {
		req.Header.Set("Idempotency-Key", key)
	}
	response, err := m.client.Do(req)
	if err != nil {
		return errors.New("email provider unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("email provider rejected message")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	var result struct {
		ID string `json:"id"`
	}
	if err != nil || json.Unmarshal(raw, &result) != nil || result.ID == "" {
		return errors.New("invalid email provider response")
	}
	return nil
}

// BrevoMailer delivers transactional messages through Brevo (formerly Sendinblue) v3 API.
type BrevoMailer struct {
	key, fromName, fromEmail string
	client                   *http.Client
}

func NewBrevoMailer(key, from string, client *http.Client) (*BrevoMailer, error) {
	if strings.TrimSpace(key) == "" || strings.ContainsAny(key, "\r\n") {
		return nil, errors.New("Brevo key required")
	}
	addr, err := mail.ParseAddress(from)
	if err != nil || strings.ContainsAny(from, "\r\n") {
		return nil, errors.New("valid sender required")
	}
	fromName := addr.Name
	if fromName == "" {
		fromName = "meuseAI"
	}
	c := http.Client{Timeout: 10 * time.Second}
	if client != nil {
		c = *client
		if c.Timeout <= 0 || c.Timeout > 10*time.Second {
			c.Timeout = 10 * time.Second
		}
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &BrevoMailer{
		key:       strings.TrimSpace(key),
		fromName:  fromName,
		fromEmail: addr.Address,
		client:    &c,
	}, nil
}

func (m *BrevoMailer) Send(ctx context.Context, message services.Message) error {
	headers := map[string]string{}
	if message.UnsubscribeURL != "" {
		headers["List-Unsubscribe"] = "<" + message.UnsubscribeURL + ">"
		headers["List-Unsubscribe-Post"] = "List-Unsubscribe=One-Click"
	}
	payload := map[string]any{
		"sender": map[string]string{
			"name":  m.fromName,
			"email": m.fromEmail,
		},
		"to": []map[string]string{
			{"email": message.To},
		},
		"subject":     message.Subject,
		"textContent": message.Text,
	}
	if len(headers) > 0 {
		payload["headers"] = headers
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return errors.New("email encoding failed")
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.brevo.com/v3/smtp/email", bytes.NewReader(body))
	if err != nil {
		return errors.New("email unavailable")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("api-key", m.key)

	response, err := m.client.Do(req)
	if err != nil {
		return errors.New("email provider unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("email provider rejected message")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	var result struct {
		MessageID string `json:"messageId"`
	}
	if err != nil || json.Unmarshal(raw, &result) != nil || result.MessageID == "" {
		return errors.New("invalid email provider response")
	}
	return nil
}

// ProvisionOnce delivers configured Tier 1 access for pending durable events.
// Completion is recorded only after delivery; Resend receives a stable idempotency
// key so crash/retry does not resend within the provider's idempotency window.
func (a *App) ProvisionOnce(ctx context.Context) error {
	sourceID := "json_extract(done.details, '$.source_event_id')"
	if a.Database.Dialect == db.Postgres {
		sourceID = "CAST(done.details->>'source_event_id' AS BIGINT)"
	}
	query := "SELECT requested.id,requested.student_id FROM activity_logs requested WHERE requested.event_type='lovable_provisioning_requested' AND requested.student_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM activity_logs done WHERE done.event_type='lovable_provisioning_delivered' AND done.student_id=requested.student_id AND " + sourceID + "=requested.id) ORDER BY requested.id LIMIT 100"
	rows, err := a.Database.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	type pending struct {
		id      int64
		student string
	}
	var events []pending
	for rows.Next() {
		var e pending
		if err = rows.Scan(&e.id, &e.student); err != nil {
			rows.Close()
			return err
		}
		events = append(events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, event := range events {
		if err = a.provisionEvent(ctx, event.id, event.student); err != nil {
			return err
		}
	}
	return nil
}
func (a *App) provisionEvent(ctx context.Context, eventID int64, student string) error {
	tx, err := a.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE students SET updated_at=updated_at WHERE id=$1", student); err != nil {
		return err
	}
	details, _ := json.Marshal(map[string]int64{"source_event_id": eventID})
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM activity_logs WHERE student_id=$1 AND event_type='lovable_provisioning_delivered' AND details=$2", student, string(details)).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	if a.config.LovableURL == "" || a.config.OnboardingURL == "" {
		return errors.New("provisioning links not configured")
	}
	var email, funnel string
	if err = tx.QueryRowContext(ctx, "SELECT email,funnel_tier FROM students WHERE id=$1", student).Scan(&email, &funnel); err != nil {
		return err
	}
	if funnel != "tier_1_active" && funnel != "tier_2_active" {
		return errors.New("provisioning entitlement unavailable")
	}
	deliveryCtx := context.WithValue(ctx, deliveryKey{}, "meuse-provision-"+strconv.FormatInt(eventID, 10))
	if err = a.config.Mailer.Send(deliveryCtx, services.Message{To: email, Subject: "Your Lovable learning access is ready", Text: fmt.Sprintf("Your Tier 1 payment is confirmed.\n\nLovable workspace template: %s\nOnboarding: %s\nLearning hub: %s/dashboard", a.config.LovableURL, a.config.OnboardingURL, a.origin)}); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO activity_logs(student_id,event_type,details) VALUES($1,'lovable_provisioning_delivered',$2)", student, string(details)); err != nil {
		return err
	}
	return tx.Commit()
}
