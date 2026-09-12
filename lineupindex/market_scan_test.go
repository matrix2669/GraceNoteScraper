package lineupindex

import (
	"context"
	"encoding/json"
	"github.com/daniel-widrick/GraceNoteScraper/web"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type marketEvidenceSpy struct {
	mu    sync.Mutex
	calls []ProviderEvidenceRequest
}

func (e *marketEvidenceSpy) FetchProviderEvidence(ctx context.Context, r ProviderEvidenceRequest) (ProviderEvidenceResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, r)
	return fakeEvidence{}.FetchProviderEvidence(ctx, r)
}
func waitMarket(t *testing.T, s *Service) MarketScanView {
	t.Helper()
	for i := 0; i < 500; i++ {
		v := s.MarketView()
		if !v.Job.Running {
			return v
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("market did not finish")
	return MarketScanView{}
}
func TestOneMarketAddressSkipsAndCounterfactual(t *testing.T) {
	a, b := testProvider("L1"), testProvider("L2")
	a.Name = "DISH Network"
	b.Name = "Xfinity"
	a.Timezone = "America/New_York"
	b.Timezone = "America/New_York"
	evidence := &marketEvidenceSpy{}
	config := ServiceConfig{Path: filepath.Join(t.TempDir(), "index.json"), Providers: &fakeProviders{responses: map[string][]web.Provider{"10001": {a, b}, "90012": {a, b}}}, Grids: &fakeGrids{responses: map[string]*web.GridResponse{"L1": {Channels: []web.JSONChannel{{ChannelID: "S1", CallSign: "ESPN"}}}, "L2": {Channels: []web.JSONChannel{{ChannelID: "S2", CallSign: "OTHER"}}}}, calls: map[string]int{}, failures: map[string]int{}}, Evidence: evidence, CurrentStations: func() map[string][]string { return map[string][]string{"S1": {"ESPN"}} }, ProviderAccess: func(p web.Provider, _ string) string {
		if p.Name == "Xfinity" {
			return "address-required"
		}
		return "public"
	}}
	s, err := NewService(config)
	if err != nil {
		t.Fatal(err)
	}
	s.index.Lineups["local"] = &LineupRecord{ProviderName: a.Name, Status: StatusComplete}
	if _, err = s.StartNextMarket(nil); err != nil {
		t.Fatal(err)
	}
	view := waitMarket(t, s)
	if len(view.Scans) != 1 || view.Next.Rank != 2 {
		t.Fatal(view)
	}
	record := view.Scans[0]
	if record.Status != StatusComplete || len(record.ProviderAudit) != 2 || record.AllProviderYield.Categories != 1 || record.NewFamilyYield.Categories != 0 {
		t.Fatalf("report %+v", record)
	}
	evidence.mu.Lock()
	if len(evidence.calls) != 1 || !evidence.calls[0].AllowChannelNumbers || evidence.calls[0].Grid.Channels[0].ChannelID != "S1" || evidence.calls[0].ServiceAddress.FormattedAddress != "" {
		t.Fatal(evidence.calls)
	}
	evidence.mu.Unlock()
	if record.ProviderAudit[1].Access != "address-required" {
		t.Fatal(record.ProviderAudit)
	}
	reopened, err := NewService(config)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.MarketView().Next.Rank != 2 {
		t.Fatal("restart lost progress")
	}
	if _, err = reopened.StartNextMarket(nil); err != nil {
		t.Fatal(err)
	}
	view = waitMarket(t, reopened)
	if len(view.Scans) != 2 || view.Next.Rank != 3 {
		t.Fatal(view)
	}
	if len(reopened.index.Lineups) != 5 {
		t.Fatalf("repeated lineup IDs overwrote market records: %d", len(reopened.index.Lineups))
	}
	grids := config.Grids.(*fakeGrids)
	grids.mu.Lock()
	before := grids.calls["L1"]
	grids.mu.Unlock()
	if _, err := reopened.StartMarket(1, nil); err != nil {
		t.Fatal(err)
	}
	view = waitMarket(t, reopened)
	grids.mu.Lock()
	after := grids.calls["L1"]
	grids.mu.Unlock()
	if len(view.Scans) != 2 || view.Next.Rank != 3 || after <= before {
		t.Fatalf("rescan must rerun existing market without consuming next: %+v, calls %d -> %d", view, before, after)
	}
	if _, err := reopened.StartMarket(0, nil); err == nil {
		t.Fatal("invalid rank accepted")
	}
	reopened.mu.Lock()
	reopened.job.Running = true
	reopened.mu.Unlock()
	if _, err := reopened.StartMarket(1, nil); err != ErrAlreadyRunning {
		t.Fatalf("rescan bypassed busy guard: %v", err)
	}
}
func TestMarketCatalogAndNumberOnlyEPGGuard(t *testing.T) {
	catalog := marketCatalog()
	if len(catalog.Markets) != 100 {
		t.Fatal(len(catalog.Markets))
	}
	for i, m := range catalog.Markets {
		if m.Rank != i+1 || len(m.PostalCode) != 5 {
			t.Fatal(m)
		}
	}
	if catalog.Markets[2].Name != "Chicago, IL" || catalog.Markets[2].PostalCode != "60611" {
		t.Fatalf("Chicago market seed = %+v", catalog.Markets[2])
	}
	if hasStrongEPGIdentityEvidence([]string{"provider-position:comcast|2"}) {
		t.Fatal("number accepted as EPG identity")
	}
	if !hasStrongEPGIdentityEvidence([]string{"identity-name:wcbs"}) {
		t.Fatal("name identity rejected")
	}
	if usableFact(StationFact{Method: "exact provider channel number plus exact identity across same-number variants; identity-policy-v2"}) {
		t.Fatal("older number-scoped evidence not quarantined")
	}
}

func TestChicagoReferenceAddressIsXfinityOnlyAndEphemeral(t *testing.T) {
	reference, ok := marketReferenceAddressForRank(3, "60611")
	if !ok || reference.ProviderFamily != "xfinity" || reference.Address.FormattedAddress != "401 N Wabash Ave, Chicago, IL 60611" {
		t.Fatalf("Chicago reference = %+v, found %v", reference, ok)
	}
	if _, ok := marketReferenceAddressForRank(3, "60601"); ok {
		t.Fatal("Chicago reference accepted for the old market ZIP")
	}

	xfinity, dish, spectrum := testProvider("L1"), testProvider("L2"), testProvider("L3")
	xfinity.Name = "Xfinity Chicago Areas 1,4,&5"
	dish.Name = "DISH Chicago"
	spectrum.Name = "Spectrum Chicago"
	for _, provider := range []*web.Provider{&xfinity, &dish, &spectrum} {
		provider.Timezone = "America/Chicago"
	}
	evidence := &marketEvidenceSpy{}
	directory := t.TempDir()
	service, err := NewService(ServiceConfig{
		Path: filepath.Join(directory, "index.json"), SnapshotDir: filepath.Join(directory, "snapshots"),
		Providers: &fakeProviders{responses: map[string][]web.Provider{"60611": {xfinity, dish, spectrum}}},
		Grids: &fakeGrids{responses: map[string]*web.GridResponse{
			"L1": {Channels: []web.JSONChannel{{ChannelID: "S1", CallSign: "ESPN"}}},
			"L2": {Channels: []web.JSONChannel{{ChannelID: "S2", CallSign: "OTHER"}}},
			"L3": {Channels: []web.JSONChannel{{ChannelID: "S3", CallSign: "THIRD"}}},
		}, calls: map[string]int{}, failures: map[string]int{}},
		Evidence: evidence,
		ProviderAccess: func(provider web.Provider, _ string) string {
			if strings.Contains(provider.Name, "Xfinity") || strings.Contains(provider.Name, "Spectrum") {
				return "address-required"
			}
			return "public"
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartMarket(3, nil); err != nil {
		t.Fatal(err)
	}
	view := waitMarket(t, service)
	if len(view.Scans) != 1 || view.Scans[0].PostalCode != "60611" {
		t.Fatalf("Chicago scan = %+v", view.Scans)
	}

	evidence.mu.Lock()
	calls := append([]ProviderEvidenceRequest(nil), evidence.calls...)
	evidence.mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("evidence calls = %+v", calls)
	}
	addresses := map[string]ProviderAddress{}
	for _, call := range calls {
		addresses[call.Provider.LineupID] = call.ServiceAddress
	}
	if addresses["L1"].FormattedAddress != reference.Address.FormattedAddress {
		t.Fatalf("Xfinity address = %+v", addresses["L1"])
	}
	if addresses["L2"].FormattedAddress != "" {
		t.Fatalf("public DISH received reference address: %+v", addresses["L2"])
	}
	if _, called := addresses["L3"]; called {
		t.Fatal("Spectrum used the Xfinity-only reference address")
	}

	viewJSON, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(viewJSON), "Wabash") {
		t.Fatal("reference address appeared in market API view")
	}
	if err := filepath.Walk(directory, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(data), "Wabash") {
			t.Fatalf("reference address persisted in %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMarketComparisonLineupLeavesRunningState(t *testing.T) {
	for _, test := range []struct {
		name       string
		failures   map[string]int
		wantStatus string
	}{
		{name: "complete", failures: map[string]int{}, wantStatus: StatusComplete},
		{name: "failed", failures: map[string]int{"COMPARE": 1}, wantStatus: StatusError},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := testProvider("MARKET")
			provider.Timezone = "America/New_York"
			service, err := NewService(ServiceConfig{
				Path:      filepath.Join(t.TempDir(), "index.json"),
				Providers: &fakeProviders{responses: map[string][]web.Provider{"10001": {provider}}},
				Grids: &fakeGrids{responses: map[string]*web.GridResponse{
					"MARKET":  {Channels: []web.JSONChannel{{ChannelID: "M1", CallSign: "MARKET"}}},
					"COMPARE": {Channels: []web.JSONChannel{{ChannelID: "C1", CallSign: "COMPARE"}}},
				}, failures: test.failures, calls: map[string]int{}},
				ProviderAccess: func(web.Provider, string) string { return "unsupported" },
			})
			if err != nil {
				t.Fatal(err)
			}
			comparison := &LineupRecord{
				ProviderName: "Selected provider", LineupID: "COMPARE", HeadendID: "COMPARE-HEADEND",
				Device: "X", Country: "USA", PostalCode: "33308", Timezone: "America/New_York",
			}
			if _, err := service.StartMarket(1, comparison); err != nil {
				t.Fatal(err)
			}
			waitMarket(t, service)
			lineup := service.index.Lineups["market:1:USA:10001|comparison"]
			if lineup == nil || lineup.Status != test.wantStatus || lineup.Status == StatusRunning {
				t.Fatalf("comparison lineup = %+v", lineup)
			}
		})
	}
}
