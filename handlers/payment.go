package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/eeyung1/meuseAI/db"
	"github.com/eeyung1/meuseAI/services"
)

// PaymentIdentity must resolve a verified server-side session, never a body or
// user-supplied student header. Server bootstrap supplies the auth adapter.
type PaymentIdentity func(*http.Request) (string, error)
type PaymentConfig struct {
	Database        *db.DB
	Paystack        *services.Paystack
	StudentIdentity PaymentIdentity
	AdminAuthorized func(*http.Request) bool
}
type Payment struct{ config PaymentConfig }

func NewPayment(config PaymentConfig) (*Payment, error) {
	if config.Database == nil || config.Database.DB == nil || config.Paystack == nil || config.StudentIdentity == nil {
		return nil, errors.New("payment requires database, Paystack and student authentication")
	}
	return &Payment{config: config}, nil
}
func (p *Payment) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/payments/checkout", p.Checkout)
	mux.HandleFunc("POST /api/v1/webhooks/paystack", p.Webhook)
	mux.HandleFunc("POST /api/v1/admin/toggle-tier-2", p.ToggleTier2)
}
func paymentJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func paymentError(w http.ResponseWriter, status int, message string) {
	paymentJSON(w, status, map[string]string{"error": message})
}
func paymentDecode(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return errors.New("expected one JSON object")
	}
	return nil
}
func paymentAudit(tx *sql.Tx, r *http.Request, id, event string, details any) error {
	raw, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(r.Context(), "INSERT INTO activity_logs(student_id,event_type,details) VALUES($1,$2,$3)", id, event, string(raw))
	return err
}

func (p *Payment) Checkout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	id, err := p.config.StudentIdentity(r)
	if err != nil || id == "" {
		paymentError(w, 401, "authentication required")
		return
	}
	var input struct {
		Tier string `json:"tier"`
	}
	if paymentDecode(w, r, &input) != nil {
		paymentError(w, 400, "invalid checkout payload")
		return
	}
	if input.Tier == "" {
		input.Tier = "tier_1"
	}
	amount, err := services.TierAmount(input.Tier)
	if err != nil {
		paymentError(w, 400, "invalid tier")
		return
	}
	tx, err := p.config.Database.BeginTx(r.Context(), nil)
	if err != nil {
		paymentError(w, 503, "checkout unavailable")
		return
	}
	defer tx.Rollback()
	// Lock the student before checking eligibility, using SQL shared by both DBs.
	_, err = tx.ExecContext(r.Context(), "UPDATE students SET updated_at=updated_at WHERE id=$1", id)
	if err != nil {
		paymentError(w, 503, "checkout unavailable")
		return
	}
	var email, stage, funnel string
	var verified, ready bool
	var unsubscribed sql.NullTime
	err = tx.QueryRowContext(r.Context(), "SELECT email,email_verified,current_stage,funnel_tier,tier_2_ready,unsubscribed_at FROM students WHERE id=$1", id).Scan(&email, &verified, &stage, &funnel, &ready, &unsubscribed)
	if errors.Is(err, sql.ErrNoRows) {
		paymentError(w, 401, "authentication required")
		return
	}
	if err != nil {
		paymentError(w, 503, "checkout unavailable")
		return
	}
	eligible := verified && !unsubscribed.Valid
	if input.Tier == "tier_1" {
		var complete bool
		err = tx.QueryRowContext(r.Context(), "SELECT completed FROM student_progress WHERE student_id=$1 AND day_number=4", id).Scan(&complete)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			paymentError(w, 503, "checkout unavailable")
			return
		}
		eligible = eligible && complete && stage == "conversion_gate" && funnel == "free_week"
		if eligible {
			var studentXP int
			_ = tx.QueryRowContext(r.Context(), "SELECT xp FROM students WHERE id=$1", id).Scan(&studentXP)
			discountPercent := (studentXP / 2000) * 10
			if discountPercent > 50 {
				discountPercent = 50
			}
			if discountPercent > 0 {
				discountKobo := (amount * int64(discountPercent)) / 100
				amount = amount - discountKobo
			}
		}
	} else {
		eligible = eligible && ready && funnel == "tier_1_active"
	}
	if !eligible {
		paymentError(w, 403, "tier is not available for this student")
		return
	}
	var random [24]byte
	if _, err = rand.Read(random[:]); err != nil {
		paymentError(w, 503, "checkout unavailable")
		return
	}
	reference := "meuse-" + hex.EncodeToString(random[:])
	_, err = tx.ExecContext(r.Context(), "INSERT INTO payments(reference,student_id,amount_kobo,currency,tier_unlocked,status) VALUES($1,$2,$3,$4,$5,'pending')", reference, id, amount, services.PaystackCurrency, input.Tier)
	if err != nil {
		paymentError(w, 503, "checkout unavailable")
		return
	}
	if err = tx.Commit(); err != nil {
		paymentError(w, 503, "checkout unavailable")
		return
	}
	// Persist the expected reference/owner/price before calling the provider so a
	// fast webhook can be validated. Ambiguous provider failures retain pending.
	result, err := p.config.Paystack.Initialize(r.Context(), services.CheckoutRequest{Email: email, Amount: amount, Currency: services.PaystackCurrency, Reference: reference, Metadata: map[string]string{"student_id": id, "tier": input.Tier}})
	if err != nil {
		paymentError(w, 502, "checkout provider unavailable")
		return
	}
	paymentJSON(w, 200, result)
}

