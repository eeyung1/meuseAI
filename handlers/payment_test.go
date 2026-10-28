package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eeyung1/meuseAI/db"
	"github.com/eeyung1/meuseAI/services"
)

type paymentTransport func(*http.Request) (*http.Response, error)

func (f paymentTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func paymentFixture(t *testing.T, postgres bool) (*db.DB, *Payment, *http.ServeMux) {
	t.Helper()
	cfg := db.Config{Driver: "sqlite"}
	if postgres {
		cfg = db.Config{Driver: "postgres", DSN: os.Getenv("MEUSEAI_TEST_POSTGRES_DSN"), Schema: fmt.Sprintf("payment_%d", time.Now().UnixNano())}
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
			if _, err := d.Exec("DROP SCHEMA " + cfg.Schema + " CASCADE"); err != nil {
				t.Error(err)
			}
		}
		d.Close()
	})
	_, err = d.Exec(`INSERT INTO students(id,full_name,email,email_verified,consent_timestamp,unsubscribe_token,current_stage) VALUES('s1','Student','student@example.com',TRUE,CURRENT_TIMESTAMP,'unsubscribe','conversion_gate')`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Exec("INSERT INTO student_progress(student_id,day_number,completed) VALUES('s1',4,TRUE)"); err != nil {
		t.Fatal(err)
	}
	client, _ := services.NewPaystack("secret", &http.Client{Transport: paymentTransport(func(r *http.Request) (*http.Response, error) {
		var request services.CheckoutRequest
		if err := json.NewDecoder(r.Body).Decode(&struct {
			Reference *string `json:"reference"`
		}{&request.Reference}); err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf(`{"status":true,"data":{"authorization_url":"https://checkout.paystack.com/code","access_code":"code","reference":%q}}`, request.Reference)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})})
	p, err := NewPayment(PaymentConfig{Database: d, Paystack: client, StudentIdentity: func(*http.Request) (string, error) { return "s1", nil }, AdminAuthorized: func(r *http.Request) bool { return r.Header.Get("Authorization") == "Bearer admin-test" }})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	p.Register(mux)
	return d, p, mux
}
func paymentRequest(mux http.Handler, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}
func paymentCheckout(t *testing.T, mux http.Handler, tier string) string {
	t.Helper()
	w := paymentRequest(mux, "/api/v1/payments/checkout", `{"tier":"`+tier+`"}`, nil)
	if w.Code != 200 {
		t.Fatalf("checkout %d %s", w.Code, w.Body.String())
	}
	var result services.Checkout
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Reference
}
func paymentEvent(reference string, amount int64) string {
	return fmt.Sprintf(`{"event":"charge.success","data":{"reference":%q,"status":"success","amount":%d,"currency":"NGN","customer":{"email":"student@example.com"}}}`, reference, amount)
}
func paymentSignature(body string) string {
	m := hmac.New(sha512.New, []byte("secret"))
	m.Write([]byte(body))
	return hex.EncodeToString(m.Sum(nil))
}
func paymentWebhook(mux http.Handler, body string) *httptest.ResponseRecorder {
	return paymentRequest(mux, "/api/v1/webhooks/paystack", body, map[string]string{"X-Paystack-Signature": paymentSignature(body)})
}
func paymentAssertCount(t *testing.T, d *db.DB, event string, want int) {
	t.Helper()
	var got int
	if err := d.QueryRow("SELECT COUNT(*) FROM activity_logs WHERE event_type=$1", event).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s events %d want %d", event, got, want)
	}
}

