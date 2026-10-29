package handlers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/eeyung1/meuseAI/db"
	"github.com/eeyung1/meuseAI/services"
	assets "github.com/eeyung1/meuseAI/static"
	"github.com/eeyung1/meuseAI/telemetry"
)

var showcaseAssetName = regexp.MustCompile(`^day[1-4]-[1-3]\.(jpg|webp)$`)

type DashboardIdentity func(*http.Request) (string, error)
type Dashboard struct {
	database *db.DB
	hub      *telemetry.Hub
	identity DashboardIdentity
}

func NewDashboard(database *db.DB, hub *telemetry.Hub, identity DashboardIdentity) (*Dashboard, error) {
	if database == nil || database.DB == nil || hub == nil || identity == nil {
		return nil, errors.New("dashboard requires database, hub and student authentication")
	}
	return &Dashboard{database, hub, identity}, nil
}
func (d *Dashboard) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/dashboard/me", d.Me)
	mux.HandleFunc("GET /api/v1/telemetry/stream", d.Stream)
	mux.HandleFunc("GET /dashboard", d.Page)
	mux.HandleFunc("GET /assets/showcase/{file}", func(w http.ResponseWriter, r *http.Request) {
		file := r.PathValue("file")
		if file != "manifest.json" && !showcaseAssetName.MatchString(file) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if file == "manifest.json" {
			w.Header().Set("Cache-Control", "no-store")
		}
		http.ServeFileFS(w, r, assets.Assets, "assets/showcase/"+file)
	})
	for _, name := range []string{"showcase.js", "dashboard.css", "dashboard.js", "landing.css", "landing.js", "verify.js", "share.js", "admin.js"} {
		name := name
		mux.HandleFunc("GET /assets/"+name, func(w http.ResponseWriter, r *http.Request) { http.ServeFileFS(w, r, assets.Assets, name) })
	}
}
func (d *Dashboard) Page(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.ServeFileFS(w, r, assets.Assets, "dashboard.html")
}

type DashboardModule struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	State        string    `json:"state"`
	XP           int       `json:"xp"`
	Prerequisite string    `json:"prerequisite"`
	UnlocksAt    time.Time `json:"unlocks_at"`
	Submission   string    `json:"submission,omitempty"`
}
type DashboardPeer struct {
	Label string `json:"label"`
	XP    int    `json:"xp"`
	Rank  int    `json:"rank"`
	You   bool   `json:"you"`
}
type DashboardActivity struct {
	ID    int64     `json:"id"`
	Title string    `json:"title"`
	At    time.Time `json:"at"`
}
type DashboardShare struct {
	DayNumber int    `json:"day"`
	ShareURL  string `json:"share_url"`
	Platform  string `json:"platform"`
	Status    string `json:"status"`
	XPAwarded int    `json:"xp_awarded"`
}
type DashboardSnapshot struct {
	StudentID       string              `json:"student_id"`
	Name            string              `json:"full_name"`
	XP              int                 `json:"xp"`
	Streak          int                 `json:"streak_days"`
	Stage           string              `json:"current_stage"`
	Funnel          string              `json:"funnel_tier"`
	Engagement      string              `json:"engagement_status"`
	Verified        bool                `json:"email_verified"`
	Unsubscribed    bool                `json:"unsubscribed"`
	Tier2Ready      bool                `json:"tier_2_ready"`
	Rank            int                 `json:"rank"`
	DiscountPercent int                 `json:"discount_percent"`
	Tier1PriceNaira int                 `json:"tier_1_price_naira"`
	SocialShares    []DashboardShare    `json:"social_shares"`
	Modules         []DashboardModule   `json:"modules"`
	Leaderboard     []DashboardPeer     `json:"leaderboard"`
	Activity        []DashboardActivity `json:"activity"`
}

func dashboardJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (d *Dashboard) authenticated(r *http.Request) (string, error) {
	id, err := d.identity(r)
	if err != nil || id == "" {
		return "", errors.New("authentication required")
	}
	return id, nil
}
func (d *Dashboard) Me(w http.ResponseWriter, r *http.Request) {
	id, err := d.authenticated(r)
	if err != nil {
		dashboardJSON(w, 401, map[string]string{"error": "authentication required"})
		return
	}
	result, err := d.snapshot(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		dashboardJSON(w, 401, map[string]string{"error": "authentication required"})
		return
	}
	if err != nil {
		dashboardJSON(w, 503, map[string]string{"error": "dashboard temporarily unavailable"})
		return
	}
	dashboardJSON(w, 200, result)
}
func (d *Dashboard) Stream(w http.ResponseWriter, r *http.Request) {
	id, err := d.authenticated(r)
	if err != nil {
		http.Error(w, "authentication required", 401)
		return
	}
	var exists string
	if err = d.database.QueryRowContext(r.Context(), "SELECT id FROM students WHERE id=$1", id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "authentication required", 401)
		} else {
			http.Error(w, "stream unavailable", 503)
		}
		return
	}
	d.hub.Serve(w, r, id)
}
func (d *Dashboard) snapshot(ctx context.Context, id string) (DashboardSnapshot, error) {
	result := DashboardSnapshot{StudentID: id, Modules: []DashboardModule{}, Leaderboard: []DashboardPeer{}, Activity: []DashboardActivity{}, SocialShares: []DashboardShare{}}
	tx, err := d.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable, ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var unsubscribed sql.NullTime
	err = tx.QueryRowContext(ctx, "SELECT full_name,xp,streak_days,current_stage,funnel_tier,engagement_status,email_verified,tier_2_ready,unsubscribed_at FROM students WHERE id=$1", id).Scan(&result.Name, &result.XP, &result.Streak, &result.Stage, &result.Funnel, &result.Engagement, &result.Verified, &result.Tier2Ready, &unsubscribed)
	if err != nil {
		return result, err
	}
	result.Unsubscribed = unsubscribed.Valid

	// Query social shares
	sRows, sErr := tx.QueryContext(ctx, "SELECT day_number, share_url, platform, status, xp_awarded FROM social_shares WHERE student_id=$1 ORDER BY day_number ASC", id)
	if sErr == nil {
		for sRows.Next() {
			var s DashboardShare
			if scanErr := sRows.Scan(&s.DayNumber, &s.ShareURL, &s.Platform, &s.Status, &s.XPAwarded); scanErr == nil {
				result.SocialShares = append(result.SocialShares, s)
			}
		}
		sRows.Close()
	}

	var started time.Time
	var explicitStart sql.NullTime
	if err := tx.QueryRowContext(ctx, "SELECT curriculum_started_at,consent_timestamp FROM students WHERE id=$1", id).Scan(&explicitStart, &started); err != nil {
		return result, err
	}
	if explicitStart.Valid {
		started = explicitStart.Time
	}
	var submissions [4]string
	var completed [4]bool
	rows, err := tx.QueryContext(ctx, "SELECT day_number,completed,COALESCE(submission_payload,'') FROM student_progress WHERE student_id=$1", id)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var day int
		var done bool
		var submission string
		if err = rows.Scan(&day, &done, &submission); err != nil {
			rows.Close()
			return result, err
		}
		if day >= 1 && day <= 4 {
			completed[day-1] = done
			submissions[day-1] = submission
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	titles := []string{"Prompt foundations", "Atmosphere & lighting", "Reference & consistency", "Conversational editing"}
	descriptions := []string{"Build an image prompt with a subject, style and setting.", "Shape mood with light, shadow and atmosphere.", "Keep characters and visual style consistent across images.", "Refine your work through feedback and submit your capstone."}
	active := result.Verified && !result.Unsubscribed && result.Engagement != "lapsed"
	previous := true
	for i := 0; i < 4; i++ {
		state := "locked"
		if completed[i] {
			state = "completed"
		} else if active && previous && result.Stage == fmt.Sprintf("day_%d", i+1) {
			state = "available"
			if time.Now().UTC().Before(started.Add(time.Duration(i) * 24 * time.Hour)) {
				state = "scheduled"
			}
		}
		prerequisite := "Verify your email"
		if i > 0 {
			prerequisite = fmt.Sprintf("Complete Day %d", i)
		}
		result.Modules = append(result.Modules, DashboardModule{ID: fmt.Sprintf("day_%d", i+1), Title: titles[i], Description: descriptions[i], State: state, XP: 250, Prerequisite: prerequisite, UnlocksAt: started.Add(time.Duration(i) * 24 * time.Hour), Submission: submissions[i]})
		previous = previous && completed[i]
	}
	tierState := "locked"
	if result.Funnel == "tier_1_active" || result.Funnel == "tier_2_active" {
		tierState = "unlocked"
	} else if active && previous && result.Stage == "conversion_gate" && result.Funnel == "free_week" {
		tierState = "available"
	}

	discountPercent := services.Tier1DiscountPercent(result.XP)
	if discountPercent > 50 {
		discountPercent = 50
	}
	result.DiscountPercent = discountPercent
	baseNaira := 5000
	result.Tier1PriceNaira = baseNaira - (baseNaira * discountPercent / 100)

	tierDesc := "Turn an image you created this week into a landing page for your business or project. Tier 1 access: ₦5,000. External tools may have separate charges."
	if discountPercent > 0 {
		tierDesc = fmt.Sprintf("Turn a course image into a landing page for your business or project. Tier 1: ₦%d (%d%% course discount from ₦5,000). External tools may have separate charges.", result.Tier1PriceNaira, discountPercent)
	} else {
		xpNeeded := 1000 - result.XP
		tierDesc = fmt.Sprintf("Turn a course image into a landing page for your business or project. Tier 1: ₦5,000; earn %d more XP for 10%% off. External tools may have separate charges.", xpNeeded)
	}
	result.Modules = append(result.Modules, DashboardModule{ID: "tier_1", Title: "Lovable landing pages", Description: tierDesc, State: tierState, Prerequisite: "Complete all four days"})
	if result.Verified && !result.Unsubscribed {
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*)+1 FROM students WHERE email_verified=TRUE AND unsubscribed_at IS NULL AND xp>$1", result.XP).Scan(&result.Rank); err != nil {
			return result, err
		}
	}
	rows, err = tx.QueryContext(ctx, "SELECT id,xp,RANK() OVER(ORDER BY xp DESC) FROM students WHERE email_verified=TRUE AND unsubscribed_at IS NULL ORDER BY xp DESC,id LIMIT 5")
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var peerID string
		var peer DashboardPeer
		if err = rows.Scan(&peerID, &peer.XP, &peer.Rank); err != nil {
			rows.Close()
			return result, err
		}
		peer.You = peerID == id
		if peer.You {
			peer.Label = "You"
		} else {
			sum := sha256.Sum256([]byte(peerID))
			peer.Label = "Learner " + hex.EncodeToString(sum[:3])
		}
		result.Leaderboard = append(result.Leaderboard, peer)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	rows, err = tx.QueryContext(ctx, "SELECT id,event_type,created_at FROM activity_logs WHERE student_id=$1 ORDER BY id DESC LIMIT 12", id)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var item DashboardActivity
		var event string
		if err = rows.Scan(&item.ID, &event, &item.At); err != nil {
			rows.Close()
			return result, err
		}
		item.Title = dashboardEventTitle(event)
		result.Activity = append(result.Activity, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}
