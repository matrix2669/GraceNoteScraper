package lineuparr

import (
	"context"
	"testing"
)

func TestProviderSourceInventoryIncludesRequestedProviders(t *testing.T) {
	sources := ProviderGuideSources()
	wanted := map[string]bool{
		"verizon-fios": false, "optimum": false, "directv": false, "dish": false,
		"att-uverse": false, "xfinity": false, "spectrum": false, "broadstar": false,
	}
	for _, source := range sources {
		if source.ID == "afn" || source.ID == "glorystar" {
			t.Fatalf("excluded enrichment source remains: %s", source.ID)
		}
		if _, ok := wanted[source.ID]; ok {
			wanted[source.ID] = source.URL != "" && source.Access != ""
		}
	}
	for id, complete := range wanted {
		if !complete {
			t.Fatalf("provider source %q is missing or incomplete", id)
		}
	}
}

func TestProviderSourceLocationRequirements(t *testing.T) {
	tests := []struct {
		provider string
		location string
		postal   string
		wantID   string
		wantMode string
		wantURL  string
	}{
		{provider: "Optimum of Woodbury - Digital Rebuild", location: "Hicksville", postal: "11743", wantID: "optimum", wantMode: "market-list", wantURL: "https://www.optimum.net/pages/channel-lineups.html"},
		{provider: "Optimum", location: "Bridgeport", postal: "06604", wantID: "optimum", wantMode: "market-list", wantURL: "https://www.optimum.net/pages/channel-lineups.html"},
		{provider: "Optimum", location: "Newark", postal: "07102", wantID: "optimum", wantMode: "market-list", wantURL: "https://www.optimum.net/pages/channel-lineups.html"},
		{provider: "Optimum", location: "Philadelphia", postal: "19103", wantID: "optimum", wantMode: "market-list", wantURL: "https://www.optimum.net/pages/channel-lineups.html"},
		{provider: "Optimum", location: "Hendersonville", wantID: "optimum", wantMode: "market-list", wantURL: "https://www.optimum.net/pages/channel-lineups.html"},
		{provider: "Optimum of West Jefferson", wantID: "optimum", wantMode: "market-list", wantURL: "https://www.optimum.net/pages/channel-lineups.html"},
		{provider: "Optimum", location: "Dallas", postal: "75001", wantID: "optimum", wantMode: "address", wantURL: "https://www.optimum.com/tvlineup"},
		{provider: "Comcast Xfinity", wantID: "xfinity", wantMode: "address", wantURL: "https://www.xfinity.com/support/local-channel-lineup/"},
		{provider: "Charter Spectrum", wantID: "spectrum", wantMode: "postal-code", wantURL: "https://www.spectrum.com/cable-tv/channel-lineup"},
		{provider: "Verizon FiOS", wantID: "verizon-fios", wantMode: "postal-code", wantURL: "https://www.verizon.com/home/fios-tv/channel-lineup/"},
		{provider: "DIRECTV", wantID: "directv", wantMode: "postal-code-county", wantURL: "https://www.directv.com/channel-lineup/"},
	}
	for _, test := range tests {
		source, ok := ProviderGuideSourceForLineup(test.provider, test.location, test.postal)
		if !ok || source.ID != test.wantID || source.LocationMode != test.wantMode || source.URL != test.wantURL {
			t.Errorf("ProviderGuideSourceForLineup(%q, %q, %q) = %+v, %v", test.provider, test.location, test.postal, source, ok)
		}
	}
	if source, ok := ProviderGuideSourceFor("Unknown Cable"); ok {
		t.Fatalf("unexpected provider source = %+v", source)
	}
}

func TestProviderGuideStatusUsesResolvedOptimumSource(t *testing.T) {
	statuses := ApplyProviderGuideAliasesForLineup("Optimum of Woodbury - Digital Rebuild", "Hicksville", "11743", nil)
	if len(statuses) != 1 || statuses[0].ID != "provider-guide-optimum" || statuses[0].LocationMode != "market-list" || statuses[0].URL != "https://www.optimum.net/pages/channel-lineups.html" {
		t.Fatalf("provider source statuses = %+v", statuses)
	}
}

func TestProviderGuideRenamePromotesCurrentNameFromFormerIdentity(t *testing.T) {
	inputs := []InputChannel{{Key: "1", StationID: "S1", CallSign: "TVG"}}
	statuses := ApplyProviderGuideAliases("Optimum", inputs)
	if inputs[0].PreferredName == nil || inputs[0].PreferredName.Value != "FanDuel TV" {
		t.Fatalf("preferred name = %+v", inputs[0].PreferredName)
	}
	service := newTestService(t, "", "")
	draft, err := service.Build(context.Background(), testContext("source-one"), inputs)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Channels[0].Name != "FanDuel TV" || !contains(draft.Channels[0].Aliases, "TVG") {
		t.Fatalf("renamed channel = %+v; statuses = %+v", draft.Channels[0], statuses)
	}
}

