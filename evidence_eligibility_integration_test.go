package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
