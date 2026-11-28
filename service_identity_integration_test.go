package main

import (
	"context"
	"encoding/json"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
	"os"
	"path/filepath"
	"testing"
)

// Exercise the real historical index -> typed candidate -> server bridge ->
// builder path. No prebuilt Draft proof is injected and no scan is started.
func TestReusableExactIDServiceEvidenceReachesBuilderWithoutRescan(t *testing.T) {
	for _, c := range [][3]string{{"FREEFRM", "FREFMHD", "Freeform"}, {"YES", "YESHDNY", "YES Network"}, {"FX", "FXHD", "FX"}, {"SEC", "SECH", "SEC Network"}, {"BIG10V", "BIG10VH", "Big Ten Network"}, {"MLBN", "MLBHD", "MLB Network"}, {"ID", "IDHD", "Investigation Discovery"}, {"OXYGEN", "OXYGNHD", "Oxygen True Crime"}, {"WE", "WEHD", "WE TV"}, {"HGTV", "HGTVD", "HGTV"}, {"COMEDY", "CCHD", "Comedy Central"}, {"E", "EHD", "E!"}, {"UP", "UPHD", "UP"}, {"TVLAND", "TVLNDHD", "TV Land"}, {"NFLNRZ", "NFLNRZD", "NFL RedZone"}} {
		t.Run(c[2], func(t *testing.T) {
			s := newLineuparrTestServer(t, true)
			idx := lineupindex.Index{SchemaVersion: 4, Stations: map[string]*lineupindex.Station{}, Lineups: map[string]*lineupindex.LineupRecord{"own": {Key: "own", ProviderName: "Fixture Provider"}}}
			inputs := []builder.InputChannel{}
			for i, id := range []string{"A", "B"} {
				idx.Stations[id] = &lineupindex.Station{StationID: id, Names: []lineupindex.StationName{{Value: c[i], Kind: lineupindex.NameCallSign}}, Facts: []lineupindex.StationFact{{Kind: lineupindex.FactAlias, Value: c[2], SourceID: "spectrum-official-lineup", StationBound: true, SourceRevision: "catalog-revision", SourceRowID: "same-row", RootSourceStationID: id, RootSourceLineupKey: "own", LineupKeys: []string{"own"}, Method: "exact Gracenote station ID from Spectrum TMSID"}}}
				inputs = append(inputs, builder.InputChannel{Key: id, StationID: id, Number: id, CallSign: c[i]})
			}
			inputs = append(inputs, builder.InputChannel{Key: "repeat", StationID: "A", Number: "repeat", CallSign: c[0]})
			b, e := json.Marshal(idx)
			if e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(t.TempDir(), "index.json")
			if e = os.WriteFile(path, b, 0600); e != nil {
				t.Fatal(e)
			}
			s.marketIndex, e = lineupindex.NewService(lineupindex.ServiceConfig{Path: path, Providers: &fakeProviderFinder{response: &web.ProviderResponse{}}, Grids: fakeMarketGridFetcher{}})
			if e != nil {
				t.Fatal(e)
			}
			s.applyMarketAliases("USA", "11743", "", inputs)
			d, e := s.builder.Build(context.Background(), builder.LineupContext{SourceFingerprint: "test"}, inputs)
			if e != nil {
				t.Fatal(e)
			}
			if len(d.DuplicateGroups) != 1 || len(d.DuplicateGroups[0].ChannelIDs) != 3 {
				t.Fatalf("historical identity not connected: %+v", d.DuplicateGroups)
			}
			after, _ := os.ReadFile(path)
			if string(after) != string(b) {
				t.Fatal("read-only re-evaluation wrote retained copy")
			}
		})
	}
}
