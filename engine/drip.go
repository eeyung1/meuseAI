// Package engine implements background student-journey evaluators.
package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/eeyung1/meuseAI/db"
)

const inactivityWindow = 48 * time.Hour

type ActionKind string

const (
	DeliverDay ActionKind = "deliver_day"
	SendNudge  ActionKind = "send_nudge"
)

// Action is a bounded delivery request emitted by an evaluator pass.
type Action struct {
	Kind      ActionKind
	StudentID string
	Email     string
	Day       int
}

// Dispatcher sends curriculum and re-engagement messages. Implementations must
// return an error when delivery fails so the associated state change can roll back.
type Dispatcher interface {
	Dispatch(context.Context, Action) error
}

type DispatchFunc func(context.Context, Action) error

func (f DispatchFunc) Dispatch(ctx context.Context, action Action) error {
	return f(ctx, action)
}

type Evaluation struct {
	Delivered int
	Nudged    int
	Lapsed    int
}

// Drip scans active curriculum students and performs one state-machine step.
type Drip struct {
	database   *db.DB
	dispatcher Dispatcher
	now        func() time.Time
}

func NewDrip(database *db.DB, dispatcher Dispatcher) (*Drip, error) {
	if database == nil || database.DB == nil || dispatcher == nil {
		return nil, errors.New("database and dispatcher are required")
	}
	return &Drip{database: database, dispatcher: dispatcher, now: time.Now}, nil
}

func dripDay(stage string) (int, bool) {
	switch stage {
	case "day_1":
		return 1, true
	case "day_2":
		return 2, true
	case "day_3":
		return 3, true
	case "day_4":
		return 4, true
	default:
		return 0, false
	}
}

// Evaluate dispatches newly unlocked content, sends one nudge after 48 hours,
// and marks students lapsed after another 48 hours without completion.
func (d *Drip) Evaluate(ctx context.Context) (Evaluation, error) {
	rows, err := d.database.QueryContext(ctx, `SELECT id FROM students
		WHERE email_verified=TRUE AND unsubscribed_at IS NULL
		AND engagement_status IN ('pending','engaged')
		AND current_stage IN ('day_1','day_2','day_3','day_4') ORDER BY id`)
	if err != nil {
		return Evaluation{}, fmt.Errorf("scan drip candidates: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return Evaluation{}, fmt.Errorf("scan drip candidate: %w", err)
		}
		ids = append(ids, id)
	}
	if err = rows.Close(); err != nil {
		return Evaluation{}, fmt.Errorf("close drip candidates: %w", err)
	}
	if err = rows.Err(); err != nil {
		return Evaluation{}, fmt.Errorf("iterate drip candidates: %w", err)
	}

	var total Evaluation
	for _, id := range ids {
		step, stepErr := d.evaluateStudent(ctx, id, d.now().UTC())
		total.Delivered += step.Delivered
		total.Nudged += step.Nudged
		total.Lapsed += step.Lapsed
		if stepErr != nil {
			return total, stepErr
		}
	}
	return total, nil
}

