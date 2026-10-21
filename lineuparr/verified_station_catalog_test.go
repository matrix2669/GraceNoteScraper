package lineuparr

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifiedCatalogCompleteProvenanceAndConflictCensus(t *testing.T) {
	var data struct {
		Channels    []verifiedStationRow `json:"channels"`
		Quarantined []string             `json:"quarantinedStationIds"`
	}
	if err := json.Unmarshal(verifiedStationCatalogData, &data); err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, r := range data.Channels {
		if r.StationID == "" || r.Revision == "" || r.Path == "" || r.RowID == "" {
			t.Fatalf("incomplete assertion: %+v", r)
		}
		if previous := names[r.StationID]; previous != "" && !strings.EqualFold(previous, r.Name) {
			t.Fatalf("conflicting ID %s", r.StationID)
		}
		names[r.StationID] = r.Name
		in := []InputChannel{{StationID: r.StationID}, {StationID: "not-" + r.StationID, Number: r.StationID, CallSign: r.Name}}
		ApplyVerifiedStationCatalogs(in)
		if len(in[0].ExternalAliases) == 0 || len(in[1].ExternalAliases) != 0 {
			t.Fatalf("exact-ID boundary: %+v", r)
		}
		for _, a := range in[0].ExternalAliases {
			if a.Identity == nil || a.Identity.RootStationID != r.StationID || a.Identity.SourceRevision == "" {
				t.Fatalf("lost provenance: %+v", a)
			}
		}
	}
	for _, id := range data.Quarantined {
		in := []InputChannel{{StationID: id}}
		ApplyVerifiedStationCatalogs(in)
		if len(in[0].ExternalAliases) != 0 {
			t.Fatalf("ambiguous source binding admitted: %s", id)
		}
	}
}

func TestVerifiedCatalogRecoversPortableDescriptorsWithoutPositions(t *testing.T) {
	for id, want := range map[string]string{"59303": "Travel Channel", "50038": "SportsNet New York", "35383": "MSG Sportsnet"} {
		in := []InputChannel{{StationID: id, Number: "unrelated"}}
		ApplyVerifiedStationCatalogs(in)
		found := false
		for _, a := range in[0].ExternalAliases {
			if strings.EqualFold(a.Value, want) {
				found = true
			}
			if strings.Contains(a.Value, "635") || strings.Contains(a.Value, "639") {
				t.Fatalf("provider position selector escaped: %+v", a)
			}
		}
		if !found {
			t.Fatalf("descriptor absent for %s: %+v", id, in)
		}
	}
}

func TestVerifiedCatalogSeparatesPacificSelectors(t *testing.T) {
	in := []InputChannel{{StationID: "59303"}, {StationID: "64525"}}
	ApplyVerifiedStationCatalogs(in)
	for _, a := range in[1].ExternalAliases {
		if !strings.Contains(strings.ToLower(a.Value), "west") && !strings.Contains(strings.ToLower(a.Value), "pacific") {
			t.Fatalf("generic selector inherited onto Pacific: %+v", a)
		}
	}
	if len(in[1].ExternalAliases) == 0 {
		t.Fatal("verified Pacific identity missing")
	}
}

func TestNationalPBSFallbackNeverBecomesLocalIdentity(t *testing.T) {
	in := []InputChannel{{StationID: "101364"}}
	ApplyVerifiedStationCatalogs(in)
	for _, a := range in[0].ExternalAliases {
		if strings.Contains(a.Value, "KACV") || strings.Contains(a.Value, "KAET") || strings.Contains(a.Value, "KAID") {
			t.Fatalf("national fallback promoted local affiliate: %+v", a)
		}
	}
	if len(in[0].ExternalAliases) == 0 {
		t.Fatal("portable national service identity lost")
	}
}

func TestVerifiedCatalogWithholdsGenericLocalSubchannels(t *testing.T) {
	inputs := []InputChannel{{Key: "one", StationID: "21526", CallSign: "WETADT2"}, {Key: "two", StationID: "116205", CallSign: "WETADT3"}}
	ApplyVerifiedStationCatalogs(inputs)
	for _, in := range inputs {
		for _, alias := range in.ExternalAliases {
			if strings.Contains(strings.ToLower(alias.Value), "pbs subchannel") {
				t.Fatalf("generic local subchannel alias admitted: %+v", alias)
			}
		}
	}
}

// Public registry IDs with synthetic distinct callsigns exercise the complete
// admission/export/consumer path; they are not assertions of live OTA positions.
func TestVerifiedSubchannelJSONFixture(t *testing.T) {
	output := os.Getenv("SUBCHANNEL_EXPORT_FIXTURE")
	if output == "" {
		t.Skip("consumer fixture output not requested")
	}
	inputs := []InputChannel{{Key: "one", StationID: "21526", CallSign: "WETADT2"}, {Key: "two", StationID: "116205", CallSign: "WETADT3"}}
	ApplyVerifiedStationCatalogs(inputs)
	store, err := LoadStateStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	draft, err := NewService(store, ServiceOptions{}).Build(context.Background(), LineupContext{SourceFingerprint: "fixture", Country: "US"}, inputs)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(ExportFromDraft(draft))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(output, data, 0600); err != nil {
		t.Fatal(err)
	}
}