func TestPaymentJourney(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		t.Run(fmt.Sprintf("postgres=%v", postgres), func(t *testing.T) {
			d, _, mux := paymentFixture(t, postgres)
			reference := paymentCheckout(t, mux, "tier_1")
			body := paymentEvent(reference, 500000)
			for i := 0; i < 3; i++ {
				if w := paymentWebhook(mux, body); w.Code != 200 {
					t.Fatalf("webhook %d %s", w.Code, w.Body.String())
				}
			}
			var funnel, payment, status string
			var paid time.Time
			if err := d.QueryRow("SELECT funnel_tier,payment_status FROM students WHERE id='s1'").Scan(&funnel, &payment); err != nil {
				t.Fatal(err)
			}
			if funnel != "tier_1_active" || payment != "paid" {
				t.Fatal("access not unlocked")
			}
			if err := d.QueryRow("SELECT status,paid_at FROM payments WHERE reference=$1", reference).Scan(&status, &paid); err != nil {
				t.Fatal(err)
			}
			if status != "success" || paid.IsZero() {
				t.Fatal("payment not confirmed")
			}
			paymentAssertCount(t, d, "payment_success", 1)
			paymentAssertCount(t, d, "lovable_provisioning_requested", 1)
			if w := paymentRequest(mux, "/api/v1/payments/checkout", `{"tier":"tier_2"}`, nil); w.Code != 403 {
				t.Fatal("tier 2 sold without readiness")
			}
			if w := paymentRequest(mux, "/api/v1/admin/toggle-tier-2", `{"student_id":"s1","tier_2_ready":true}`, nil); w.Code != 403 {
				t.Fatal("public admin toggle")
			}
			w := paymentRequest(mux, "/api/v1/admin/toggle-tier-2", `{"student_id":"s1","tier_2_ready":true}`, map[string]string{"Authorization": "Bearer admin-test"})
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			ref2 := paymentCheckout(t, mux, "tier_2")
			if w = paymentWebhook(mux, paymentEvent(ref2, 2500000)); w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			if err := d.QueryRow("SELECT funnel_tier FROM students WHERE id='s1'").Scan(&funnel); err != nil {
				t.Fatal(err)
			}
			if funnel != "tier_2_active" {
				t.Fatal("tier 2 not unlocked")
			}
			paymentAssertCount(t, d, "tier_2_provisioning_requested", 1)
			paymentAssertCount(t, d, "payment_success", 2)
			// Replaying Tier 1 must not downgrade Tier 2.
			if w = paymentWebhook(mux, body); w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			if err := d.QueryRow("SELECT funnel_tier FROM students WHERE id='s1'").Scan(&funnel); err != nil || funnel != "tier_2_active" {
				t.Fatal("tier 1 replay downgraded access")
			}
		})
	}
}

func TestPaymentMismatchedAndUnauthenticatedWebhooks(t *testing.T) {
	d, _, mux := paymentFixture(t, false)
	ref := paymentCheckout(t, mux, "tier_1")
	valid := paymentEvent(ref, 500000)
	for _, body := range []string{paymentEvent(ref, 1), strings.Replace(valid, "NGN", "USD", 1), strings.Replace(valid, "student@example.com", "attacker@example.com", 1), paymentEvent("unknown", 500000), strings.Replace(valid, `"status":"success"`, `"status":"failed"`, 1), `bad`, valid + ` {}`} {
		if w := paymentWebhook(mux, body); w.Code != 400 {
			t.Fatalf("invalid charge %d %s", w.Code, w.Body.String())
		}
	}
	for _, signature := range []string{"", strings.Repeat("0", 128), paymentSignature(valid + " ")} {
		if w := paymentRequest(mux, "/api/v1/webhooks/paystack", valid, map[string]string{"X-Paystack-Signature": signature}); w.Code != 401 {
			t.Fatal("unauthenticated webhook accepted")
		}
	}
	if w := paymentWebhook(mux, `{"event":"subscription.create","data":{}}`); w.Code != 200 {
		t.Fatal("unrelated signed event rejected")
	}
	paymentAssertCount(t, d, "payment_success", 0)
	var status string
	if err := d.QueryRow("SELECT status FROM payments WHERE reference=$1", ref).Scan(&status); err != nil || status != "pending" {
		t.Fatal("bad webhook mutated payment")
	}
}

func TestPaymentCheckoutEligibilityAndIdentity(t *testing.T) {
	for _, change := range []string{"UPDATE students SET email_verified=FALSE", "UPDATE students SET unsubscribed_at=CURRENT_TIMESTAMP", "UPDATE students SET current_stage='day_3'", "UPDATE students SET funnel_tier='free_tier_lapsed'", "UPDATE student_progress SET completed=FALSE"} {
		t.Run(change, func(t *testing.T) {
			d, _, mux := paymentFixture(t, false)
			if _, err := d.Exec(change); err != nil {
				t.Fatal(err)
			}
			if w := paymentRequest(mux, "/api/v1/payments/checkout", `{}`, nil); w.Code != 403 {
				t.Fatalf("ineligible checkout %d", w.Code)
			}
		})
	}
	_, p, mux := paymentFixture(t, false)
	for _, body := range []string{`{"amount":1}`, `{"student_id":"someone"}`, `{"tier":"other"}`, `{} {}`, strings.Repeat("x", 5000)} {
		if w := paymentRequest(mux, "/api/v1/payments/checkout", body, nil); w.Code != 400 {
			t.Fatal("invalid checkout accepted")
		}
	}
	p.config.StudentIdentity = func(*http.Request) (string, error) { return "", errors.New("missing session") }
	if w := paymentRequest(mux, "/api/v1/payments/checkout", `{}`, nil); w.Code != 401 {
		t.Fatal("missing auth accepted")
	}
}

