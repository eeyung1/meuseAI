package static

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	"strings"
	"testing"
	"time"
)

func TestShowcasePublicationGate(t *testing.T) {
	raw, err := Assets.ReadFile("assets/showcase/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Version   int  `json:"version"`
		Published bool `json:"published"`
		Terms     struct {
			Status     string `json:"status"`
			ReviewedAt string `json:"reviewed_at"`
		} `json:"terms_review"`
		Entries []struct {
			ID          string            `json:"id"`
			Day         int               `json:"day"`
			Title       string            `json:"title"`
			Prompt      string            `json:"prompt"`
			Image       string            `json:"image"`
			Alt         string            `json:"alt"`
			Explanation string            `json:"explanation"`
			Status      string            `json:"status"`
			References  []string          `json:"references"`
			Model       string            `json:"model"`
			SHA256      string            `json:"sha256"`
			Attempts    []json.RawMessage `json:"attempts"`
		} `json:"entries"`
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != 1 || len(manifest.Entries) != 12 {
		t.Fatal("manifest must contain twelve version-1 examples")
	}
	if manifest.Published {
		if manifest.Terms.Status != "approved" {
			t.Fatal("terms review required before publication")
		}
		if _, err = time.Parse(time.RFC3339, manifest.Terms.ReviewedAt); err != nil {
			t.Fatal("record terms review date")
		}
	}
	seen := map[string]bool{}
	counts := map[int]int{}
	for _, e := range manifest.Entries {
		counts[e.Day]++
		expected := fmt.Sprintf("day%d-%d", e.Day, counts[e.Day])
		if e.Day < 1 || e.Day > 4 || e.ID != expected || seen[e.ID] || e.Image != "/assets/showcase/"+e.ID+".jpg" || e.Prompt == "" || e.Title == "" || e.Alt == "" || e.Explanation == "" {
			t.Fatalf("invalid example %s", e.ID)
		}
		for _, ref := range e.References {
			if !seen[ref] {
				t.Fatalf("%s references missing or later example %s", e.ID, ref)
			}
		}
		seen[e.ID] = true
		if manifest.Published && e.Status != "approved" {
			t.Fatalf("unapproved published example %s", e.ID)
		}
		if e.Status == "approved" {
			if e.Model == "" || len(e.Attempts) == 0 {
				t.Fatalf("record generation evidence for %s", e.ID)
			}
			data, err := Assets.ReadFile(strings.TrimPrefix(e.Image, "/"))
			if err != nil {
				t.Fatal(err)
			}
			if len(data) > 300*1024 {
				t.Fatalf("%s exceeds 300 KB", e.ID)
			}
			config, format, err := image.DecodeConfig(bytes.NewReader(data))
			if err != nil || format != "jpeg" || config.Width != 800 || config.Height != 600 {
				t.Fatalf("%s must be an 800x600 JPEG: %+v %s %v", e.ID, config, format, err)
			}
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != e.SHA256 {
				t.Fatalf("record selected image checksum for %s", e.ID)
			}
		}
	}
	for day := 1; day <= 4; day++ {
		if counts[day] != 3 {
			t.Fatalf("day %d requires three examples", day)
		}
	}
}
