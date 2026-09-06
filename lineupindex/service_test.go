package lineupindex

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
	mu          sync.Mutex
	responses   map[string]*web.GridResponse
	responsesAt map[string]map[int64]*web.GridResponse
	failures    map[string]int
	calls       map[string]int
}

type variantGrids struct {
	mu    sync.Mutex
	calls map[string]int
}

type blockingGrid struct {
	started chan struct{}
}

type fakeEvidence struct{}

type crossStationCategoryEvidence struct{}

type captureEvidence struct {
	requests chan ProviderEvidenceRequest
	fail     bool
}

func (f captureEvidence) FetchProviderEvidence(_ context.Context, request ProviderEvidenceRequest) (ProviderEvidenceResult, error) {
	f.requests <- request
	result := ProviderEvidenceResult{Sources: []EvidenceSourceRecord{{
		ID: "official-" + request.Provider.LineupID, Label: request.Provider.Name + " official source",
		Status: StatusError, Message: "source unavailable",
	}}}
	if f.fail {
		return result, errors.New("source unavailable")
	}
	return result, nil
}

func (fakeEvidence) FetchProviderEvidence(_ context.Context, request ProviderEvidenceRequest) (ProviderEvidenceResult, error) {
	if request.Provider.LineupID != "L1" {
		return ProviderEvidenceResult{}, nil
	}
	return ProviderEvidenceResult{
		Facts: []ProviderFact{
			{StationID: "S1", Kind: FactAlias, Value: "ESPN Full Name", SourceID: "provider-one", SourceLabel: "Provider One official lineup", Method: "unique exact provider callsign or name"},
			{StationID: "S1", Kind: FactCategory, Value: "Sports", SourceID: "provider-one", SourceLabel: "Provider One official lineup", Method: "unique exact provider callsign or name"},
		},
		Sources: []EvidenceSourceRecord{{ID: "provider-one", Label: "Provider One official lineup", Status: "complete", Matched: 1, Aliases: 1, Categories: 1}},
	}, nil
}

func (crossStationCategoryEvidence) FetchProviderEvidence(_ context.Context, request ProviderEvidenceRequest) (ProviderEvidenceResult, error) {
	if request.Provider.LineupID != "L2" {
		return ProviderEvidenceResult{}, nil
	}
	return ProviderEvidenceResult{
		Facts: []ProviderFact{{
			StationID: "RIGHT", Kind: FactCategory, Value: "Movies", SourceID: "directv-official", SourceLabel: "DIRECTV official lineup", Method: "unique exact provider callsign or name",
		}},
		Sources: []EvidenceSourceRecord{{ID: "directv-official", Label: "DIRECTV official lineup", Status: StatusComplete, Matched: 1, Categories: 1}},
	}, nil
}