func TestPaymentConcurrentWebhooks(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		t.Run(fmt.Sprintf("postgres=%v", postgres), func(t *testing.T) {
			d, _, mux := paymentFixture(t, postgres)
			ref1 := paymentCheckout(t, mux, "tier_1")
			ref2 := paymentCheckout(t, mux, "tier_1")
			var wg sync.WaitGroup
			for i := 0; i < 20; i++ {
				ref := ref1
				if i%2 == 0 {
					ref = ref2
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					w := paymentWebhook(mux, paymentEvent(ref, 500000))
					if w.Code != 200 {
						t.Errorf("concurrent webhook %d %s", w.Code, w.Body.String())
					}
				}()
			}
			wg.Wait()
			paymentAssertCount(t, d, "payment_success", 2)
			paymentAssertCount(t, d, "lovable_provisioning_requested", 1)
		})
	}
}

func TestPaymentAuditRollback(t *testing.T) {
	d, _, mux := paymentFixture(t, false)
	ref := paymentCheckout(t, mux, "tier_1")
	if _, err := d.Exec(`CREATE TRIGGER fail_payment_audit BEFORE INSERT ON activity_logs BEGIN SELECT RAISE(ABORT,'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if w := paymentWebhook(mux, paymentEvent(ref, 500000)); w.Code != 503 {
		t.Fatal("audit failure accepted")
	}
	var status, funnel string
	if err := d.QueryRow("SELECT status FROM payments WHERE reference=$1", ref).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow("SELECT funnel_tier FROM students WHERE id='s1'").Scan(&funnel); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || funnel != "free_week" {
		t.Fatal("partial payment commit")
	}
	if _, err := d.Exec("DROP TRIGGER fail_payment_audit"); err != nil {
		t.Fatal(err)
	}
	if w := paymentWebhook(mux, paymentEvent(ref, 500000)); w.Code != 200 {
		t.Fatal("retry failed")
	}
	paymentAssertCount(t, d, "lovable_provisioning_requested", 1)
}
func TestPaymentTier2ReadinessRevokedBeforeWebhook(t *testing.T) {
	d, _, mux := paymentFixture(t, false)
	if _, err := d.Exec("UPDATE students SET funnel_tier='tier_1_active',tier_2_ready=TRUE"); err != nil {
		t.Fatal(err)
	}
	ref := paymentCheckout(t, mux, "tier_2")
	if _, err := d.Exec("UPDATE students SET tier_2_ready=FALSE"); err != nil {
		t.Fatal(err)
	}
	if w := paymentWebhook(mux, paymentEvent(ref, 2500000)); w.Code != 409 {
		t.Fatal("revoked readiness bypassed")
	}
	paymentAssertCount(t, d, "tier_2_provisioning_requested", 0)
}
func TestPaymentProviderFailureAndConfiguration(t *testing.T) {
	d, p, mux := paymentFixture(t, false)
	p.config.Paystack, _ = services.NewPaystack("secret", &http.Client{Transport: paymentTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("provider secret") })})
	w := paymentRequest(mux, "/api/v1/payments/checkout", `{}`, nil)
	if w.Code != 502 || strings.Contains(w.Body.String(), "secret") {
		t.Fatal("provider error leaked/accepted")
	}
	var n int
	if err := d.QueryRow("SELECT COUNT(*) FROM payments WHERE status='pending'").Scan(&n); err != nil || n != 1 {
		t.Fatal("ambiguous provider request lost pending reference")
	}
	if _, err := NewPayment(PaymentConfig{}); err == nil {
		t.Fatal("incomplete config accepted")
	}
	p.config.AdminAuthorized = nil
	if w := paymentRequest(mux, "/api/v1/admin/toggle-tier-2", `{"student_id":"s1","tier_2_ready":true}`, nil); w.Code != 403 {
		t.Fatal("admin authorization defaults open")
	}
}
