package marketindex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
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

type fakeEvidence struct{}

func (fakeEvidence) FetchProviderEvidence(_ context.Context, request ProviderEvidenceRequest) (ProviderEvidenceResult, error) {
	if request.Provider.LineupID != "L1" {
		return ProviderEvidenceResult{}, nil
	}
	return ProviderEvidenceResult{
		Facts: []ProviderFact{
			{StationID: "S1", Kind: FactAlias, Value: "ESPN Full Name", SourceID: "provider-one", SourceLabel: "Provider One official lineup", Method: "exact provider channel number"},
			{StationID: "S1", Kind: FactCategory, Value: "Sports", SourceID: "provider-one", SourceLabel: "Provider One official lineup", Method: "exact provider channel number"},
		},
		Sources: []EvidenceSourceRecord{{ID: "provider-one", Label: "Provider One official lineup", Status: "complete", Matched: 1, Aliases: 1, Categories: 1}},
	}, nil
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
		if !snapshot.Job.Running && len(snapshot.Batches) >= batchCount {
			return snapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("market scan did not finish; snapshot = %+v", service.Snapshot().Job)
	return Snapshot{}
}

func waitForPostal(t *testing.T, service *Service, country, postalCode string) Snapshot {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := service.SnapshotForPostal(country, postalCode)
		if !snapshot.Job.Running && snapshot.PostalScan != nil {
			return snapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("postal scan did not finish; snapshot = %+v", service.SnapshotForPostal(country, postalCode).Job)
	return Snapshot{}
}

func TestPostalScanEnrichesEveryProviderBeforeCrossLineupReuse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "market_index.json")
	providers := &fakeProviders{responses: map[string][]web.Provider{
		"11743": {testProvider("L1"), testProvider("L2"), testProvider("L1")},
	}}
	grids := &fakeGrids{responses: map[string]*web.GridResponse{
		"L1": {Channels: []web.JSONChannel{{ChannelID: "S1", ChannelNo: "206", CallSign: "ESPN"}}},
		"L2": {Channels: []web.JSONChannel{{ChannelID: "S1", ChannelNo: "210", CallSign: "ESPNHD"}, {ChannelID: "S2", ChannelNo: "5", CallSign: "WNYW"}}},
	}, failures: map[string]int{}, calls: map[string]int{}}
	service, err := NewService(ServiceConfig{
		Path: path, Catalog: testCatalog(MarketSeed{Rank: 1, Name: "New York", Country: "USA", PostalCode: "10001"}),
		Providers: providers, Grids: grids, Evidence: fakeEvidence{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(RunRequest{Action: "postal", Country: "USA", PostalCode: "11743", Language: "en-us"}); err != nil {
		t.Fatal(err)
	}
	snapshot := waitForPostal(t, service, "USA", "11743")
	if snapshot.PostalScan.Status != StatusComplete || snapshot.PostalScan.ProviderCount != 2 || snapshot.PostalScan.LineupsScanned != 2 {
		t.Fatalf("postal scan = %+v", snapshot.PostalScan)
	}
	if grids.calls["L1"] != 1 || grids.calls["L2"] != 1 {
		t.Fatalf("grid calls = %+v", grids.calls)
	}
	aliases := service.AliasesForStations([]string{"S1"})["S1"]
	foundOfficialAlias := false
	for _, alias := range aliases {
		if alias.Value == "ESPN Full Name" && alias.SourceID == "provider-one" {
			foundOfficialAlias = true
		}
	}
	if !foundOfficialAlias {
		t.Fatalf("aliases = %+v", aliases)
	}
	category, ok := service.CategoriesForStations([]string{"S1"})["S1"]
	if !ok || category.Value != "Sports" || len(category.SourceIDs) != 1 || category.SourceIDs[0] != "provider-one" {
		t.Fatalf("category = %+v, %v", category, ok)
	}
}

func TestCategoriesForStationsRejectsConflictingOfficialSources(t *testing.T) {
	service, err := NewService(ServiceConfig{
		Path:      filepath.Join(t.TempDir(), "market_index.json"),
		Catalog:   testCatalog(MarketSeed{Rank: 1, Name: "New York", Country: "USA", PostalCode: "10001"}),
		Providers: &fakeProviders{responses: map[string][]web.Provider{}},
		Grids:     &fakeGrids{responses: map[string]*web.GridResponse{}, failures: map[string]int{}, calls: map[string]int{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	service.index.Stations["S1"] = &Station{StationID: "S1", Facts: []StationFact{
		{Kind: FactCategory, Value: "Sports", Normalized: "SPORTS", SourceID: "one"},
		{Kind: FactCategory, Value: "News", Normalized: "NEWS", SourceID: "two"},
	}}
	service.mu.Unlock()
	if _, ok := service.CategoriesForStations([]string{"S1"})["S1"]; ok {
		t.Fatal("conflicting categories were applied automatically")
	}
}

func TestServiceDeduplicatesLineupsAndReportsAliasYield(t *testing.T) {
	path := filepath.Join(t.TempDir(), "market_index.json")
	providers := &fakeProviders{responses: map[string][]web.Provider{
		"10001": {testProvider("L1"), testProvider("SHARED")},
		"90012": {testProvider("L2"), testProvider("SHARED")},
	}}
	grids := &fakeGrids{
		responses: map[string]*web.GridResponse{
			"L1": {Channels: []web.JSONChannel{
				{ChannelID: "S1", ChannelNo: "101", CallSign: "WAAA", AffiliateName: "Example Network", Events: []web.JSONEvent{{Program: web.JSONProgram{Title: "must not persist"}}}},
				{ChannelID: "S2", CallSign: "NEWS"},
			}},
			"SHARED": {Channels: []web.JSONChannel{
				{ChannelID: "S1", CallSign: "ALPHA"},
				{ChannelID: "S3", CallSign: "KCCC"},
			}},
			"L2": {Channels: []web.JSONChannel{
				{ChannelID: "S1", CallSign: "WAAA-TV"},
				{ChannelID: "S5", CallSign: "NEWS"},
			}},
		},
		failures: map[string]int{},
		calls:    map[string]int{},
	}
	service, err := NewService(ServiceConfig{
		Path:      path,
		Catalog:   testCatalog(MarketSeed{Rank: 1, Name: "New York", Country: "USA", PostalCode: "10001"}, MarketSeed{Rank: 2, Name: "Los Angeles", Country: "USA", PostalCode: "90012"}),
		Providers: providers,
		Grids:     grids,
		CurrentStations: func() map[string][]string {
			return map[string][]string{"S1": {"WAAA"}}
		},
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if _, err := service.Start(RunRequest{Action: "continue", BatchSize: 2}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	snapshot := waitForBatch(t, service, 1)
	if snapshot.Summary.CompletedMarkets != 2 || snapshot.Summary.Lineups != 3 || snapshot.Summary.Stations != 4 {
		t.Fatalf("unexpected summary = %+v", snapshot.Summary)
	}
	if snapshot.Summary.MeaningfulAliases != 2 || snapshot.Summary.CurrentLineupAliases != 2 || snapshot.Summary.Conflicts != 1 {
		t.Fatalf("unexpected alias summary = %+v", snapshot.Summary)
	}
	batch := snapshot.Batches[0]
	if batch.NewLineups != 3 || batch.ReusedLineups != 1 || batch.GridRequests != 3 {
		t.Fatalf("unexpected lineup metrics = %+v", batch)
	}
	if batch.NewCallSignAliases != 2 || batch.CurrentLineupAliases != 2 || batch.Conflicts != 1 {
		t.Fatalf("unexpected batch alias metrics = %+v", batch)
	}
	if grids.calls["SHARED"] != 1 {
		t.Fatalf("shared lineup fetch count = %d, want 1", grids.calls["SHARED"])
	}
	if _, err := service.Start(RunRequest{Action: "continue"}); !errors.Is(err, ErrNoWork) {
		t.Fatalf("second Start() error = %v, want ErrNoWork", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(data), "must not persist") || strings.Contains(string(data), "events") {
		t.Fatal("programme data was persisted in the market index")
	}
	var persisted Index
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("decoding persisted index: %v", err)
	}
	if got := persisted.Stations["S1"].Observations[0].ChannelNo; got != "101" {
		t.Fatalf("persisted channel number = %q, want 101", got)
	}
}

func TestServiceResumesOnlyIncompleteLineups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "market_index.json")
	providers := &fakeProviders{responses: map[string][]web.Provider{
		"10001": {testProvider("GOOD"), testProvider("RETRY")},
	}}
	grids := &fakeGrids{
		responses: map[string]*web.GridResponse{
			"GOOD":  {Channels: []web.JSONChannel{{ChannelID: "S1", CallSign: "ONE"}}},
			"RETRY": {Channels: []web.JSONChannel{{ChannelID: "S2", CallSign: "TWO"}}},
		},
		failures: map[string]int{"RETRY": 1},
		calls:    map[string]int{},
	}
	service, err := NewService(ServiceConfig{
		Path:      path,
		Catalog:   testCatalog(MarketSeed{Rank: 1, Name: "New York", Country: "USA", PostalCode: "10001"}),
		Providers: providers,
		Grids:     grids,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if _, err := service.Start(RunRequest{Action: "continue", BatchSize: 1}); err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	first := waitForBatch(t, service, 1)
	if first.Summary.ErrorMarkets != 1 || first.Batches[0].Errors != 1 {
		t.Fatalf("first scan did not retain retry state: %+v", first.Summary)
	}

	if _, err := service.Start(RunRequest{Action: "continue", BatchSize: 1}); err != nil {
		t.Fatalf("resume Start() error = %v", err)
	}
	second := waitForBatch(t, service, 2)
	if second.Summary.CompletedMarkets != 1 || second.Summary.ErrorMarkets != 0 {
		t.Fatalf("resume summary = %+v", second.Summary)
	}
	if grids.calls["GOOD"] != 1 || grids.calls["RETRY"] != 2 {
		t.Fatalf("grid calls = %+v, want GOOD=1 RETRY=2", grids.calls)
	}
	if second.Batches[1].ReusedLineups != 1 || second.Batches[1].GridRequests != 1 {
		t.Fatalf("resume batch = %+v", second.Batches[1])
	}
}

func TestServiceKeepsPostalSpecificOTAPlaceholderLineupsSeparate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "market_index.json")
	ota := web.Provider{
		Type:      "OTA",
		LineupID:  "USA-lineupId-DEFAULT",
		Name:      "Local Over the Air Broadcast",
		HeadendID: "lineupId",
	}
	providers := &fakeProviders{responses: map[string][]web.Provider{
		"10001": {ota},
		"90012": {ota},
	}}
	grids := &fakeGrids{
		responses: map[string]*web.GridResponse{
			"USA-lineupId-DEFAULT": {Channels: []web.JSONChannel{{ChannelID: "LOCAL", CallSign: "LOCAL"}}},
		},
		failures: map[string]int{},
		calls:    map[string]int{},
	}
	service, err := NewService(ServiceConfig{
		Path:      path,
		Catalog:   testCatalog(MarketSeed{Rank: 1, Name: "New York", Country: "USA", PostalCode: "10001"}, MarketSeed{Rank: 2, Name: "Los Angeles", Country: "USA", PostalCode: "90012"}),
		Providers: providers,
		Grids:     grids,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if _, err := service.Start(RunRequest{Action: "continue", BatchSize: 2}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	snapshot := waitForBatch(t, service, 1)
	if snapshot.Summary.Lineups != 2 || grids.calls["USA-lineupId-DEFAULT"] != 2 {
		t.Fatalf("OTA placeholder lineups were collapsed: summary=%+v calls=%+v", snapshot.Summary, grids.calls)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var persisted Index
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("decoding persisted index: %v", err)
	}
	if persisted.Lineups["USA-lineupId-DEFAULT@10001"] == nil || persisted.Lineups["USA-lineupId-DEFAULT@90012"] == nil {
		t.Fatalf("postal-specific OTA keys missing: %+v", persisted.Lineups)
	}
}

func TestServiceRefreshAndRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "market_index.json")
	providers := &fakeProviders{responses: map[string][]web.Provider{"10001": {testProvider("L1")}}}
	grids := &fakeGrids{
		responses: map[string]*web.GridResponse{"L1": {Channels: []web.JSONChannel{{ChannelID: "S1", CallSign: "ONE"}}}},
		failures:  map[string]int{},
		calls:     map[string]int{},
	}
	service, err := NewService(ServiceConfig{
		Path:      path,
		Catalog:   testCatalog(MarketSeed{Rank: 1, Name: "New York", Country: "USA", PostalCode: "10001"}),
		Providers: providers,
		Grids:     grids,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if _, err := service.Start(RunRequest{Action: "continue", BatchSize: 1}); err != nil {
		t.Fatalf("initial Start() error = %v", err)
	}
	waitForBatch(t, service, 1)
	if _, err := service.Start(RunRequest{Action: "refresh", Ranks: []int{1}, BatchSize: 1}); err != nil {
		t.Fatalf("refresh Start() error = %v", err)
	}
	waitForBatch(t, service, 2)
	if grids.calls["L1"] != 2 {
		t.Fatalf("grid calls after refresh = %d, want 2", grids.calls["L1"])
	}

	if _, err := service.Start(RunRequest{Action: "rebuild", BatchSize: 1}); err != nil {
		t.Fatalf("rebuild Start() error = %v", err)
	}
	snapshot := waitForBatch(t, service, 1)
	if snapshot.Summary.CompletedMarkets != 1 || len(snapshot.Batches) != 1 {
		t.Fatalf("rebuild did not replace index: %+v", snapshot)
	}
	if grids.calls["L1"] != 3 {
		t.Fatalf("grid calls after rebuild = %d, want 3", grids.calls["L1"])
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("rebuild backup missing: %v", err)
	}
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
	if _, err := service.Start(RunRequest{Action: "continue", BatchSize: 1}); err != nil {
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

func TestServiceStopLeavesWorkResumable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "market_index.json")
	providers := &fakeProviders{responses: map[string][]web.Provider{"10001": {testProvider("BLOCK")}}}
	grid := &blockingGrid{started: make(chan struct{})}
	service, err := NewService(ServiceConfig{
		Path:      path,
		Catalog:   testCatalog(MarketSeed{Rank: 1, Name: "New York", Country: "USA", PostalCode: "10001"}),
		Providers: providers,
		Grids:     grid,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if _, err := service.Start(RunRequest{Action: "continue", BatchSize: 1}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-grid.started:
	case <-time.After(2 * time.Second):
		t.Fatal("grid request did not start")
	}
	if !service.Stop() {
		t.Fatal("Stop() reported no running scan")
	}
	snapshot := waitForBatch(t, service, 1)
	if !snapshot.Batches[0].Stopped || snapshot.Summary.PendingMarkets != 1 || snapshot.Summary.ErrorMarkets != 0 {
		t.Fatalf("stopped scan is not resumable: batch=%+v summary=%+v", snapshot.Batches[0], snapshot.Summary)
	}
}
