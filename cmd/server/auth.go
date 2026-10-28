package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
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
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return a.config.AdminToken != "" && r.Header.Get("Authorization") == "Bearer "+token && subtle.ConstantTimeCompare([]byte(token), []byte(a.config.AdminToken)) == 1
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
