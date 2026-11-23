package providersource

import (
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
	"testing"
)

func TestWrongHeadendNumberCannotRenameUSA(t *testing.T) {
	result := matchCatalog(lineupindex.ProviderEvidenceRequest{Grid: &web.GridResponse{Channels: []web.JSONChannel{
		{ChannelID: "11207", ChannelNo: "104", CallSign: "USA"},
		{ChannelID: "ID", ChannelNo: "105", CallSign: "ID", AffiliateName: "Investigation Discovery"},
	}}}, catalogSource{ID: "broadstar", Entries: []catalogEntry{{Numbers: []string{"104"}, Name: "Investigation Discovery", Category: "Entertainment"}}})
	for _, fact := range result.Facts {
		if fact.StationID == "11207" {
			t.Fatalf("wrong number contaminated USA: %+v", fact)
		}
	}
	if result.Sources[0].Matched != 1 {
		t.Fatal("unique identity on different number was not recovered")
	}
}

func TestProviderIdentityKeepsNumberedSubchannels(t *testing.T) {
	if identityKey("WCBSDT") != identityKey("WCBS") {
		t.Fatal("terminal DT not normalized")
	}
	if identityKey("WCBSDT2") == identityKey("WCBS") {
		t.Fatal("subchannel lost")
	}
	if identityKey("USA HD") != identityKey("USA") {
		t.Fatal("HD not normalized")
	}
}
