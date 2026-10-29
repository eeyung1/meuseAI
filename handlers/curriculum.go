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

type CurriculumIdentity func(*http.Request) (string, error)
type AdminAuthorized func(*http.Request) bool

// Curriculum implements completion-gated advancement through the free course.
type Curriculum struct {
	database        *db.DB
	now             func() time.Time
	identity        CurriculumIdentity
	adminAuthorized AdminAuthorized
}

func NewCurriculum(database *db.DB) (*Curriculum, error) {
	if database == nil || database.DB == nil {
		return nil, errors.New("database is required")
	}
	return &Curriculum{database: database, now: time.Now}, nil
}

func (h *Curriculum) WithAuth(identity CurriculumIdentity, adminAuthorized AdminAuthorized) *Curriculum {
	h.identity = identity
	h.adminAuthorized = adminAuthorized
	return h
}

func (h *Curriculum) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/curriculum/complete", h.Complete)
	mux.HandleFunc("POST /api/v1/curriculum/resume", h.Resume)
	mux.HandleFunc("POST /api/v1/curriculum/share-link", h.ShareLink)
	mux.HandleFunc("GET /api/v1/curriculum/share-status", h.ShareStatus)
	mux.HandleFunc("GET /api/v1/admin/social-shares", h.AdminListShares)
	mux.HandleFunc("POST /api/v1/admin/social-shares/review", h.AdminReviewShare)
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
	if h.identity != nil {
		id, authErr := h.identity(r)
		if authErr != nil || id == "" {
			curriculumError(w, 401, "authentication required")
			return
		}
		if (input.StudentID != "" && input.StudentID != id) || (r.Header.Get("X-Student-ID") != "" && r.Header.Get("X-Student-ID") != id) || (studentID != "" && studentID != id) {
			curriculumError(w, 400, "student identity does not match session")
			return
		}
		studentID, ok = id, true
	}
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
	var started time.Time
	var explicitStart sql.NullTime
	if err = tx.QueryRowContext(r.Context(), "SELECT curriculum_started_at,consent_timestamp FROM students WHERE id=$1", studentID).Scan(&explicitStart, &started); err != nil {
		curriculumError(w, 503, "course schedule unavailable")
		return
	}
	if explicitStart.Valid {
		started = explicitStart.Time
	}
	if now.Before(started.Add(time.Duration(input.Day-1) * 24 * time.Hour)) {
		curriculumJSON(w, 403, map[string]any{"error": "lesson is scheduled for later", "unlocks_at": started.Add(time.Duration(input.Day-1) * 24 * time.Hour)})
		return
	}
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

// ShareLink records a public social media link submission for a completed day.
func (h *Curriculum) ShareLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		StudentID string `json:"student_id"`
		Day       int    `json:"day"`
		ShareURL  string `json:"share_url"`
		Platform  string `json:"platform"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		curriculumError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	studentID, ok := curriculumIdentity(r, input.StudentID)
	if h.identity != nil {
		id, authErr := h.identity(r)
		if authErr != nil || id == "" {
			curriculumError(w, 401, "authentication required")
			return
		}
		if (input.StudentID != "" && input.StudentID != id) || (r.Header.Get("X-Student-ID") != "" && r.Header.Get("X-Student-ID") != id) || (studentID != "" && studentID != id) {
			curriculumError(w, 400, "student identity does not match session")
			return
		}
		studentID, ok = id, true
	}
	if !ok || input.Day < 1 || input.Day > 4 {
		curriculumError(w, http.StatusBadRequest, "valid student and day between 1 and 4 required")
		return
	}
	shareURL := strings.TrimSpace(input.ShareURL)
	if shareURL == "" || len(shareURL) > 1024 || !(strings.HasPrefix(shareURL, "http://") || strings.HasPrefix(shareURL, "https://")) {
		curriculumError(w, http.StatusBadRequest, "valid public post URL required (http/https)")
		return
	}
	platform := strings.ToLower(strings.TrimSpace(input.Platform))
	if platform == "" {
		platform = "social"
	}

	tx, err := h.database.BeginTx(r.Context(), nil)
	if err != nil {
		curriculumError(w, http.StatusServiceUnavailable, "submission unavailable")
		return
	}
	defer tx.Rollback()

	// Ensure the student completed this day's curriculum exercise first
	var completed bool
	err = tx.QueryRowContext(r.Context(), "SELECT completed FROM student_progress WHERE student_id=$1 AND day_number=$2", studentID, input.Day).Scan(&completed)
	if err != nil || !completed {
		curriculumError(w, http.StatusForbidden, "exercise must be completed before submitting social share for bonus XP")
		return
	}

	// Check if share link already submitted for this day
	var existingStatus string
	err = tx.QueryRowContext(r.Context(), "SELECT status FROM social_shares WHERE student_id=$1 AND day_number=$2", studentID, input.Day).Scan(&existingStatus)
	if err == nil && existingStatus != "rejected" {
		curriculumError(w, http.StatusConflict, "a social share link has already been submitted for this day")
		return
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		curriculumError(w, http.StatusServiceUnavailable, "submission unavailable")
		return
	}

	now := h.now().UTC()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO social_shares (student_id, day_number, share_url, platform, status, xp_awarded, created_at)
		VALUES ($1, $2, $3, $4, 'pending', 0, $5) ON CONFLICT(student_id,day_number) DO UPDATE SET share_url=excluded.share_url,platform=excluded.platform,status='pending',created_at=excluded.created_at,reviewed_at=NULL WHERE social_shares.status='rejected'`, studentID, input.Day, shareURL, platform, now)
	if err != nil {
		curriculumError(w, http.StatusServiceUnavailable, "submission unavailable")
		return
	}

	details, _ := json.Marshal(map[string]any{"day": input.Day, "platform": platform, "url": shareURL})
	_, _ = tx.ExecContext(r.Context(), "INSERT INTO activity_logs (student_id, event_type, details) VALUES ($1, 'social_share_submitted', $2)", studentID, string(details))

	if err = tx.Commit(); err != nil {
		curriculumError(w, http.StatusServiceUnavailable, "submission unavailable")
		return
	}

	curriculumJSON(w, http.StatusOK, map[string]any{
		"status":    "pending",
		"day":       input.Day,
		"message":   "Post link submitted! Your +200 bonus XP will be awarded once verified by admin.",
		"share_url": shareURL,
		"platform":  platform,
	})
}

