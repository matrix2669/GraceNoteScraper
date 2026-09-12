package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/guide"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

func TestSoleAlignedCategoryRelationRemainsReviewableThroughBuilder(t *testing.T) {
	s := newLineuparrTestServer(t, true)
	idx := lineupindex.Index{SchemaVersion: lineupindex.CurrentIndexVersion,
		Stations:          map[string]*lineupindex.Station{"S1": {StationID: "S1", Names: []lineupindex.StationName{{Value: "UNBRANDEDTEST", Normalized: "UNBRANDEDTEST", Kind: lineupindex.NameCallSign}}}},
		CategoryRelations: []lineupindex.ProviderCategoryRelation{{StationID: "S1", AliasValue: "UNBRANDEDTEST", AliasNormalized: "UNBRANDEDTEST", Category: "News", SourceID: "directv-official-lineup", SourceLabel: "DIRECTV", SourceRowID: "row-1", LineupKeys: []string{"directv"}, Method: "unique provider-local channel number; number-policy-provider-alias-v3; provider-source-alignment-v1"}},
	}
	data, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "index.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s.marketIndex, err = lineupindex.NewService(lineupindex.ServiceConfig{Path: path, Providers: &fakeProviderFinder{response: &web.ProviderResponse{}}, Grids: fakeMarketGridFetcher{}})
	if err != nil {
		t.Fatal(err)
	}
	inputs := []builder.InputChannel{{StationID: "S1", Key: "S1", CallSign: "UNBRANDEDTEST"}}
	s.applyMarketAliases("USA", "11743", "directv-official-lineup", inputs)
	draft, err := s.builder.Build(context.Background(), builder.LineupContext{Country: "USA", SourceFingerprint: "test"}, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Channels) != 1 || draft.Channels[0].Category != "News & Weather" || !draft.Channels[0].NeedsCategoryReview {
		t.Fatalf("sole aligned relation cannot auto-confirm category: %+v", draft)
	}
}

func TestGuideEnrichmentCopyDoesNotMutatePublishedRawFilters(t *testing.T) {
	original := &guide.TVGuide{Programs: []guide.Program{{RawFilters: []string{"news"}}}}
	copy := copyGuideForTMDB(original)
	copy.Programs[0].RawFilters[0] = "movie"
	if original.Programs[0].RawFilters[0] != "news" {
		t.Fatal("background enrichment can mutate published independent evidence")
	}
}

func TestOfficialAdultCategoryUsesEvidenceQualityNotContentLabel(t *testing.T) {
	for _, tc := range []struct {
		name, method string
		priority     int
		review       bool
	}{
		{"clear official evidence", "exact provider identity; identity-policy-v2", 2, false},
		{"explicit low quality evidence", "exact provider identity; identity-policy-v2; priority-4", 4, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newLineuparrTestServer(t, true)
			idx := lineupindex.Index{SchemaVersion: lineupindex.CurrentIndexVersion,
				Stations:          map[string]*lineupindex.Station{"S1": {StationID: "S1", Names: []lineupindex.StationName{{Value: "UNBRANDEDTEST", Normalized: "UNBRANDEDTEST", Kind: lineupindex.NameCallSign}}, Facts: []lineupindex.StationFact{{Kind: lineupindex.FactCategory, Value: "Other", RawValue: "Adult", SourceID: "dish-official-lineup", Method: tc.method}}}},
				CategoryRelations: []lineupindex.ProviderCategoryRelation{{StationID: "S1", AliasValue: "UNBRANDEDTEST", AliasNormalized: "UNBRANDEDTEST", Category: "Other", RawCategory: "Adult", SourceID: "dish-official-lineup", SourceLabel: "DISH", SourceRowID: "row-1", Method: tc.method}},
			}
			data, err := json.Marshal(idx)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "index.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			s.marketIndex, err = lineupindex.NewService(lineupindex.ServiceConfig{Path: path, Providers: &fakeProviderFinder{response: &web.ProviderResponse{}}, Grids: fakeMarketGridFetcher{}})
			if err != nil {
				t.Fatal(err)
			}
			candidate := s.marketIndex.CategoriesForStations([]string{"S1"})["S1"]
			if candidate.Value != "Other" || candidate.Priority != tc.priority {
				t.Fatalf("candidate: %+v", candidate)
			}
			inputs := []builder.InputChannel{{StationID: "S1", Key: "S1", CallSign: "UNBRANDEDTEST"}}
			s.applyMarketAliases("USA", "11743", "dish-official-lineup", inputs)
			draft, err := s.builder.Build(context.Background(), builder.LineupContext{Country: "USA", SourceFingerprint: "test"}, inputs)
			if err != nil {
				t.Fatal(err)
			}
			if len(draft.Channels) != 1 || draft.Channels[0].Category != "Other" || draft.Channels[0].CategoryPriority != tc.priority || draft.Channels[0].NeedsCategoryReview != tc.review {
				t.Fatalf("draft: %+v", draft)
			}
		})
	}
}

