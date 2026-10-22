package lineuparr

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityRepresentationJSONFixture(t *testing.T) {
	store, err := LoadStateStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	proof := ServiceIdentityEvidence{BindingKind: "station-id-catalog-v1", RootStationID: "103", Value: "Family Brand RetroPlex", SourceID: "fixture", SourceRowID: "row", SourceRevision: "revision"}
	svc := NewService(store, ServiceOptions{})
	d, err := svc.Build(context.Background(), LineupContext{SourceFingerprint: "fixture", Country: "US"}, []InputChannel{
		{Key: "grio", StationID: "101", CallSign: "GRIOCH", ExternalAliases: []AttributedAlias{{Value: "TheGrio", Source: "fixture", Method: "admitted exact row"}}},
		{Key: "metvplus", StationID: "102", CallSign: "WZMEDT2", Affiliate: "METV+"},
		{Key: "retro", StationID: "103", CallSign: "RETROHD", ExternalAliases: []AttributedAlias{{Value: "Retro", Source: "schedule", Method: "admitted raw station alias"}, {Value: proof.Value, Source: proof.SourceID, Method: "exact station catalog row", Identity: &proof}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if output := os.Getenv("IDENTITY_EXPORT_FIXTURE"); output != "" {
		b, err := json.Marshal(ExportFromDraft(d))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(output, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
