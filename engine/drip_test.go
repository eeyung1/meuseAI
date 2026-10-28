package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eeyung1/meuseAI/db"
)

type captureDispatcher struct {
	mu      sync.Mutex
	actions []Action
	fail    error
}

func (d *captureDispatcher) Dispatch(ctx context.Context, action Action) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.fail != nil {
		return d.fail
	}
	d.actions = append(d.actions, action)
	return nil
}

func (d *captureDispatcher) Actions() []Action {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]Action(nil), d.actions...)
}

func dripDatabase(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(context.Background(), db.Config{Driver: "sqlite"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func dripStudent(t *testing.T, database *db.DB, id string, verified bool, stage, engagement string, now time.Time) {
	t.Helper()
	_, err := database.Exec(`INSERT INTO students
		(id,full_name,email,email_verified,consent_timestamp,unsubscribe_token,current_stage,engagement_status,updated_at)
		VALUES ($1,'Ada Learner',$2,$3,$4,$5,$6,$7,$4)`, id, id+"@example.test", verified, now, "unsub-"+id, stage, engagement)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDripNudgeAndLapseLifecycle(t *testing.T) {
	database := dripDatabase(t)
	dispatcher := &captureDispatcher{}
	evaluator, err := NewDrip(database, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	evaluator.now = func() time.Time { return now }
	dripStudent(t, database, "ada", true, "day_1", "pending", now.Add(-time.Hour))

	result, err := evaluator.Evaluate(context.Background())
	if err != nil || result != (Evaluation{Delivered: 1}) {
		t.Fatalf("delivery result=%+v err=%v", result, err)
	}
	actions := dispatcher.Actions()
	if len(actions) != 1 || actions[0].Kind != DeliverDay || actions[0].Day != 1 || actions[0].StudentID != "ada" {
		t.Fatalf("delivery actions: %+v", actions)
	}
	var completed bool
	var lastNudge *time.Time
	if err := database.QueryRow("SELECT completed,last_nudge_at FROM student_progress WHERE student_id='ada' AND day_number=1").Scan(&completed, &lastNudge); err != nil {
		t.Fatal(err)
	}
	if completed || lastNudge != nil {
		t.Fatal("delivery incorrectly completed or nudged progress")
	}

	now = now.Add(inactivityWindow)
	result, err = evaluator.Evaluate(context.Background())
	if err != nil || result != (Evaluation{Nudged: 1}) {
		t.Fatalf("nudge result=%+v err=%v", result, err)
	}
	actions = dispatcher.Actions()
	if len(actions) != 2 || actions[1].Kind != SendNudge || actions[1].Day != 1 {
		t.Fatalf("nudge actions: %+v", actions)
	}

	now = now.Add(inactivityWindow)
	result, err = evaluator.Evaluate(context.Background())
	if err != nil || result != (Evaluation{Lapsed: 1}) {
		t.Fatalf("lapse result=%+v err=%v", result, err)
	}
	var status string
	if err := database.QueryRow("SELECT engagement_status FROM students WHERE id='ada'").Scan(&status); err != nil {
		t.Fatal(err)
	}
	var audits int
	if err := database.QueryRow("SELECT COUNT(*) FROM activity_logs WHERE student_id='ada'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if status != "lapsed" || audits != 3 || len(dispatcher.Actions()) != 2 {
		t.Fatalf("status=%s audits=%d actions=%+v", status, audits, dispatcher.Actions())
	}
	if result, err = evaluator.Evaluate(context.Background()); err != nil || result != (Evaluation{}) {
		t.Fatalf("terminal reevaluation result=%+v err=%v", result, err)
	}
}

func TestDripDeliversCurrentUnlockedDay(t *testing.T) {
	database := dripDatabase(t)
	dispatcher := &captureDispatcher{}
	evaluator, err := NewDrip(database, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	evaluator.now = func() time.Time { return now }
	dripStudent(t, database, "day-three", true, "day_3", "engaged", now)
	dripStudent(t, database, "unverified", false, "day_1", "pending", now)
	dripStudent(t, database, "converted", true, "conversion_gate", "engaged", now)
	dripStudent(t, database, "lapsed", true, "day_2", "lapsed", now)
	result, err := evaluator.Evaluate(context.Background())
	if err != nil || result != (Evaluation{Delivered: 1}) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	actions := dispatcher.Actions()
	if len(actions) != 1 || actions[0].StudentID != "day-three" || actions[0].Day != 3 {
		t.Fatalf("actions: %+v", actions)
	}
}

func TestDripRespectsRecentActivity(t *testing.T) {
	database := dripDatabase(t)
	dispatcher := &captureDispatcher{}
	evaluator, err := NewDrip(database, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	evaluator.now = func() time.Time { return now }
	dripStudent(t, database, "ada", true, "day_2", "engaged", now.Add(-24*time.Hour))
	if _, err := database.Exec("INSERT INTO student_progress (student_id,day_number,completed) VALUES ('ada',2,FALSE)"); err != nil {
		t.Fatal(err)
	}
	if result, err := evaluator.Evaluate(context.Background()); err != nil || result != (Evaluation{}) {
		t.Fatalf("recent result=%+v err=%v", result, err)
	}
	if len(dispatcher.Actions()) != 0 {
		t.Fatal("recently active student was nudged")
	}
}

func TestDripDeliveryFailureRollsBack(t *testing.T) {
	database := dripDatabase(t)
	dispatcher := &captureDispatcher{fail: errors.New("provider credential must not leak")}
	evaluator, err := NewDrip(database, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	evaluator.now = func() time.Time { return now }
	dripStudent(t, database, "ada", true, "day_1", "pending", now.Add(-time.Hour))
	if _, err = evaluator.Evaluate(context.Background()); err == nil || !errors.Is(err, dispatcher.fail) {
		t.Fatalf("delivery failure: %v", err)
	}
	for _, table := range []string{"student_progress", "activity_logs"} {
		var count int
		if err := database.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s changed after delivery failure", table)
		}
	}
}

func TestDripConcurrentEvaluationDispatchesOnce(t *testing.T) {
	database := dripDatabase(t)
	dispatcher := &captureDispatcher{}
	now := time.Now().UTC()
	dripStudent(t, database, "ada", true, "day_1", "pending", now)
	const workers = 8
	var wait sync.WaitGroup
	errorsCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		evaluator, err := NewDrip(database, dispatcher)
		if err != nil {
			t.Fatal(err)
		}
		evaluator.now = func() time.Time { return now }
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := evaluator.Evaluate(context.Background())
			errorsCh <- err
		}()
	}
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Error(err)
		}
	}
	if actions := dispatcher.Actions(); len(actions) != 1 {
		t.Fatalf("duplicate dispatches: %+v", actions)
	}
}

func TestDripConfigurationAndDatabaseFailure(t *testing.T) {
	dispatcher := &captureDispatcher{}
	if _, err := NewDrip(nil, dispatcher); err == nil {
		t.Fatal("nil database accepted")
	}
	database := dripDatabase(t)
	if _, err := NewDrip(database, nil); err == nil {
		t.Fatal("nil dispatcher accepted")
	}
	evaluator, err := NewDrip(database, dispatcher)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := evaluator.Evaluate(context.Background()); err == nil {
		t.Fatal("closed database evaluation succeeded")
	}
}
