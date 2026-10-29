package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestShowcaseEmbeddedRoutes(t *testing.T) {
	database, _, mux := dashboardFixture(t, false)
	showcase, err := NewShowcase(database, func(*http.Request) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	showcase.Register(mux)
	for _, path := range []string{"/assets/showcase.js", "/assets/showcase/manifest.json"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if path == "/assets/showcase/manifest.json" && (!strings.Contains(w.Body.String(), `"published":false`) || w.Header().Get("Cache-Control") != "no-store") {
			t.Fatal("unreviewed content unexpectedly published or cached")
		}
	}
	for _, path := range []string{"/assets/showcase/not-allowed.txt", "/assets/showcase/day1-1.jpg", "/assets/showcase/dashboard.js"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 404 {
			t.Fatalf("%s: expected 404, got %d", path, w.Code)
		}
	}
}
