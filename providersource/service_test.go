package providersource

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/marketindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestDISHOfficialServiceMatchesExactChannelNumber(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != dishURL {
			t.Fatalf("request URL = %s", request.URL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"Channels":[{"name":"FOX News (FXNWS) HD","catg":"News & Info|","calltr":"FXNWS","ChannelNo":"205"},{"name":"SEC (SEC) HD","catg":"News & Info|","calltr":"SEC","ChannelNo":"404"}]}`)),
			Header:     make(http.Header),
		}, nil
	})}
	service := newService(client)
	result, err := service.FetchProviderEvidence(context.Background(), marketindex.ProviderEvidenceRequest{
		Provider: web.Provider{Name: "DISH Satellite", LineupID: "DISH"}, LineupKey: "DISH", PostalCode: "11743",
		Grid: &web.GridResponse{Channels: []web.JSONChannel{
			{ChannelID: "NEWS", ChannelNo: "205", CallSign: "FXNWS"},
			{ChannelID: "SEC", ChannelNo: "404", CallSign: "SEC"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	categories := make(map[string]string)
	aliases := make(map[string][]string)
	for _, fact := range result.Facts {
		if fact.Kind == marketindex.FactCategory {
			categories[fact.StationID] = fact.Value
		} else {
			aliases[fact.StationID] = append(aliases[fact.StationID], fact.Value)
		}
	}
	if categories["NEWS"] != "News & Weather" || categories["SEC"] != "Sports" {
		t.Fatalf("categories = %+v", categories)
	}
	if !contains(aliases["NEWS"], "FOX News") || !contains(aliases["NEWS"], "FXNWS") {
		t.Fatalf("aliases = %+v", aliases)
	}
	if len(result.Sources) != 1 || result.Sources[0].Matched != 2 {
		t.Fatalf("source status = %+v", result.Sources)
	}
}

func TestOptimumSnapshotRequiresMatchingPostalCode(t *testing.T) {
	service := NewService(Options{UseEmbeddedCatalogs: true})
	request := marketindex.ProviderEvidenceRequest{
		Provider:   web.Provider{Name: "Optimum of Woodbury - Digital Rebuild"},
		PostalCode: "11743",
		Grid:       &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: "ESPN", ChannelNo: "210", CallSign: "ESPN"}}},
	}
	result, err := service.FetchProviderEvidence(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, fact := range result.Facts {
		if fact.StationID == "ESPN" && fact.Kind == marketindex.FactCategory && fact.Value == "Sports" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Optimum category facts = %+v", result.Facts)
	}
	request.PostalCode = "75001"
	result, err = service.FetchProviderEvidence(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Facts) != 0 || len(result.Sources) != 0 {
		t.Fatalf("out-of-market evidence = %+v", result)
	}
}

func TestEmbeddedProviderCatalogsAreOffByDefault(t *testing.T) {
	request := marketindex.ProviderEvidenceRequest{
		Provider: web.Provider{Name: "Optimum of Woodbury - Digital Rebuild"}, PostalCode: "11743",
		Grid: &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: "ESPN", ChannelNo: "210", CallSign: "ESPN"}}},
	}
	result, err := NewService().FetchProviderEvidence(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Facts) != 0 || len(result.Sources) != 0 {
		t.Fatalf("default embedded provider evidence = %+v", result)
	}
}

func TestProviderCatalogDoesNotCreateAliasesSharedByDifferentStations(t *testing.T) {
	result := matchCatalog(marketindex.ProviderEvidenceRequest{Grid: &web.GridResponse{Channels: []web.JSONChannel{
		{ChannelID: "ONE", ChannelNo: "850"}, {ChannelID: "TWO", ChannelNo: "851"},
	}}}, catalogSource{
		ID: "music", Label: "Official music range", Entries: []catalogEntry{
			{Numbers: []string{"850"}, Name: "Stingray Music", Category: "Music"},
			{Numbers: []string{"851"}, Name: "Stingray Music", Category: "Music"},
		},
	})
	aliases := 0
	categories := 0
	for _, fact := range result.Facts {
		if fact.Kind == marketindex.FactAlias {
			aliases++
		} else if fact.Kind == marketindex.FactCategory {
			categories++
		}
	}
	if aliases != 0 || categories != 2 {
		t.Fatalf("facts = %+v", result.Facts)
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
