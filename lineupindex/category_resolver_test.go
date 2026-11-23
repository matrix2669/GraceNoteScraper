package lineupindex

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/channelcategory"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

func categoryResolverService(t *testing.T) *Service {
	t.Helper()
	s, err := NewService(ServiceConfig{Path: filepath.Join(t.TempDir(), "index.json"), Providers: &fakeProviders{responses: map[string][]web.Provider{}}, Grids: &fakeGrids{responses: map[string]*web.GridResponse{}, failures: map[string]int{}, calls: map[string]int{}}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCategoriesForStationsUsesDistinctRootProviderVotesAndStrongestTier(t *testing.T) {
	s := categoryResolverService(t)
	s.mu.Lock()
	s.index.Lineups = map[string]*LineupRecord{
		"d1": {ProviderName: "DIRECTV", Device: "A"}, "d2": {ProviderName: "DIRECTV", Device: "B"},
		"v1": {ProviderName: "Verizon FiOS", Device: "A"}, "sp": {ProviderName: "Spectrum", Device: "A"},
		"xf": {ProviderName: "Xfinity", Device: "A"},
	}
	s.index.Stations["S1"] = &Station{StationID: "S1", Facts: []StationFact{
		{Kind: FactCategory, Value: "News", SourceID: "directv-official-lineup", LineupKeys: []string{"d1"}},
		{Kind: FactCategory, Value: "News", SourceID: "directv-official-lineup", LineupKeys: []string{"d2"}},
		{Kind: FactCategory, Value: "News", SourceID: "verizon-official-lineup", LineupKeys: []string{"v1"}},
		{Kind: FactCategory, Value: "Entertainment", SourceID: "spectrum-official-lineup", LineupKeys: []string{"sp"}},
		{Kind: FactCategory, Value: "News", SourceID: "xfinity-official-lineup", Method: "category-quality-v1; priority-4", LineupKeys: []string{"xf"}},
	}}
	s.mu.Unlock()
	candidate, ok := s.CategoriesForStations([]string{"S1"})["S1"]
	if !ok || candidate.Value != channelcategory.NewsWeather || candidate.Priority != 2 {
		t.Fatalf("candidate = %+v, %v", candidate, ok)
	}
	joined := strings.Join(candidate.Methods, " | ")
	if !strings.Contains(joined, "News & Weather = [0, 2, 0, 1]") || !strings.Contains(joined, "Entertainment = [0, 1, 0, 0]") || !strings.Contains(joined, "provider disagreement") {
		t.Fatalf("candidate methods omit independent choices/review reason: %s", joined)
	}
	if len(candidate.SourceIDs) != 3 {
		t.Fatalf("source evidence was not retained: %v", candidate.SourceIDs)
	}
}

func TestCategoriesForStationsConsumesBoundProviderCategoryRelations(t *testing.T) {
	s := categoryResolverService(t)
	s.mu.Lock()
	s.index.Stations["S1"] = &Station{StationID: "S1", Names: []StationName{{Value: "NEWSHD", Normalized: normalizeName("NEWSHD"), Kind: NameCallSign}}}
	s.index.CategoryRelations = []ProviderCategoryRelation{{StationID: "S1", AliasValue: "NEWSHD", AliasNormalized: normalizeName("NEWSHD"), Category: "News", RawCategory: "News & Info", SourceID: "directv-official-lineup", SourceLabel: "DIRECTV", SourceRowID: "row-1", LineupKeys: []string{"directv"}, Method: "unique provider-local channel number; number-policy-provider-alias-v3; provider-source-alignment-v1"}}
	s.mu.Unlock()
	candidate, ok := s.CategoriesForStations([]string{"S1"})["S1"]
	if !ok || candidate.Value != channelcategory.NewsWeather || !strings.Contains(strings.Join(candidate.Methods, " "), "provider-category-relation") || !strings.Contains(strings.Join(candidate.Methods, " "), "requires review") {
		t.Fatalf("relation candidate = %+v, %v", candidate, ok)
	}
}

func TestCategoriesForStationsTieUsesTaxonomyOrderAndSelfConflictIsReviewable(t *testing.T) {
	s := categoryResolverService(t)
	s.mu.Lock()
	s.index.Stations["S1"] = &Station{StationID: "S1", Facts: []StationFact{
		{Kind: FactCategory, Value: "Sports", SourceID: "provider-one"},
		{Kind: FactCategory, Value: "News", SourceID: "provider-two"},
	}}
	s.index.Stations["S2"] = &Station{StationID: "S2", Facts: []StationFact{
		{Kind: FactCategory, Value: "Sports", SourceID: "provider-one"},
		{Kind: FactCategory, Value: "News", SourceID: "provider-one"},
	}}
	s.index.Stations["S3"] = &Station{StationID: "S3", Facts: []StationFact{
		{Kind: FactCategory, Value: "News", SourceID: "provider-one"},
		{Kind: FactCategory, Value: "Entertainment", SourceID: "provider-two"},
		{Kind: FactCategory, Value: "Entertainment", SourceID: "xfinity-official-lineup", Method: "category-quality-v1; priority-4"},
	}}
	s.mu.Unlock()
	candidates := s.CategoriesForStations([]string{"S1", "S2", "S3"})
	if candidates["S1"].Value != channelcategory.NewsWeather || !strings.Contains(strings.Join(candidates["S1"].Methods, " "), "provider disagreement") {
		t.Fatalf("taxonomy tie = %+v", candidates["S1"])
	}
	if candidates["S2"].Value != channelcategory.NewsWeather || !strings.Contains(strings.Join(candidates["S2"].Methods, " "), "abstained") {
		t.Fatalf("self-conflict = %+v", candidates["S2"])
	}
	if candidates["S3"].Value != channelcategory.Entertainment {
		t.Fatalf("lexicographic vector tie-break = %+v", candidates["S3"])
	}
}
