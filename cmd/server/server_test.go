package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/eeyung1/meuseAI/db"
	"github.com/eeyung1/meuseAI/engine"
	"github.com/eeyung1/meuseAI/services"
)

func testConfig() Config {
	return Config{Database: db.Config{Driver: "sqlite"}, PublicURL: "https://example.com", SessionKey: []byte(strings.Repeat("s", 32)), AdminToken: strings.Repeat("a", 32), PaystackSecret: "test-secret", Mailer: &services.DryRunMailer{}}
}
func TestTokenPurposeAndExpiry(t *testing.T) {
	a, err := New(context.Background(), testConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	token, _ := a.sign("student", "unsubscribe", time.Hour)
	if _, err = a.validate(token, "session"); err == nil {
		t.Fatal("unsubscribe became session")
	}
	if got, err := a.validate(token, "unsubscribe"); err != nil || got != "student" {
		t.Fatal("valid token failed")
	}
	expired, _ := a.sign("student", "session", -time.Second)
	if _, err = a.validate(expired, "session"); err == nil {
		t.Fatal("expired session accepted")
	}
	for _, bad := range []string{"", token + "x", strings.Repeat("a", 2048)} {
		if _, err = a.validate(bad, "unsubscribe"); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
}
func TestConfigValidationAndClosedLifecycle(t *testing.T) {
	for _, change := range []func(*Config){func(c *Config) { c.PublicURL = "http://public.example" }, func(c *Config) { c.PublicURL = "https://example.com/path" }, func(c *Config) { c.PublicURL = "https://user:secret@example.com" }, func(c *Config) { c.SessionKey = nil }, func(c *Config) { c.AdminToken = "weak" }, func(c *Config) { c.Mailer = nil }, func(c *Config) { c.LovableURL = "http://example.com" }, func(c *Config) { c.PaystackSecret = "" }} {
		config := testConfig()
		change(&config)
		if app, err := New(context.Background(), config); err == nil {
			app.Close()
			t.Fatal("bad configuration accepted")
		}
	}
	a, err := New(context.Background(), testConfig())
	if err != nil {
		t.Fatal(err)
	}
	a.Close()
	if a.Start(context.Background()) == nil {
		t.Fatal("closed app started")
	}
}
func TestRecoveryAndSafeLogs(t *testing.T) {
	a, err := New(context.Background(), testConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	handler := a.middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("private panic secret") }))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/secret?token=private", nil))
	if w.Code != 500 || strings.Contains(w.Body.String(), "secret") {
		t.Fatal("panic leaked")
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestResendAdapterAndIdempotency(t *testing.T) {
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.resend.com/emails" || r.Header.Get("Authorization") != "Bearer resend-test" || r.Header.Get("Idempotency-Key") != "delivery-1" {
			t.Fatal("email provider request mismatch")
		}
		raw, _ := io.ReadAll(r.Body)
		for _, value := range []string{"List-Unsubscribe", "One-Click", "student@example.com"} {
			if !strings.Contains(string(raw), value) {
				t.Fatal("missing email fields")
			}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":"email-1"}`)), Header: make(http.Header)}, nil
	})}
	mailer, err := NewResendMailer("resend-test", "meuseAI <hello@example.com>", client)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), deliveryKey{}, "delivery-1")
	if err = mailer.Send(ctx, services.Message{To: "student@example.com", Subject: "Welcome", Text: "Hello", UnsubscribeURL: "https://example.com/unsubscribe"}); err != nil {
		t.Fatal(err)
	}
	client.Transport = transportFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("secret transport error") })
	failing, _ := NewResendMailer("resend-test", "hello@example.com", client)
	if err = failing.Send(ctx, services.Message{}); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("provider error leaked")
	}
}
func TestProvisionFailureRemainsPending(t *testing.T) {
	config := testConfig()
	a, err := New(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	_, err = a.Database.Exec("INSERT INTO students(id,full_name,email,consent_timestamp,unsubscribe_token,funnel_tier) VALUES('s','Student','s@example.com',CURRENT_TIMESTAMP,'unsub','tier_1_active')")
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Database.Exec("INSERT INTO activity_logs(student_id,event_type,details) VALUES('s','lovable_provisioning_requested','{}')")
	if err != nil {
		t.Fatal(err)
	}
	if a.ProvisionOnce(context.Background()) == nil {
		t.Fatal("missing access links marked delivered")
	}
	var count int
	a.Database.QueryRow("SELECT COUNT(*) FROM activity_logs WHERE event_type='lovable_provisioning_delivered'").Scan(&count)
	if count != 0 {
		t.Fatal("failed delivery consumed event")
	}
}

func TestDripUnsubscribePurposeAndPersistence(t *testing.T) {
	config := testConfig()
	a, err := New(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err = a.Database.Exec("INSERT INTO students(id,full_name,email,consent_timestamp,unsubscribe_token,email_verified) VALUES('s','Student','s@example.com',CURRENT_TIMESTAMP,'unsub',TRUE)"); err != nil {
		t.Fatal(err)
	}
	if err = a.dispatch(context.Background(), engine.Action{StudentID: "s", Email: "s@example.com", Day: 1, Kind: engine.DeliverDay}); err != nil {
		t.Fatal(err)
	}
	messages := config.Mailer.(*services.DryRunMailer).Messages()
	if len(messages) != 1 {
		t.Fatal("drip message missing")
	}
	link := messages[0].UnsubscribeURL
	if link == "" {
		t.Fatal("drip unsubscribe missing")
	}
	parsed, _ := url.Parse(link)
	if _, err = a.validate(parsed.Query().Get("token"), "session"); err == nil {
		t.Fatal("email opt-out link grants session")
	}
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		a.Handler.ServeHTTP(w, httptest.NewRequest("POST", link, strings.NewReader("List-Unsubscribe=One-Click")))
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	var status string
	var count int
	a.Database.QueryRow("SELECT engagement_status FROM students WHERE id='s'").Scan(&status)
	a.Database.QueryRow("SELECT COUNT(*) FROM activity_logs WHERE event_type='unsubscribe'").Scan(&count)
	if status != "unsubscribed" || count != 1 {
		t.Fatal("unsubscribe not persisted/idempotent")
	}
}
func TestConfigFromEnv(t *testing.T) {
	for key, value := range map[string]string{"MEUSEAI_DB_DRIVER": "sqlite", "MEUSEAI_DB_DSN": ":memory:", "MEUSEAI_DRY_RUN": "true", "MEUSEAI_PUBLIC_URL": "http://localhost:8080", "MEUSEAI_SESSION_KEY": strings.Repeat("s", 32), "MEUSEAI_ADMIN_TOKEN": strings.Repeat("a", 32), "PAYSTACK_SECRET_KEY": "test-key"} {
		t.Setenv(key, value)
	}
	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if config.Database.Driver != "sqlite" || config.Database.DSN != ":memory:" {
		t.Fatal("database config missing")
	}
	t.Setenv("MEUSEAI_DB_DRIVER", "postgres")
	config, err = ConfigFromEnv()
	if err != nil || config.Database.Schema != "meuseai" {
		t.Fatal("shared PostgreSQL schema not isolated")
	}
	t.Setenv("MEUSEAI_DRY_RUN", "typo")
	if _, err = ConfigFromEnv(); err == nil {
		t.Fatal("invalid dry-run accepted")
	}
}

func TestLandingAndMagicLoginJourney(t *testing.T) {
	ctx := context.Background()
	config := testConfig()
	mailer := &services.DryRunMailer{}
	config.Mailer = mailer
	a, err := New(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	// 1. Unauthenticated visitor landing on GET / receives landing.html
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	a.Handler.ServeHTTP(w, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Claim Your Free Seat") {
		t.Fatalf("landing page failed: code=%d body=%s", w.Code, w.Body.String())
	}

	// Insert an existing verified student
	now := time.Now().UTC()
	_, err = a.Database.Exec("INSERT INTO students (id, full_name, email, email_verified, consent_timestamp, unsubscribe_token, created_at, updated_at) VALUES ('student-1', 'Jane Doe', 'jane@example.com', TRUE, $1, 'unsub-token-1', $1, $1)", now)
	if err != nil {
		t.Fatal(err)
	}

	// 2. Request Magic Link for existing verified student
	reqML := httptest.NewRequest("POST", "/api/v1/students/magic-link", strings.NewReader(`{"email":"jane@example.com"}`))
	reqML.Header.Set("Content-Type", "application/json")
	wML := httptest.NewRecorder()
	a.Handler.ServeHTTP(wML, reqML)
	if wML.Code != 202 {
		t.Fatalf("magic link request failed: code=%d body=%s", wML.Code, wML.Body.String())
	}
	messages := mailer.Messages()
	if len(messages) == 0 {
		t.Fatal("magic link email not sent")
	}
	msg := messages[len(messages)-1]
	if !strings.Contains(msg.Subject, "Sign-In Link") {
		t.Fatalf("unexpected email subject: %s", msg.Subject)
	}

	// Extract token from magic login link in email
	parsedURL := ""
	for _, word := range strings.Fields(msg.Text) {
		if strings.Contains(word, "/api/v1/students/magic-login?token=") {
			parsedURL = word
			break
		}
	}
	if parsedURL == "" {
		t.Fatalf("magic link not found in email text: %s", msg.Text)
	}
	u, _ := url.Parse(parsedURL)
	token := u.Query().Get("token")
	if token == "" {
		t.Fatal("empty magic token in email")
	}

	// 3. Redeem magic login in browser (Accept: text/html)
	reqRedeem := httptest.NewRequest("GET", "/api/v1/students/magic-login?token="+url.QueryEscape(token), nil)
	reqRedeem.Header.Set("Accept", "text/html")
	wRedeem := httptest.NewRecorder()
	a.Handler.ServeHTTP(wRedeem, reqRedeem)
	if wRedeem.Code != http.StatusSeeOther || wRedeem.Header().Get("Location") != "/dashboard" {
		t.Fatalf("magic login failed: code=%d loc=%s", wRedeem.Code, wRedeem.Header().Get("Location"))
	}
	var sessionCookieVal string
	for _, cookie := range wRedeem.Result().Cookies() {
		if cookie.Name == sessionCookie {
			sessionCookieVal = cookie.Value
			break
		}
	}
	if sessionCookieVal == "" {
		t.Fatal("session cookie missing after magic login")
	}

	// 4. Authenticated visitor on GET / redirects to /dashboard
	reqAuth := httptest.NewRequest("GET", "/", nil)
	reqAuth.AddCookie(&http.Cookie{Name: sessionCookie, Value: sessionCookieVal})
	wAuth := httptest.NewRecorder()
	a.Handler.ServeHTTP(wAuth, reqAuth)
	if wAuth.Code != http.StatusSeeOther || wAuth.Header().Get("Location") != "/dashboard" {
		t.Fatalf("authenticated root did not redirect to dashboard: code=%d loc=%s", wAuth.Code, wAuth.Header().Get("Location"))
	}

	// 5. Logout clears the cookie
	reqLogout := httptest.NewRequest("POST", "/api/v1/students/logout", nil)
	reqLogout.Header.Set("Accept", "text/html")
	reqLogout.AddCookie(&http.Cookie{Name: sessionCookie, Value: sessionCookieVal})
	wLogout := httptest.NewRecorder()
	a.Handler.ServeHTTP(wLogout, reqLogout)
	if wLogout.Code != http.StatusSeeOther || wLogout.Header().Get("Location") != "/" {
		t.Fatalf("logout did not redirect to root: code=%d", wLogout.Code)
	}
	cookieCleared := false
	for _, cookie := range wLogout.Result().Cookies() {
		if cookie.Name == sessionCookie && cookie.MaxAge < 0 {
			cookieCleared = true
			break
		}
	}
	if !cookieCleared {
		t.Fatal("logout did not clear session cookie")
	}
}
