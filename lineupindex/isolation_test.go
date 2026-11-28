package lineupindex

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/web"
)

func TestNoCatalogRequiredAndRankedActionsCannotChangeEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "market_index.json")
	legacy := newIndex(SeedCatalog{Digest: "legacy-digest", AsOf: "2025-09"}, time.Now())
	legacy.Stations["S1"] = &Station{StationID: "S1", Names: []StationName{{Kind: NameCallSign, Value: "ONE", Normalized: "one"}}}
	if err := writeIndex(path, legacy); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	providers := &fakeProviders{responses: map[string][]web.Provider{}}
	service, err := NewService(ServiceConfig{Path: path, Providers: providers, Grids: &fakeGrids{}})
	if err != nil {
		t.Fatal(err)
	}
	if service.index.SeedDigest != "legacy-digest" || service.index.Stations["S1"] == nil {
		t.Fatal("legacy evidence lost")
	}
	for _, request := range []RunRequest{
		{Action: "continue"}, {Action: "refresh", Ranks: []int{1}}, {Action: "rebuild"}, {},
		{Action: "postal", Country: "USA", PostalCode: "11743", Ranks: []int{1}},
		{Action: "postal", Country: "USA", PostalCode: "11743", BatchSize: 25},
		{Action: "postal"},
	} {
		if _, err := service.Start(request); err == nil {
			t.Fatalf("accepted forbidden request: %+v", request)
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || len(providers.calls) != 0 {
		t.Fatal("rejected scan modified evidence or called provider")
	}
	if _, err := service.Start(RunRequest{Action: "postal", Country: "USA", PostalCode: "11743"}); err != nil {
		t.Fatal(err)
	}
	waitForBatch(t, service, 1)
	if len(providers.calls) != 1 || providers.calls[0] != "11743" {
		t.Fatalf("unexpected postal lookups: %v", providers.calls)
	}
	reopened, err := NewService(ServiceConfig{Path: path, Providers: providers, Grids: &fakeGrids{}})
	if err != nil || reopened.index.Stations["S1"] == nil {
		t.Fatalf("restart lost evidence: %v", err)
	}
}

func TestPostalStopKeepsEvidenceAndCanBeRetried(t *testing.T) {
	grids := &blockingGrid{started: make(chan struct{})}
	providers := &fakeProviders{responses: map[string][]web.Provider{"10001": {testProvider("L1")}}}
	service, err := NewService(ServiceConfig{Path: filepath.Join(t.TempDir(), "market_index.json"), Providers: providers, Grids: grids})
	if err != nil {
		t.Fatal(err)
	}
	request := RunRequest{Action: "postal", Country: "USA", PostalCode: "10001"}
	if _, err := service.Start(request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-grids.started:
	case <-time.After(3 * time.Second):
		t.Fatal("grid did not start")
	}
	if _, err := service.Start(request); err != ErrAlreadyRunning {
		t.Fatalf("concurrent scan error: %v", err)
	}
	if !service.Stop() {
		t.Fatal("scan did not stop")
	}
	waitForBatch(t, service, 1)
	if len(service.index.Lineups) != 1 {
		t.Fatal("stopped scan discarded lineup evidence")
	}
}
