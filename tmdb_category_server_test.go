package main

import (
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTMDBCategoryStatusStates(t *testing.T) {
	s := newLineuparrTestServer(t, true)
	check := func(want string) {
		t.Helper()
		w := httptest.NewRecorder()
		s.handleTMDBCategories(w, httptest.NewRequest("GET", "/api/lineuparr/tmdb-categories", nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), want) {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	check("TMDB_TOKEN")
	s.tmdbConfigured = true
	check("waiting-for-genres")
	s.tmdbEnriching = func() bool { return true }
	check("enriching")
	s.tmdbEnriching = func() bool { return false }
	c, _, _ := s.store.Get()
	s.state.UpdateForSource(&guide.TVGuide{Programs: []guide.Program{{Channel: "1", Start: "20260905000000 +0000", Stop: "20260905010000 +0000", TMDBGenresCaptured: true, TMDBMediaType: "tv", TMDBGenreIDs: []int{35}}}}, c.Fingerprint())
	check("ready")
}

func TestTMDBGenreEvidenceIsOptional(t *testing.T) {
	p := guide.Program{TMDBMediaType: "tv", TMDBGenreIDs: []int{35}}
	if len(tmdbGenreFilters(p)) != 0 {
		t.Fatal("legacy genre availability inferred")
	}
	p.TMDBGenresCaptured = true
	if got := tmdbGenreFilters(p); len(got) != 1 || got[0] != "entertainment" {
		t.Fatal(got)
	}
	p.TMDBGenreIDs = []int{16, 10767}
	if len(tmdbGenreFilters(p)) != 0 {
		t.Fatal("animation or talk forced into Kids or Entertainment")
	}
}
