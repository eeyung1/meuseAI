package handlers

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/eeyung1/meuseAI/db"
	assets "github.com/eeyung1/meuseAI/static"
)

const showcaseUploadLimit = 8 << 20
const showcaseJPEGMax = 300 << 10

type ShowcaseExample struct {
	ID          string   `json:"id"`
	Day         int      `json:"day"`
	Title       string   `json:"title"`
	Prompt      string   `json:"prompt"`
	Image       string   `json:"image"`
	Alt         string   `json:"alt"`
	Explanation string   `json:"explanation"`
	References  []string `json:"references"`
	Status      string   `json:"status"`
	Source      string   `json:"source,omitempty"`
	SHA256      string   `json:"sha256,omitempty"`
	ReviewNote  string   `json:"review_note,omitempty"`
	HasImage    bool     `json:"has_image"`
}
type Showcase struct {
	database    *db.DB
	admin       func(*http.Request) bool
	examples    []ShowcaseExample
	uploadSlots chan struct{}
}

func NewShowcase(database *db.DB, admin func(*http.Request) bool) (*Showcase, error) {
	raw, err := assets.Assets.ReadFile("assets/showcase/manifest.json")
	if err != nil {
		return nil, err
	}
	var manifest struct {
		Entries []ShowcaseExample `json:"entries"`
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		return nil, err
	}
	if database == nil || admin == nil || len(manifest.Entries) != 12 {
		return nil, errors.New("showcase configuration unavailable")
	}
	return &Showcase{database: database, admin: admin, examples: manifest.Entries, uploadSlots: make(chan struct{}, 1)}, nil
}
func (s *Showcase) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/showcase", s.List)
	mux.HandleFunc("POST /api/v1/admin/showcase/{id}/upload", s.Upload)
	mux.HandleFunc("GET /api/v1/admin/showcase/{id}/image", s.Preview)
	mux.HandleFunc("POST /api/v1/admin/showcase/{id}/review", s.Review)
	mux.HandleFunc("POST /api/v1/admin/showcase/publish", s.Publish)
	mux.HandleFunc("GET /assets/showcase/{file}", s.PublicAsset)
}
func (s *Showcase) authorized(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Cache-Control", "no-store")
	if !s.admin(r) {
		dashboardJSON(w, 401, map[string]string{"error": "admin authorization required"})
		return false
	}
	return true
}
func (s *Showcase) example(id string) (ShowcaseExample, bool) {
	for _, e := range s.examples {
		if e.ID == id {
			return e, true
		}
	}
	return ShowcaseExample{}, false
}
func showcaseFail(w http.ResponseWriter) {
	dashboardJSON(w, 503, map[string]string{"error": "gallery storage unavailable; please retry"})
}
func (s *Showcase) snapshot(r *http.Request) ([]ShowcaseExample, bool, error) {
	tx, err := s.database.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	var published bool
	if err = tx.QueryRowContext(r.Context(), "SELECT published FROM showcase_settings WHERE id=1").Scan(&published); err != nil {
		return nil, false, err
	}
	examples := append([]ShowcaseExample(nil), s.examples...)
	for i := range examples {
		examples[i].Status = "pending"
		examples[i].Source = ""
		examples[i].SHA256 = ""
		examples[i].ReviewNote = ""
		examples[i].HasImage = false
	}
	rows, err := tx.QueryContext(r.Context(), "SELECT id,sha256,source,approved,review_note FROM showcase_images")
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, hash, source, note string
		var approved bool
		if err = rows.Scan(&id, &hash, &source, &approved, &note); err != nil {
			return nil, false, err
		}
		for i := range examples {
			if examples[i].ID == id {
				e := &examples[i]
				e.HasImage = true
				e.SHA256 = hash
				e.Source = source
				e.ReviewNote = note
				e.Status = "uploaded"
				if approved {
					e.Status = "approved"
				}
				break
			}
		}
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	return examples, published, nil
}
func (s *Showcase) List(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(w, r) {
		return
	}
	entries, published, err := s.snapshot(r)
	if err != nil {
		showcaseFail(w)
		return
	}
	dashboardJSON(w, 200, map[string]any{"published": published, "entries": entries})
}

