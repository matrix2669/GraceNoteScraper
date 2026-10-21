package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/guide"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

func TestEvidenceEligibilitySharedByDraftExportAndMatcher(t *testing.T) {
	s := newLineuparrTestServer(t, true)
	idx := lineupindex.Index{SchemaVersion: lineupindex.CurrentIndexVersion, Stations: map[string]*lineupindex.Station{"100": {StationID: "100", Facts: []lineupindex.StationFact{
		{Kind: lineupindex.FactAlias, Value: "UnsafeLegacy", SourceID: "gracenote-weekday-epg-old", Method: "pair-level identity (NEWS); identity-policy-v2", LineupKeys: []string{"old-A", "old-B"}},
		{Kind: lineupindex.FactAlias, Value: "ReusableIdentity", SourceID: "spectrum-official-lineup", SourceRowID: "row", SourceRevision: "catalog-revision", StationBound: true, Method: "exact Gracenote station ID from Spectrum TMSID"},
	}}}}
	before, _ := json.Marshal(idx)
	path := filepath.Join(t.TempDir(), "index.json")
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	var err error
	s.marketIndex, err = lineupindex.NewService(lineupindex.ServiceConfig{Path: path, Providers: &fakeProviderFinder{response: &web.ProviderResponse{}}, Grids: fakeMarketGridFetcher{}})
	if err != nil {
		t.Fatal(err)
	}
	config, _, _ := s.store.Get()
	if err = s.builder.UpdateChannelsCategory(config.Fingerprint(), []string{"1001"}, "Sports"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	draft, _, _, ok := s.buildDraft(w, httptest.NewRequest(http.MethodGet, "/api/lineuparr/draft", nil))
	if !ok {
		t.Fatal(w.Body.String())
	}
	matching, _ := dispatcharrSnapshotChannels(draft)
	for _, value := range []any{draft, builder.ExportFromDraft(draft), matching} {
		data, _ := json.Marshal(value)
		if strings.Contains(string(data), "UnsafeLegacy") || !strings.Contains(string(data), "ReusableIdentity") {
			t.Fatalf("inconsistent evidence projection: %s", data)
		}
	}
	for _, c := range draft.Channels {
		if c.ID == "1001" && c.Category != "Sports" {
			t.Fatal("reviewed category changed")
		}
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("read changed retained evidence")
	}
}

func TestShortRawGroupingThroughEvidenceAdmission(t *testing.T) {
	for _, raw := range []string{"FX", "ID", "E"} {
		for _, valid := range []bool{true, false} {
			t.Run(raw+fmt.Sprint(valid), func(t *testing.T) {
				s := newLineuparrTestServer(t, true)
				cfg, _, _ := s.store.Get()
				s.state.UpdateForSource(&guide.TVGuide{LineupChannels: []guide.Channel{
					{ID: "A", PlacementID: "a", ChannelNo: "1", CallSign: raw},
					{ID: "B", PlacementID: "b", ChannelNo: "2", CallSign: raw + "HD"},
					{ID: "A", PlacementID: "repeat", ChannelNo: "3", CallSign: raw},
				}}, cfg.Fingerprint())
				included := false
				if err := s.builder.UpdateChannel(cfg.Fingerprint(), "repeat", builder.ChannelUpdate{Included: &included}); err != nil {
					t.Fatal(err)
				}
				idx := lineupindex.Index{SchemaVersion: lineupindex.CurrentIndexVersion, Stations: map[string]*lineupindex.Station{}}
				for _, id := range []string{"A", "B"} {
					label := raw
					if id == "B" {
						label += " HD"
					}
					fact := lineupindex.StationFact{Kind: lineupindex.FactAlias, Value: label, SourceID: "spectrum-official-lineup", SourceRowID: "row-" + id, SourceRevision: "catalog-revision", StationBound: true, RootSourceStationID: id, Method: "exact Gracenote station ID from Spectrum TMSID"}
					if !valid && id == "B" {
						fact.SourceRowID = ""
					}
					idx.Stations[id] = &lineupindex.Station{StationID: id, Facts: []lineupindex.StationFact{fact, {Kind: lineupindex.FactAlias, Value: "Contaminated", SourceID: "gracenote-weekday-epg-old", Method: "identity-policy-v2"}}}
				}
				before, _ := json.Marshal(idx)
				path := filepath.Join(t.TempDir(), "index.json")
				if err := os.WriteFile(path, before, 0600); err != nil {
					t.Fatal(err)
				}
				var err error
				s.marketIndex, err = lineupindex.NewService(lineupindex.ServiceConfig{Path: path, Providers: &fakeProviderFinder{response: &web.ProviderResponse{}}, Grids: fakeMarketGridFetcher{}})
				if err != nil {
					t.Fatal(err)
				}
				w := httptest.NewRecorder()
				draft, _, _, ok := s.buildDraft(w, httptest.NewRequest(http.MethodGet, "/api/lineuparr/draft", nil))
				if !ok {
					t.Fatal(w.Body.String())
				}
				together := false
				for _, g := range draft.DuplicateGroups {
					if len(g.ChannelIDs) == 3 {
						together = true
					}
				}
				if together != valid {
					t.Fatalf("eligible short raw grouping=%v want=%v: %+v", together, valid, draft.DuplicateGroups)
				}
				matching, _ := dispatcharrSnapshotChannels(draft)
				for _, v := range []any{draft, builder.ExportFromDraft(draft), matching} {
					b, _ := json.Marshal(v)
					if strings.Contains(string(b), "Contaminated") {
						t.Fatal("legacy alias leaked")
					}
				}
				for _, c := range draft.Channels {
					if c.ID == "repeat" && c.Included {
						t.Fatal("repeat exclusion lost")
					}
				}
				after, _ := os.ReadFile(path)
				if string(before) != string(after) {
					t.Fatal("retained evidence changed")
				}
			})
		}
	}
}