func (d *Drip) evaluateStudent(ctx context.Context, studentID string, now time.Time) (Evaluation, error) {
	tx, err := d.database.BeginTx(ctx, nil)
	if err != nil {
		return Evaluation{}, fmt.Errorf("begin drip evaluation: %w", err)
	}
	defer tx.Rollback()
	query := `SELECT email,current_stage,engagement_status,email_verified,unsubscribed_at,updated_at
		FROM students WHERE id=$1`
	if d.database.Dialect == db.Postgres {
		query += " FOR UPDATE"
	}
	var email, stage, engagement string
	var verified bool
	var unsubscribed sql.NullTime
	var updatedAt time.Time
	err = tx.QueryRowContext(ctx, query, studentID).Scan(&email, &stage, &engagement, &verified, &unsubscribed, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Evaluation{}, nil
	}
	if err != nil {
		return Evaluation{}, fmt.Errorf("read drip student: %w", err)
	}
	day, active := dripDay(stage)
	if !verified || unsubscribed.Valid || !active || (engagement != "pending" && engagement != "engaged") {
		return Evaluation{}, nil
	}

	var started time.Time
	var explicitStart sql.NullTime
	if err = tx.QueryRowContext(ctx, "SELECT curriculum_started_at,consent_timestamp FROM students WHERE id=$1", studentID).Scan(&explicitStart, &started); err != nil {
		return Evaluation{}, err
	}
	if explicitStart.Valid {
		started = explicitStart.Time
	}
	if now.Before(started.Add(time.Duration(day-1) * 24 * time.Hour)) {
		return Evaluation{}, nil
	}
	var completed bool
	var lastNudge, deliveredAt sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT completed,last_nudge_at,delivered_at FROM student_progress
		WHERE student_id=$1 AND day_number=$2`, studentID, day).Scan(&completed, &lastNudge, &deliveredAt)
	if errors.Is(err, sql.ErrNoRows) {
		action := Action{Kind: DeliverDay, StudentID: studentID, Email: email, Day: day}
		if err = d.dispatcher.Dispatch(ctx, action); err != nil {
			return Evaluation{}, fmt.Errorf("deliver day %d: %w", day, err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO student_progress (student_id,day_number,completed,delivered_at)
			VALUES ($1,$2,FALSE,$3)`, studentID, day, now); err != nil {
			return Evaluation{}, fmt.Errorf("record day delivery: %w", err)
		}
		if _, err = tx.ExecContext(ctx, "UPDATE students SET updated_at=$1 WHERE id=$2", now, studentID); err != nil {
			return Evaluation{}, fmt.Errorf("timestamp day delivery: %w", err)
		}
		if err = dripAudit(ctx, tx, studentID, "drip_delivery", map[string]any{"day": day}); err != nil {
			return Evaluation{}, err
		}
		if err = tx.Commit(); err != nil {
			return Evaluation{}, fmt.Errorf("commit day delivery: %w", err)
		}
		return Evaluation{Delivered: 1}, nil
	}
	if err != nil {
		return Evaluation{}, fmt.Errorf("read drip progress: %w", err)
	}
	if completed {
		return Evaluation{}, nil
	}
	if deliveredAt.Valid {
		updatedAt = deliveredAt.Time
	}
	cutoff := now.Add(-inactivityWindow)
	if !lastNudge.Valid {
		if updatedAt.After(cutoff) {
			return Evaluation{}, nil
		}
		action := Action{Kind: SendNudge, StudentID: studentID, Email: email, Day: day}
		if err = d.dispatcher.Dispatch(ctx, action); err != nil {
			return Evaluation{}, fmt.Errorf("send day %d nudge: %w", day, err)
		}
		if _, err = tx.ExecContext(ctx, "UPDATE student_progress SET last_nudge_at=$1 WHERE student_id=$2 AND day_number=$3", now, studentID, day); err != nil {
			return Evaluation{}, fmt.Errorf("record nudge: %w", err)
		}
		if err = dripAudit(ctx, tx, studentID, "nudge", map[string]any{"day": day}); err != nil {
			return Evaluation{}, err
		}
		if err = tx.Commit(); err != nil {
			return Evaluation{}, fmt.Errorf("commit nudge: %w", err)
		}
		return Evaluation{Nudged: 1}, nil
	}
	if lastNudge.Time.After(cutoff) {
		return Evaluation{}, nil
	}
	if _, err = tx.ExecContext(ctx, "UPDATE students SET engagement_status='lapsed',updated_at=$1 WHERE id=$2", now, studentID); err != nil {
		return Evaluation{}, fmt.Errorf("mark student lapsed: %w", err)
	}
	if err = dripAudit(ctx, tx, studentID, "lapsed", map[string]any{"day": day}); err != nil {
		return Evaluation{}, err
	}
	if err = tx.Commit(); err != nil {
		return Evaluation{}, fmt.Errorf("commit lapse: %w", err)
	}
	return Evaluation{Lapsed: 1}, nil
}

func dripAudit(ctx context.Context, tx *sql.Tx, studentID, event string, details map[string]any) error {
	payload, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("encode %s audit: %w", event, err)
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO activity_logs (student_id,event_type,details) VALUES ($1,$2,$3)", studentID, event, string(payload)); err != nil {
		return fmt.Errorf("record %s audit: %w", event, err)
	}
	return nil
}
