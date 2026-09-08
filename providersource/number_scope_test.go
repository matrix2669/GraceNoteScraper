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
	}}}
	source := catalogSource{ID: "fixture", Entries: []catalogEntry{{Numbers: []string{"1"}, Name: "MUSIC", Category: "Music"}}}
	if got := matchCatalog(request, source); len(got.Facts) != 0 {
		t.Fatal("number narrowed ambiguous competing-provider identity", got.Facts)
	}
	request.AllowChannelNumbers = true
	if got := matchCatalog(request, source); len(got.Facts) == 0 {
		t.Fatal("provider cannot use corroborated number against its own grid")
	}
	request.Grid.Channels[0].CallSign = "UNRELATED"
	got := matchCatalog(request, source)
	if len(got.Facts) != 1 || got.Facts[0].StationID != "ONE" || got.Facts[0].Kind != lineupindex.FactAlias || got.Facts[0].Value != "MUSIC" {
		t.Fatal("unique provider-local number did not recover only the alias", got.Facts)
	}
}

func TestSameNumberAcrossProviderGridsDoesNotTransferCategories(t *testing.T) {
	source := catalogSource{ID: "verizon-fixture", Entries: []catalogEntry{{Numbers: []string{"50"}, Name: "USA", Category: "Entertainment"}}}
	for _, tc := range []struct {
		station, name string
		wantCategory  bool
	}{{"FIOS-USA", "USA", true}, {"OTHER-ID", "Investigation Discovery", false}} {
		request := lineupindex.ProviderEvidenceRequest{AllowChannelNumbers: true, Grid: &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: tc.station, ChannelNo: "50", CallSign: tc.name}}}}
		got := matchCatalog(request, source)
		aliases, categories := 0, 0
		for _, fact := range got.Facts {
			if fact.StationID != tc.station {
				t.Fatalf("wrong identity or provenance: %+v", fact)
			}
			if fact.Kind == lineupindex.FactAlias {
				aliases++
			} else if fact.Kind == lineupindex.FactCategory {
				categories++
			}
		}
		if aliases != 1 || (categories > 0) != tc.wantCategory {
			t.Fatalf("%s: aliases=%d categories=%d facts=%+v", tc.station, aliases, categories, got.Facts)
		}
		method := got.Facts[0].Method
		if tc.wantCategory && !strings.Contains(method, "number-policy-provider-v2") {
			t.Fatalf("exact identity provenance = %q", method)
		}
		if !tc.wantCategory && !strings.Contains(method, "number-policy-provider-alias-v3") {
			t.Fatalf("provider-local alias provenance = %q", method)
		}
	}
}
