package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eeyung1/meuseAI/db"
	"github.com/eeyung1/meuseAI/services"
)

func intakeDatabase(t *testing.T, postgres bool) *db.DB {
	t.Helper()
	cfg := db.Config{Driver: "sqlite"}
	if postgres {
		cfg = db.Config{Driver: "postgres", DSN: os.Getenv("MEUSEAI_TEST_POSTGRES_DSN"), Schema: fmt.Sprintf("intake_%d", time.Now().UnixNano())}
		if cfg.DSN == "" {
			t.Skip("MEUSEAI_TEST_POSTGRES_DSN unavailable; PostgreSQL not run")
		}
	}
	d, err := db.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if postgres {
			_, err := d.Exec("DROP SCHEMA " + cfg.Schema + " CASCADE")
			if err != nil {
				t.Error(err)
			}
		}
		d.Close()
	})
	return d
}
func intakeRequest(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func intakeFixture(t *testing.T, postgres bool) (*db.DB, *Intake, *services.DryRunMailer, *http.ServeMux) {
	t.Helper()
	d := intakeDatabase(t, postgres)
	m := &services.DryRunMailer{}
	h, err := NewIntake(d, m, "https://learn.example.com")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Register(mux)
	return d, h, m, mux
}
func intakeLinks(t *testing.T, m *services.DryRunMailer) (string, string) {
	t.Helper()
	messages := m.Messages()
	if len(messages) == 0 {
		t.Fatal("missing verification email")
	}
	msg := messages[len(messages)-1]
	links := regexp.MustCompile(`https://[^\s]+`).FindAllString(msg.Text, -1)
	if len(links) != 2 || msg.UnsubscribeURL != links[1] {
		t.Fatalf("bad email: %+v", msg)
	}
	return links[0], links[1]
}

const intakePayload = `{"full_name":"  Ada Learner  ","email":"Ada@Example.com","consent":true}`

func TestIntakeJourney(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		t.Run(fmt.Sprintf("postgres=%v", postgres), func(t *testing.T) {
			d, _, m, mux := intakeFixture(t, postgres)
			w := intakeRequest(mux, "POST", "/api/v1/students/signup", intakePayload)
			if w.Code != 202 {
				t.Fatalf("signup %d %s", w.Code, w.Body.String())
			}
			verify, unsub := intakeLinks(t, m)
			var id, name, email, verificationHash, unsubHash string
			var verified bool
			var consent time.Time
			if err := d.QueryRow("SELECT id,full_name,email,verification_token,unsubscribe_token,email_verified,consent_timestamp FROM students").Scan(&id, &name, &email, &verificationHash, &unsubHash, &verified, &consent); err != nil {
				t.Fatal(err)
			}
			if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(id) || name != "Ada Learner" || email != "ada@example.com" || verified || consent.IsZero() {
				t.Fatalf("invalid student %s %s %s %v", id, name, email, verified)
			}
			vu, _ := url.Parse(verify)
			uu, _ := url.Parse(unsub)
			if verificationHash != tokenDigest(vu.Query().Get("token")) || unsubHash != tokenDigest(uu.Query().Get("token")) || verificationHash == unsubHash {
				t.Fatal("token hashing or independence failed")
			}
			if strings.Contains(w.Body.String(), id) || strings.Contains(w.Body.String(), vu.Query().Get("token")) {
				t.Fatal("signup leaked credentials")
			}
			duplicate := intakeRequest(mux, "POST", "/api/v1/students/signup", intakePayload)
			if duplicate.Code != 202 || duplicate.Body.String() != w.Body.String() || len(m.Messages()) != 1 {
				t.Fatal("duplicate signup was not private/idempotent")
			}
			if w = intakeRequest(mux, "GET", verify, ""); w.Code != 200 {
				t.Fatalf("verify %d %s", w.Code, w.Body.String())
			}
			var stage, status string
			var token *string
			if err := d.QueryRow("SELECT email_verified,current_stage,engagement_status,verification_token FROM students").Scan(&verified, &stage, &status, &token); err != nil {
				t.Fatal(err)
			}
			if !verified || stage != "day_1" || status != "pending" || token != nil {
				t.Fatal("verification did not consume token/unlock day 1")
			}
			if w = intakeRequest(mux, "GET", verify, ""); w.Code != 400 {
				t.Fatal("verification replay accepted")
			}
			for _, method := range []string{"GET", "POST", "GET"} {
				if w = intakeRequest(mux, method, unsub, ""); w.Code != 200 {
					t.Fatalf("unsubscribe %d %s", w.Code, w.Body.String())
				}
			}
			var unsubscribed time.Time
			if err := d.QueryRow("SELECT engagement_status,unsubscribed_at FROM students").Scan(&status, &unsubscribed); err != nil {
				t.Fatal(err)
			}
			if status != "unsubscribed" || unsubscribed.IsZero() {
				t.Fatal("unsubscribe not persisted")
			}
			var events int
			if err := d.QueryRow("SELECT COUNT(*) FROM activity_logs").Scan(&events); err != nil {
				t.Fatal(err)
			}
			if events != 3 {
				t.Fatalf("audit events %d", events)
			}
			if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatal("bearer response cached/leaked")
			}
		})
	}
}

