package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/guide"
	lineuparrbuilder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
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
	check("waiting-for-evidence")
	s.tmdbEnriching = func() bool { return true }
	check("enriching")
	s.tmdbEnriching = func() bool { return false }
	c, _, _ := s.store.Get()
	s.state.UpdateForSource(&guide.TVGuide{Programs: []guide.Program{{Channel: "1", Start: "20260905000000 +0000", Stop: "20260905010000 +0000", TMDBGenresCaptured: true, TMDBMediaType: "tv", TMDBGenreIDs: []int{35}}}}, c.Fingerprint())
	check("ready")
}

func TestTMDBGuideRevisionIncludesOriginalLanguageEvidence(t *testing.T) {
	first := &guide.TVGuide{Programs: []guide.Program{{Channel: "1", Start: "20260907000000 +0000", Stop: "20260907010000 +0000", Title: "Example", OrigLanguage: "es"}}}
	revision, count := tmdbGuideRevision(first)
	if count != 1 || revision == "" {
		t.Fatalf("language-only revision = %q, %d", revision, count)
	}
	second := &guide.TVGuide{Programs: append([]guide.Program(nil), first.Programs...)}
	second.Programs[0].OrigLanguage = "en"
	secondRevision, _ := tmdbGuideRevision(second)
	if revision == secondRevision {
		t.Fatal("original-language change did not invalidate the category scan")
	}
}

