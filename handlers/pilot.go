package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/eeyung1/meuseAI/services"
	"net/http"
	"time"
)

// Interest captures non-binding demand without creating a payment or entitlement.
func (p *Payment) Interest(w http.ResponseWriter, r *http.Request) {
	id, err := p.config.StudentIdentity(r)
	if err != nil || id == "" {
		paymentError(w, 401, "authentication required")
		return
	}
	if !p.config.PilotMode {
		paymentError(w, 409, "interest registration is only available during the pilot")
		return
	}
	var input struct {
		Tracks []string `json:"tracks"`
	}
	if paymentDecode(w, r, &input) != nil || input.Tracks == nil {
		paymentError(w, 400, "select one or both tracks")
		return
	}
	tracks, err := services.CanonicalTracks(input.Tracks)
	if err != nil {
		paymentError(w, 400, "invalid track selection")
		return
	}
	tx, err := p.config.Database.BeginTx(r.Context(), nil)
	if err != nil {
		paymentError(w, 503, "interest unavailable")
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), "UPDATE students SET updated_at=updated_at WHERE id=$1", id); err != nil {
		paymentError(w, 503, "interest unavailable")
		return
	}
	var verified bool
	var unsubscribed sql.NullTime
	if err = tx.QueryRowContext(r.Context(), "SELECT email_verified,unsubscribed_at FROM students WHERE id=$1", id).Scan(&verified, &unsubscribed); errors.Is(err, sql.ErrNoRows) {
		paymentError(w, 401, "authentication required")
		return
	} else if err != nil {
		paymentError(w, 503, "interest unavailable")
		return
	}
	var complete bool
	err = tx.QueryRowContext(r.Context(), "SELECT completed FROM student_progress WHERE student_id=$1 AND day_number=4", id).Scan(&complete)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		paymentError(w, 503, "interest unavailable")
		return
	}
	if !verified || unsubscribed.Valid || !complete {
		paymentError(w, 403, "complete the free sprint with a verified account first")
		return
	}
	var raw string
	err = tx.QueryRowContext(r.Context(), "SELECT details FROM activity_logs WHERE student_id=$1 AND event_type='tier1_interest' ORDER BY id DESC LIMIT 1", id).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		paymentError(w, 503, "interest unavailable")
		return
	}
	var prior struct {
		Tracks string `json:"tracks"`
	}
	_ = json.Unmarshal([]byte(raw), &prior)
	if prior.Tracks != tracks {
		if err = paymentAudit(tx, r, id, "tier1_interest", map[string]string{"tracks": tracks, "kind": "non_binding_interest"}); err != nil {
			paymentError(w, 503, "interest unavailable")
			return
		}
	}
	if err = tx.Commit(); err != nil {
		paymentError(w, 503, "interest unavailable")
		return
	}
	paymentJSON(w, 200, map[string]string{"status": "interest_saved", "message": "Interest saved. No payment was taken, no place is reserved and paid access has not been unlocked."})
}
func (p *Payment) ListInterest(w http.ResponseWriter, r *http.Request) {
	if p.config.AdminAuthorized == nil || !p.config.AdminAuthorized(r) {
		paymentError(w, 401, "admin authorization required")
		return
	}
	rows, err := p.config.Database.QueryContext(r.Context(), `SELECT s.id,s.full_name,s.email,a.details,a.created_at FROM students s JOIN activity_logs a ON a.student_id=s.id WHERE a.event_type='tier1_interest' AND a.id=(SELECT MAX(latest.id) FROM activity_logs latest WHERE latest.student_id=s.id AND latest.event_type='tier1_interest') ORDER BY a.id DESC LIMIT 500`)
	if err != nil {
		paymentError(w, 503, "interest unavailable")
		return
	}
	defer rows.Close()
	type entry struct {
		StudentID string    `json:"student_id"`
		Name      string    `json:"name"`
		Email     string    `json:"email"`
		Tracks    string    `json:"tracks"`
		At        time.Time `json:"at"`
	}
	entries := []entry{}
	for rows.Next() {
		var e entry
		var raw string
		if err = rows.Scan(&e.StudentID, &e.Name, &e.Email, &raw, &e.At); err != nil {
			paymentError(w, 503, "interest unavailable")
			return
		}
		var detail struct {
			Tracks string `json:"tracks"`
		}
		if json.Unmarshal([]byte(raw), &detail) != nil {
			continue
		}
		e.Tracks = detail.Tracks
		entries = append(entries, e)
	}
	if rows.Err() != nil {
		paymentError(w, 503, "interest unavailable")
		return
	}
	paymentJSON(w, 200, map[string]any{"entries": entries, "limit": 500})
}
