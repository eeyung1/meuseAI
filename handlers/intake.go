package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/eeyung1/meuseAI/db"
	"github.com/eeyung1/meuseAI/services"
	assets "github.com/eeyung1/meuseAI/static"
)

const verificationLifetime = 24 * time.Hour

// Intake handles public signup and bearer-token verification/unsubscribe.
// Construct with NewIntake, then Register on the server's shared ServeMux.
type Intake struct {
	database *db.DB
	mailer   services.Mailer
	baseURL  string
	now      func() time.Time
}

func NewIntake(database *db.DB, mailer services.Mailer, publicURL string) (*Intake, error) {
	u, err := url.Parse(publicURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("public URL must be an absolute HTTP(S) URL without credentials, query or fragment")
	}
	if database == nil || database.DB == nil || mailer == nil {
		return nil, errors.New("database and mailer are required")
	}
	return &Intake{database: database, mailer: mailer, baseURL: strings.TrimRight(publicURL, "/"), now: time.Now}, nil
}

func (h *Intake) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/students/signup", h.Signup)
	mux.HandleFunc("POST /api/v1/students/resend-verification", h.ResendVerification)
	mux.HandleFunc("GET /api/v1/students/verify", h.Verify)
	mux.HandleFunc("GET /verify", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		http.ServeFileFS(w, r, assets.Assets, "verify.html")
	})
	mux.HandleFunc("GET /api/v1/students/unsubscribe", h.Unsubscribe)
	// Support RFC 8058 one-click requests as well as the required GET route.
	mux.HandleFunc("POST /api/v1/students/unsubscribe", h.Unsubscribe)
}

func intakeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func intakeError(w http.ResponseWriter, status int, message string) {
	intakeJSON(w, status, map[string]string{"error": message})
}
func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func intakeToken() (string, error) {
	var b [32]byte
	_, err := rand.Read(b[:])
	return hex.EncodeToString(b[:]), err
}
func intakeID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
func intakeAudit(ctx context.Context, tx *sql.Tx, id, event string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO activity_logs (student_id,event_type,details) VALUES ($1,$2,$3)", id, event, "{}")
	return err
}