func TestProviderGuideRenamesRemainAliases(t *testing.T) {
	inputs := []InputChannel{{Key: "1", StationID: "S1", CallSign: "FanDuel TV"}}
	statuses := ApplyProviderGuideAliases("Optimum", inputs)
	if len(inputs[0].ExternalAliases) != 1 || inputs[0].ExternalAliases[0].Value != "TVG" {
		t.Fatalf("renamed aliases = %+v", inputs[0].ExternalAliases)
	}
	foundEvidence := false
	for _, status := range statuses {
		if status.ID == "provider-guide-broadstar" && status.Matched == 1 {
			foundEvidence = true
		}
	}
	if !foundEvidence {
		t.Fatalf("provider source statuses = %+v", statuses)
	}
}

func TestNetworkCatalogUsesExactGracenoteStationID(t *testing.T) {
	inputs := []InputChannel{
		{Key: "match", StationID: "78808", CallSign: "AHC"},
		{Key: "miss", StationID: "different", CallSign: "AHC"},
	}
	statuses := ApplyNetworkCatalog(inputs)
	if len(statuses) != 1 || statuses[0].ID != "prismcast-network-catalog" || statuses[0].Matched != 1 {
		t.Fatalf("network status = %+v", statuses)
	}
	if inputs[0].CategoryHint == nil || inputs[0].CategoryHint.Value != "Entertainment" {
		t.Fatalf("network category = %+v", inputs[0].CategoryHint)
	}
	if len(inputs[0].ExternalAliases) < 2 || inputs[0].ExternalAliases[0].Value != "American Heroes" {
		t.Fatalf("network aliases = %+v", inputs[0].ExternalAliases)
	}
	if inputs[1].CategoryHint != nil || len(inputs[1].ExternalAliases) != 0 {
		t.Fatalf("nonmatching station received evidence = %+v", inputs[1])
	}
}

func TestPBSCatalogUsesExactGracenoteStationID(t *testing.T) {
	inputs := []InputChannel{{Key: "wnet", StationID: "26182", CallSign: "WNETDT"}}
	statuses := ApplyPBSCatalog(inputs)
	if len(statuses) != 1 || statuses[0].Matched != 1 || statuses[0].ID != "pbs-gracenote-station-map" {
		t.Fatalf("PBS status = %+v", statuses)
	}
	if inputs[0].CategoryHint == nil || inputs[0].CategoryHint.Value != "Local & Public" {
		t.Fatalf("PBS category = %+v", inputs[0].CategoryHint)
	}
	if len(inputs[0].ExternalAliases) == 0 {
		t.Fatal("PBS exact-ID aliases were not applied")
	}
}

func TestEmbeddedCatalogsRequireExplicitOptIn(t *testing.T) {
	inputs := []InputChannel{{StationID: "26182", CallSign: "WNETDT"}}
	service := NewService(nil, ServiceOptions{})
	if statuses := service.ApplyEmbeddedCatalogs(inputs); len(statuses) != 0 || inputs[0].CategoryHint != nil {
		t.Fatalf("default embedded catalogs = %+v, input = %+v", statuses, inputs[0])
	}
	service = NewService(nil, ServiceOptions{UseEmbeddedCatalogs: true})
	if statuses := service.ApplyEmbeddedCatalogs(inputs); len(statuses) != 2 || inputs[0].CategoryHint == nil {
		t.Fatalf("enabled embedded catalogs = %+v, input = %+v", statuses, inputs[0])
	}
}

func TestPBSCatalogSuppressesAliasesOwnedByDifferentStations(t *testing.T) {
	inputs := []InputChannel{{StationID: "30481"}, {StationID: "45073"}}
	ApplyPBSCatalog(inputs)
	for _, input := range inputs {
		for _, alias := range input.ExternalAliases {
			if providerNameKey(alias.Value) == "PBS" {
				t.Fatalf("shared PBS alias applied to station %s: %+v", input.StationID, input.ExternalAliases)
			}
		}
	}
}

func TestExactStationCatalogSuppressesConflictingCategories(t *testing.T) {
	inputs := []InputChannel{{StationID: "100"}}
	first := []byte(`{"schemaVersion":1,"source":{"id":"one","label":"One","url":"https://example.com/one","license":"test","commit":"1234567890123","method":"exact ID"},"channels":[{"stationId":"100","name":"Example","category":"Sports"}]}`)
	second := []byte(`{"schemaVersion":1,"source":{"id":"two","label":"Two","url":"https://example.com/two","license":"test","commit":"1234567890123","method":"exact ID"},"channels":[{"stationId":"100","name":"Example","category":"News"}]}`)
	applyExactStationCatalog(first, "one", "One", inputs)
	status := applyExactStationCatalog(second, "two", "Two", inputs)
	if inputs[0].CategoryHint != nil || !inputs[0].CategoryConflict {
		t.Fatalf("conflicting category was retained: %+v", inputs[0])
	}
	if len(status) != 1 || status[0].Ambiguous != 1 {
		t.Fatalf("conflict status = %+v", status)
	}
}
