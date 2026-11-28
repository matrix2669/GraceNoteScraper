package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

func TestKnownAliasRetainsProviderAttribution(t *testing.T) {
	idx := lineupindex.Index{SchemaVersion: lineupindex.CurrentIndexVersion, Stations: map[string]*lineupindex.Station{
		"123": {Facts: []lineupindex.StationFact{{Kind: lineupindex.FactAlias, Value: "NEWS", SourceID: "xfinity-official-lineup"}}},
	}, PostalScans: map[string]*lineupindex.PostalScanRecord{"USA:33308": {Sources: []lineupindex.EvidenceSourceRecord{{ID: "xfinity-official-lineup", Matched: 1}}}}}
	path := filepath.Join(t.TempDir(), "index.json")
	data, err := json.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	svc, err := lineupindex.NewService(lineupindex.ServiceConfig{Path: path, Providers: &fakeProviderFinder{response: &web.ProviderResponse{}}, Grids: fakeMarketGridFetcher{}})
	if err != nil {
		t.Fatal(err)
	}
	s := &lineuparrServer{marketIndex: svc}
	inputs := []builder.InputChannel{{StationID: "123", CallSign: "NEWS"}}
	statuses := s.applyMarketAliases("USA", "33308", "xfinity-official-lineup", inputs)
	if len(inputs[0].ExternalAliases) != 1 || inputs[0].ExternalAliases[0].Source != "xfinity-official-lineup" {
		t.Fatalf("lost attribution: %+v", inputs)
	}
	found := false
	for _, status := range statuses {
		if status.ID == "xfinity-official-lineup" {
			found = true
			if status.Matched != 1 {
				t.Fatalf("wrong matched count: %+v", status)
			}
		}
	}
	if !found {
		t.Fatal("missing source")
	}
}