func TestIntakeValidation(t *testing.T) {
	d, _, m, mux := intakeFixture(t, false)
	bodies := []string{`{}`, `null`, `{"full_name":"A","email":"a@b.com"}`, `{"full_name":"A","email":"bad","consent":true}`, `{"full_name":"A","email":"Ada <a@b.com>","consent":true}`, `{"full_name":"A","email":"a@b.com","consent":false}`, intakePayload + ` {}`, `{"extra":true}`, `{"full_name":"` + strings.Repeat("x", 9000) + `"}`, `{"full_name":"` + strings.Repeat("x", 256) + `","email":"a@b.com","consent":true}`}
	for _, body := range bodies {
		if w := intakeRequest(mux, "POST", "/api/v1/students/signup", body); w.Code != 400 {
			t.Fatalf("invalid input returned %d", w.Code)
		}
	}
	for _, path := range []string{"verify", "unsubscribe"} {
		for _, query := range []string{"", "?token=bad", "?token=" + strings.Repeat("g", 64), "?token=" + strings.Repeat("a", 64), "?token=" + strings.Repeat("a", 64) + "&token=" + strings.Repeat("b", 64)} {
			if w := intakeRequest(mux, "GET", "/api/v1/students/"+path+query, ""); w.Code != 400 {
				t.Fatalf("invalid bearer returned %d", w.Code)
			}
		}
	}
	if w := intakeRequest(mux, "GET", "/api/v1/students/signup", ""); w.Code != 405 {
		t.Fatal("method guard")
	}
	var n int
	if err := d.QueryRow("SELECT COUNT(*) FROM students").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 || len(m.Messages()) != 0 {
		t.Fatal("invalid request persisted/sent")
	}
}

func TestIntakeExpiredAndUnsubscribed(t *testing.T) {
	for _, scenario := range []string{"expired", "unsubscribed", "boundary"} {
		t.Run(scenario, func(t *testing.T) {
			_, h, m, mux := intakeFixture(t, false)
			now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
			h.now = func() time.Time { return now }
			if w := intakeRequest(mux, "POST", "/api/v1/students/signup", intakePayload); w.Code != 202 {
				t.Fatal(w.Body.String())
			}
			verify, unsub := intakeLinks(t, m)
			if scenario == "unsubscribed" {
				if w := intakeRequest(mux, "GET", unsub, ""); w.Code != 200 {
					t.Fatal(w.Body.String())
				}
			} else if scenario == "expired" {
				now = now.Add(25 * time.Hour)
			} else {
				now = now.Add(24 * time.Hour)
			}
			if w := intakeRequest(mux, "GET", verify, ""); w.Code != 400 {
				t.Fatalf("%s token accepted: %d", scenario, w.Code)
			}
		})
	}
}

type intakeFailMailer struct{}

func (intakeFailMailer) Send(context.Context, services.Message) error {
	return errors.New("provider secret must not leak")
}
func TestIntakeDeliveryFailure(t *testing.T) {
	d, h, _, mux := intakeFixture(t, false)
	h.mailer = intakeFailMailer{}
	w := intakeRequest(mux, "POST", "/api/v1/students/signup", intakePayload)
	if w.Code != 503 || strings.Contains(w.Body.String(), "provider secret") {
		t.Fatal(w.Body.String())
	}
	for _, table := range []string{"students", "activity_logs"} {
		var n int
		if err := d.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatal("delivery failure did not roll back")
		}
	}
	h.mailer = &services.DryRunMailer{}
	if w = intakeRequest(mux, "POST", "/api/v1/students/signup", intakePayload); w.Code != 202 {
		t.Fatal("signup retry failed")
	}
}
func TestIntakeConcurrentSignupAndVerification(t *testing.T) {
	d, _, m, mux := intakeFixture(t, false)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := intakeRequest(mux, "POST", "/api/v1/students/signup", intakePayload); w.Code != 202 {
				t.Errorf("signup %d", w.Code)
			}
		}()
	}
	wg.Wait()
	if len(m.Messages()) != 1 {
		t.Fatal("duplicate email delivery")
	}
	verify, _ := intakeLinks(t, m)
	codes := make(chan int, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- intakeRequest(mux, "GET", verify, "").Code }()
	}
	wg.Wait()
	close(codes)
	success := 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code != 400 {
			t.Errorf("verify %d", code)
		}
	}
	if success != 1 {
		t.Fatalf("verification successes %d", success)
	}
	var n int
	if err := d.QueryRow("SELECT COUNT(*) FROM activity_logs WHERE event_type='verify'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("duplicate verify audit")
	}
}
func TestIntakeConfigurationAndDatabaseFailure(t *testing.T) {
	d, _, m, mux := intakeFixture(t, false)
	for _, base := range []string{"", "/relative", "ftp://example.com", "https://user:secret@example.com", "https://example.com?token=x", "https://example.com/#frag"} {
		if _, err := NewIntake(d, m, base); err == nil {
			t.Fatalf("accepted URL %s", base)
		}
	}
	if _, err := NewIntake(nil, m, "https://example.com"); err == nil {
		t.Fatal("nil DB accepted")
	}
	if _, err := NewIntake(d, nil, "https://example.com"); err == nil {
		t.Fatal("nil mailer accepted")
	}
	d.Close()
	if w := intakeRequest(mux, "POST", "/api/v1/students/signup", intakePayload); w.Code != 503 {
		t.Fatal("closed DB signup")
	}
	if w := intakeRequest(mux, "GET", "/api/v1/students/verify?token="+strings.Repeat("a", 64), ""); w.Code != 503 {
		t.Fatal("closed DB verify")
	}
}