func (p *Payment) Webhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		paymentError(w, 413, "webhook too large or unreadable")
		return
	}
	signatures := r.Header.Values("X-Paystack-Signature")
	if len(signatures) != 1 || !p.config.Paystack.ValidSignature(body, signatures[0]) {
		paymentError(w, 401, "invalid signature")
		return
	}
	var event struct {
		Event string `json:"event"`
		Data  struct {
			Reference string `json:"reference"`
			Status    string `json:"status"`
			Amount    int64  `json:"amount"`
			Currency  string `json:"currency"`
			Customer  struct {
				Email string `json:"email"`
			} `json:"customer"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &event) != nil || event.Event == "" {
		paymentError(w, 400, "invalid webhook")
		return
	}
	if event.Event != "charge.success" {
		paymentJSON(w, 200, map[string]string{"status": "ignored"})
		return
	}
	data := event.Data
	if data.Status != "success" || data.Reference == "" || len(data.Reference) > 128 {
		paymentError(w, 400, "invalid successful charge")
		return
	}
	tx, err := p.config.Database.BeginTx(r.Context(), nil)
	if err != nil {
		paymentError(w, 503, "payment unavailable")
		return
	}
	defer tx.Rollback()
	var id, tier, status, email string
	var amount int64
	var currency string
	err = tx.QueryRowContext(r.Context(), "SELECT student_id,tier_unlocked,status,amount_kobo,currency FROM payments WHERE reference=$1", data.Reference).Scan(&id, &tier, &status, &amount, &currency)
	if errors.Is(err, sql.ErrNoRows) {
		paymentError(w, 400, "unknown payment reference")
		return
	}
	if err != nil {
		paymentError(w, 503, "payment unavailable")
		return
	}
	_, err = tx.ExecContext(r.Context(), "UPDATE students SET updated_at=updated_at WHERE id=$1", id)
	if err != nil {
		paymentError(w, 503, "payment unavailable")
		return
	}
	var ready bool
	var funnel string
	if err = tx.QueryRowContext(r.Context(), "SELECT email,tier_2_ready,funnel_tier FROM students WHERE id=$1", id).Scan(&email, &ready, &funnel); err != nil {
		paymentError(w, 503, "payment unavailable")
		return
	}
	expected, err := services.TierAmount(tier)
	if err != nil || amount != expected || amount != data.Amount || currency != services.PaystackCurrency || data.Currency != currency || !strings.EqualFold(email, data.Customer.Email) {
		paymentError(w, 400, "payment does not match checkout")
		return
	}
	// Read status again under the student lock: concurrent deliveries may have
	// observed the same pending row before one committed its state transition.
	if err = tx.QueryRowContext(r.Context(), "SELECT status FROM payments WHERE reference=$1", data.Reference).Scan(&status); err != nil {
		paymentError(w, 503, "payment unavailable")
		return
	}
	if status == "success" {
		paymentJSON(w, 200, map[string]string{"status": "already processed"})
		return
	}
	if status != "pending" || (tier == "tier_2" && (!ready || (funnel != "tier_1_active" && funnel != "tier_2_active"))) {
		paymentError(w, 409, "payment cannot unlock this tier")
		return
	}
	now := time.Now().UTC()
	_, err = tx.ExecContext(r.Context(), "UPDATE payments SET status='success',paid_at=$1,paystack_signature=$2,raw_payload=$3 WHERE reference=$4", now, signatures[0], string(body), data.Reference)
	if err != nil {
		paymentError(w, 503, "payment unavailable")
		return
	}
	target := "tier_1_active"
	provisionEvent := "lovable_provisioning_requested"
	shouldProvision := funnel != "tier_1_active" && funnel != "tier_2_active"
	if tier == "tier_2" {
		target = "tier_2_active"
		provisionEvent = "tier_2_provisioning_requested"
		shouldProvision = funnel != "tier_2_active"
	}
	if shouldProvision {
		_, err = tx.ExecContext(r.Context(), "UPDATE students SET funnel_tier=$1,payment_status='paid',updated_at=$2 WHERE id=$3", target, now, id)
		if err != nil {
			paymentError(w, 503, "payment unavailable")
			return
		}
		if err = paymentAudit(tx, r, id, provisionEvent, map[string]string{"reference": data.Reference, "tier": tier}); err != nil {
			paymentError(w, 503, "payment unavailable")
			return
		}
	}
	if err = paymentAudit(tx, r, id, "payment_success", map[string]string{"reference": data.Reference, "tier": tier}); err != nil {
		paymentError(w, 503, "payment unavailable")
		return
	}
	if err = tx.Commit(); err != nil {
		paymentError(w, 503, "payment unavailable")
		return
	}
	paymentJSON(w, 200, map[string]string{"status": "processed"})
}

func (p *Payment) ToggleTier2(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	if p.config.AdminAuthorized == nil || !p.config.AdminAuthorized(r) {
		paymentError(w, 403, "admin authorization required")
		return
	}
	var input struct {
		StudentID string `json:"student_id"`
		Ready     *bool  `json:"tier_2_ready"`
	}
	if paymentDecode(w, r, &input) != nil || input.StudentID == "" || input.Ready == nil {
		paymentError(w, 400, "student_id and tier_2_ready are required")
		return
	}
	tx, err := p.config.Database.BeginTx(r.Context(), nil)
	if err != nil {
		paymentError(w, 503, "admin update unavailable")
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), "UPDATE students SET tier_2_ready=$1,updated_at=$2 WHERE id=$3", *input.Ready, time.Now().UTC(), input.StudentID)
	if err != nil {
		paymentError(w, 503, "admin update unavailable")
		return
	}
	n, err := result.RowsAffected()
	if err != nil {
		paymentError(w, 503, "admin update unavailable")
		return
	}
	if n == 0 {
		paymentError(w, 404, "student not found")
		return
	}
	if err = paymentAudit(tx, r, input.StudentID, "tier_2_readiness_changed", map[string]bool{"tier_2_ready": *input.Ready}); err != nil {
		paymentError(w, 503, "admin update unavailable")
		return
	}
	if err = tx.Commit(); err != nil {
		paymentError(w, 503, "admin update unavailable")
		return
	}
	paymentJSON(w, 200, map[string]bool{"tier_2_ready": *input.Ready})
}
