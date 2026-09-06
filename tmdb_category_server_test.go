package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/guide"
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

type tmdbTimezoneProviders struct{}

func (tmdbTimezoneProviders) FindProviders(_ context.Context, _, postalCode, _ string) (*web.ProviderResponse, error) {
	return &web.ProviderResponse{
		StdUTCOffset: "-300", DSTUTCOffset: "-240",
		Providers: []web.Provider{{LineupID: "USA-TEST", Device: "X", PostalCode: postalCode}},
	}, nil
}

type tmdbUnusedGrid struct{}

func (tmdbUnusedGrid) FetchGrid(context.Context, web.Preferences, int64) (*web.GridResponse, error) {
	return nil, nil
}

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

func TestLegacyGuideGenresReusedWithoutMutatingGuide(t *testing.T) {
	calls := 0
	s := &lineuparrServer{tmdbCachedEvidence: func(title string, movie bool, id int) ([]int, []string, bool) {
		calls++
		if title != "test & show" || movie || id != 42 {
			t.Fatalf("unexpected identity %q %v %d", title, movie, id)
		}
		return nil, []string{"Comedy"}, true
	}}
	g := &guide.TVGuide{Programs: []guide.Program{{Title: "Test &amp; Show", EpisodeNumbers: []guide.EpisodeNumber{{System: "themoviedb.org", EpisodeNumber: "series/42"}}}, {Title: "Unmatched"}}}
	adapted := s.categoryEvidenceGuide(g)
	if calls != 1 || !adapted.Programs[0].TMDBGenresCaptured || g.Programs[0].TMDBGenresCaptured {
		t.Fatal("missing evidence or mutated published guide")
	}
	if got := tmdbGenreFilters(adapted.Programs[0]); len(got) != 1 || got[0] != "entertainment" {
		t.Fatal(got)
	}
	if adapted.Programs[1].TMDBGenresCaptured {
		t.Fatal("title-only programme inferred")
	}
	_, count := tmdbGuideRevision(adapted)
	if count != 1 {
		t.Fatal(count)
	}
}

func TestTMDBCategoryScanRepairsLegacyTimezoneAndSavesProposals(t *testing.T) {
	s := newLineuparrTestServer(t, true)
	s.tmdbConfigured = true
	marketIndex, err := lineupindex.NewService(lineupindex.ServiceConfig{
		Path: filepath.Join(t.TempDir(), "market_index.json"), Providers: tmdbTimezoneProviders{}, Grids: tmdbUnusedGrid{},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.marketIndex = marketIndex
	c, _, _ := s.store.Get()
	programs := make([]guide.Program, 0, 14*24)
	start := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)
	for hour := 0; hour < 14*24; hour++ {
		a := start.Add(time.Duration(hour) * time.Hour)
		programs = append(programs, guide.Program{
			Channel: "100", Start: a.Format("20060102150405 -0700"), Stop: a.Add(time.Hour).Format("20060102150405 -0700"), Title: "Entertainment programme",
			TMDBGenresCaptured: true, TMDBMediaType: "tv", TMDBGenreIDs: []int{35}, TMDBGenreNames: []string{"Comedy"},
		})
	}
	s.state.UpdateForSource(&guide.TVGuide{Programs: programs, LineupChannels: []guide.Channel{{ID: "100", PlacementID: "1001", ChannelNo: "2", CallSign: "TEST"}}}, c.Fingerprint())

	request := httptest.NewRequest(http.MethodPost, "/api/lineuparr/tmdb-categories", strings.NewReader("{}"))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	s.handleTMDBCategories(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("scan response = %d %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		State         string `json:"state"`
		CategoryCount int    `json:"categoryCount"`
		Message       string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.State != "current" || response.CategoryCount != 1 || !strings.Contains(response.Message, "1 provisional channel categories") {
		t.Fatalf("scan response = %+v", response)
	}
	if category := s.builder.TMDBCategoryScan(c.Fingerprint()).Categories["100"]; category.Value != "Entertainment" || category.Priority != 4 {
		t.Fatalf("saved category = %+v", category)
	}
	draftRecorder := httptest.NewRecorder()
	s.handleDraft(draftRecorder, httptest.NewRequest(http.MethodGet, "/api/lineuparr/draft", nil))
	if draftRecorder.Code != http.StatusOK {
		t.Fatalf("draft response = %d %s", draftRecorder.Code, draftRecorder.Body.String())
	}
	var draft struct {
		Categorized   int `json:"categorized"`
		Uncategorized int `json:"uncategorized"`
		Channels      []struct {
			Category            string `json:"category"`
			NeedsCategoryReview bool   `json:"needsCategoryReview"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(draftRecorder.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	if draft.Categorized != 1 || draft.Uncategorized != 0 || len(draft.Channels) != 1 || draft.Channels[0].Category != "Entertainment" || !draft.Channels[0].NeedsCategoryReview {
		t.Fatalf("refreshed draft = %+v", draft)
	}
}
