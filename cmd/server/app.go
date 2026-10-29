// Package server assembles the student journey and owns application resources.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/eeyung1/meuseAI/db"
	"github.com/eeyung1/meuseAI/engine"
	"github.com/eeyung1/meuseAI/handlers"
	"github.com/eeyung1/meuseAI/services"
	"github.com/eeyung1/meuseAI/telemetry"
)

type Config struct {
	Database                                           db.Config
	PublicURL                                          string
	SessionKey                                         []byte
	AdminToken                                         string
	PaystackSecret                                     string
	HTTPClient                                         *http.Client
	Mailer                                             services.Mailer
	Logger                                             *log.Logger
	Address                                            string
	DripInterval, TelemetryInterval, ProvisionInterval time.Duration
	LovableURL, OnboardingURL                          string
}
type App struct {
	config          Config
	Database        *db.DB
	Handler         http.Handler
	Hub             *telemetry.Hub
	drip            *engine.Drip
	dashboard       *handlers.Dashboard
	origin          string
	secure          bool
	cancel          context.CancelFunc
	workers         sync.WaitGroup
	mu              sync.Mutex
	started, closed bool
	closeOnce       sync.Once
	closeErr        error
	limiter         rateLimiter
}

func New(ctx context.Context, config Config) (*App, error) {
	u, err := url.Parse(config.PublicURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, errors.New("public URL must be an HTTP(S) origin")
	}
	if len(config.SessionKey) < 32 || len(config.AdminToken) < 32 || config.Mailer == nil {
		return nil, errors.New("session key, admin token (at least 32 bytes) and mailer are required")
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return nil, errors.New("public HTTP is allowed only on loopback")
	}
	for _, link := range []string{config.LovableURL, config.OnboardingURL} {
		if link != "" {
			v, e := url.Parse(link)
			if e != nil || v.Scheme != "https" || v.Host == "" || v.User != nil {
				return nil, errors.New("provisioning links require HTTPS")
			}
		}
	}
	if config.Logger == nil {
		config.Logger = log.New(io.Discard, "", 0)
	}
	config.SessionKey = append([]byte(nil), config.SessionKey...)
	if config.DripInterval <= 0 {
		config.DripInterval = time.Minute
	}
	if config.TelemetryInterval <= 0 {
		config.TelemetryInterval = time.Second
	}
	if config.ProvisionInterval <= 0 {
		config.ProvisionInterval = 30 * time.Second
	}
	if config.Address == "" {
		config.Address = "127.0.0.1:8080"
	}
	database, err := db.Open(ctx, config.Database)
	if err != nil {
		return nil, err
	}
	a := &App{config: config, Database: database, Hub: telemetry.NewHub(), origin: u.Scheme + "://" + u.Host, secure: u.Scheme == "https", limiter: rateLimiter{entries: make(map[string]rateEntry)}}
	ok := false
	defer func() {
		if !ok {
			database.Close()
			a.Hub.Close()
		}
	}()
	client, err := services.NewPaystack(config.PaystackSecret, config.HTTPClient)
	if err != nil {
		return nil, err
	}
	intake, err := handlers.NewIntake(database, config.Mailer, a.origin)
	if err != nil {
		return nil, err
	}
	curriculum, err := handlers.NewCurriculum(database)
	if err != nil {
		return nil, err
	}
	payment, err := handlers.NewPayment(handlers.PaymentConfig{Database: database, Paystack: client, StudentIdentity: a.identity, AdminAuthorized: a.admin})
	if err != nil {
		return nil, err
	}
	a.dashboard, err = handlers.NewDashboard(database, a.Hub, a.identity)
	if err != nil {
		return nil, err
	}
	a.drip, err = engine.NewDrip(database, engine.DispatchFunc(a.dispatch))
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	intake.Register(mux)
	curriculum.Register(mux)
	payment.Register(mux)
	a.dashboard.Register(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if a.Database.PingContext(ctx) != nil {
			http.Error(w, "unavailable", 503)
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /{$}", a.rootHandler)
	mux.HandleFunc("GET /signup", a.rootHandler)
	mux.HandleFunc("GET /login", a.loginHandler)
	mux.HandleFunc("GET /share", a.shareHandler)
	mux.HandleFunc("POST /api/v1/students/magic-link", a.sendMagicLink)
	mux.HandleFunc("GET /api/v1/students/magic-login", a.magicLogin)
	mux.HandleFunc("POST /api/v1/students/logout", a.logout)
	mux.HandleFunc("GET /logout", a.logout)
	mux.HandleFunc("GET /api/v1/email/unsubscribe", a.unsubscribe)
	mux.HandleFunc("POST /api/v1/email/unsubscribe", a.unsubscribe)
	a.Handler = a.middleware(a.verifySession(mux))
	ok = true
	return a, nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// Start launches exactly one set of supervised background workers.
func (a *App) Start(parent context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return errors.New("application closed")
	}
	if a.started {
		return errors.New("workers already started")
	}
	ctx, cancel := context.WithCancel(parent)
	a.cancel = cancel
	a.started = true
	a.workers.Add(3)
	go func() {
		defer a.workers.Done()
		for ctx.Err() == nil {
			if err := a.dashboard.RunTelemetry(ctx, a.config.TelemetryInterval); err != nil && ctx.Err() == nil {
				a.config.Logger.Print("telemetry worker failed; retrying")
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(a.config.TelemetryInterval):
			}
		}
	}()
	go a.periodic(ctx, a.config.DripInterval, "drip", func(ctx context.Context) error { _, err := a.drip.Evaluate(ctx); return err })
	go a.periodic(ctx, a.config.ProvisionInterval, "provisioning", a.ProvisionOnce)
	return nil
}
func (a *App) periodic(ctx context.Context, interval time.Duration, name string, fn func(context.Context) error) {
	defer a.workers.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := fn(ctx); err != nil && ctx.Err() == nil {
			a.config.Logger.Printf("%s worker failed; retrying", name)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Close cancels workers and streams, waits for delivery transactions, then closes
// the owned DB. Serve also gracefully drains active HTTP requests before Close.
func (a *App) Close() error {
	a.closeOnce.Do(func() {
		a.mu.Lock()
		a.closed = true
		if a.cancel != nil {
			a.cancel()
		}
		a.Hub.Close()
		a.mu.Unlock()
		a.workers.Wait()
		a.closeErr = a.Database.Close()
	})
	return a.closeErr
}
func (a *App) Serve(ctx context.Context) error {
	if err := a.Start(ctx); err != nil {
		return err
	}
	defer a.Close()
	srv := &http.Server{Addr: a.config.Address, Handler: a.Handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		a.mu.Lock()
		a.cancel()
		a.Hub.Close()
		a.mu.Unlock()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := srv.Shutdown(shutdown)
		if err != nil {
			_ = srv.Close()
		}
		<-done
		return err
	}
}
func (a *App) unsubscribe(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()["token"]
	if len(values) != 1 {
		http.Error(w, "invalid token", 400)
		return
	}
	id, err := a.validate(values[0], "unsubscribe")
	if err != nil {
		http.Error(w, "invalid token", 400)
		return
	}
	tx, err := a.Database.BeginTx(r.Context(), nil)
	if err != nil {
		http.Error(w, "unsubscribe unavailable", 503)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), "UPDATE students SET unsubscribed_at=$1,engagement_status='unsubscribed',verification_token=NULL,updated_at=$1 WHERE id=$2 AND unsubscribed_at IS NULL", time.Now().UTC(), id)
	if err != nil {
		http.Error(w, "unsubscribe unavailable", 503)
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "unsubscribe unavailable", 503)
		return
	}
	if count > 0 {
		_, err = tx.ExecContext(r.Context(), "INSERT INTO activity_logs(student_id,event_type,details) VALUES($1,'unsubscribe',$2)", id, "{}")
		if err != nil {
			http.Error(w, "unsubscribe unavailable", 503)
			return
		}
	}
	if tx.Commit() != nil {
		http.Error(w, "unsubscribe unavailable", 503)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "unsubscribed"})
}
func (a *App) dispatch(ctx context.Context, action engine.Action) error {
	titles := []string{"Prompt foundations", "Atmosphere and lighting", "Reference images and consistency", "Conversational editing and capstone"}
	exercises := []string{"Create a prompt using a subject, style and setting. Try it in Gemini and describe the result.", "Explore golden-hour lighting and dramatic shadows. Share the prompt and what changed.", "Use a reference image to keep a character consistent across two scenes. Share your prompt and reflection.", "Improve an image through conversational feedback. Submit your final prompt and what you learned."}
	if action.Day < 1 || action.Day > 4 {
		return errors.New("invalid curriculum day")
	}
	token, err := a.sign(action.StudentID, "unsubscribe", 365*24*time.Hour)
	if err != nil {
		return err
	}
	unsubscribe := a.origin + "/api/v1/email/unsubscribe?token=" + url.QueryEscape(token)
	subject := titles[action.Day-1]
	text := exercises[action.Day-1]
	if action.Kind == engine.SendNudge {
		subject = "Ready for your next creative step?"
		text = "Your Day " + string(rune('0'+action.Day)) + " exercise is waiting. Pick up where you left off when you are ready."
	}
	return a.config.Mailer.Send(ctx, services.Message{To: action.Email, Subject: subject, Text: text + "\n\nOpen your learning hub: " + a.origin + "/dashboard\n\nUnsubscribe: " + unsubscribe, UnsubscribeURL: unsubscribe})
}
