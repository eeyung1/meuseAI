package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLessonProgressionIsSelfPaced(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		t.Run(fmt.Sprint(postgres), func(t *testing.T) {
			database, h, mux := curriculumFixture(t, postgres)
			start := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
			now := start
			h.now = func() time.Time { return now }
			curriculumStudent(t, database, "self-paced", true, "day_1", "pending", start)
			if w := curriculumRequest(mux, "self-paced", 1, "first prompt and reflection"); w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			if w := curriculumRequest(mux, "self-paced", 2, "second prompt and reflection"); w.Code != 200 {
				t.Fatalf("immediate next lesson unavailable: %d %s", w.Code, w.Body.String())
			}
			var xp int
			_ = database.QueryRow("SELECT xp FROM students WHERE id='self-paced'").Scan(&xp)
			if xp != 550 {
				t.Fatalf("same-day momentum XP=%d", xp)
			}
			now = start.Add(10 * time.Minute)
			if w := curriculumRequest(mux, "self-paced", 3, "third prompt and reflection"); w.Code != 200 {
				t.Fatal(w.Body.String())
			}
		})
	}
}
func TestResumeRequiresSessionAndPreservesProgress(t *testing.T) {
	database, h, mux := curriculumFixture(t, false)
	now := time.Now().UTC()
	curriculumStudent(t, database, "paused", true, "day_1", "lapsed", now.Add(-100*time.Hour))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/curriculum/resume", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, request)
	if w.Code != 401 {
		t.Fatal("unsigned resume accepted")
	}
	h.WithAuth(func(*http.Request) (string, error) { return "paused", nil }, nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, request)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var xp int
	var state string
	if err := database.QueryRowContext(context.Background(), "SELECT xp,engagement_status FROM students WHERE id='paused'").Scan(&xp, &state); err != nil {
		t.Fatal(err)
	}
	if xp != 0 || state != "engaged" {
		t.Fatal("resume changed completion rewards")
	}
}
func TestDiscountedPaymentUsesAgreedPrice(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		t.Run(fmt.Sprint(postgres), func(t *testing.T) {
			d, _, mux := paymentFixture(t, postgres)
			if _, err := d.Exec("UPDATE students SET xp=1000 WHERE id='s1'"); err != nil {
				t.Fatal(err)
			}
			reference := paymentCheckout(t, mux, "tier_1")
			var amount int64
			if err := d.QueryRow("SELECT amount_kobo FROM payments WHERE reference=$1", reference).Scan(&amount); err != nil {
				t.Fatal(err)
			}
			if amount != 450000 {
				t.Fatalf("discounted price %d", amount)
			}
			// Changing XP after checkout cannot change the signed transaction's price.
			if _, err := d.Exec("UPDATE students SET xp=0 WHERE id='s1'"); err != nil {
				t.Fatal(err)
			}
			if w := paymentWebhook(mux, paymentEvent(reference, 500000)); w.Code != 400 {
				t.Fatal("wrong amount accepted")
			}
			if w := paymentWebhook(mux, paymentEvent(reference, 450000)); w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			if w := paymentWebhook(mux, paymentEvent(reference, 450000)); w.Code != 200 {
				t.Fatal("repeat webhook failed")
			}
			paymentAssertCount(t, d, "lovable_provisioning_requested", 1)
		})
	}
}

func TestRejectedShareCanBeCorrected(t *testing.T) {
	database, _, mux := curriculumFixture(t, false)
	curriculumStudent(t, database, "sharing", true, "day_2", "engaged", time.Now().UTC().Add(-24*time.Hour))
	if _, err := database.Exec("INSERT INTO student_progress(student_id,day_number,completed) VALUES('sharing',1,TRUE)"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("INSERT INTO social_shares(student_id,day_number,share_url,platform,status) VALUES('sharing',1,'https://example.com/old','social','rejected')"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/curriculum/share-link", strings.NewReader(`{"student_id":"sharing","day":1,"share_url":"https://example.com/new"}`))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var status, url string
	if err := database.QueryRow("SELECT status,share_url FROM social_shares WHERE student_id='sharing'").Scan(&status, &url); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || url != "https://example.com/new" {
		t.Fatal("corrected link not saved")
	}
}

func TestSocialShareAwards250Once(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		t.Run(fmt.Sprint(postgres), func(t *testing.T) {
			database, h, mux := curriculumFixture(t, postgres)
			h.WithAuth(nil, func(*http.Request) bool { return true })
			curriculumStudent(t, database, "share-reward", true, "day_1", "pending", time.Now())
			var id int64
			if err := database.QueryRow("INSERT INTO social_shares(student_id,day_number,share_url,platform,status) VALUES('share-reward',1,'https://example.com/post','social','pending') RETURNING id").Scan(&id); err != nil {
				t.Fatal(err)
			}
			review := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodPost, "/api/v1/admin/social-shares/review", strings.NewReader(fmt.Sprintf(`{"share_id":%d,"action":"approve"}`, id)))
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				return w
			}
			if w := review(); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			if w := review(); w.Code != 409 {
				t.Fatal("duplicate award accepted", w.Code)
			}
			var xp, awarded int
			if err := database.QueryRow("SELECT xp FROM students WHERE id='share-reward'").Scan(&xp); err != nil {
				t.Fatal(err)
			}
			if err := database.QueryRow("SELECT xp_awarded FROM social_shares WHERE id=$1", id).Scan(&awarded); err != nil {
				t.Fatal(err)
			}
			if xp != 250 || awarded != 250 {
				t.Fatalf("student XP=%d, share award=%d", xp, awarded)
			}
		})
	}
}

func TestShareTriggerAwards250InstantlyAndOnce(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		t.Run(fmt.Sprint(postgres), func(t *testing.T) {
			database, _, mux := curriculumFixture(t, postgres)
			curriculumStudent(t, database, "story-share", true, "day_2", "engaged", time.Now())
			if _, err := database.Exec("INSERT INTO student_progress(student_id,day_number,completed) VALUES('story-share',1,TRUE)"); err != nil {
				t.Fatal(err)
			}
			share := func(platform string) *httptest.ResponseRecorder {
				body := fmt.Sprintf(`{"student_id":"story-share","day":1,"share_url":"https://example.com/share/day-1","platform":%q,"share_triggered":true}`, platform)
				req := httptest.NewRequest(http.MethodPost, "/api/v1/curriculum/share-link", strings.NewReader(body))
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, req)
				return w
			}
			if w := share("email"); w.Code != http.StatusBadRequest {
				t.Fatalf("unsupported platform accepted: %d", w.Code)
			}
			for _, platform := range []string{"whatsapp", "facebook", "instagram"} {
				if w := share(platform); w.Code != http.StatusOK {
					t.Fatalf("%s share failed: %d %s", platform, w.Code, w.Body.String())
				}
			}
			var xp, awarded, rewards int
			var status string
			if err := database.QueryRow("SELECT xp FROM students WHERE id='story-share'").Scan(&xp); err != nil {
				t.Fatal(err)
			}
			if err := database.QueryRow("SELECT status,xp_awarded FROM social_shares WHERE student_id='story-share' AND day_number=1").Scan(&status, &awarded); err != nil {
				t.Fatal(err)
			}
			if err := database.QueryRow("SELECT COUNT(*) FROM activity_logs WHERE student_id='story-share' AND event_type='social_share_rewarded'").Scan(&rewards); err != nil {
				t.Fatal(err)
			}
			if xp != 250 || status != "approved" || awarded != 250 || rewards != 1 {
				t.Fatalf("xp=%d status=%s awarded=%d rewards=%d", xp, status, awarded, rewards)
			}
		})
	}
}
