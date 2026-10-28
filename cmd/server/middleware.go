package server

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/eeyung1/meuseAI/handlers"
	"github.com/eeyung1/meuseAI/telemetry"
)

type rateEntry struct {
	since time.Time
	count int
}
type rateLimiter struct {
	mu      sync.Mutex
	entries map[string]rateEntry
}

func (l *rateLimiter) allow(key string, limit int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	entry := l.entries[key]
	if now.Sub(entry.since) >= time.Minute {
		entry = rateEntry{since: now}
	}
	if entry.count >= limit {
		return false
	}
	if len(l.entries) >= 10000 {
		for key, value := range l.entries {
			if now.Sub(value.since) >= time.Minute {
				delete(l.entries, key)
			}
		}
		if _, exists := l.entries[key]; !exists && len(l.entries) >= 10000 {
			return false
		}
	}
	entry.count++
	l.entries[key] = entry
	return true
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *responseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(p)
}
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *responseWriter) Flush() {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}
func (a *App) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(original http.ResponseWriter, r *http.Request) {
		w := &responseWriter{ResponseWriter: original}
		start := time.Now()
		defer func() {
			if recover() != nil {
				a.config.Logger.Print("request panic recovered")
				if w.status == 0 {
					http.Error(w, "internal server error", 500)
				}
			}
			pattern := r.Pattern
			if pattern == "" {
				pattern = "unmatched"
			}
			a.config.Logger.Printf("request method=%s route=%s status=%d duration_ms=%d", r.Method, pattern, w.status, time.Since(start).Milliseconds())
		}()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		if origin != "" && origin != a.origin {
			http.Error(w, "origin not allowed", 403)
			return
		}
		if origin == a.origin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		if r.Method == "OPTIONS" {
			if origin == "" {
				http.Error(w, "origin required", 403)
				return
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.WriteHeader(204)
			return
		}
		if r.URL.Path != "/api/v1/telemetry/stream" {
			ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
			defer cancel()
			r = r.WithContext(ctx)
		}
		protected := r.URL.Path == "/api/v1/curriculum/complete" || r.URL.Path == "/api/v1/payments/checkout" || r.URL.Path == "/api/v1/dashboard/me" || r.URL.Path == "/api/v1/telemetry/stream"
		student := ""
		if protected {
			var err error
			student, err = a.identity(r)
			if err != nil {
				http.Error(w, "authentication required", 401)
				return
			}
			if r.Method != "GET" && r.Method != "HEAD" && !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") && origin != a.origin {
				http.Error(w, "same-origin request required", 403)
				return
			}
			r.Header.Del("X-Student-ID")
			r = r.WithContext(handlers.WithCurriculumStudent(r.Context(), student))
		}
		limit := 120
		group := "api"
		if r.URL.Path == "/api/v1/students/signup" {
			limit = 5
			group = "signup"
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/v1/webhooks/paystack" {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}
			key := group + ":" + ip
			if student != "" {
				key = "student:" + student
			}
			if !a.limiter.allow(key, limit) {
				w.Header().Set("Retry-After", "60")
				http.Error(w, "too many requests", 429)
				return
			}
		}
		next.ServeHTTP(w, r)
		// Publish successful student writes directly: the polling bridge may start
		// after a transaction commits and intentionally begins at the latest cursor.
		if student != "" && r.Method == http.MethodPost && w.status == http.StatusOK {
			_ = a.Hub.PublishStudent(student, telemetry.Event{Type: "progress", Data: map[string]bool{"refresh": true}})
			_ = a.Hub.PublishCohort(telemetry.Event{Type: "rank", Data: map[string]bool{"refresh": true}})
		}
	})
}
