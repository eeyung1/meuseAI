package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func showcaseFixture(t *testing.T, postgres bool) (*Showcase, *http.ServeMux) {
	t.Helper()
	database := curriculumDatabase(t, postgres)
	s, err := NewShowcase(database, func(r *http.Request) bool { return r.Header.Get("Authorization") == "Bearer showcase-admin" })
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	return s, mux
}
func showcaseRequest(mux http.Handler, method, path, body string, admin bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if admin {
		r.Header.Set("Authorization", "Bearer showcase-admin")
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}
func showcasePicture(t *testing.T) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 1200, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 1200; x++ {
			picture.SetRGBA(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 100, 255})
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, picture); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func showcaseUpload(t *testing.T, mux http.Handler, id string, raw []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("image", "example.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(raw)
	_ = form.WriteField("source", "ChatGPT")
	_ = form.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/admin/showcase/"+id+"/upload", &body)
	r.Header.Set("Authorization", "Bearer showcase-admin")
	r.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}
func showcaseHash(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("upload %d: %s", w.Code, w.Body.String())
	}
	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result["sha256"]
}
func approveShowcase(t *testing.T, mux http.Handler, id, hash string) *httptest.ResponseRecorder {
	t.Helper()
	return showcaseRequest(mux, "POST", "/api/v1/admin/showcase/"+id+"/review", fmt.Sprintf(`{"sha256":%q,"note":"Visually checked composition and prompt fidelity","rights_confirmed":true}`, hash), true)
}
func TestManualShowcaseLifecycle(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		t.Run(fmt.Sprint(postgres), func(t *testing.T) {
			s, mux := showcaseFixture(t, postgres)
			raw := showcasePicture(t)
			if w := showcaseRequest(mux, "GET", "/api/v1/admin/showcase", "", false); w.Code != 401 {
				t.Fatal("unauthenticated list accepted")
			}
			if w := showcaseRequest(mux, "POST", "/api/v1/admin/showcase/day1-1/upload", "", false); w.Code != 401 {
				t.Fatal("unauthenticated upload accepted")
			}
			hash := showcaseHash(t, showcaseUpload(t, mux, "day1-1", raw))
			preview := showcaseRequest(mux, "GET", "/api/v1/admin/showcase/day1-1/image", "", true)
			if preview.Code != 200 {
				t.Fatal(preview.Body.String())
			}
			config, format, err := image.DecodeConfig(bytes.NewReader(preview.Body.Bytes()))
			if err != nil || format != "jpeg" || config.Width != 800 || config.Height != 600 || preview.Body.Len() > showcaseJPEGMax {
				t.Fatal("invalid normalized output")
			}
			decoded, _, _ := image.Decode(bytes.NewReader(preview.Body.Bytes()))
			rr, gg, bb, _ := decoded.At(0, 0).RGBA()
			if rr < 60000 || gg < 60000 || bb < 60000 {
				t.Fatal("non-4:3 upload was cropped or stretched instead of padded")
			}
			if w := showcaseRequest(mux, "GET", "/api/v1/admin/showcase/day1-1/image", "", false); w.Code != 401 {
				t.Fatal("private draft preview leaked")
			}
			if w := showcaseRequest(mux, "GET", "/assets/showcase/day1-1.jpg", "", false); w.Code != 404 {
				t.Fatal("unpublished image leaked")
			}
			if w := approveShowcase(t, mux, "day1-1", strings.Repeat("0", 64)); w.Code != 409 {
				t.Fatal("stale approval accepted")
			}
			if w := showcaseRequest(mux, "POST", "/api/v1/admin/showcase/day1-1/review", fmt.Sprintf(`{"sha256":%q,"note":"Reviewed"}`, hash), true); w.Code != 400 {
				t.Fatal("rights confirmation omitted")
			}
			if w := approveShowcase(t, mux, "day1-1", hash); w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			publication := `{"published":true,"rights_confirmed":true,"note":"Checked source tool terms and image rights today."}`
			if w := showcaseRequest(mux, "POST", "/api/v1/admin/showcase/publish", publication, true); w.Code != 200 {
				t.Fatal("single approved image could not publish")
			}
			partial := showcaseRequest(mux, "GET", "/assets/showcase/manifest.json", "", false)
			var partialManifest struct {
				Entries []ShowcaseExample `json:"entries"`
			}
			if err := json.Unmarshal(partial.Body.Bytes(), &partialManifest); err != nil || len(partialManifest.Entries) != 1 || partialManifest.Entries[0].ID != "day1-1" {
				t.Fatal("partial publication did not expose exactly the approved image")
			}
			for _, e := range s.examples[1:] {
				h := showcaseHash(t, showcaseUpload(t, mux, e.ID, raw))
				if w := approveShowcase(t, mux, e.ID, h); w.Code != 200 {
					t.Fatalf("%s: %s", e.ID, w.Body.String())
				}
			}
			if w := showcaseRequest(mux, "POST", "/api/v1/admin/showcase/publish", publication, true); w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			public := showcaseRequest(mux, "GET", "/assets/showcase/manifest.json", "", false)
			var manifest struct {
				Published bool              `json:"published"`
				Entries   []ShowcaseExample `json:"entries"`
			}
			if err := json.Unmarshal(public.Body.Bytes(), &manifest); err != nil {
				t.Fatal(err)
			}
			if !manifest.Published || len(manifest.Entries) != 12 {
				t.Fatal("published manifest unavailable")
			}
			if strings.Contains(public.Body.String(), "Visually checked") {
				t.Fatal("private review note leaked")
			}
			if w := showcaseRequest(mux, "GET", "/assets/showcase/day1-1.jpg", "", false); w.Code != 200 {
				t.Fatal("published image missing")
			}
			if w := showcaseUpload(t, mux, "day1-1", raw); w.Code != 409 {
				t.Fatal("live image overwritten without unpublishing")
			}
			// A fresh handler sees durable uploads and publication state.
			restored, err := NewShowcase(s.database, s.admin)
			if err != nil {
				t.Fatal(err)
			}
			restoredMux := http.NewServeMux()
			restored.Register(restoredMux)
			if w := showcaseRequest(restoredMux, "GET", "/assets/showcase/day1-1.jpg", "", false); w.Code != 200 {
				t.Fatal("image not persisted")
			}
			if w := showcaseRequest(mux, "POST", "/api/v1/admin/showcase/publish", `{"published":false}`, true); w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			showcaseHash(t, showcaseUpload(t, mux, "day3-1", raw))
			for _, id := range []string{"day3-1", "day3-2", "day3-3"} {
				var approved bool
				if err := s.database.QueryRow("SELECT approved FROM showcase_images WHERE id=$1", id).Scan(&approved); err != nil {
					t.Fatal(err)
				}
				if approved {
					t.Fatalf("reference replacement did not invalidate %s", id)
				}
			}
			if w := showcaseRequest(mux, "POST", "/api/v1/admin/showcase/publish", publication, true); w.Code != 200 {
				t.Fatal("remaining approved images could not publish")
			}
			if w := showcaseRequest(mux, "GET", "/assets/showcase/day3-2.jpg", "", false); w.Code != 404 {
				t.Fatal("invalidated reference leaked")
			}
		})
	}
}
func TestManualShowcaseRejectsInvalidUploads(t *testing.T) {
	_, mux := showcaseFixture(t, false)
	for _, raw := range [][]byte{[]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), []byte("not an image")} {
		if w := showcaseUpload(t, mux, "day1-1", raw); w.Code != 422 {
			t.Fatalf("invalid image: %d", w.Code)
		}
	}
	if w := showcaseUpload(t, mux, "day1-1", bytes.Repeat([]byte("x"), showcaseUploadLimit+1)); w.Code != 413 {
		t.Fatalf("oversized upload: %d", w.Code)
	}
	if w := showcaseUpload(t, mux, "not-a-slot", showcasePicture(t)); w.Code != 404 {
		t.Fatal("unbounded image slots allowed")
	}
}
