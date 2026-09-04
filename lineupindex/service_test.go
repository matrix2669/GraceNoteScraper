package lineupindex

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/web"
)

type fakeProviders struct {
	mu        sync.Mutex
	responses map[string][]web.Provider
	calls     []string
}

func (f *fakeProviders) FindProviders(_ context.Context, _, postalCode, _ string) (*web.ProviderResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, postalCode)
	return &web.ProviderResponse{Providers: append([]web.Provider(nil), f.responses[postalCode]...)}, nil
}

type fakeGrids struct {
	mu        sync.Mutex
	responses map[string]*web.GridResponse
	failures  map[string]int
	calls     map[string]int
}

type blockingGrid struct {
	started chan struct{}
}

func (f *blockingGrid) FetchGrid(ctx context.Context, _ web.Preferences, _ int64) (*web.GridResponse, error) {
	close(f.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func (f *fakeGrids) FetchGrid(_ context.Context, preferences web.Preferences, _ int64) (*web.GridResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[preferences.LineupId]++
	if f.failures[preferences.LineupId] > 0 {
		f.failures[preferences.LineupId]--
		return nil, errors.New("temporary grid failure")
	}
	return f.responses[preferences.LineupId], nil
}

func testCatalog(markets ...MarketSeed) SeedCatalog {
	return SeedCatalog{
		SchemaVersion:   1,
		Name:            "Test markets",
		AsOf:            "test",
		RankingSource:   "https://example.test/ranks",
		SelectionMethod: "Test seeds",
		Markets:         markets,
		Digest:          strings.Repeat("a", 64),
	}
}

func testProvider(lineupID string) web.Provider {
	return web.Provider{
		Type:       "CABLE",
		Device:     "X",
		LineupID:   lineupID,
		Name:       "Provider " + lineupID,
		HeadendID:  "HEADEND" + lineupID,
		PostalCode: "10001",
	}
}

func waitForBatch(t *testing.T, service *Service, batchCount int) Snapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := service.Snapshot()
		if !snapshot.Job.Running && snapshot.Job.CompletedAt != "" {
			return snapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("market scan did not finish; snapshot = %+v", service.Snapshot().Job)
	return Snapshot{}
}

func TestServiceRetainsDistinctEventCallSignsAsAliasEvidence(t *testing.T) {
	providers := &fakeProviders{responses: map[string][]web.Provider{"10001": {testProvider("L1")}}}
	grids := &fakeGrids{
		responses: map[string]*web.GridResponse{"L1": {Channels: []web.JSONChannel{{
			ChannelID: "S1", CallSign: "ONE", Events: []web.JSONEvent{{CallSign: "ONE HD"}},
		}}}},
		failures: map[string]int{}, calls: map[string]int{},
	}
	service, err := NewService(ServiceConfig{
		Path:      filepath.Join(t.TempDir(), "market_index.json"),
		Catalog:   testCatalog(MarketSeed{Rank: 1, Name: "New York", Country: "USA", PostalCode: "10001"}),
		Providers: providers, Grids: grids,
		CurrentStations: func() map[string][]string { return map[string][]string{"S1": {"ONE"}} },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(RunRequest{Action: "postal", Country: "USA", PostalCode: "10001"}); err != nil {
		t.Fatal(err)
	}
	snapshot := waitForBatch(t, service, 1)
	if snapshot.Summary.MeaningfulAliases != 1 || snapshot.Summary.CurrentLineupAliases != 1 {
		t.Fatalf("event callsign summary = %+v", snapshot.Summary)
	}
	candidates := service.AliasesForStations([]string{"S1"})["S1"]
	if len(candidates) != 2 || candidates[1].Value != "ONE HD" || candidates[1].Kind != NameEventCallSign {
		t.Fatalf("event callsign candidates = %+v", candidates)
	}
}
