package providersource

import (
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
	"testing"
)

func TestCompetingProvidersCannotUseNumberDisambiguation(t *testing.T) {
	request := lineupindex.ProviderEvidenceRequest{Grid: &web.GridResponse{Channels: []web.JSONChannel{
		{ChannelID: "ONE", ChannelNo: "1", CallSign: "MUSIC"}, {ChannelID: "TWO", ChannelNo: "2", CallSign: "MUSIC"},
	}}}
	source := catalogSource{ID: "fixture", Entries: []catalogEntry{{Numbers: []string{"1"}, Name: "MUSIC", Category: "Music"}}}
	if got := matchCatalog(request, source); len(got.Facts) != 0 {
		t.Fatal("number narrowed ambiguous competing-provider identity", got.Facts)
	}
	request.AllowChannelNumbers = true
	if got := matchCatalog(request, source); len(got.Facts) == 0 {
		t.Fatal("selected local provider cannot use corroborated number")
	}
	request.Grid.Channels[0].CallSign = "UNRELATED"
	if got := matchCatalog(request, source); len(got.Facts) > 0 && got.Facts[0].StationID == "ONE" {
		t.Fatal("number alone established identity")
	}
}
