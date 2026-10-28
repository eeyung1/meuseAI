package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eeyung1/meuseAI/db"
	"github.com/eeyung1/meuseAI/telemetry"
)

func dashboardFixture(t *testing.T, postgres bool) (*db.DB, *Dashboard, *http.ServeMux) {
	t.Helper()
	cfg := db.Config{Driver: "sqlite"}
	if postgres {
		cfg = db.Config{Driver: "postgres", DSN: os.Getenv("MEUSEAI_TEST_POSTGRES_DSN"), Schema: fmt.Sprintf("dashboard_%d", time.Now().UnixNano())}
		if cfg.DSN == "" {
			t.Skip("MEUSEAI_TEST_POSTGRES_DSN unavailable; PostgreSQL not run")
		}
	}
	database, err := db.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	hub := telemetry.NewHub()
	t.Cleanup(func() {
		hub.Close()
		if postgres {
			if _, err := database.Exec("DROP SCHEMA " + cfg.Schema + " CASCADE"); err != nil {
				t.Error(err)
			}
		}
		database.Close()
	})
	for _, student := range []struct {
		id, name, email string
		xp              int
	}{{"a", "Ada Learner", "ada@example.com", 250}, {"b", "Secret Peer Name", "peer@example.com", 500}, {"c", "Another Peer", "other@example.com", 250}} {
		_, err = database.Exec("INSERT INTO students(id,full_name,email,email_verified,consent_timestamp,unsubscribe_token,current_stage,xp,streak_days) VALUES($1,$2,$3,TRUE,CURRENT_TIMESTAMP,$1,'day_2',$4,2)", student.id, student.name, student.email, student.xp)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = database.Exec("INSERT INTO student_progress(student_id,day_number,completed) VALUES('a',1,TRUE)"); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Exec("INSERT INTO activity_logs(student_id,event_type,details) VALUES('a','verify',$1),('b','payment_success',$1)", `{"token":"do-not-expose"}`); err != nil {
		t.Fatal(err)
	}
	dashboard, err := NewDashboard(database, hub, func(*http.Request) (string, error) { return "a", nil })
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	dashboard.Register(mux)
	return database, dashboard, mux
}
func dashboardGet(mux http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return w
}
func dashboardRead(t *testing.T, mux http.Handler) DashboardSnapshot {
	t.Helper()
	w := dashboardGet(mux, "/api/v1/dashboard/me")
	if w.Code != 200 {
		t.Fatalf("dashboard %d %s", w.Code, w.Body.String())
	}
	var result DashboardSnapshot
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func TestDashboardSnapshotAndPrivacy(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		t.Run(fmt.Sprintf("postgres=%v", postgres), func(t *testing.T) {
			_, _, mux := dashboardFixture(t, postgres)
			result := dashboardRead(t, mux)
			if result.Name != "Ada Learner" || result.XP != 250 || result.Streak != 2 || result.Rank != 2 {
				t.Fatalf("wrong profile %+v", result)
			}
			if len(result.Modules) != 5 || result.Modules[0].State != "completed" || result.Modules[1].State != "available" || result.Modules[2].State != "locked" || result.Modules[4].State != "locked" {
				t.Fatalf("module gates %+v", result.Modules)
			}
			if len(result.Activity) != 1 || result.Activity[0].Title != "Email verified" {
				t.Fatal("activity scope leaked")
			}
			if len(result.Leaderboard) != 3 || result.Leaderboard[0].XP != 500 || result.Leaderboard[0].Rank != 1 {
				t.Fatal("leaderboard order")
			}
			for _, peer := range result.Leaderboard {
				if peer.XP == 250 && peer.Rank != 2 {
					t.Fatal("tie rank mismatch")
				}
			}
			body := dashboardGet(mux, "/api/v1/dashboard/me").Body.String()
			for _, secret := range []string{"Secret Peer Name", "peer@example.com", "do-not-expose", "unsubscribe_token", "verification_token", "submission_payload"} {
				if strings.Contains(body, secret) {
					t.Fatalf("private data leaked: %s", secret)
				}
			}
			if dashboardGet(mux, "/api/v1/dashboard/me").Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private response cached")
			}
		})
	}
}
func TestDashboardModuleGates(t *testing.T) {
	for _, change := range []string{"UPDATE students SET email_verified=FALSE WHERE id='a'", "UPDATE students SET unsubscribed_at=CURRENT_TIMESTAMP WHERE id='a'", "UPDATE students SET engagement_status='lapsed' WHERE id='a'", "DELETE FROM student_progress WHERE student_id='a'"} {
		t.Run(change, func(t *testing.T) {
			database, _, mux := dashboardFixture(t, false)
			if _, err := database.Exec(change); err != nil {
				t.Fatal(err)
			}
			if dashboardRead(t, mux).Modules[1].State != "locked" {
				t.Fatal("module unlocked without prerequisites")
			}
		})
	}
	database, _, mux := dashboardFixture(t, false)
	for day := 2; day <= 4; day++ {
		if _, err := database.Exec("INSERT INTO student_progress(student_id,day_number,completed) VALUES('a',$1,TRUE)", day); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec("UPDATE students SET current_stage='conversion_gate' WHERE id='a'"); err != nil {
		t.Fatal(err)
	}
	if dashboardRead(t, mux).Modules[4].State != "available" {
		t.Fatal("Tier 1 not offered after capstone")
	}
	if _, err := database.Exec("UPDATE students SET funnel_tier='tier_1_active',xp=1000 WHERE id='a'"); err != nil {
		t.Fatal(err)
	}
	result := dashboardRead(t, mux)
	if result.Modules[4].State != "unlocked" || result.XP != 1000 {
		t.Fatal("paid access/profile stale")
	}
}
func TestDashboardAuthAndAssets(t *testing.T) {
	database, d, mux := dashboardFixture(t, false)
	for path, content := range map[string]string{"/dashboard": "Your learning hub", "/assets/dashboard.js": "EventSource", "/assets/dashboard.css": "#ffd60a"} {
		w := dashboardGet(mux, path)
		if w.Code != 200 || !strings.Contains(w.Body.String(), content) {
			t.Fatalf("asset %s %d", path, w.Code)
		}
	}
	page := dashboardGet(mux, "/dashboard")
	if !strings.Contains(page.Header().Get("Content-Security-Policy"), "script-src 'self'") || strings.Contains(page.Body.String(), "cdn.") {
		t.Fatal("unsafe/external assets")
	}
	if w := dashboardGet(mux, "/assets/../db/schema.sql"); w.Code == 200 {
		t.Fatal("source file exposed")
	}
	d.identity = func(*http.Request) (string, error) { return "", errors.New("secret error") }
	for _, path := range []string{"/api/v1/dashboard/me", "/api/v1/telemetry/stream"} {
		w := dashboardGet(mux, path)
		if w.Code != 401 || strings.Contains(w.Body.String(), "secret") {
			t.Fatal("anonymous request accepted/error leaked")
		}
	}
	d.identity = func(*http.Request) (string, error) { return "missing", nil }
	if dashboardGet(mux, "/api/v1/dashboard/me").Code != 401 {
		t.Fatal("deleted session accepted")
	}
	database.Close()
	if dashboardGet(mux, "/api/v1/dashboard/me").Code != 503 {
		t.Fatal("database error hidden")
	}
	if _, err := NewDashboard(nil, telemetry.NewHub(), d.identity); err == nil {
		t.Fatal("nil DB accepted")
	}
}

type dashboardPipeWriter struct {
	header http.Header
	writer *io.PipeWriter
}

func (w *dashboardPipeWriter) Header() http.Header         { return w.header }
func (w *dashboardPipeWriter) WriteHeader(int)             {}
func (w *dashboardPipeWriter) Write(p []byte) (int, error) { return w.writer.Write(p) }
func (w *dashboardPipeWriter) Flush()                      {}
func TestDashboardActivityBridge(t *testing.T) {
	database, d, mux := dashboardFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	req := httptest.NewRequest("GET", "/api/v1/telemetry/stream", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer writer.Close()
		mux.ServeHTTP(&dashboardPipeWriter{header: make(http.Header), writer: writer}, req)
	}()
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		if scanner.Text() == "" {
			break
		}
	}
	var cursor int64
	if err := database.QueryRow("SELECT MAX(id) FROM activity_logs").Scan(&cursor); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO activity_logs(student_id,event_type,details) VALUES('a','payment_success',$1),('b','verify',$1)", `{"private":"do-not-expose"}`); err != nil {
		t.Fatal(err)
	}
	next, err := d.pollTelemetry(ctx, cursor)
	if err != nil || next <= cursor {
		t.Fatalf("bridge cursor %d %v", next, err)
	}
	var data []string
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data: ") {
			data = append(data, scanner.Text())
			if len(data) == 2 {
				break
			}
		}
	}
	joined := strings.Join(data, " ")
	if len(data) != 2 || !strings.Contains(joined, "Payment confirmed") || strings.Contains(joined, "Email verified") || strings.Contains(joined, "private") {
		t.Fatalf("bridge audience/content %s", joined)
	}
	if _, err = d.pollTelemetry(ctx, next); err != nil {
		t.Fatal(err)
	}
	cancel()
	reader.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream did not stop")
	}
	canceled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	if err = d.RunTelemetry(canceled, time.Millisecond); !errors.Is(err, context.Canceled) {
		t.Fatalf("bridge cancellation %v", err)
	}
}
func TestDashboardEmptyActivityAndMethod(t *testing.T) {
	database, _, mux := dashboardFixture(t, false)
	if _, err := database.Exec("DELETE FROM activity_logs"); err != nil {
		t.Fatal(err)
	}
	result := dashboardRead(t, mux)
	if result.Activity == nil || len(result.Activity) != 0 {
		t.Fatal("activity must be empty array")
	}
	req := httptest.NewRequest("POST", "/api/v1/dashboard/me", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Fatal("method guard missing")
	}
}
