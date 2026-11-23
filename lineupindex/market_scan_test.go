package lineupindex

import (
	"context"
	"github.com/daniel-widrick/GraceNoteScraper/web"
	"path/filepath"
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
	if len(evidence.calls) != 1 || evidence.calls[0].AllowChannelNumbers || evidence.calls[0].ServiceAddress.FormattedAddress != "" {
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
