package providersource

import (
	"context"
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
	"net/http"
	"strings"
	"testing"
)

func TestContradictedNumberRowsWithPassingAlignment(t *testing.T) {
	for _, tc := range []struct{ claim, alias, target string }{{"FX", "FX", "ESPN2"}, {"Oxygen True Crime", "OXYGEN", "VH1"}, {"E! Entertainment Television", "E", "FNC"}, {"Comedy Central", "COMEDY", "FOOD"}, {"MLB Network", "MLBN", "ACCSD"}} {
		t.Run(tc.target, func(t *testing.T) {
			grid := &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: "wrong", ChannelNo: "24", CallSign: tc.target}, {ChannelID: "real", ChannelNo: "56", CallSign: tc.alias}, {ChannelID: "anchor", ChannelNo: "7", CallSign: "ANCHOR"}}}
			source := catalogSource{ID: "fixture-official-lineup", Entries: []catalogEntry{{Name: tc.claim, Aliases: []string{tc.alias}, Numbers: []string{"24"}}, {Name: "ANCHOR", Numbers: []string{"7"}}}}
			nums := map[string][]web.JSONChannel{"24": {grid.Channels[0]}, "56": {grid.Channels[1]}, "7": {grid.Channels[2]}}
			a := catalogNumberAlignment(source.Entries, nums, ambiguousCatalogNumbers(source.Entries))
			if !a.allowsAliasRecovery() {
				t.Fatal("negative case did not pass alignment")
			}
			r := matchCatalog(lineupindex.ProviderEvidenceRequest{AllowChannelNumbers: true, Grid: grid}, source)
			for _, f := range append(r.Facts, r.IdentityFacts...) {
				if f.StationID == "wrong" && f.Kind == lineupindex.FactAlias {
					t.Fatalf("contradicted number became identity: %+v", f)
				}
			}
		})
	}
}
func TestCatalogNamedContradictionWithoutAlternateGridPosition(t *testing.T) {
	source := catalogSource{ID: "fixture", Entries: []catalogEntry{{Name: "Foo", Numbers: []string{"1"}}, {Name: "Bar", Numbers: []string{"999"}}, {Name: "Anchor", Numbers: []string{"2"}}}}
	grid := &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: "bar", ChannelNo: "1", CallSign: "BAR"}, {ChannelID: "a", ChannelNo: "2", CallSign: "ANCHOR"}}}
	r := matchCatalog(lineupindex.ProviderEvidenceRequest{AllowChannelNumbers: true, Grid: grid}, source)
	for _, f := range r.Facts {
		if f.StationID == "bar" && f.Value == "Foo" {
			t.Fatal("known different catalog identity overridden", f)
		}
	}
}
func TestFrontierNeverRequestsVerizonSource(t *testing.T) {
	svc := newService(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Host, "verizon") {
			t.Fatal("Frontier routed to Verizon")
		}
		return jsonResponse(r, `[]`), nil
	})})
	if OfficialSourceID("Frontier Fios Pomona") != "" {
		t.Fatal("Frontier classified as Verizon")
	}
	_, _ = svc.FetchProviderEvidence(context.Background(), lineupindex.ProviderEvidenceRequest{Provider: web.Provider{Name: "Frontier Fios Pomona"}, Grid: &web.GridResponse{}})
}
func TestXfinityWithoutHeadendBindingIsIdentityOnly(t *testing.T) {
	src := catalogSource{ID: "xfinity-official-lineup", IdentityOnly: true, Entries: []catalogEntry{{Name: "Long Name", Numbers: []string{"1"}}, {Name: "Anchor", Numbers: []string{"2"}}}}
	grid := &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: "wrong", CallSign: "OTHER", ChannelNo: "1"}, {ChannelID: "anchor", CallSign: "ANCHOR", ChannelNo: "2"}}}
	r := matchCatalog(lineupindex.ProviderEvidenceRequest{AllowChannelNumbers: true, Grid: grid}, src)
	for _, f := range r.Facts {
		if f.StationID == "wrong" {
			t.Fatal("address-only response authorized number identity")
		}
	}
}