func TestIntakeAuditRollback(t *testing.T) {
	d, _, m, mux := intakeFixture(t, false)
	if w := intakeRequest(mux, "POST", "/api/v1/students/signup", intakePayload); w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	verify, unsub := intakeLinks(t, m)
	if _, err := d.Exec(`CREATE TRIGGER reject_audit BEFORE INSERT ON activity_logs BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{verify, unsub} {
		if w := intakeRequest(mux, "GET", link, ""); w.Code != 503 {
			t.Fatalf("audit failure returned %d", w.Code)
		}
	}
	var verified bool
	var token *string
	var unsubscribed *time.Time
	if err := d.QueryRow("SELECT email_verified,verification_token,unsubscribed_at FROM students").Scan(&verified, &token, &unsubscribed); err != nil {
		t.Fatal(err)
	}
	if verified || token == nil || unsubscribed != nil {
		t.Fatal("state changed despite failed audit")
	}
	if _, err := d.Exec("DROP TRIGGER reject_audit"); err != nil {
		t.Fatal(err)
	}
	if w := intakeRequest(mux, "GET", verify, ""); w.Code != 200 {
		t.Fatal("token consumed by failed transaction")
	}
}

func TestResendVerificationAndHTMLVerify(t *testing.T) {
	d, _, m, mux := intakeFixture(t, false)
	// Initial signup
	if w := intakeRequest(mux, "POST", "/api/v1/students/signup", intakePayload); w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	if len(m.Messages()) != 1 {
		t.Fatalf("expected 1 email, got %d", len(m.Messages()))
	}
	initialVerify, _ := intakeLinks(t, m)

	// Resend verification for existing unverified student
	resendBody := `{"email":"ada@example.com"}`
	w := intakeRequest(mux, "POST", "/api/v1/students/resend-verification", resendBody)
	if w.Code != 202 {
		t.Fatalf("resend failed with %d: %s", w.Code, w.Body.String())
	}
	if len(m.Messages()) != 2 {
		t.Fatalf("expected 2 emails after resend, got %d", len(m.Messages()))
	}
	newVerify, _ := intakeLinks(t, m)
	if initialVerify == newVerify {
		t.Fatal("resend did not issue a new distinct verification token")
	}

	// Resend for non-existent email returns 202 without leaking info or sending email
	w = intakeRequest(mux, "POST", "/api/v1/students/resend-verification", `{"email":"ghost@example.com"}`)
	if w.Code != 202 || len(m.Messages()) != 2 {
		t.Fatalf("resend for non-existent email failed code=%d msgs=%d", w.Code, len(m.Messages()))
	}

	// Invalid input to resend
	if w = intakeRequest(mux, "POST", "/api/v1/students/resend-verification", `{"email":"bad-email"}`); w.Code != 400 {
		t.Fatalf("expected 400 for bad email, got %d", w.Code)
	}

	// HTML Verify GET with Accept: text/html
	rHtml := httptest.NewRequest("GET", newVerify, nil)
	rHtml.Header.Set("Accept", "text/html,application/xhtml+xml")
	wHtml := httptest.NewRecorder()
	mux.ServeHTTP(wHtml, rHtml)
	if wHtml.Code != 200 || !strings.Contains(wHtml.Body.String(), "Welcome to meuseAI") {
		t.Fatalf("HTML verification failed: code=%d body=%s", wHtml.Code, wHtml.Body.String())
	}

	// Second verify attempt with HTML redirects to /verify?error=expired
	wReplay := httptest.NewRecorder()
	mux.ServeHTTP(wReplay, rHtml)
	if wReplay.Code != http.StatusSeeOther || !strings.Contains(wReplay.Header().Get("Location"), "/verify?error=expired") {
		t.Fatalf("expected redirect to expired, got code=%d loc=%s", wReplay.Code, wReplay.Header().Get("Location"))
	}

	// /verify route serves verify.html
	rVerifyPage := httptest.NewRequest("GET", "/verify", nil)
	wVerifyPage := httptest.NewRecorder()
	mux.ServeHTTP(wVerifyPage, rVerifyPage)
	if wVerifyPage.Code != 200 || !strings.Contains(wVerifyPage.Body.String(), "Verification") {
		t.Fatalf("GET /verify failed: code=%d", wVerifyPage.Code)
	}
	_ = d
}
