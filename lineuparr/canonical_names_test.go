package lineuparr

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func canonicalBuild(t *testing.T, inputs []InputChannel) *Draft {
	t.Helper()
	store, err := LoadStateStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewService(store, ServiceOptions{}).Build(context.Background(), LineupContext{SourceFingerprint: "test", Country: "US"}, inputs)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestCanonicalCatalogNameAndRawAliases(t *testing.T) {
	inputs := []InputChannel{{Key: "id", StationID: "59296", CallSign: "WEHD", Number: "649"}}
	ApplyVerifiedStationCatalogs(inputs)
	d := canonicalBuild(t, inputs)
	c := d.Channels[0]
	if c.Name != "WE tv" || c.OriginalName != "WEHD" || c.CallSign != "WEHD" || c.ID != "id" || !containsFold(c.Aliases, "WEHD") || !containsFold(c.Aliases, "WE TV") {
		t.Fatalf("identity changed: %+v", c)
	}
	if c.NameSource != "prismcast-network-catalog" || !strings.Contains(c.NameMethod, "wetv:stationId") {
		t.Fatalf("missing provenance: %+v", c)
	}
	e := ExportFromDraft(d)
	if len(e.Categories[c.Category]) != 1 || e.Categories[c.Category][0].Name != c.Name {
		t.Fatal("export did not use resolved name")
	}
}
func TestCanonicalRejectsUnboundOrReviewOnly(t *testing.T) {
	for _, p := range []ServiceIdentityEvidence{{BindingKind: "user-attested-review-v1", RootStationID: "s", SourceID: "source", SourceRowID: "row", SourceRevision: "rev", Policy: "verified-portable-station-alias-v1"}, {BindingKind: "station-id-catalog-v1", RootStationID: "wrong", SourceID: "source", SourceRowID: "row", SourceRevision: "rev", Policy: "verified-portable-station-alias-v1"}, {BindingKind: "station-id-catalog-v1", RootStationID: "s", SourceID: "source", SourceRowID: "", SourceRevision: "rev", Policy: "verified-portable-station-alias-v1"}} {
		a := AttributedAlias{Value: "Description", Source: "source", Method: "row", Identity: &p}
		d := canonicalBuild(t, []InputChannel{{Key: "id", StationID: "s", CallSign: "RAW", ExternalAliases: []AttributedAlias{a}, CanonicalNameCandidates: []AttributedAlias{a}}})
		if d.Channels[0].Name != "RAW" {
			t.Fatalf("unsafe evidence: %+v", p)
		}
	}
}
func TestCanonicalCollisionAndPreferredPrecedence(t *testing.T) {
	p := ServiceIdentityEvidence{BindingKind: "station-id-catalog-v1", RootStationID: "s", SourceID: "source", SourceRowID: "row", SourceRevision: "rev", Policy: "verified-portable-station-alias-v1"}
	a := AttributedAlias{Value: "Service", Source: "source", Method: "row", Identity: &p}
	in := []InputChannel{{Key: "one", StationID: "s", CallSign: "RAW", ExternalAliases: []AttributedAlias{a}, CanonicalNameCandidates: []AttributedAlias{a}}, {Key: "two", StationID: "t", CallSign: " service "}}
	d := canonicalBuild(t, in)
	by := map[string]DraftChannel{}
	for _, c := range d.Channels {
		by[c.ID] = c
	}
	if by["one"].Name != "RAW" || by["one"].NameDiagnostic == "" {
		t.Fatalf("collision: %+v", by)
	}
	in[0].PreferredName = &AttributedAlias{Value: "Preferred", Source: "rename", Method: "explicit"}
	d = canonicalBuild(t, in)
	for _, c := range d.Channels {
		if c.ID == "one" && c.Name != "Preferred" {
			t.Fatal(c.Name)
		}
	}
}

// TestCanonicalCapturedDraftExport is an opt-in offline replay of an already
// captured draft. It does not fetch or mutate live state; paths are operator-owned.
func TestCanonicalCapturedDraftExport(t *testing.T) {
	input, output := os.Getenv("CANONICAL_CAPTURE_DRAFT"), os.Getenv("CANONICAL_CAPTURE_OUTPUT")
	if input == "" || output == "" {
		t.Skip("no private captured draft requested")
	}
	data, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	var d Draft
	if err = json.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	inputs := make([]InputChannel, len(d.Channels))
	for i, c := range d.Channels {
		inputs[i] = InputChannel{Key: c.ID, StationID: c.StationID, CallSign: c.CallSign}
		if strings.HasPrefix(c.NameSource, "provider-guide-") {
			inputs[i].PreferredName = &AttributedAlias{Value: c.Name, Source: c.NameSource, Method: c.NameMethod}
		}
	}
	ApplyVerifiedStationCatalogs(inputs)
	work := make([]*channelWork, len(inputs))
	for i, in := range inputs {
		work[i] = newChannelWork(in)
		work[i].draft = d.Channels[i]
	}
	resolveCanonicalNames(d.Channels, work)
	if out := os.Getenv("CANONICAL_CAPTURE_RESOLVED_DRAFT"); out != "" {
		b, err := json.Marshal(&d)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	b, err := json.Marshal(ExportFromDraft(&d))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(output, b, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalHonorsSuppressionInclusionAndCategory(t *testing.T) {
	store, err := LoadStateStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetAliasSuppressed("test", "id", "WE tv - East", true); err != nil {
		t.Fatal(err)
	}
	included := false
	category := "Sports"
	svc := NewService(store, ServiceOptions{})
	if err = svc.UpdateChannel("test", "id", ChannelUpdate{Included: &included, Category: &category}); err != nil {
		t.Fatal(err)
	}
	in := []InputChannel{{Key: "id", StationID: "59296", CallSign: "WEHD", Number: "649"}}
	ApplyVerifiedStationCatalogs(in)
	d, err := svc.Build(context.Background(), LineupContext{SourceFingerprint: "test", Country: "US"}, in)
	if err != nil {
		t.Fatal(err)
	}
	c := d.Channels[0]
	if c.Name != "WEHD" || c.Included || c.Category != "Sports" || containsFold(c.Aliases, "WE tv - East") || !containsFold(c.Aliases, "WEHD") {
		t.Fatalf("saved choices changed: %+v", c)
	}
}
func TestCanonicalConflictingRowsNeverFirstWins(t *testing.T) {
	p := ServiceIdentityEvidence{BindingKind: "station-id-catalog-v1", RootStationID: "s", SourceID: "source", SourceRowID: "row", SourceRevision: "rev", Policy: "verified-portable-station-alias-v1"}
	a := AttributedAlias{Value: "Network East", Source: "source", Method: "row", Identity: &p}
	b := a
	b.Value = "Network West"
	for _, options := range [][]AttributedAlias{{a, b}, {b, a}} {
		d := canonicalBuild(t, []InputChannel{{Key: "one", StationID: "s", CallSign: "RAW", ExternalAliases: options, CanonicalNameCandidates: options}})
		c := d.Channels[0]
		if c.Name != "RAW" || !strings.Contains(c.NameDiagnostic, "Conflicting") {
			t.Fatalf("order-dependent conflict: %+v", c)
		}
	}
}

func TestDefaultEastNameRetainsDistinctBrandsAndFeeds(t *testing.T) {
	for in, want := range map[string]string{"WE tv - East": "WE tv", "WE tv (East)": "WE tv", "East Network": "East Network", "Middle East TV": "Middle East TV", "Network - West": "Network - West", "Network Pacific": "Network Pacific", "Eastern Sports": "Eastern Sports", "Network HD": "Network HD"} {
		if got := defaultEastName(in); got != want {
			t.Fatalf("%q => %q; want %q", in, got, want)
		}
	}
}