func (f *blockingGrid) FetchGrid(ctx context.Context, _ web.Preferences, _ int64) (*web.GridResponse, error) {
	close(f.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func (f *fakeGrids) FetchGrid(_ context.Context, preferences web.Preferences, at int64) (*web.GridResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[preferences.LineupId]++
	if f.failures[preferences.LineupId] > 0 {
		f.failures[preferences.LineupId]--
		return nil, errors.New("temporary grid failure")
	}
	if byTime := f.responsesAt[preferences.LineupId]; byTime != nil && byTime[at] != nil {
		return byTime[at], nil
	}
	return f.responses[preferences.LineupId], nil
}

func (f *variantGrids) FetchGrid(_ context.Context, preferences web.Preferences, _ int64) (*web.GridResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := preferences.LineupId + "@" + preferences.Device
	f.calls[key]++
	stationID := "STATION-" + preferences.Device
	if preferences.Device == "" {
		stationID = "STATION-NONE"
	}
	return &web.GridResponse{Channels: []web.JSONChannel{{
		ChannelID: stationID, ChannelNo: "1", CallSign: stationID,
	}}}, nil
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

func TestPostalScanKeepsGracenoteDeviceVariantsSeparate(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "market_index.json")
	base := web.Provider{
		Type: "CABLE", LineupID: "USA-NY58806-DEFAULT", HeadendID: "NY58806",
		Name: "Optimum of Woodbury", PostalCode: "11743",
	}
	providers := &fakeProviders{responses: map[string][]web.Provider{"11743": {
		base,
		func() web.Provider { value := base; value.Device = "D"; value.Name += " - Digital"; return value }(),
		func() web.Provider {
			value := base
			value.Device = "L"
			value.Name += " - Digital Rebuild"
			return value
		}(),
		func() web.Provider { value := base; value.Device = "X"; value.Name += " - Digital"; return value }(),
		func() web.Provider { value := base; value.Device = "D"; value.Name += " - duplicate"; return value }(),
	}}}
	grids := &variantGrids{calls: make(map[string]int)}
	service, err := NewService(ServiceConfig{
		Path: path, SnapshotDir: filepath.Join(directory, "snapshots"),
		Catalog:   testCatalog(MarketSeed{Rank: 1, Name: "New York", Country: "USA", PostalCode: "10001"}),
		Providers: providers, Grids: grids,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(RunRequest{Action: "postal", Country: "USA", PostalCode: "11743", Language: "en-us"}); err != nil {
		t.Fatal(err)
	}
	snapshot := waitForPostal(t, service, "USA", "11743")
	if snapshot.PostalScan.Status != StatusComplete || snapshot.PostalScan.ProviderCount != 4 || snapshot.PostalScan.LineupsScanned != 4 {
		t.Fatalf("postal device variants = %+v", snapshot.PostalScan)
	}
	for _, device := range []string{"", "D", "L", "X"} {
		if grids.calls["USA-NY58806-DEFAULT@"+device] != 1 {
			t.Fatalf("grid calls = %+v", grids.calls)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted Index
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{
		"USA-NY58806-DEFAULT",
		"USA-NY58806-DEFAULT@device=D",
		"USA-NY58806-DEFAULT@device=L",
		"USA-NY58806-DEFAULT@device=X",
	}
	for _, key := range wantKeys {
		if persisted.Lineups[key] == nil {
			t.Fatalf("missing lineup variant %q in %+v", key, persisted.Lineups)
		}
	}
	files, err := filepath.Glob(filepath.Join(directory, "snapshots", "USA", "11743", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 {
		t.Fatalf("snapshot files = %d, want 4: %v", len(files), files)
	}
}

func TestPostalScanKeepsProviderAddressEphemeralAndSourceFailuresPartial(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "market_index.json")
	xfinity := testProvider("X1")
	xfinity.Name = "Xfinity Huntington"
	spectrum := testProvider("S1")
	spectrum.Name = "Spectrum Long Island"
	providers := &fakeProviders{responses: map[string][]web.Provider{"11743": {xfinity, spectrum}}}
	grids := &fakeGrids{responses: map[string]*web.GridResponse{
		"X1": {Channels: []web.JSONChannel{{ChannelID: "X", ChannelNo: "2", CallSign: "WCBS"}}},
		"S1": {Channels: []web.JSONChannel{{ChannelID: "S", ChannelNo: "5", CallSign: "WNYW"}}},
	}, failures: map[string]int{}, calls: map[string]int{}}
	captured := captureEvidence{requests: make(chan ProviderEvidenceRequest, 2), fail: true}
	service, err := NewService(ServiceConfig{
		Path: path, SnapshotDir: filepath.Join(directory, "snapshots"),
		Catalog:   testCatalog(MarketSeed{Rank: 1, Name: "New York", Country: "USA", PostalCode: "10001"}),
		Providers: providers, Grids: grids, Evidence: captured,
	})
	if err != nil {
		t.Fatal(err)
	}
	const address = "123 Private Street, Huntington, NY 11743"
	providerAddress := ProviderAddress{FormattedAddress: address, StreetAddress: "123 Private Street", City: "Huntington", State: "NY", PostalCode: "11743", CountryCode: "US"}
	if _, err := service.Start(RunRequest{
		Action: "postal", Country: "USA", PostalCode: "11743", Language: "en-us",
		ProviderAddress: providerAddress, AddressProvider: "Comcast",
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := waitForPostal(t, service, "USA", "11743")
	if snapshot.PostalScan.Status != StatusComplete || snapshot.PostalScan.LineupsScanned != 2 {
		t.Fatalf("provider source failures invalidated postal scan: %+v", snapshot.PostalScan)
	}
	addresses := make(map[string]ProviderAddress)
	for range 2 {
		request := <-captured.requests
		if !request.AllowChannelNumbers || len(request.Grid.Channels) != 1 || request.Grid.Channels[0].ChannelID != strings.TrimSuffix(request.Provider.LineupID, "1") {
			t.Fatalf("provider evidence must use its own grid with corroborated numbering: %+v", request)
		}
		addresses[request.Provider.LineupID] = request.ServiceAddress
	}
	if addresses["X1"].FormattedAddress != address || addresses["S1"].FormattedAddress != "" {
		t.Fatalf("address scope = %+v", addresses)
	}
	err = filepath.Walk(directory, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(data), address) {
			t.Fatalf("provider address persisted in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPostalScanConfirmsCrossStationAliasesWithTwoWeekdayBlocks(t *testing.T) {
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	directory := t.TempDir()
	providerOne := testProvider("L1")
	providerOne.Name = "Optimum of Woodbury"
	providerOne.Timezone = "America/New_York"
	providerTwo := testProvider("L2")
	providerTwo.Name = "DIRECTV"
	providerTwo.Timezone = "America/New_York"
	providers := &fakeProviders{responses: map[string][]web.Provider{"11743": {providerOne, providerTwo}}}
	blocks, _, err := weekdayEPGBlocks(now, []web.Provider{providerOne, providerTwo}, &web.ProviderResponse{})
	if err != nil {
		t.Fatal(err)
	}
	grids := &fakeGrids{
		responses: map[string]*web.GridResponse{}, responsesAt: map[string]map[int64]*web.GridResponse{},
		failures: map[string]int{}, calls: map[string]int{},
	}
	for _, provider := range []web.Provider{providerOne, providerTwo} {
		stationID := "LEFT"
		callSign := "WCBSDT"
		programPrefix := "LEFT"
		if provider.LineupID == "L2" {
			stationID, callSign, programPrefix = "RIGHT", "WCBSHD", "RIGHT"
		}
		grids.responsesAt[provider.LineupID] = map[int64]*web.GridResponse{
			blocks[0].Start.Unix(): {Channels: []web.JSONChannel{testEPGChannel(stationID, callSign, "CBS", blocks[0], []string{"News", "Prime 1", "Prime 2", "Prime 3", "Prime 4", "Late News"}, programPrefix)}},
			blocks[1].Start.Unix(): {Channels: []web.JSONChannel{testEPGChannel(stationID, callSign, "CBS", blocks[1], []string{"Talk 1", "Talk 2", "Drama 1", "Drama 2", "News 1", "News 2"}, programPrefix)}},
		}
	}
	service, err := NewService(ServiceConfig{
		Path:        filepath.Join(directory, "market_index.json"),
		SnapshotDir: filepath.Join(directory, "snapshots"),
		Catalog:     testCatalog(MarketSeed{Rank: 1, Name: "New York", Country: "USA", PostalCode: "10001"}),
		Providers:   providers, Grids: grids, Evidence: crossStationCategoryEvidence{}, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Start(RunRequest{Action: "postal", Country: "USA", PostalCode: "11743", Language: "en-us"}); err != nil {
		t.Fatal(err)
	}
	snapshot := waitForPostal(t, service, "USA", "11743")
	if snapshot.PostalScan.Status != StatusComplete || snapshot.PostalScan.EPGMatches != 1 || snapshot.PostalScan.EPGQuestionable != 0 || snapshot.PostalScan.EPGRejected != 0 {
		t.Fatalf("postal scan = %+v", snapshot.PostalScan)
	}
	if grids.calls["L1"] != 2 || grids.calls["L2"] != 2 {
		t.Fatalf("grid calls = %+v", grids.calls)
	}
	if snapshot.Job.CompletedCount != 4 || snapshot.Job.TotalCount != 4 {
		t.Fatalf("EPG request progress = %+v", snapshot.Job)
	}
	aliases := service.AliasesForStations([]string{"LEFT"})["LEFT"]
	foundRaw, foundNormalized := false, false
	for _, alias := range aliases {
		foundRaw = foundRaw || alias.Value == "WCBSHD"
		foundNormalized = foundNormalized || alias.Value == "WCBS"
	}
	if !foundRaw || !foundNormalized {
		t.Fatalf("cross-station aliases = %+v", aliases)
	}
	category, ok := service.CategoriesForStations([]string{"LEFT"})["LEFT"]
	if !ok || category.Value != "Movies" {
		t.Fatalf("cross-station category = %+v, %v", category, ok)
	}
	snapshotData, err := os.ReadFile(lineupSnapshotPath(service.snapshotDir, "USA", "11743", "L1"))
	if err != nil {
		t.Fatal(err)
	}
	var lineupSnapshot LineupSnapshot
	if err := json.Unmarshal(snapshotData, &lineupSnapshot); err != nil {
		t.Fatal(err)
	}
	if len(lineupSnapshot.Channels) != 1 || lineupSnapshot.Channels[0].Category != "Movies" {
		t.Fatalf("rewritten lineup snapshot = %+v", lineupSnapshot.Channels)
	}
	data, err := os.ReadFile(service.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Prime 1") || strings.Contains(string(data), "Talk 1") || strings.Contains(string(data), "events") {
		t.Fatal("programme payloads were persisted with EPG evidence")
	}
}

func TestProviderFamilyKeySupportsNationalLineupVariants(t *testing.T) {
	tests := map[string]string{
		"DIRECTV New York": "directv",
		"DISH Network":     "dish",
		"DISH New York":    "dish",
		"GLORYSTAR":        "glorystar",
	}
	for input, want := range tests {
		if got := providerFamilyKey(input); got != want {
			t.Errorf("providerFamilyKey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestReplaceEPGFactsReplacesPriorPostalEvidence(t *testing.T) {
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
	service.index.Stations["S1"] = &Station{StationID: "S1"}
	service.mu.Unlock()
	sourceID := weekdayEPGSourceID("USA", "11743")
	if _, _, _, err := service.replaceEPGFacts(sourceID, []epgDerivedFact{{
		ProviderFact: ProviderFact{StationID: "S1", Kind: FactAlias, Value: "OLD", SourceID: sourceID}, LineupKeys: []string{"L1"},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := service.replaceEPGFacts(sourceID, []epgDerivedFact{{
		ProviderFact: ProviderFact{StationID: "S1", Kind: FactAlias, Value: "NEW", SourceID: sourceID}, LineupKeys: []string{"L2"},
	}}); err != nil {
		t.Fatal(err)
	}
	aliases := service.AliasesForStations([]string{"S1"})["S1"]
	if len(aliases) != 1 || aliases[0].Value != "NEW" || len(aliases[0].LineupKeys) != 1 || aliases[0].LineupKeys[0] != "L2" {
		t.Fatalf("replacement aliases = %+v", aliases)
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

func TestCategoriesForStationsPrefersSelectedOfficialSource(t *testing.T) {
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
		{Kind: FactCategory, Value: "Entertainment", SourceID: "optimum-official-lineup", SourceLabel: "Optimum official lineup", Method: "unique exact provider callsign or name"},
		{Kind: FactCategory, Value: "Music", SourceID: "verizon-fios-official-lineup", SourceLabel: "Verizon FiOS official lineup", Method: "unique exact provider callsign or name"},
	}}
	service.mu.Unlock()

	category, ok := service.CategoriesForStationsWithPreferredSource([]string{"S1"}, "optimum-official-lineup")["S1"]
	if !ok || category.Value != "Entertainment" {
		t.Fatalf("preferred category = %+v, %v", category, ok)
	}
	if len(category.SourceIDs) != 1 || category.SourceIDs[0] != "optimum-official-lineup" {
		t.Fatalf("preferred category sources = %v", category.SourceIDs)
	}
	if _, ok := service.CategoriesForStations([]string{"S1"})["S1"]; ok {
		t.Fatal("legacy all-source lookup stopped rejecting the conflict")
	}
}

func TestCategoriesForStationsReevaluatesBroadProviderGroupsFromRawEvidence(t *testing.T) {
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
	service.index.Stations["local"] = &Station{StationID: "local", Names: []StationName{
		{Value: "WABC", Kind: NameCallSign}, {Value: "AMERICAN BROADCASTING COMPANY", Kind: NameAffiliateName},
	}, Facts: []StationFact{{
		Kind: FactCategory, Value: "Entertainment", RawValue: "Networks", Normalized: "ENTERTAINMENT",
		SourceID: "optimum-official-lineup", SourceLabel: "Optimum official lineup",
	}}}
	service.index.Stations["cable"] = &Station{StationID: "cable", Names: []StationName{
		{Value: "AETVHD", Kind: NameCallSign},
	}, Facts: []StationFact{{
		Kind: FactCategory, Value: "Entertainment", RawValue: "Networks", Normalized: "ENTERTAINMENT",
		SourceID: "optimum-official-lineup", SourceLabel: "Optimum official lineup",
	}}}
	service.mu.Unlock()

	categories := service.CategoriesForStationsWithPreferredSource([]string{"local", "cable"}, "optimum-official-lineup")
	if local, ok := categories["local"]; !ok || local.Value != "Local & Public" {
		t.Fatalf("local category = %+v, %v", local, ok)
	}
	if _, ok := categories["cable"]; ok {
		t.Fatal("broad Networks heading categorized an ordinary cable channel")
	}
}

func TestCategoriesForStationsRejectsPreferredSourceConflict(t *testing.T) {
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
		{Kind: FactCategory, Value: "Entertainment", SourceID: "optimum-official-lineup"},
		{Kind: FactCategory, Value: "Movies", SourceID: "optimum-official-lineup"},
		{Kind: FactCategory, Value: "Entertainment", SourceID: "verizon-fios-official-lineup"},
	}}
	service.mu.Unlock()

	if _, ok := service.CategoriesForStationsWithPreferredSource([]string{"S1"}, "optimum-official-lineup")["S1"]; ok {
		t.Fatal("conflicting categories within the preferred source were applied")
	}
}

func TestCategoriesForStationsRequiresAgreementWithoutPreferredEvidence(t *testing.T) {
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
		{Kind: FactCategory, Value: "Sports", SourceID: "dish-official-lineup"},
		{Kind: FactCategory, Value: "Sports", SourceID: "verizon-fios-official-lineup"},
	}}
	service.index.Stations["S2"] = &Station{StationID: "S2", Facts: []StationFact{
		{Kind: FactCategory, Value: "Sports", SourceID: "dish-official-lineup"},
		{Kind: FactCategory, Value: "News", SourceID: "verizon-fios-official-lineup"},
	}}
	service.mu.Unlock()

	categories := service.CategoriesForStationsWithPreferredSource([]string{"S1", "S2"}, "optimum-official-lineup")
	if category, ok := categories["S1"]; !ok || category.Value != "Sports" || len(category.SourceIDs) != 2 {
		t.Fatalf("agreed fallback category = %+v, %v", category, ok)
	}
	if _, ok := categories["S2"]; ok {
		t.Fatal("conflicting fallback categories were applied")
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
