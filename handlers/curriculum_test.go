package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eeyung1/meuseAI/db"
)

func curriculumDatabase(t *testing.T, postgres bool) *db.DB {
	t.Helper()
	cfg := db.Config{Driver: "sqlite"}
	if postgres {
		cfg = db.Config{Driver: "postgres", DSN: os.Getenv("MEUSEAI_TEST_POSTGRES_DSN"), Schema: fmt.Sprintf("curriculum_%d", time.Now().UnixNano())}
		if cfg.DSN == "" {
			t.Skip("MEUSEAI_TEST_POSTGRES_DSN unavailable; PostgreSQL not run")
		}
	}
	database, err := db.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if postgres {
			if _, err := database.Exec("DROP SCHEMA " + cfg.Schema + " CASCADE"); err != nil {
				t.Error(err)
			}
		}
		database.Close()
	})
	return database
}

func curriculumStudent(t *testing.T, database *db.DB, id string, verified bool, stage, engagement string, now time.Time) {
	t.Helper()
	_, err := database.Exec(`INSERT INTO students
		(id,full_name,email,email_verified,consent_timestamp,unsubscribe_token,current_stage,engagement_status,updated_at)
		VALUES ($1,'Ada Learner',$2,$3,$4,$5,$6,$7,$4)`, id, id+"@example.test", verified, now, "unsub-"+id, stage, engagement)
	if err != nil {
		t.Fatal(err)
	}
}

func curriculumFixture(t *testing.T, postgres bool) (*db.DB, *Curriculum, *http.ServeMux) {
	t.Helper()
	database := curriculumDatabase(t, postgres)
	handler, err := NewCurriculum(database)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handler.Register(mux)
	return database, handler, mux
}

