package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/eeyung1/meuseAI/services"
	assets "github.com/eeyung1/meuseAI/static"
)

const sessionCookie = "meuse_session"

type claims struct {
	Student string `json:"sub"`
	Purpose string `json:"purpose"`
	Expires int64  `json:"exp"`
}

func (a *App) sign(student, purpose string, lifetime time.Duration) (string, error) {
	raw, err := json.Marshal(claims{student, purpose, time.Now().Add(lifetime).Unix()})
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, a.config.SessionKey)
	mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (a *App) validate(token, purpose string) (string, error) {
	if len(token) > 1024 {
		return "", errors.New("invalid token")
	}
	payload, signature, ok := strings.Cut(token, ".")
	if !ok {
		return "", errors.New("invalid token")
	}
	sig, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return "", errors.New("invalid token")
	}
	mac := hmac.New(sha256.New, a.config.SessionKey)
	mac.Write([]byte(payload))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return "", errors.New("invalid token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", errors.New("invalid token")
	}
	var c claims
	if json.Unmarshal(raw, &c) != nil || c.Student == "" || len(c.Student) > 64 || c.Purpose != purpose || c.Expires <= time.Now().Unix() {
		return "", errors.New("invalid token")
	}
	return c.Student, nil
}
func (a *App) identity(r *http.Request) (string, error) {
	header := r.Header.Get("Authorization")
	if header != "" {
		if !strings.HasPrefix(header, "Bearer ") {
			return "", errors.New("authentication required")
		}
		return a.validate(strings.TrimPrefix(header, "Bearer "), "session")
	}
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", errors.New("authentication required")
	}
	return a.validate(cookie.Value, "session")
}
func (a *App) admin(r *http.Request) bool {
	token := ""
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token = strings.TrimPrefix(authHeader, "Bearer ")
	}
	if token == "" {
		token = r.URL.Query().Get("admin_token")
	}
	if a.config.AdminToken == "" || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(a.config.AdminToken)) == 1
}

func (a *App) adminHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.ServeFileFS(w, r, assets.Assets, "admin.html")
}

type verificationWriter struct {
	http.ResponseWriter
	app     *App
	student string
	wrote   bool
}

func (w *verificationWriter) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.wrote = true
	if status == 200 && w.student != "" {
		token, err := w.app.sign(w.student, "session", 7*24*time.Hour)
		if err == nil {
			http.SetCookie(w.ResponseWriter, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", MaxAge: 604800, HttpOnly: true, Secure: w.app.secure, SameSite: http.SameSiteLaxMode})
		}
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *verificationWriter) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(p)
}
func (a *App) verifySession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/api/v1/students/verify" {
			token := r.URL.Query().Get("token")
			var student string
			if len(token) == 64 {
				digest := sha256.Sum256([]byte(token))
				_ = a.Database.QueryRowContext(r.Context(), "SELECT id FROM students WHERE verification_token=$1", fmtDigest(digest)).Scan(&student)
			}
			next.ServeHTTP(&verificationWriter{ResponseWriter: w, app: a, student: student}, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func fmtDigest(sum [32]byte) string {
	const chars = "0123456789abcdef"
	var result [64]byte
	for i, b := range sum {
		result[i*2] = chars[b>>4]
		result[i*2+1] = chars[b&15]
	}
	return string(result[:])
}

func (a *App) rootHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := a.identity(r); err == nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.ServeFileFS(w, r, assets.Assets, "landing.html")
}

func (a *App) loginHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := a.identity(r); err == nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.ServeFileFS(w, r, assets.Assets, "landing.html")
}

func (a *App) shareHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.ServeFileFS(w, r, assets.Assets, "share.html")
}

func (a *App) sendMagicLink(w http.ResponseWriter, r *http.Request) {
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
		writeJSON(w, 400, map[string]string{"error": "invalid payload"})
		return
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	addr, err := mail.ParseAddress(email)
	if email == "" || len(email) > 255 || err != nil || addr.Address != email || strings.ContainsAny(email, "\r\n") {
		writeJSON(w, 400, map[string]string{"error": "valid email is required"})
		return
	}

	var id, name string
	var verified bool
	err = a.Database.QueryRowContext(r.Context(), "SELECT id, full_name, email_verified FROM students WHERE LOWER(email) = $1 AND unsubscribed_at IS NULL", email).Scan(&id, &name, &verified)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, 202, map[string]string{"message": "If an active account exists, a sign-in link has been sent to your email."})
		return
	}
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "service unavailable"})
		return
	}

	if verified {
		token, err := a.sign(id, "magic_link", 1*time.Hour)
		if err == nil {
			link := a.origin + "/api/v1/students/magic-login?token=" + url.QueryEscape(token)
			message := services.Message{
				To:      email,
				Subject: "Your meuseAI Sign-In Link",
				Text:    "Hi " + name + ",\n\nClick the link below to sign in to your meuseAI learning hub:\n" + link + "\n\nThis link is valid for 1 hour.\n\nHappy learning,\nmeuseAI Team",
			}
			_ = a.config.Mailer.Send(r.Context(), message)
		}
	} else {
		var b [32]byte
		_, _ = rand.Read(b[:])
		verifyToken := hex.EncodeToString(b[:])
		sum := sha256.Sum256([]byte(verifyToken))
		digest := hex.EncodeToString(sum[:])
		now := time.Now().UTC()
		_, _ = a.Database.ExecContext(r.Context(), "UPDATE students SET verification_token = $1, updated_at = $2 WHERE id = $3", digest, now, id)
		link := a.origin + "/api/v1/students/verify?token=" + verifyToken
		message := services.Message{
			To:      email,
			Subject: "Verify your meuseAI email",
			Text:    "Hi " + name + ",\n\nPlease verify your email to access your learning hub:\n" + link + "\n\nThis link is valid for 24 hours.\n\nmeuseAI Team",
		}
		_ = a.config.Mailer.Send(r.Context(), message)
	}

	writeJSON(w, 202, map[string]string{"message": "If an active account exists, a sign-in link has been sent to your email."})
}

func (a *App) magicLogin(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	studentID, err := a.validate(token, "magic_link")
	if err != nil {
		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.Redirect(w, r, "/login?error=expired", http.StatusSeeOther)
			return
		}
		writeJSON(w, 400, map[string]string{"error": "invalid or expired sign-in link"})
		return
	}

	sessionToken, err := a.sign(studentID, "session", 7*24*time.Hour)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "session signing failed"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    sessionToken,
		Path:     "/",
		MaxAge:   604800,
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})

	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "authenticated", "student_id": studentID})
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
	})
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "logged_out"})
}
