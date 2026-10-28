package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/eeyung1/meuseAI/db"
)

const (
	curriculumXPAward       = 250
	maxCurriculumSubmission = 8000
)

type curriculumStudentKey struct{}

// WithCurriculumStudent records the authenticated student identity for Complete.
// Authentication middleware should use this instead of trusting request payloads.
func WithCurriculumStudent(ctx context.Context, studentID string) context.Context {
	return context.WithValue(ctx, curriculumStudentKey{}, strings.TrimSpace(studentID))
}

// Curriculum implements completion-gated advancement through the free course.
type Curriculum struct {
	database *db.DB
	now      func() time.Time
}

func NewCurriculum(database *db.DB) (*Curriculum, error) {
	if database == nil || database.DB == nil {
		return nil, errors.New("database is required")
	}
	return &Curriculum{database: database, now: time.Now}, nil
}

func (h *Curriculum) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/curriculum/complete", h.Complete)
}

func curriculumJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func curriculumError(w http.ResponseWriter, status int, message string) {
	curriculumJSON(w, status, map[string]string{"error": message})
}

func curriculumDay(stage string) (int, bool) {
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

func nextCurriculumStage(day int) string {
	if day == 4 {
		return "conversion_gate"
	}
	return "day_" + string(rune('0'+day+1))
}

func curriculumIdentity(r *http.Request, payloadID string) (string, bool) {
	contextID, _ := r.Context().Value(curriculumStudentKey{}).(string)
	headerID := strings.TrimSpace(r.Header.Get("X-Student-ID"))
	payloadID = strings.TrimSpace(payloadID)
	studentID := contextID
	if studentID == "" {
		studentID = headerID
	}
	if studentID == "" {
		studentID = payloadID
	}
	if studentID == "" || len(studentID) > 64 || strings.ContainsAny(studentID, "\r\n\t ") {
		return "", false
	}
	for _, candidate := range []string{contextID, headerID, payloadID} {
		if candidate != "" && candidate != studentID {
			return "", false
		}
	}
	return studentID, true
}

// Complete validates one prompt submission and atomically grants its XP once.
func (h *Curriculum) Complete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		StudentID  string `json:"student_id"`
		Day        int    `json:"day"`
		Submission string `json:"submission"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		curriculumError(w, http.StatusBadRequest, "invalid completion payload")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		curriculumError(w, http.StatusBadRequest, "expected one JSON object")
		return
	}
	studentID, ok := curriculumIdentity(r, input.StudentID)
	input.Submission = strings.TrimSpace(input.Submission)
	if !ok || input.Day < 1 || input.Day > 4 || input.Submission == "" || !utf8.ValidString(input.Submission) || utf8.RuneCountInString(input.Submission) > maxCurriculumSubmission {
		curriculumError(w, http.StatusBadRequest, "student, day and prompt submission are required")
		return
	}

	tx, err := h.database.BeginTx(r.Context(), nil)
	if err != nil {
		curriculumError(w, http.StatusServiceUnavailable, "completion unavailable")
		return
	}
	defer tx.Rollback()
	query := `SELECT email_verified,current_stage,engagement_status,xp,streak_days,unsubscribed_at
		FROM students WHERE id=$1`
	if h.database.Dialect == db.Postgres {
		query += " FOR UPDATE"
	}
	var verified bool
	var stage, engagement string
	var xp, streak int
	var unsubscribed sql.NullTime
	err = tx.QueryRowContext(r.Context(), query, studentID).Scan(&verified, &stage, &engagement, &xp, &streak, &unsubscribed)
	if errors.Is(err, sql.ErrNoRows) {
		curriculumError(w, http.StatusNotFound, "student not found")
		return
	}
	if err != nil {
		curriculumError(w, http.StatusServiceUnavailable, "completion unavailable")
		return
	}
	expectedDay, activeStage := curriculumDay(stage)
	if !verified || unsubscribed.Valid {
		curriculumError(w, http.StatusForbidden, "student is not eligible for curriculum")
		return
	}
	if !activeStage || engagement == "lapsed" || engagement == "unsubscribed" || expectedDay != input.Day {
		curriculumError(w, http.StatusConflict, "submission does not match the unlocked day")
		return
	}

	now := h.now().UTC()
	completed := false
	var completedAt sql.NullTime
	err = tx.QueryRowContext(r.Context(), "SELECT completed,completed_at FROM student_progress WHERE student_id=$1 AND day_number=$2", studentID, input.Day).Scan(&completed, &completedAt)
	progressExists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		curriculumError(w, http.StatusServiceUnavailable, "completion unavailable")
		return
	}
	if completed {
		curriculumError(w, http.StatusConflict, "day is already complete")
		return
	}

	newStreak := 1
	if streak > 0 {
		newStreak = streak
	}
	if input.Day > 1 {
		var previous time.Time
		if err = tx.QueryRowContext(r.Context(), "SELECT completed_at FROM student_progress WHERE student_id=$1 AND day_number=$2 AND completed=TRUE", studentID, input.Day-1).Scan(&previous); err != nil {
			curriculumError(w, http.StatusConflict, "previous day is not complete")
			return
		}
		previousDay := time.Date(previous.UTC().Year(), previous.UTC().Month(), previous.UTC().Day(), 0, 0, 0, 0, time.UTC)
		currentDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		switch currentDay.Sub(previousDay) {
		case 24 * time.Hour:
			newStreak++
		case 0:
		default:
			newStreak = 1
		}
	}

	if progressExists {
		result, updateErr := tx.ExecContext(r.Context(), `UPDATE student_progress
			SET completed=TRUE,submission_payload=$1,completed_at=$2
			WHERE student_id=$3 AND day_number=$4 AND completed=FALSE`, input.Submission, now, studentID, input.Day)
		if updateErr != nil {
			curriculumError(w, http.StatusServiceUnavailable, "completion unavailable")
			return
		}
		if count, countErr := result.RowsAffected(); countErr != nil || count != 1 {
			curriculumError(w, http.StatusConflict, "day is already complete")
			return
		}
	} else if _, err = tx.ExecContext(r.Context(), `INSERT INTO student_progress
		(student_id,day_number,completed,submission_payload,completed_at) VALUES ($1,$2,TRUE,$3,$4)`, studentID, input.Day, input.Submission, now); err != nil {
		curriculumError(w, http.StatusServiceUnavailable, "completion unavailable")
		return
	}

	nextStage := nextCurriculumStage(input.Day)
	result, err := tx.ExecContext(r.Context(), `UPDATE students SET current_stage=$1,engagement_status='engaged',
		xp=xp+$2,streak_days=$3,updated_at=$4 WHERE id=$5 AND current_stage=$6`, nextStage, curriculumXPAward, newStreak, now, studentID, stage)
	if err != nil {
		curriculumError(w, http.StatusServiceUnavailable, "completion unavailable")
		return
	}
	if count, countErr := result.RowsAffected(); countErr != nil || count != 1 {
		curriculumError(w, http.StatusConflict, "student stage changed")
		return
	}
	details, _ := json.Marshal(map[string]any{"day": input.Day, "next_stage": nextStage, "xp_awarded": curriculumXPAward})
	if _, err = tx.ExecContext(r.Context(), "INSERT INTO activity_logs (student_id,event_type,details) VALUES ($1,'complete_day',$2)", studentID, string(details)); err != nil {
		curriculumError(w, http.StatusServiceUnavailable, "completion unavailable")
		return
	}
	if err = tx.Commit(); err != nil {
		curriculumError(w, http.StatusServiceUnavailable, "completion unavailable")
		return
	}
	curriculumJSON(w, http.StatusOK, map[string]any{
		"student_id": studentID, "completed_day": input.Day, "current_stage": nextStage,
		"xp": xp + curriculumXPAward, "xp_awarded": curriculumXPAward, "streak_days": newStreak,
	})
}