func dashboardEventTitle(event string) string {
	switch event {
	case "intake":
		return "Your learning account was created"
	case "verify":
		return "Email verified"
	case "complete_day", "day_completed", "complete":
		return "Learning progress updated"
	case "social_share_submitted":
		return "Submitted social share link for review"
	case "social_share_approved":
		return "Social share confirmed (+200 XP awarded!)"
	case "social_share_rejected":
		return "Social share review completed"
	case "payment_success":
		return "Payment confirmed"
	case "lovable_provisioning_requested":
		return "Tier 1 access is being prepared"
	case "nudge":
		return "A learning reminder is available"
	case "unsubscribe":
		return "Email updates paused"
	default:
		return "Your learning activity updated"
	}
}

// RunTelemetry bridges durable student activity to live invalidation events.
// Server bootstrap runs this once with its shutdown context. Clients always
// fetch a snapshot on connect: the hub intentionally does not replay history.
func (d *Dashboard) RunTelemetry(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Second
	}
	var cursor int64
	if err := d.database.QueryRowContext(ctx, "SELECT COALESCE(MAX(id),0) FROM activity_logs").Scan(&cursor); err != nil {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			var err error
			cursor, err = d.pollTelemetry(ctx, cursor)
			if err != nil {
				return err
			}
		}
	}
}
func (d *Dashboard) pollTelemetry(ctx context.Context, cursor int64) (int64, error) {
	rows, err := d.database.QueryContext(ctx, "SELECT id,student_id,event_type,created_at FROM activity_logs WHERE id>$1 ORDER BY id LIMIT 100", cursor)
	if err != nil {
		return cursor, err
	}
	defer rows.Close()
	changed := false
	for rows.Next() {
		var item DashboardActivity
		var owner sql.NullString
		var event string
		if err = rows.Scan(&item.ID, &owner, &event, &item.At); err != nil {
			return cursor, err
		}
		if owner.Valid {
			if err = d.hub.PublishStudent(owner.String, telemetry.Event{Type: "activity", Data: map[string]any{"title": dashboardEventTitle(event), "at": item.At}}); err != nil {
				return cursor, err
			}
			changed = true
		}
		cursor = item.ID
	}
	if err = rows.Err(); err != nil {
		return cursor, err
	}
	if changed {
		if err = d.hub.PublishCohort(telemetry.Event{Type: "rank", Data: map[string]bool{"refresh": true}}); err != nil {
			return cursor, err
		}
	}
	return cursor, nil
}
