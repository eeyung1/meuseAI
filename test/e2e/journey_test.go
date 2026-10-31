package e2e

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	server "github.com/eeyung1/meuseAI/cmd/server"
	"github.com/eeyung1/meuseAI/db"
	"github.com/eeyung1/meuseAI/services"
)

type providerTransport func(*http.Request) (*http.Response, error)

func (f providerTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fixture struct {
	app  *server.App
	mail *services.DryRunMailer
	logs *bytes.Buffer
}

func newFixture(t *testing.T, postgres bool) fixture {
	t.Helper()
	database := db.Config{Driver: "sqlite"}
	if postgres {
		database = db.Config{Driver: "postgres", DSN: os.Getenv("MEUSEAI_TEST_POSTGRES_DSN"), Schema: fmt.Sprintf("e2e_%d", time.Now().UnixNano())}
		if database.DSN == "" {
			t.Skip("MEUSEAI_TEST_POSTGRES_DSN unavailable; PostgreSQL not run")
		}
	}
	mail := &services.DryRunMailer{}
	logs := &bytes.Buffer{}
	config := server.Config{Database: database, PublicURL: "https://learn.example.com", SessionKey: []byte(strings.Repeat("s", 32)), AdminToken: strings.Repeat("a", 32), PaystackSecret: "paystack-test-secret", Mailer: mail, Logger: log.New(logs, "", 0), LovableURL: "https://lovable.dev/template/test", OnboardingURL: "https://learn.example.com/onboarding", DripInterval: 20 * time.Millisecond, TelemetryInterval: time.Millisecond, ProvisionInterval: 20 * time.Millisecond, HTTPClient: &http.Client{Transport: providerTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.paystack.co/transaction/initialize" {
			return nil, fmt.Errorf("unexpected request")
		}
		var request struct {
			Reference string `json:"reference"`
			Amount    int64  `json:"amount"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return nil, err
		}
		if request.Amount != 450000 && request.Amount != 500000 && request.Amount != 2500000 {
			return nil, fmt.Errorf("unexpected amount")
		}
		body := fmt.Sprintf(`{"status":true,"data":{"authorization_url":"https://checkout.paystack.com/test","access_code":"test","reference":%q}}`, request.Reference)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	app, err := server.New(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if postgres {
			if _, err := app.Database.Exec("DROP SCHEMA " + database.Schema + " CASCADE"); err != nil {
				t.Error(err)
			}
		}
		app.Close()
	})
	return fixture{app, mail, logs}
}
func perform(app *server.App, method, path, body string, cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
		req.Header.Set("Origin", "https://learn.example.com")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	app.Handler.ServeHTTP(w, req)
	return w
}
func register(t *testing.T, f fixture) (string, *http.Cookie, string) {
	t.Helper()
	w := perform(f.app, "POST", "/api/v1/students/signup", `{"full_name":"Ada Learner","email":"ada@example.com","consent":true}`, nil, nil)
	if w.Code != 202 {
		t.Fatalf("signup %d %s", w.Code, w.Body.String())
	}
	messages := f.mail.Messages()
	if len(messages) != 1 {
		t.Fatal("verification email not captured")
	}
	links := regexp.MustCompile(`https://[^\s]+`).FindAllString(messages[0].Text, -1)
	if len(links) != 2 {
		t.Fatal("verification/unsubscribe links missing")
	}
	w = perform(f.app, "GET", links[0], "", nil, nil)
	if w.Code != 200 {
		t.Fatalf("verify %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("verification did not issue a session")
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 604800 {
		t.Fatal("unsafe session cookie")
	}
	var id string
	if err := f.app.Database.QueryRow("SELECT id FROM students WHERE email='ada@example.com'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id, cookie, links[1]
}
func complete(t *testing.T, f fixture, cookie *http.Cookie, day int) {
	t.Helper()
	body := fmt.Sprintf(`{"day":%d,"submission":"Subject, style, setting: an origami fox in a moonlit garden."}`, day)
	w := perform(f.app, "POST", "/api/v1/curriculum/complete", body, cookie, nil)
	if w.Code != 200 {
		t.Fatalf("day %d: %d %s", day, w.Code, w.Body.String())
	}
}
func webhookBody(reference string, amount int64) string {
	return fmt.Sprintf(`{"event":"charge.success","data":{"reference":%q,"status":"success","amount":%d,"currency":"NGN","customer":{"email":"ada@example.com"}}}`, reference, amount)
}
func webhook(app *server.App, body string) *httptest.ResponseRecorder {
	mac := hmac.New(sha512.New, []byte("paystack-test-secret"))
	mac.Write([]byte(body))
	return perform(app, "POST", "/api/v1/webhooks/paystack", body, nil, map[string]string{"X-Paystack-Signature": hex.EncodeToString(mac.Sum(nil))})
}
func TestStudentJourney(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		t.Run(fmt.Sprintf("postgres=%v", postgres), func(t *testing.T) {
			f := newFixture(t, postgres)
			id, cookie, unsubscribe := register(t, f)
			if _, err := f.app.Database.Exec("UPDATE students SET curriculum_started_at=$1 WHERE id=$2", time.Now().UTC().Add(-72*time.Hour), id); err != nil {
				t.Fatal(err)
			}
			for day := 1; day <= 4; day++ {
				complete(t, f, cookie, day)
			}
			w := perform(f.app, "GET", "/api/v1/dashboard/me", "", cookie, nil)
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			var profile struct {
				XP    int    `json:"xp"`
				Stage string `json:"current_stage"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &profile); err != nil {
				t.Fatal(err)
			}
			if profile.XP != 1150 || profile.Stage != "conversion_gate" {
				t.Fatal("completion-gated journey did not reach conversion")
			}
			w = perform(f.app, "POST", "/api/v1/payments/checkout", `{"tier":"tier_1"}`, cookie, nil)
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			var checkout services.Checkout
			if json.Unmarshal(w.Body.Bytes(), &checkout) != nil {
				t.Fatal("invalid checkout response")
			}
			for i := 0; i < 2; i++ {
				w = webhook(f.app, webhookBody(checkout.Reference, 450000))
				if w.Code != 200 {
					t.Fatal(w.Body.String())
				}
			}
			var funnel, payment string
			if err := f.app.Database.QueryRow("SELECT funnel_tier,payment_status FROM students WHERE id=$1", id).Scan(&funnel, &payment); err != nil {
				t.Fatal(err)
			}
			if funnel != "tier_1_active" || payment != "paid" {
				t.Fatal("paid access missing")
			}
			for i := 0; i < 2; i++ {
				if err := f.app.ProvisionOnce(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			messages := f.mail.Messages()
			if len(messages) != 2 || !strings.Contains(messages[1].Text, "https://lovable.dev/template/test") {
				t.Fatalf("provisioning delivery count/content %+v", messages)
			}
			var count int
			if err := f.app.Database.QueryRow("SELECT COUNT(*) FROM activity_logs WHERE event_type='lovable_provisioning_delivered'").Scan(&count); err != nil || count != 1 {
				t.Fatal("provisioning not idempotent")
			}
			if w = perform(f.app, "POST", "/api/v1/payments/checkout", `{"tier":"tier_2"}`, cookie, nil); w.Code != 403 {
				t.Fatal("Tier 2 unlocked without manual gate")
			}
			if w = perform(f.app, "GET", unsubscribe, "", nil, nil); w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			logs := f.logs.String()
			for _, secret := range []string{cookie.Value, "token=", "ada@example.com", "paystack-test-secret", "origami fox"} {
				if strings.Contains(logs, secret) {
					t.Fatalf("request log leaked %s", secret)
				}
			}
		})
	}
}
func TestAuthenticationAndOriginIsolation(t *testing.T) {
	f := newFixture(t, false)
	id, cookie, _ := register(t, f)
	for _, path := range []string{"/api/v1/dashboard/me", "/api/v1/curriculum/complete", "/api/v1/payments/checkout", "/api/v1/telemetry/stream"} {
		method := "GET"
		if strings.HasSuffix(path, "complete") || strings.HasSuffix(path, "checkout") {
			method = "POST"
		}
		w := perform(f.app, method, path, `{"day":1,"student_id":"`+id+`","submission":"forged"}`, nil, map[string]string{"X-Student-ID": id})
		if w.Code != 401 {
			t.Fatalf("unsigned identity accepted %s %d", path, w.Code)
		}
	}
	tampered := *cookie
	tampered.Value += "x"
	if perform(f.app, "GET", "/api/v1/dashboard/me", "", &tampered, nil).Code != 401 {
		t.Fatal("tampered session accepted")
	}
	for _, origin := range []string{"https://evil.example", ""} {
		w := perform(f.app, "POST", "/api/v1/curriculum/complete", `{"day":1,"submission":"valid prompt"}`, cookie, map[string]string{"Origin": origin})
		if w.Code != 403 {
			t.Fatal("cookie CSRF accepted")
		}
	}
	w := perform(f.app, "POST", "/api/v1/curriculum/complete", `{"student_id":"other","day":1,"submission":"valid prompt"}`, cookie, nil)
	if w.Code != 400 {
		t.Fatal("body identity overrode session")
	}
	complete(t, f, cookie, 1)
	w = perform(f.app, "OPTIONS", "/api/v1/curriculum/complete", "", nil, map[string]string{"Origin": "https://learn.example.com"})
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != "https://learn.example.com" || w.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("same-origin preflight failed")
	}
	w = perform(f.app, "POST", "/api/v1/admin/toggle-tier-2", `{"student_id":"`+id+`","tier_2_ready":true}`, nil, map[string]string{"Authorization": "Bearer " + strings.Repeat("a", 32)})
	if w.Code != 200 {
		t.Fatal("admin route not wired")
	}
}
func TestSignupRateLimitAndHealth(t *testing.T) {
	f := newFixture(t, false)
	for i := 0; i < 5; i++ {
		w := perform(f.app, "POST", "/api/v1/students/signup", `{}`, nil, nil)
		if w.Code != 400 {
			t.Fatal("unexpected signup")
		}
	}
	if w := perform(f.app, "POST", "/api/v1/students/signup", `{}`, nil, nil); w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal("signup rate limit missing")
	}
	if perform(f.app, "GET", "/healthz", "", nil, nil).Code != 200 {
		t.Fatal("health endpoint")
	}
	if w := perform(f.app, "GET", "/dashboard", "", nil, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "/assets/dashboard.js") {
		t.Fatal("dashboard not served")
	}
}

type streamWriter struct {
	header http.Header
	writer *io.PipeWriter
}

func (w *streamWriter) Header() http.Header         { return w.header }
func (w *streamWriter) WriteHeader(int)             {}
func (w *streamWriter) Write(p []byte) (int, error) { return w.writer.Write(p) }
func (w *streamWriter) Flush()                      {}
func TestAuthenticatedStreamAndWorkerShutdown(t *testing.T) {
	f := newFixture(t, false)
	_, cookie, _ := register(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := f.app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.app.Start(ctx); err == nil {
		t.Fatal("duplicate workers allowed")
	}
	reader, writer := io.Pipe()
	defer reader.Close()
	req := httptest.NewRequest("GET", "/api/v1/telemetry/stream", nil).WithContext(ctx)
	req.AddCookie(cookie)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer writer.Close()
		f.app.Handler.ServeHTTP(&streamWriter{header: make(http.Header), writer: writer}, req)
	}()
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		if scanner.Text() == "" {
			break
		}
	}
	complete(t, f, cookie, 1)
	event := false
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "event: ") {
			event = true
			break
		}
	}
	if !event {
		t.Fatal("live worker did not deliver invalidation")
	}
	cancel()
	reader.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream did not cancel")
	}
	if err := f.app.Close(); err != nil {
		t.Fatal(err)
	}
	if f.app.Database.Ping() == nil {
		t.Fatal("database remained open")
	}
	if err := f.app.Close(); err != nil {
		t.Fatal(err)
	}
}
