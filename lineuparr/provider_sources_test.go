package lineuparr

import (
	"context"
	"testing"
)

func TestProviderSourceInventoryIncludesRequestedProviders(t *testing.T) {
	sources := ProviderGuideSources()
	wanted := map[string]bool{
		"verizon-fios": false, "optimum": false, "directv": false, "dish": false, "afn": false,
		"glorystar": false, "att-uverse": false, "xfinity": false, "spectrum": false, "broadstar": false,
	}
	for _, source := range sources {
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
		wantID   string
		wantMode string
	}{
		{provider: "Optimum of Woodbury - Digital Rebuild", wantID: "optimum", wantMode: "address"},
		{provider: "Comcast Xfinity", wantID: "xfinity", wantMode: "address"},
		{provider: "Charter Spectrum", wantID: "spectrum", wantMode: "address"},
		{provider: "Verizon FiOS", wantID: "verizon-fios", wantMode: "postal-code"},
		{provider: "DIRECTV", wantID: "directv", wantMode: "postal-code-county"},
	}
	for _, test := range tests {
		source, ok := ProviderGuideSourceFor(test.provider)
		if !ok || source.ID != test.wantID || source.LocationMode != test.wantMode {
			t.Errorf("ProviderGuideSourceFor(%q) = %+v, %v", test.provider, source, ok)
		}
	}
	if source, ok := ProviderGuideSourceFor("Unknown Cable"); ok {
		t.Fatalf("unexpected provider source = %+v", source)
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
