package providersource

import (
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
	"strings"
	"testing"
)

func TestProviderOwnGridRequiresIdentityForNumberDisambiguation(t *testing.T) {
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
	if got := matchCatalog(request, source); len(got.Facts) > 0 && got.Facts[0].StationID == "ONE" {
		t.Fatal("number alone established identity")
	}
}

func TestSameNumberAcrossProviderGridsDoesNotTransferCategories(t *testing.T) {
	source := catalogSource{ID: "verizon-fixture", Entries: []catalogEntry{{Numbers: []string{"50"}, Name: "USA", Category: "Entertainment"}}}
	for _, tc := range []struct {
		station, name string
		want          bool
	}{{"FIOS-USA", "USA", true}, {"OTHER-ID", "Investigation Discovery", false}} {
		request := lineupindex.ProviderEvidenceRequest{AllowChannelNumbers: true, Grid: &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: tc.station, ChannelNo: "50", CallSign: tc.name}}}}
		got := matchCatalog(request, source)
		if (len(got.Facts) > 0) != tc.want {
			t.Fatalf("%s: %+v", tc.station, got.Facts)
		}
		for _, fact := range got.Facts {
			if fact.StationID != tc.station || !strings.Contains(fact.Method, "number-policy-provider-v2") {
				t.Fatalf("wrong identity or provenance: %+v", fact)
			}
		}
	}
}