// ShareStatus returns all social share submissions and review statuses for the authenticated student.
func (h *Curriculum) ShareStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	studentID, ok := curriculumIdentity(r, "")
	if h.identity != nil {
		id, authErr := h.identity(r)
		if authErr != nil || id == "" {
			curriculumError(w, 401, "authentication required")
			return
		}
		if studentID != "" && studentID != id {
			curriculumError(w, 400, "student identity does not match session")
			return
		}
		studentID, ok = id, true
	}
	if !ok {
		curriculumError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	rows, err := h.database.QueryContext(r.Context(), `SELECT day_number, share_url, platform, status, xp_awarded, created_at, reviewed_at
		FROM social_shares WHERE student_id=$1 ORDER BY day_number ASC`, studentID)
	if err != nil {
		curriculumError(w, http.StatusServiceUnavailable, "unable to fetch share status")
		return
	}
	defer rows.Close()

	type shareItem struct {
		DayNumber  int        `json:"day"`
		ShareURL   string     `json:"share_url"`
		Platform   string     `json:"platform"`
		Status     string     `json:"status"`
		XPAwarded  int        `json:"xp_awarded"`
		CreatedAt  time.Time  `json:"created_at"`
		ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
	}
	shares := []shareItem{}
	for rows.Next() {
		var s shareItem
		var reviewed sql.NullTime
		if err := rows.Scan(&s.DayNumber, &s.ShareURL, &s.Platform, &s.Status, &s.XPAwarded, &s.CreatedAt, &reviewed); err == nil {
			if reviewed.Valid {
				s.ReviewedAt = &reviewed.Time
			}
			shares = append(shares, s)
		}
	}
	curriculumJSON(w, http.StatusOK, map[string]any{"shares": shares})
}

// AdminListShares returns all submitted social shares for admin verification.
func (h *Curriculum) AdminListShares(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.adminAuthorized == nil || !h.adminAuthorized(r) {
		curriculumError(w, http.StatusUnauthorized, "admin authorization required")
		return
	}
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	query := `SELECT s.id, s.student_id, st.full_name, st.email, s.day_number, s.share_url, s.platform, s.status, s.xp_awarded, s.created_at
		FROM social_shares s
		JOIN students st ON st.id = s.student_id`
	var args []any
	if statusFilter != "" {
		query += " WHERE s.status = $1"
		args = append(args, statusFilter)
	}
	query += " ORDER BY s.id DESC LIMIT 100"

	rows, err := h.database.QueryContext(r.Context(), query, args...)
	if err != nil {
		curriculumError(w, http.StatusServiceUnavailable, "unable to list shares")
		return
	}
	defer rows.Close()

	type adminShareItem struct {
		ID           int64     `json:"id"`
		StudentID    string    `json:"student_id"`
		StudentName  string    `json:"student_name"`
		StudentEmail string    `json:"student_email"`
		DayNumber    int       `json:"day"`
		ShareURL     string    `json:"share_url"`
		Platform     string    `json:"platform"`
		Status       string    `json:"status"`
		XPAwarded    int       `json:"xp_awarded"`
		CreatedAt    time.Time `json:"created_at"`
	}
	items := []adminShareItem{}
	for rows.Next() {
		var item adminShareItem
		if err := rows.Scan(&item.ID, &item.StudentID, &item.StudentName, &item.StudentEmail, &item.DayNumber, &item.ShareURL, &item.Platform, &item.Status, &item.XPAwarded, &item.CreatedAt); err == nil {
			items = append(items, item)
		}
	}
	curriculumJSON(w, http.StatusOK, map[string]any{"shares": items})
}