func TestPreferTMDBLanguageHintAtEqualPriority(t *testing.T) {
	language := lineuparrbuilder.AttributedCategory{Priority: 3, Value: "International", Source: "tmdb-language-schedule"}
	genre := lineuparrbuilder.AttributedCategory{Priority: 4, Value: "Entertainment", Source: "tmdb-schedule"}
	schedule := &lineuparrbuilder.AttributedCategory{Priority: 3, Value: "Movies", Source: "gracenote-schedule"}
	provider := &lineuparrbuilder.AttributedCategory{Priority: 2, Value: "Movies", Source: "provider"}
	if !preferTMDBCategoryHint(nil, language) || !preferTMDBCategoryHint(schedule, language) {
		t.Fatal("validated language evidence was not accepted over an equal-priority content profile")
	}
	if preferTMDBCategoryHint(provider, language) || preferTMDBCategoryHint(schedule, genre) {
		t.Fatal("weaker TMDB evidence replaced stronger category evidence")
	}
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
			OrigLanguage: "en",
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
	if draft.Categorized != 1 || draft.Uncategorized != 0 || len(draft.Channels) != 1 || draft.Channels[0].Category != "Entertainment" || draft.Channels[0].NeedsCategoryReview {
		t.Fatalf("refreshed draft = %+v", draft)
	}
	if confirmed := s.builder.TMDBCategoryScan(c.Fingerprint()).IndependentCategories["100"]; len(confirmed) != 1 || confirmed[0] != "Entertainment" {
		t.Fatalf("100 percent independent genre evidence must confirm without changing priority: %v", confirmed)
	}
	for _, mutation := range []string{"unclassified overlap", "genre availability removed"} {
		t.Run(mutation, func(t *testing.T) {
			changed := append([]guide.Program(nil), programs...)
			if mutation == "unclassified overlap" {
				changed = append(changed, guide.Program{Channel: "100", Title: "Unclassified interval", Start: programs[10].Start, Stop: programs[10].Stop})
			} else {
				for i := range changed {
					changed[i].TMDBGenresCaptured = false
				}
			}
			g := &guide.TVGuide{Programs: changed, LineupChannels: []guide.Channel{{ID: "100", PlacementID: "1001", ChannelNo: "2", CallSign: "TEST"}}}
			revision, _ := tmdbGuideRevision(g)
			if revision == s.builder.TMDBCategoryScan(c.Fingerprint()).Revision {
				t.Fatal("assessment-relevant change did not invalidate persisted confirmation")
			}
			s.state.UpdateForSource(g, c.Fingerprint())
			w := httptest.NewRecorder()
			s.handleDraft(w, httptest.NewRequest(http.MethodGet, "/api/lineuparr/draft", nil))
			var current lineuparrbuilder.Draft
			if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &current) != nil || len(current.Channels) != 1 {
				t.Fatalf("draft: %d %s", w.Code, w.Body.String())
			}
			if current.Channels[0].Category != "Entertainment" || !current.Channels[0].NeedsCategoryReview {
				t.Fatalf("stale confirmation must not clear review: %+v", current.Channels[0])
			}
		})
	}
	t.Run("timezone provenance survives reload and detects later change", func(t *testing.T) {
		// The initial explicit scan resolved its timezone through discovery;
		// this index intentionally has no retained lineup record yet.
		scan := s.builder.TMDBCategoryScan(c.Fingerprint())
		if scan.Timezone != "America/New_York" || marketIndex.LineupTimezone(c.Gracenote.Country, c.Gracenote.PostalCode, c.Gracenote.LineupID, c.Gracenote.Device) != nil {
			t.Fatalf("discovered timezone was not saved independently: %+v", scan)
		}
		statePath := filepath.Join(t.TempDir(), "lineuparr.json")
		stateStore, err := lineuparrbuilder.LoadStateStore(statePath)
		if err != nil {
			t.Fatal(err)
		}
		persisted := lineuparrbuilder.NewService(stateStore, lineuparrbuilder.ServiceOptions{})
		if err := persisted.SaveTMDBCategoryScan(c.Fingerprint(), scan); err != nil {
			t.Fatal(err)
		}
		stateStore, err = lineuparrbuilder.LoadStateStore(statePath)
		if err != nil {
			t.Fatal(err)
		}
		s.builder = lineuparrbuilder.NewService(stateStore, lineuparrbuilder.ServiceOptions{})
		s.state.UpdateForSource(&guide.TVGuide{Programs: programs, LineupChannels: []guide.Channel{{ID: "100", PlacementID: "1001", ChannelNo: "2", CallSign: "TEST"}}}, c.Fingerprint())
		checkReview := func(want bool) {
			t.Helper()
			w := httptest.NewRecorder()
			s.handleDraft(w, httptest.NewRequest(http.MethodGet, "/api/lineuparr/draft", nil))
			var draft lineuparrbuilder.Draft
			if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &draft) != nil || len(draft.Channels) != 1 || draft.Channels[0].Category != "Entertainment" || draft.Channels[0].NeedsCategoryReview != want {
				t.Fatalf("timezone-bound review=%v: %d %s", want, w.Code, w.Body.String())
			}
		}
		checkReview(false)
		if got := s.builder.TMDBCategoryScan(c.Fingerprint()).Timezone; got != scan.Timezone {
			t.Fatalf("timezone lost after reload: %q", got)
		}
		// A legacy saved result without provenance cannot clear review even
		// though its programme hash and category support are otherwise current.
		legacy := scan
		legacy.Timezone = ""
		if err := s.builder.SaveTMDBCategoryScan(c.Fingerprint(), legacy); err != nil {
			t.Fatal(err)
		}
		checkReview(true)
		if err := s.builder.SaveTMDBCategoryScan(c.Fingerprint(), scan); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "changed-timezone.json")
		if err := os.WriteFile(path, []byte(`{"schemaVersion":4,"lineups":{"selected":{"country":"USA","postalCode":"11743","lineupId":"USA-TEST","device":"X","timezone":"America/Los_Angeles"}}}`), 0600); err != nil {
			t.Fatal(err)
		}
		s.marketIndex, err = lineupindex.NewService(lineupindex.ServiceConfig{Path: path, Providers: tmdbTimezoneProviders{}, Grids: tmdbUnusedGrid{}})
		if err != nil {
			t.Fatal(err)
		}
		checkReview(true)
		w := httptest.NewRecorder()
		s.handleTMDBCategories(w, httptest.NewRequest(http.MethodGet, "/api/lineuparr/tmdb-categories", nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"state":"ready"`) {
			t.Fatalf("timezone change did not request a fresh explicit scan: %d %s", w.Code, w.Body.String())
		}
	})
}

func TestRawScheduleConfirmationIndependentOfProposal(t *testing.T) {
	s := newLineuparrTestServer(t, true)
	path := filepath.Join(t.TempDir(), "index.json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion":4,"lineups":{"selected":{"country":"USA","postalCode":"11743","lineupId":"USA-TEST","device":"X","timezone":"America/New_York"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	index, err := lineupindex.NewService(lineupindex.ServiceConfig{
		Path: path, Providers: tmdbTimezoneProviders{}, Grids: tmdbUnusedGrid{},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.marketIndex = index
	c, _, _ := s.store.Get()
	if _, err := index.ResolveLineupTimezone(context.Background(), c.Gracenote.Country, c.Gracenote.PostalCode, c.Gracenote.LineupID, c.Gracenote.Device, c.Gracenote.Language); err != nil {
		t.Fatal(err)
	}
	loc := index.LineupTimezone(c.Gracenote.Country, c.Gracenote.PostalCode, c.Gracenote.LineupID, c.Gracenote.Device)
	start := time.Date(2026, time.September, 7, 0, 0, 0, 0, loc)
	for _, scenario := range []struct{ name, proposal, confirmed string }{
		{"different proposal", "Movies", "Entertainment"},
		{"no proposal", "", "News & Weather"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var programs []guide.Program
			for slot := 0; slot < 14*12; slot++ {
				a := start.Add(time.Duration(slot*2) * time.Hour)
				filters := []string{"filter-entertainment"}
				if slot%5 < 3 {
					filters = append(filters, "filter-movie")
				}
				if scenario.proposal == "" {
					filters = []string{"filter-news", "filter-sports"}
				}
				programs = append(programs, guide.Program{Channel: "100", Title: "Programme", Start: a.Format("20060102150405 -0700"), Stop: a.Add(2 * time.Hour).Format("20060102150405 -0700"), RawFilters: filters})
			}
			g := &guide.TVGuide{Programs: programs, LineupChannels: []guide.Channel{{ID: "100", PlacementID: "1001", CallSign: "UNBRANDEDTEST"}}}
			hint := s.weekdayCategoryHints(g, c)["100"]
			if hint == nil || hint.Value != scenario.proposal || hint.IndependentConfirmation || !containsCategory(hint.IndependentCategories, scenario.confirmed) {
				t.Fatalf("proposal and target support must remain separate: %+v", hint)
			}
			s.state.UpdateForSource(g, c.Fingerprint())
			_, inputs, ok := s.activeInputs(httptest.NewRecorder())
			if !ok || len(inputs) != 1 || !inputs[0].IndependentScheduleConfirmed || !containsCategory(inputs[0].IndependentCategories, scenario.confirmed) {
				t.Fatalf("independent support lost in draft inputs: %+v", inputs)
			}
			// Simulate the provider bridge selecting its supported category.
			inputs[0].CategoryHint = &lineuparrbuilder.AttributedCategory{Value: scenario.confirmed, Priority: 2, Source: "provider", Method: "independent provider majority"}
			inputs[0].CategoryConflict = true
			draft, err := s.builder.Build(context.Background(), lineuparrbuilder.LineupContext{SourceFingerprint: c.Fingerprint()}, inputs)
			if err != nil {
				t.Fatal(err)
			}
			if len(draft.Channels) != 1 || draft.Channels[0].Category != scenario.confirmed || draft.Channels[0].NeedsCategoryReview {
				t.Fatalf("selected provider target was not confirmed: %+v", draft)
			}
		})
	}
}

func TestTMDBCategoryScanProposesInternationalFromLanguageEvidence(t *testing.T) {
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
	start := time.Date(2026, time.September, 7, 4, 0, 0, 0, time.UTC)
	for hour := 0; hour < 14*24; hour++ {
		a := start.Add(time.Duration(hour) * time.Hour)
		language := "en"
		if hour%10 < 7 {
			language = "es"
		}
		programs = append(programs, guide.Program{
			Channel: "200", Start: a.Format("20060102150405 -0700"), Stop: a.Add(time.Hour).Format("20060102150405 -0700"),
			Title: fmt.Sprintf("Programme %d", hour%10), OrigLanguage: language,
		})
	}
	s.state.UpdateForSource(&guide.TVGuide{Programs: programs, LineupChannels: []guide.Channel{{ID: "200", PlacementID: "2001", ChannelNo: "20", CallSign: "UNKNOWN"}}}, c.Fingerprint())

	request := httptest.NewRequest(http.MethodPost, "/api/lineuparr/tmdb-categories", strings.NewReader("{}"))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	s.handleTMDBCategories(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("scan response = %d %s", recorder.Code, recorder.Body.String())
	}
	category := s.builder.TMDBCategoryScan(c.Fingerprint()).Categories["200"]
	if category.Value != "International" || category.Priority != 3 || category.Source != "tmdb-language-schedule" {
		t.Fatalf("saved language category = %+v", category)
	}
	draftRecorder := httptest.NewRecorder()
	s.handleDraft(draftRecorder, httptest.NewRequest(http.MethodGet, "/api/lineuparr/draft", nil))
	var draft struct {
		Channels []struct {
			Category            string `json:"category"`
			NeedsCategoryReview bool   `json:"needsCategoryReview"`
		} `json:"channels"`
	}
	if draftRecorder.Code != http.StatusOK {
		t.Fatalf("draft response = %d %s", draftRecorder.Code, draftRecorder.Body.String())
	}
	if err := json.Unmarshal(draftRecorder.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	if len(draft.Channels) != 1 || draft.Channels[0].Category != "International" || !draft.Channels[0].NeedsCategoryReview {
		t.Fatalf("language proposal draft = %+v", draft)
	}
}
