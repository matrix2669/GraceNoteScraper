package lineuparr

import (
	"context"
	"encoding/json"
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
	if c.Name != "WEHD" || c.OriginalName != "WEHD" || c.CallSign != "WEHD" || c.ID != "id" || !containsFold(c.Aliases, "WEHD") || !containsFold(c.Aliases, "WE TV") {
		t.Fatalf("identity changed: %+v", c)
	}
	if len(d.CanonicalNameProposals) != 1 || d.CanonicalNameProposals[0].ProposedName != "WE tv" || !strings.Contains(d.CanonicalNameProposals[0].Method, "wetv:stationId") {
		t.Fatalf("proposal missing provenance: %+v", d.CanonicalNameProposals)
	}
	e := ExportFromDraft(d)
	if len(e.Categories[c.Category]) != 1 || e.Categories[c.Category][0].Name != "WEHD" {
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
	if by["one"].Name != "RAW" || len(d.CanonicalNameProposals) != 1 || d.CanonicalNameProposals[0].Status != "held" {
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
		if c.Name != "RAW" || len(d.CanonicalNameProposals) != 0 {
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

func TestCanonicalSuppressionFinalFormEvenWithoutActiveAlias(t *testing.T) {
	store, err := LoadStateStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetAliasSuppressed("test", "id", "WE tv", true); err != nil {
		t.Fatal(err)
	}
	p := ServiceIdentityEvidence{BindingKind: "station-id-catalog-v1", RootStationID: "s", SourceID: "source", SourceRowID: "row", SourceRevision: "rev", Policy: "verified-portable-station-alias-v1"}
	a := AttributedAlias{Value: "WE tv - East", Source: "source", Method: "row", Identity: &p}
	d, err := NewService(store, ServiceOptions{}).Build(context.Background(), LineupContext{SourceFingerprint: "test", Country: "US"}, []InputChannel{{Key: "id", StationID: "s", CallSign: "WEHD", ExternalAliases: []AttributedAlias{a}, CanonicalNameCandidates: []AttributedAlias{a}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.CanonicalNameProposals) != 0 || d.Channels[0].Name != "WEHD" {
		t.Fatalf("resurrected suppression: %+v", d.CanonicalNameProposals)
	}
}
func TestCanonicalCrossCategoryGlobalAliasPoolCollision(t *testing.T) {
	p := ServiceIdentityEvidence{BindingKind: "station-id-catalog-v1", RootStationID: "s", SourceID: "source", SourceRowID: "row", SourceRevision: "rev", Policy: "verified-portable-station-alias-v1"}
	a := AttributedAlias{Value: "WE tv - East", Source: "source", Method: "row", Identity: &p}
	in := []InputChannel{{Key: "one", StationID: "s", CallSign: "WEHD", ExternalAliases: []AttributedAlias{a}, CanonicalNameCandidates: []AttributedAlias{a}, CategoryHint: &AttributedCategory{Value: "Entertainment", Source: "fixture", Method: "test"}}, {Key: "two", StationID: "t", CallSign: "WE tv", ExternalAliases: []AttributedAlias{{Value: "Zqx Sports Only", Source: "test", Method: "fixture"}}, CategoryHint: &AttributedCategory{Value: "Sports", Source: "fixture", Method: "test"}}}
	store, err := LoadStateStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(store, ServiceOptions{})
	sports := "Sports"
	if err := svc.UpdateChannel("test", "two", ChannelUpdate{Category: &sports}); err != nil {
		t.Fatal(err)
	}
	d, err := svc.Build(context.Background(), LineupContext{SourceFingerprint: "test", Country: "US"}, in)
	if err != nil {
		t.Fatal(err)
	}
	if d.Channels[0].Category == d.Channels[1].Category {
		t.Fatalf("fixture categories did not differ")
	}
	if len(d.CanonicalNameProposals) != 1 || d.CanonicalNameProposals[0].Status != "held" {
		t.Fatalf("pool collision unheld: %+v", d.CanonicalNameProposals)
	}
}

func TestCanonicalValidationCopyChangesOnlyProspectiveExport(t *testing.T) {
	in := []InputChannel{{Key: "id", StationID: "59296", CallSign: "WEHD", Number: "649"}}
	ApplyVerifiedStationCatalogs(in)
	base := canonicalBuild(t, in)
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	var prospective Draft
	if err = json.Unmarshal(raw, &prospective); err != nil {
		t.Fatal(err)
	}
	if err = ApplyCanonicalProposalsForValidation(&prospective, map[string]bool{base.CanonicalNameProposals[0].Hash: true}); err != nil {
		t.Fatal(err)
	}
	old, new := ExportFromDraft(base), ExportFromDraft(&prospective)
	c := base.Channels[0].Category
	if old.Categories[c][0].Name != "WEHD" || new.Categories[c][0].Name != "WE tv" {
		t.Fatalf("bad exports %s -> %s", old.Categories[c][0].Name, new.Categories[c][0].Name)
	}
	if strings.Join(old.Categories[c][0].Aliases, "|") != strings.Join(new.Categories[c][0].Aliases, "|") {
		t.Fatal("proposal changed aliases")
	}
}