// AdminReviewShare updates the approval status of a student's social share and grants 200 XP if approved.
func (h *Curriculum) AdminReviewShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.adminAuthorized == nil || !h.adminAuthorized(r) {
		curriculumError(w, http.StatusUnauthorized, "admin authorization required")
		return
	}
	var input struct {
		ShareID int64  `json:"share_id"`
		Action  string `json:"action"` // "approve" or "reject"
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		curriculumError(w, http.StatusBadRequest, "invalid review payload")
		return
	}
	input.Action = strings.ToLower(strings.TrimSpace(input.Action))
	if input.ShareID <= 0 || (input.Action != "approve" && input.Action != "reject") {
		curriculumError(w, http.StatusBadRequest, "valid share_id and action (approve/reject) required")
		return
	}

	tx, err := h.database.BeginTx(r.Context(), nil)
	if err != nil {
		curriculumError(w, http.StatusServiceUnavailable, "review unavailable")
		return
	}
	defer tx.Rollback()

	var studentID, currentStatus string
	var dayNumber int
	reviewQuery := "SELECT student_id, day_number, status FROM social_shares WHERE id = $1"
	if h.database.Dialect == db.Postgres {
		reviewQuery += " FOR UPDATE"
	}
	err = tx.QueryRowContext(r.Context(), reviewQuery, input.ShareID).Scan(&studentID, &dayNumber, &currentStatus)
	if errors.Is(err, sql.ErrNoRows) {
		curriculumError(w, http.StatusNotFound, "share not found")
		return
	}
	if err != nil {
		curriculumError(w, http.StatusServiceUnavailable, "review unavailable")
		return
	}
	if currentStatus != "pending" {
		curriculumError(w, http.StatusConflict, "share has already been reviewed")
		return
	}

	now := h.now().UTC()
	xpToAward := 0
	newStatus := "rejected"
	if input.Action == "approve" {
		xpToAward = 200
		newStatus = "approved"
		// Credit the 200 XP to the student
		_, err = tx.ExecContext(r.Context(), "UPDATE students SET xp = xp + $1, updated_at = $2 WHERE id = $3", xpToAward, now, studentID)
		if err != nil {
			curriculumError(w, http.StatusServiceUnavailable, "review unavailable")
			return
		}
		details, _ := json.Marshal(map[string]any{"day": dayNumber, "xp_awarded": xpToAward, "share_id": input.ShareID})
		_, _ = tx.ExecContext(r.Context(), "INSERT INTO activity_logs (student_id, event_type, details) VALUES ($1, 'social_share_approved', $2)", studentID, string(details))
	} else {
		details, _ := json.Marshal(map[string]any{"day": dayNumber, "share_id": input.ShareID})
		_, _ = tx.ExecContext(r.Context(), "INSERT INTO activity_logs (student_id, event_type, details) VALUES ($1, 'social_share_rejected', $2)", studentID, string(details))
	}

	_, err = tx.ExecContext(r.Context(), "UPDATE social_shares SET status = $1, xp_awarded = $2, reviewed_at = $3 WHERE id = $4", newStatus, xpToAward, now, input.ShareID)
	if err != nil {
		curriculumError(w, http.StatusServiceUnavailable, "review unavailable")
		return
	}

	if err = tx.Commit(); err != nil {
		curriculumError(w, http.StatusServiceUnavailable, "review unavailable")
		return
	}

	curriculumJSON(w, http.StatusOK, map[string]any{
		"share_id":   input.ShareID,
		"status":     newStatus,
		"xp_awarded": xpToAward,
	})
}

// Resume restarts the reminder window without granting progress or resubscribing.
func (h *Curriculum) Resume(w http.ResponseWriter, r *http.Request) {
	if h.identity == nil {
		curriculumError(w, 401, "authentication required")
		return
	}
	id, err := h.identity(r)
	if err != nil || id == "" {
		curriculumError(w, 401, "authentication required")
		return
	}
	tx, err := h.database.BeginTx(r.Context(), nil)
	if err != nil {
		curriculumError(w, 503, "resume unavailable")
		return
	}
	defer tx.Rollback()
	var stage string
	err = tx.QueryRowContext(r.Context(), "SELECT current_stage FROM students WHERE id=$1 AND email_verified=TRUE AND unsubscribed_at IS NULL AND engagement_status='lapsed'", id).Scan(&stage)
	day, ok := curriculumDay(stage)
	if err != nil || !ok {
		curriculumError(w, 409, "no paused lesson to resume")
		return
	}
	now := h.now().UTC()
	if _, err = tx.ExecContext(r.Context(), "UPDATE students SET engagement_status='engaged',updated_at=$1 WHERE id=$2", now, id); err != nil {
		curriculumError(w, 503, "resume unavailable")
		return
	}
	if _, err = tx.ExecContext(r.Context(), "UPDATE student_progress SET delivered_at=$1,last_nudge_at=NULL WHERE student_id=$2 AND day_number=$3 AND completed=FALSE", now, id, day); err != nil {
		curriculumError(w, 503, "resume unavailable")
		return
	}
	if err = tx.Commit(); err != nil {
		curriculumError(w, 503, "resume unavailable")
		return
	}
	curriculumJSON(w, 200, map[string]string{"status": "resumed"})
}
