package providersource

import (
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
	"strings"
	"testing"
)

func TestProviderOwnGridUsesUniqueNumberForAliasesOnly(t *testing.T) {
	request := lineupindex.ProviderEvidenceRequest{Grid: &web.GridResponse{Channels: []web.JSONChannel{
		{ChannelID: "ONE", ChannelNo: "1", CallSign: "MUSIC"}, {ChannelID: "TWO", ChannelNo: "2", CallSign: "MUSIC"},
		{ChannelID: "ANCHOR", ChannelNo: "3", CallSign: "ESPN"},
	}}}
	source := catalogSource{ID: "fixture", Entries: []catalogEntry{
		{Numbers: []string{"1"}, Name: "MUSIC", Category: "Music"},
		{Numbers: []string{"3"}, Name: "ESPN"},
	}}
	if got := factsForStation(matchCatalog(request, source).Facts, "ONE"); len(got) != 0 {
		t.Fatal("number narrowed ambiguous competing-provider identity", got)
	}
	request.AllowChannelNumbers = true
	if got := factsForStation(matchCatalog(request, source).Facts, "ONE"); len(got) == 0 {
		t.Fatal("provider cannot use corroborated number against its own grid")
	}
	request.Grid.Channels[0].CallSign = "UNRELATED"
	got := factsForStation(matchCatalog(request, source).Facts, "ONE")
	if len(got) != 1 || got[0].Kind != lineupindex.FactAlias || got[0].Value != "MUSIC" || !strings.Contains(got[0].Method, lineupindex.ProviderSourceAlignmentV1) {
		t.Fatal("unique provider-local number did not recover only the aligned alias", got)
	}
}

func TestSameNumberAcrossProviderGridsDoesNotTransferCategories(t *testing.T) {
	source := catalogSource{ID: "verizon-fixture", Entries: []catalogEntry{
		{Numbers: []string{"50"}, Name: "USA", Category: "Entertainment"},
		{Numbers: []string{"206"}, Name: "ESPN"},
	}}
	for _, tc := range []struct {
		station, name string
		wantCategory  bool
	}{{"FIOS-USA", "USA", true}, {"OTHER-ID", "Investigation Discovery", false}} {
		request := lineupindex.ProviderEvidenceRequest{AllowChannelNumbers: true, Grid: &web.GridResponse{Channels: []web.JSONChannel{
			{ChannelID: tc.station, ChannelNo: "50", CallSign: tc.name},
			{ChannelID: "ANCHOR", ChannelNo: "206", CallSign: "ESPN"},
		}}}
		got := matchCatalog(request, source)
		aliases, categories := 0, 0
		targetFacts := factsForStation(got.Facts, tc.station)
		for _, fact := range targetFacts {
			if fact.Kind == lineupindex.FactAlias {
				aliases++
			} else if fact.Kind == lineupindex.FactCategory {
				categories++
			}
		}
		if aliases != 1 || (categories > 0) != tc.wantCategory {
			t.Fatalf("%s: aliases=%d categories=%d facts=%+v", tc.station, aliases, categories, got.Facts)
		}
		method := targetFacts[0].Method
		if tc.wantCategory && !strings.Contains(method, "number-policy-provider-v2") {
			t.Fatalf("exact identity provenance = %q", method)
		}
		if !tc.wantCategory && !strings.Contains(method, "number-policy-provider-alias-v3") {
			t.Fatalf("provider-local alias provenance = %q", method)
		}
	}
}

func factsForStation(facts []lineupindex.ProviderFact, stationID string) []lineupindex.ProviderFact {
	var result []lineupindex.ProviderFact
	for _, fact := range facts {
		if fact.StationID == stationID {
			result = append(result, fact)
		}
	}
	return result
}