// This exercises the complete persisted-index -> provider bridge -> maintained
// identity -> HTTP draft -> manual review path. A classifier-only test misses
// the priority-1 HLN identity that otherwise hides the provider disagreement.
func TestProviderDisagreementSurvivesMaintainedHLNIdentityAndManualReview(t *testing.T) {
	s := newLineuparrTestServer(t, true)
	config, _, _ := s.store.Get()
	s.state.UpdateForSource(&guide.TVGuide{LineupChannels: []guide.Channel{{
		ID: "64549", PlacementID: "hln397", ChannelNo: "397", CallSign: "HLNHD",
	}}}, config.Fingerprint())
	fact := func(source, category, extra string) lineupindex.StationFact {
		return lineupindex.StationFact{Kind: lineupindex.FactCategory, Value: category, RawValue: category,
			SourceID: source, SourceLabel: source, Method: "identity-policy-v2; number-policy-provider-v2; " + extra}
	}
	idx := lineupindex.Index{SchemaVersion: lineupindex.CurrentIndexVersion, Lineups: map[string]*lineupindex.LineupRecord{
		"selected": {Country: "USA", PostalCode: "11743", LineupID: "USA-TEST", Device: "X", Timezone: "America/New_York"},
	}, Stations: map[string]*lineupindex.Station{
		"64549": {StationID: "64549", Facts: []lineupindex.StationFact{
			fact("directv-official-lineup", "News & Weather", "exact provider identity"),
			fact("verizon-official-lineup", "News & Weather", "exact provider identity"),
			fact("spectrum-official-lineup", "Entertainment", "exact station ID"),
			fact("xfinity-official-lineup", "News & Weather", "category-quality-v1; priority-4"),
		}},
	}}
	path := filepath.Join(t.TempDir(), "index.json")
	data, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s.marketIndex, err = lineupindex.NewService(lineupindex.ServiceConfig{Path: path,
		Providers: &fakeProviderFinder{response: &web.ProviderResponse{}}, Grids: fakeMarketGridFetcher{}})
	if err != nil {
		t.Fatal(err)
	}
	unrecognized := []builder.InputChannel{{StationID: "64549", CallSign: "UNBRANDEDTEST"}}
	s.applyMarketAliases("USA", "11743", "spectrum-official-lineup", unrecognized)
	if hint := unrecognized[0].CategoryHint; hint == nil || hint.Value != "News & Weather" || !unrecognized[0].CategoryConflict {
		t.Fatalf("provider majority must reach an unknown channel as a reviewable proposal: %+v", unrecognized[0])
	}
	read := func() builder.DraftChannel {
		t.Helper()
		w := httptest.NewRecorder()
		s.handleDraft(w, httptest.NewRequest(http.MethodGet, "/api/lineuparr/draft", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("draft: %d %s", w.Code, w.Body.String())
		}
		var draft builder.Draft
		if err := json.Unmarshal(w.Body.Bytes(), &draft); err != nil {
			t.Fatal(err)
		}
		if len(draft.Channels) != 1 {
			t.Fatalf("channels: %+v", draft.Channels)
		}
		return draft.Channels[0]
	}
	channel := read()
	if !strings.Contains(channel.CategoryMethod, "News & Weather priority 2 = 2 provider") || !strings.Contains(channel.CategoryMethod, "Entertainment priority 2 = 1 provider") {
		t.Fatalf("actual review row must retain provider choices despite maintained identity: %s", channel.CategoryMethod)
	}
	if channel.Category != "News & Weather" || !channel.NeedsCategoryReview {
		t.Fatalf("HLN must keep the supported News proposal in review without independent schedule confirmation: %+v", channel)
	}
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.September, 7, 0, 0, 0, 0, location)
	for _, scenario := range []struct {
		name    string
		filters func(int) []string
		review  bool
	}{
		{"different category cannot confirm News", func(int) []string { return []string{"filter-movie"} }, true},
		{"exactly eighty percent remains reviewable", func(hour int) []string {
			if hour%5 != 0 {
				return []string{"filter-news"}
			}
			return nil
		}, true},
		{"over eighty percent confirms News", func(hour int) []string {
			if hour%6 != 0 {
				return []string{"filter-news"}
			}
			return nil
		}, false},
		{"legacy injected labels cannot confirm", func(int) []string { return nil }, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			programs := make([]guide.Program, 0, 14*24)
			weekdayHour := 0
			for hour := 0; hour < 14*24; hour++ {
				a := start.Add(time.Duration(hour) * time.Hour)
				programs = append(programs, guide.Program{Channel: "64549", Title: "Synthetic programme", Start: a.Format("20060102150405 -0700"), Stop: a.Add(time.Hour).Format("20060102150405 -0700"), RawFilters: scenario.filters(weekdayHour), Categories: []guide.Category{{Name: "News"}}})
				if a.Weekday() != time.Saturday && a.Weekday() != time.Sunday {
					weekdayHour++
				}
			}
			s.state.UpdateForSource(&guide.TVGuide{LineupChannels: []guide.Channel{{ID: "64549", PlacementID: "hln397", ChannelNo: "397", CallSign: "HLNHD"}}, Programs: programs}, config.Fingerprint())
			got := read()
			if got.Category != "News & Weather" || got.NeedsCategoryReview != scenario.review {
				t.Fatalf("confirmation must apply only to selected category using independent evidence: %+v", got)
			}
		})
	}
	request := httptest.NewRequest(http.MethodPost, "/api/lineuparr/approve-categories", strings.NewReader(
		`{"sourceFingerprint":"`+config.Fingerprint()+`","channels":[{"id":"hln397","category":"News & Weather"}]}`))
	request.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleApproveCategories(w, request)
	if w.Code != http.StatusOK {
		t.Fatalf("manual confirmation: %d %s", w.Code, w.Body.String())
	}
	channel = read()
	if channel.Category != "News & Weather" || channel.NeedsCategoryReview || channel.CategoryReview == nil {
		t.Fatalf("saved manual confirmation must survive provider disagreement: %+v", channel)
	}
}