func curriculumRequest(handler http.Handler, id string, day int, submission string) *httptest.ResponseRecorder {
	payload, _ := json.Marshal(map[string]any{"student_id": id, "day": day, "submission": submission})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/curriculum/complete", strings.NewReader(string(payload)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestCurriculumJourney(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		t.Run(fmt.Sprintf("postgres=%v", postgres), func(t *testing.T) {
			database, handler, mux := curriculumFixture(t, postgres)
			now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
			handler.now = func() time.Time { return now }
			curriculumStudent(t, database, "ada", true, "day_1", "pending", now)
			expectedStages := []string{"day_2", "day_3", "day_4", "conversion_gate"}
			for day := 1; day <= 4; day++ {
				response := curriculumRequest(mux, "ada", day, fmt.Sprintf("  detailed prompt for day %d  ", day))
				if response.Code != http.StatusOK {
					t.Fatalf("day %d: %d %s", day, response.Code, response.Body.String())
				}
				var body struct {
					Stage      string `json:"current_stage"`
					XP         int    `json:"xp"`
					Award      int    `json:"xp_awarded"`
					StreakDays int    `json:"streak_days"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Stage != expectedStages[day-1] || body.XP != day*curriculumXPAward || body.Award != curriculumXPAward || body.StreakDays != day {
					t.Fatalf("day %d response: %+v", day, body)
				}
				now = now.Add(24 * time.Hour)
			}
			var stage, engagement string
			var xp, streak, progress, events int
			if err := database.QueryRow("SELECT current_stage,engagement_status,xp,streak_days FROM students WHERE id='ada'").Scan(&stage, &engagement, &xp, &streak); err != nil {
				t.Fatal(err)
			}
			if err := database.QueryRow("SELECT COUNT(*) FROM student_progress WHERE completed=TRUE AND submission_payload LIKE 'detailed prompt%'").Scan(&progress); err != nil {
				t.Fatal(err)
			}
			if err := database.QueryRow("SELECT COUNT(*) FROM activity_logs WHERE event_type='complete_day'").Scan(&events); err != nil {
				t.Fatal(err)
			}
			if stage != "conversion_gate" || engagement != "engaged" || xp != 1000 || streak != 4 || progress != 4 || events != 4 {
				t.Fatalf("final state: stage=%s engagement=%s xp=%d streak=%d progress=%d events=%d", stage, engagement, xp, streak, progress, events)
			}
		})
	}
}

func TestCurriculumValidationAndEligibility(t *testing.T) {
	database, handler, mux := curriculumFixture(t, false)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	handler.now = func() time.Time { return now }
	curriculumStudent(t, database, "ada", true, "day_1", "pending", now)
	curriculumStudent(t, database, "unverified", false, "day_1", "pending", now)
	curriculumStudent(t, database, "lapsed", true, "day_1", "lapsed", now)

	invalidBodies := []string{
		`{}`, `null`, `{"student_id":"ada","day":1}`, `{"student_id":"ada","day":0,"submission":"prompt"}`,
		`{"student_id":"ada","day":5,"submission":"prompt"}`, `{"student_id":"ada","day":1,"submission":"   "}`,
		`{"student_id":"ada","day":1,"submission":"prompt","extra":true}`, `{"student_id":"ada","day":1,"submission":"prompt"} {}`,
		`{"student_id":"ada id","day":1,"submission":"prompt"}`, `{"student_id":"ada","day":1,"submission":"` + strings.Repeat("x", maxCurriculumSubmission+1) + `"}`,
	}
	for _, body := range invalidBodies {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/curriculum/complete", strings.NewReader(body))
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid body returned %d: %s", response.Code, body)
		}
	}
	if response := curriculumRequest(mux, "missing", 1, "prompt"); response.Code != http.StatusNotFound {
		t.Fatalf("missing student: %d", response.Code)
	}
	if response := curriculumRequest(mux, "unverified", 1, "prompt"); response.Code != http.StatusForbidden {
		t.Fatalf("unverified student: %d", response.Code)
	}
	if response := curriculumRequest(mux, "lapsed", 1, "prompt"); response.Code != http.StatusConflict {
		t.Fatalf("lapsed student: %d", response.Code)
	}
	if response := curriculumRequest(mux, "ada", 2, "prompt"); response.Code != http.StatusConflict {
		t.Fatalf("locked day: %d", response.Code)
	}
	response := curriculumRequest(mux, "ada", 1, "valid prompt")
	if response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	if response = curriculumRequest(mux, "ada", 1, "replay"); response.Code != http.StatusConflict {
		t.Fatalf("completion replay: %d", response.Code)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/curriculum/complete", strings.NewReader(`{"student_id":"ada","day":2,"submission":"prompt"}`))
	request.Header.Set("X-Student-ID", "someone-else")
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("identity mismatch: %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/curriculum/complete", nil)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method guard: %d", response.Code)
	}
}

func TestCurriculumAuthenticatedContext(t *testing.T) {
	database, _, mux := curriculumFixture(t, false)
	curriculumStudent(t, database, "ada", true, "day_1", "pending", time.Now().UTC())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/curriculum/complete", strings.NewReader(`{"day":1,"submission":"context-authenticated prompt"}`))
	request = request.WithContext(WithCurriculumStudent(request.Context(), "ada"))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("context identity: %d %s", response.Code, response.Body.String())
	}
}

func TestCurriculumConcurrentCompletion(t *testing.T) {
	database, _, mux := curriculumFixture(t, false)
	curriculumStudent(t, database, "ada", true, "day_1", "pending", time.Now().UTC())
	const workers = 12
	codes := make(chan int, workers)
	var wait sync.WaitGroup
	for i := 0; i < workers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			codes <- curriculumRequest(mux, "ada", 1, "concurrent prompt").Code
		}()
	}
	wait.Wait()
	close(codes)
	success := 0
	for code := range codes {
		if code == http.StatusOK {
			success++
		} else if code != http.StatusConflict {
			t.Errorf("unexpected status %d", code)
		}
	}
	var xp, progress, audits int
	if err := database.QueryRow("SELECT xp FROM students WHERE id='ada'").Scan(&xp); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT COUNT(*) FROM student_progress WHERE student_id='ada' AND completed=TRUE").Scan(&progress); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT COUNT(*) FROM activity_logs WHERE student_id='ada' AND event_type='complete_day'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if success != 1 || xp != curriculumXPAward || progress != 1 || audits != 1 {
		t.Fatalf("success=%d xp=%d progress=%d audits=%d", success, xp, progress, audits)
	}
}

func TestCurriculumAuditRollback(t *testing.T) {
	database, _, mux := curriculumFixture(t, false)
	curriculumStudent(t, database, "ada", true, "day_1", "pending", time.Now().UTC())
	if _, err := database.Exec(`CREATE TRIGGER reject_curriculum_audit BEFORE INSERT ON activity_logs BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if response := curriculumRequest(mux, "ada", 1, "prompt"); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("audit failure: %d", response.Code)
	}
	var stage string
	var xp, progress int
	if err := database.QueryRow("SELECT current_stage,xp FROM students WHERE id='ada'").Scan(&stage, &xp); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT COUNT(*) FROM student_progress").Scan(&progress); err != nil {
		t.Fatal(err)
	}
	if stage != "day_1" || xp != 0 || progress != 0 {
		t.Fatalf("failed audit changed state: %s %d %d", stage, xp, progress)
	}
}

func TestCurriculumConfigurationAndDatabaseFailure(t *testing.T) {
	if _, err := NewCurriculum(nil); err == nil {
		t.Fatal("nil database accepted")
	}
	database, _, mux := curriculumFixture(t, false)
	curriculumStudent(t, database, "ada", true, "day_1", "pending", time.Now().UTC())
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if response := curriculumRequest(mux, "ada", 1, "prompt"); response.Code != http.StatusServiceUnavailable {
		t.Fatalf("closed database: %d", response.Code)
	}
}