func (h *Intake) Signup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		FullName string `json:"full_name"`
		Email    string `json:"email"`
		Consent  bool   `json:"consent"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		intakeError(w, 400, "invalid signup payload")
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		intakeError(w, 400, "expected one JSON object")
		return
	}
	input.FullName = strings.TrimSpace(input.FullName)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	addr, err := mail.ParseAddress(input.Email)
	if !input.Consent || input.FullName == "" || utf8.RuneCountInString(input.FullName) > 255 || len(input.Email) > 255 || err != nil || addr.Address != input.Email || strings.ContainsAny(input.Email, "\r\n") {
		intakeError(w, 400, "name, valid email and explicit consent are required")
		return
	}
	id, err := intakeID()
	if err != nil {
		intakeError(w, 500, "signup unavailable")
		return
	}
	verify, err := intakeToken()
	if err != nil {
		intakeError(w, 500, "signup unavailable")
		return
	}
	unsub, err := intakeToken()
	if err != nil {
		intakeError(w, 500, "signup unavailable")
		return
	}
	tx, err := h.database.BeginTx(r.Context(), nil)
	if err != nil {
		intakeError(w, 503, "signup unavailable")
		return
	}
	defer tx.Rollback()
	now := h.now().UTC()
	result, err := tx.ExecContext(r.Context(), `INSERT INTO students
 (id,full_name,email,verification_token,consent_timestamp,unsubscribe_token,created_at,updated_at)
 VALUES ($1,$2,$3,$4,$5,$6,$5,$5) ON CONFLICT DO NOTHING`, id, input.FullName, input.Email, tokenDigest(verify), now, tokenDigest(unsub))
	if err != nil {
		intakeError(w, 503, "signup unavailable")
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		intakeError(w, 503, "signup unavailable")
		return
	}
	if count == 0 {
		intakeJSON(w, 202, map[string]string{"message": "If eligible, check your email for verification."})
		return
	}
	if err = intakeAudit(r.Context(), tx, id, "intake"); err != nil {
		intakeError(w, 503, "signup unavailable")
		return
	}
	unsubscribeURL := h.baseURL + "/api/v1/students/unsubscribe?token=" + unsub
	message := services.Message{To: input.Email, Subject: "Verify your meuseAI email", Text: "Verify your email within 24 hours:\n" + h.baseURL + "/api/v1/students/verify?token=" + verify + "\n\nUnsubscribe:\n" + unsubscribeURL, UnsubscribeURL: unsubscribeURL}
	// Delivery failure rolls back intake, allowing signup to be retried. A durable
	// outbox is needed for guaranteed delivery across a crash after Send/commit.
	if err = h.mailer.Send(r.Context(), message); err != nil {
		intakeError(w, 503, "email delivery unavailable; retry signup")
		return
	}
	if err = tx.Commit(); err != nil {
		intakeError(w, 503, "signup unavailable; retry signup")
		return
	}
	intakeJSON(w, 202, map[string]string{"message": "If eligible, check your email for verification."})
}

func intakeBearer(r *http.Request) (string, bool) {
	values := r.URL.Query()["token"]
	if len(values) != 1 || len(values[0]) != 64 {
		return "", false
	}
	b, err := hex.DecodeString(values[0])
	return values[0], err == nil && len(b) == 32
}

func (h *Intake) Verify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	token, ok := intakeBearer(r)
	if !ok {
		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.Redirect(w, r, "/verify?error=invalid", http.StatusSeeOther)
			return
		}
		intakeError(w, 400, "invalid or expired token")
		return
	}
	h.transition(w, r, token, true)
}
func (h *Intake) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	token, ok := intakeBearer(r)
	if !ok {
		intakeError(w, 400, "invalid token")
		return
	}
	h.transition(w, r, token, false)
}

func (h *Intake) transition(w http.ResponseWriter, r *http.Request, token string, verify bool) {
	tx, err := h.database.BeginTx(r.Context(), nil)
	if err != nil {
		intakeError(w, 503, "request unavailable")
		return
	}
	defer tx.Rollback()
	now := h.now().UTC()
	var id string
	var query string
	var args []any
	if verify {
		query = `UPDATE students SET email_verified=TRUE,curriculum_started_at=$1,verification_token=NULL,current_stage='day_1',engagement_status='pending',updated_at=$1
   WHERE verification_token=$2 AND email_verified=FALSE AND unsubscribed_at IS NULL AND created_at>$3 RETURNING id`
		args = []any{now, tokenDigest(token), now.Add(-verificationLifetime)}
	} else {
		query = `UPDATE students SET unsubscribed_at=COALESCE(unsubscribed_at,$1),verification_token=NULL,engagement_status='unsubscribed',updated_at=$1
   WHERE unsubscribe_token=$2 AND unsubscribed_at IS NULL RETURNING id`
		args = []any{now, tokenDigest(token)}
	}
	err = tx.QueryRowContext(r.Context(), query, args...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		if !verify {
			// Repeated unsubscribe is successful without duplicate audit events.
			err = tx.QueryRowContext(r.Context(), "SELECT id FROM students WHERE unsubscribe_token=$1 AND unsubscribed_at IS NOT NULL", tokenDigest(token)).Scan(&id)
			if err == nil {
				intakeJSON(w, 200, map[string]string{"status": "unsubscribed"})
				return
			}
		}
		if errors.Is(err, sql.ErrNoRows) {
			if verify && strings.Contains(r.Header.Get("Accept"), "text/html") {
				http.Redirect(w, r, "/verify?error=expired", http.StatusSeeOther)
				return
			}
			intakeError(w, 400, "invalid or expired token")
			return
		}
	}
	if err != nil {
		intakeError(w, 503, "request unavailable")
		return
	}
	event, status := "unsubscribe", "unsubscribed"
	if verify {
		event, status = "verify", "verified"
	}
	if err = intakeAudit(r.Context(), tx, id, event); err != nil {
		intakeError(w, 503, "request unavailable")
		return
	}
	if err = tx.Commit(); err != nil {
		intakeError(w, 503, "request unavailable")
		return
	}
	if verify && strings.Contains(r.Header.Get("Accept"), "text/html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		http.ServeFileFS(w, r, assets.Assets, "verify.html")
		return
	}
	intakeJSON(w, 200, map[string]string{"status": status})
}

func (h *Intake) ResendVerification(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var input struct {
		Email string `json:"email"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&input); err != nil {
		intakeError(w, 400, "invalid payload")
		return
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	addr, err := mail.ParseAddress(email)
	if email == "" || len(email) > 255 || err != nil || addr.Address != email || strings.ContainsAny(email, "\r\n") {
		intakeError(w, 400, "valid email is required")
		return
	}
	tx, err := h.database.BeginTx(r.Context(), nil)
	if err != nil {
		intakeError(w, 503, "request unavailable")
		return
	}
	defer tx.Rollback()

	var id, name, unsubToken string
	var verified bool
	err = tx.QueryRowContext(r.Context(), "SELECT id, full_name, email_verified, unsubscribe_token FROM students WHERE LOWER(email) = $1 AND unsubscribed_at IS NULL", email).Scan(&id, &name, &verified, &unsubToken)
	if errors.Is(err, sql.ErrNoRows) {
		// Generic 202 response to prevent email harvesting
		intakeJSON(w, 202, map[string]string{"message": "If eligible, check your email for verification."})
		return
	}
	if err != nil {
		intakeError(w, 503, "request unavailable")
		return
	}

	if !verified {
		verify, err := intakeToken()
		if err != nil {
			intakeError(w, 503, "request unavailable")
			return
		}
		now := h.now().UTC()
		_, err = tx.ExecContext(r.Context(), "UPDATE students SET verification_token = $1, updated_at = $2 WHERE id = $3", tokenDigest(verify), now, id)
		if err != nil {
			intakeError(w, 503, "request unavailable")
			return
		}
		_ = intakeAudit(r.Context(), tx, id, "resend_verification")
		unsubscribeURL := h.baseURL + "/api/v1/students/unsubscribe"
		message := services.Message{
			To:             email,
			Subject:        "Verify your meuseAI email",
			Text:           "Verify your email within 24 hours:\n" + h.baseURL + "/api/v1/students/verify?token=" + verify + "\n\nUnsubscribe:\n" + unsubscribeURL,
			UnsubscribeURL: unsubscribeURL,
		}
		if err = h.mailer.Send(r.Context(), message); err != nil {
			intakeError(w, 503, "email delivery unavailable; retry signup")
			return
		}
		if err = tx.Commit(); err != nil {
			intakeError(w, 503, "request unavailable")
			return
		}
	} else {
		_ = tx.Commit()
		message := services.Message{
			To:      email,
			Subject: "Your meuseAI account is already verified",
			Text:    "Hi " + name + ",\n\nYour meuseAI account is already active! You can access your learning hub anytime here:\n" + h.baseURL + "/dashboard\n\nIf you need a new sign-in link, request a magic link from our home page:\n" + h.baseURL + "/login\n\nHappy learning,\nmeuseAI Team",
		}
		_ = h.mailer.Send(r.Context(), message)
	}

	intakeJSON(w, 202, map[string]string{"message": "If eligible, check your email for verification."})
}