// Every write locks the publication row first, so uploads, reviews and publish
// cannot interleave to expose an incomplete gallery across server instances.
func (s *Showcase) writeTx(r *http.Request) (*sql.Tx, bool, error) {
	tx, err := s.database.BeginTx(r.Context(), nil)
	if err != nil {
		return nil, false, err
	}
	if _, err = tx.ExecContext(r.Context(), "UPDATE showcase_settings SET published=published WHERE id=1"); err != nil {
		tx.Rollback()
		return nil, false, err
	}
	var published bool
	if err = tx.QueryRowContext(r.Context(), "SELECT published FROM showcase_settings WHERE id=1").Scan(&published); err != nil {
		tx.Rollback()
		return nil, false, err
	}
	return tx, published, nil
}
func (s *Showcase) Upload(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(w, r) {
		return
	}
	select {
	case s.uploadSlots <- struct{}{}:
		defer func() { <-s.uploadSlots }()
	default:
		dashboardJSON(w, 429, map[string]string{"error": "another upload is processing; retry shortly"})
		return
	}
	id := r.PathValue("id")
	if _, ok := s.example(id); !ok {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, showcaseUploadLimit+(64<<10))
	if err := r.ParseMultipartForm(showcaseUploadLimit); err != nil {
		dashboardJSON(w, 413, map[string]string{"error": "upload must be a JPEG or PNG up to 8 MB"})
		return
	}
	defer r.MultipartForm.RemoveAll()
	source := r.FormValue("source")
	if source != "Gemini" && source != "ChatGPT" && source != "Other" {
		dashboardJSON(w, 400, map[string]string{"error": "choose the image generation source"})
		return
	}
	file, _, err := r.FormFile("image")
	if err != nil {
		dashboardJSON(w, 400, map[string]string{"error": "select an image"})
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, showcaseUploadLimit+1))
	if err != nil || len(raw) > showcaseUploadLimit {
		dashboardJSON(w, 413, map[string]string{"error": "image exceeds 8 MB"})
		return
	}
	encoded, err := showcaseJPEG(raw)
	if err != nil {
		dashboardJSON(w, 422, map[string]string{"error": err.Error()})
		return
	}
	sum := sha256.Sum256(encoded)
	hash := hex.EncodeToString(sum[:])
	tx, published, err := s.writeTx(r)
	if err != nil {
		showcaseFail(w)
		return
	}
	defer tx.Rollback()
	var exists bool
	if err = tx.QueryRowContext(r.Context(), "SELECT EXISTS(SELECT 1 FROM showcase_images WHERE id=$1)", id).Scan(&exists); err != nil {
		showcaseFail(w)
		return
	}
	if published && exists {
		dashboardJSON(w, 409, map[string]string{"error": "unpublish the gallery before replacing an image"})
		return
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO showcase_images(id,content,sha256,source,approved,review_note,uploaded_at)
 VALUES($1,$2,$3,$4,FALSE,'',$5) ON CONFLICT(id) DO UPDATE SET content=excluded.content,sha256=excluded.sha256,source=excluded.source,approved=FALSE,review_note='',uploaded_at=excluded.uploaded_at,reviewed_at=NULL`, id, encoded, hash, source, time.Now().UTC())
	if err != nil {
		showcaseFail(w)
		return
	}
	// Replacing a reference invalidates approval of every dependent example.
	changed := map[string]bool{id: true}
	for _, e := range s.examples {
		for _, reference := range e.References {
			if changed[reference] {
				changed[e.ID] = true
			}
		}
	}
	for dependent := range changed {
		if dependent != id {
			if _, err = tx.ExecContext(r.Context(), "UPDATE showcase_images SET approved=FALSE,review_note='',reviewed_at=NULL WHERE id=$1", dependent); err != nil {
				showcaseFail(w)
				return
			}
		}
	}
	if err = tx.Commit(); err != nil {
		showcaseFail(w)
		return
	}
	dashboardJSON(w, 200, map[string]string{"status": "uploaded", "sha256": hash})
}
func showcaseJPEG(raw []byte) ([]byte, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "jpeg" && format != "png") {
		return nil, errors.New("only genuine JPEG and PNG images are accepted")
	}
	if cfg.Width < 800 || cfg.Height < 600 || cfg.Width > 8192 || cfg.Height > 8192 || int64(cfg.Width)*int64(cfg.Height) > 16_000_000 {
		return nil, errors.New("image must be at least 800×600 pixels and at most 16 megapixels (8192 pixels per side)")
	}
	source, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("image could not be decoded")
	}
	width, height := 800, 600
	if cfg.Width*600 > cfg.Height*800 {
		height = cfg.Height * 800 / cfg.Width
	} else {
		width = cfg.Width * 600 / cfg.Height
	}
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 800, 600))
	for i := range canvas.Pix {
		canvas.Pix[i] = 255
	}
	offsetX, offsetY := (800-width)/2, (600-height)/2
	// Area averaging handles high-resolution uploads without a new dependency.
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			x0, x1 := x*cfg.Width/width, (x+1)*cfg.Width/width
			y0, y1 := y*cfg.Height/height, (y+1)*cfg.Height/height
			if x1 <= x0 {
				x1 = x0 + 1
			}
			if y1 <= y0 {
				y1 = y0 + 1
			}
			var red, green, blue, count uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					r, g, b, a := source.At(source.Bounds().Min.X+sx, source.Bounds().Min.Y+sy).RGBA()
					red += uint64(r + 65535 - a)
					green += uint64(g + 65535 - a)
					blue += uint64(b + 65535 - a)
					count++
				}
			}
			canvas.SetRGBA(x+offsetX, y+offsetY, color.RGBA{uint8(red / count >> 8), uint8(green / count >> 8), uint8(blue / count >> 8), 255})
		}
	}
	for _, quality := range []int{88, 82, 76, 68} {
		var output bytes.Buffer
		if err = jpeg.Encode(&output, canvas, &jpeg.Options{Quality: quality}); err != nil {
			return nil, errors.New("image conversion failed")
		}
		if output.Len() <= showcaseJPEGMax {
			return output.Bytes(), nil
		}
	}
	return nil, errors.New("image is too complex to compress below 300 KB; try a smaller or simpler export")
}
func (s *Showcase) Preview(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(w, r) {
		return
	}
	s.serveImage(w, r, r.PathValue("id"), false)
}
func (s *Showcase) serveImage(w http.ResponseWriter, r *http.Request, id string, public bool) {
	if _, ok := s.example(id); !ok {
		http.NotFound(w, r)
		return
	}
	query := "SELECT content FROM showcase_images WHERE id=$1"
	if public {
		query += " AND approved=TRUE AND EXISTS(SELECT 1 FROM showcase_settings WHERE id=1 AND published=TRUE)"
	}
	var data []byte
	err := s.database.QueryRowContext(r.Context(), query, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		showcaseFail(w)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}
func (s *Showcase) Review(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(w, r) {
		return
	}
	id := r.PathValue("id")
	example, ok := s.example(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var input struct {
		SHA256          string `json:"sha256"`
		Note            string `json:"note"`
		RightsConfirmed bool   `json:"rights_confirmed"`
	}
	if paymentDecode(w, r, &input) != nil || len(strings.TrimSpace(input.Note)) < 5 || len(input.Note) > 1000 || !input.RightsConfirmed {
		dashboardJSON(w, 400, map[string]string{"error": "record a visual-review note and confirm image rights"})
		return
	}
	tx, _, err := s.writeTx(r)
	if err != nil {
		showcaseFail(w)
		return
	}
	defer tx.Rollback()
	for _, reference := range example.References {
		var approved bool
		if err = tx.QueryRowContext(r.Context(), "SELECT approved FROM showcase_images WHERE id=$1", reference).Scan(&approved); err != nil || !approved {
			dashboardJSON(w, 409, map[string]string{"error": "approve the reference images first"})
			return
		}
	}
	result, err := tx.ExecContext(r.Context(), "UPDATE showcase_images SET approved=TRUE,review_note=$1,reviewed_at=$2 WHERE id=$3 AND sha256=$4", strings.TrimSpace(input.Note), time.Now().UTC(), id, input.SHA256)
	if err != nil {
		showcaseFail(w)
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		showcaseFail(w)
		return
	}
	if count != 1 {
		dashboardJSON(w, 409, map[string]string{"error": "image changed or is missing; refresh before approval"})
		return
	}
	if err = tx.Commit(); err != nil {
		showcaseFail(w)
		return
	}
	dashboardJSON(w, 200, map[string]string{"status": "approved"})
}
func (s *Showcase) Publish(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(w, r) {
		return
	}
	var input struct {
		Published       bool   `json:"published"`
		RightsConfirmed bool   `json:"rights_confirmed"`
		Note            string `json:"note"`
	}
	if paymentDecode(w, r, &input) != nil {
		dashboardJSON(w, 400, map[string]string{"error": "invalid publication request"})
		return
	}
	tx, _, err := s.writeTx(r)
	if err != nil {
		showcaseFail(w)
		return
	}
	defer tx.Rollback()
	if input.Published {
		if !input.RightsConfirmed || len(strings.TrimSpace(input.Note)) < 10 || len(input.Note) > 1000 {
			dashboardJSON(w, 400, map[string]string{"error": "confirm the usage rights and record your terms-review note"})
			return
		}
		var count int
		if err = tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM showcase_images WHERE approved=TRUE").Scan(&count); err != nil {
			showcaseFail(w)
			return
		}
		if count == 0 {
			dashboardJSON(w, 409, map[string]string{"error": "upload and approve at least one image before publishing"})
			return
		}
	}
	if _, err = tx.ExecContext(r.Context(), "UPDATE showcase_settings SET published=$1,rights_note=$2,published_at=$3 WHERE id=1", input.Published, strings.TrimSpace(input.Note), time.Now().UTC()); err != nil {
		showcaseFail(w)
		return
	}
	if err = tx.Commit(); err != nil {
		showcaseFail(w)
		return
	}
	dashboardJSON(w, 200, map[string]bool{"published": input.Published})
}
func (s *Showcase) PublicAsset(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	if file == "manifest.json" {
		entries, published, err := s.snapshot(r)
		if err != nil {
			showcaseFail(w)
			return
		}
		visible := make([]ShowcaseExample, 0, len(entries))
		if published {
			for _, entry := range entries {
				if entry.Status == "approved" && entry.HasImage {
					entry.ReviewNote = ""
					visible = append(visible, entry)
				}
			}
		}
		entries = visible
		dashboardJSON(w, 200, map[string]any{"version": 1, "published": published, "provenance": "Manually uploaded AI-generated course examples; not student work.", "entries": entries})
		return
	}
	if !strings.HasSuffix(file, ".jpg") {
		http.NotFound(w, r)
		return
	}
	s.serveImage(w, r, strings.TrimSuffix(file, ".jpg"), true)
}
