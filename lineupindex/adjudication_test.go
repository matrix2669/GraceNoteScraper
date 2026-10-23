package lineupindex

import (
	"bytes"
	"encoding/json"
	"github.com/daniel-widrick/GraceNoteScraper/web"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func adjudicationFixture() (Index, StationFact, ProviderCategoryRelation) {
	a := reviewedAssignmentAdjudications[0].Assignment
	f := StationFact{Kind: FactAlias, Value: "Unsupported", Normalized: "UNSUPPORTED", SourceID: a.SourceID, SourceRevision: "original", SourceRowID: a.SourceRowID, RootSourceStationID: a.RootStationID, RootSourceLineupKey: a.RootLineupKey, LineupKeys: []string{a.RootLineupKey}, EvidencePolicy: ProviderIdentityPolicy, Method: "number-policy-provider-alias-v3; provider-source-alignment-v1"}
	r := ProviderCategoryRelation{StationID: a.RootStationID, AliasValue: f.Value, AliasNormalized: f.Normalized, Category: "Local & Public", RawCategory: "Local channels", SourceID: f.SourceID, SourceRowID: f.SourceRowID, SourceLineupKey: f.RootSourceLineupKey, LineupKeys: f.LineupKeys, EvidencePolicy: ProviderIdentityPolicy, Method: f.Method}
	idx := Index{SchemaVersion: CurrentIndexVersion, Stations: map[string]*Station{a.RootStationID: {StationID: a.RootStationID, Names: []StationName{{Kind: NameCallSign, Value: "RAW", Normalized: "RAW"}}, Facts: []StationFact{f}}}, Lineups: map[string]*LineupRecord{a.RootLineupKey: {Key: a.RootLineupKey, Country: "USA", PostalCode: "synthetic", LineupID: "synthetic", ProviderName: "Verizon Fios", Device: "X"}}, CategoryRelations: []ProviderCategoryRelation{r}}
	return idx, f, r
}
func TestAdjudicationFullRootClosureAuditAndIndependentSupport(t *testing.T) {
	idx, f, r := adjudicationFixture()
	root := f.RootSourceStationID
	derived := f
	derived.SourceID = weekdayEPGSourcePrefix + "synthetic"
	derived.RootSourceID = f.SourceID
	derived.Value = "Different copy"
	derived.Normalized = "DIFFERENTCOPY"
	derived.Method = "identity-policy-v2"
	derived.EvidencePolicy = ProviderIdentityPolicy
	cat := derived
	cat.Kind = FactCategory
	cat.Value = "Sports"
	cat.Normalized = "SPORTS"
	independent := f
	independent.SourceID = "spectrum-official-lineup"
	independent.SourceRowID = "independent"
	independent.StationBound = true
	independent.Method = "exact Gracenote station ID"
	independent.SourceRevision = "catalog"
	independent.RootSourceLineupKey = ""
	independent.Value = f.Value
	otherStation := f
	otherStation.RootSourceStationID = "other"
	otherScope := f
	otherScope.RootSourceLineupKey = "other-scope"
	idx.Stations["copy"] = &Station{StationID: "copy", Facts: []StationFact{derived, cat}}
	idx.Stations["other"] = &Station{StationID: "other", Facts: []StationFact{otherStation}}
	idx.Stations[root].Facts = append(idx.Stations[root].Facts, independent, otherScope)
	dr := r
	dr.StationID = "copy"
	dr.SourceID = "derived-source"
	dr.RootSourceID = f.SourceID
	dr.RootSourceStationID = root
	dr.RootSourceLineupKey = f.RootSourceLineupKey
	idx.CategoryRelations = append(idx.CategoryRelations, dr)
	before, _ := json.Marshal(idx)
	audit := ApplyEvidenceQuarantine(&idx)
	after, _ := json.Marshal(idx)
	if !bytes.Equal(before, after) {
		t.Fatal("raw audit records mutated")
	}
	if usableFact(idx.Stations[root].Facts[0]) || usableFact(idx.Stations["copy"].Facts[0]) || usableFact(idx.Stations["copy"].Facts[1]) {
		t.Fatal("direct/derived closure active")
	}
	if !usableFact(idx.Stations[root].Facts[1]) || !usableFact(idx.Stations[root].Facts[2]) || !usableFact(idx.Stations["other"].Facts[0]) {
		t.Fatal("same text/row independent root removed")
	}
	if relationEligible(&Service{index: idx}, r) || rejectedRelation(dr) == nil {
		t.Fatal("relation admitted independently of invalid assignment")
	}
	if len(audit.Adjudications) != 1 || len(audit.Adjudications[0].OriginalAssertions) != 2 || len(audit.Facts) != 5 {
		t.Fatal("audit binding missing", audit)
	}
	for _, q := range audit.Facts {
		if q.AdjudicationID == "" || q.Assignment == nil {
			t.Fatal("audit full assignment omitted", q)
		}
	}
	// The original may disappear or be replaced by a new revision/method. The
	// compiled disposition survives and does not rely on finding a direct row.
	delete(idx.Stations, root)
	idx.Stations["copy"].Facts[0].SourceRevision = "changed"
	idx.Stations["copy"].Facts[0].Method = "rewritten"
	ApplyEvidenceQuarantine(&idx)
	if usableFact(idx.Stations["copy"].Facts[0]) {
		t.Fatal("orphan copied assignment rehabilitated")
	}
	idx.Stations = map[string]*Station{}
	idx.CategoryRelations = nil
	if len(ApplyEvidenceQuarantine(&idx).Adjudications[0].OriginalAssertions) != 2 {
		t.Fatal("reconciliation erased original audit")
	}
}
func TestAdjudicationContextCannotRehabilitateTuple(t *testing.T) {
	idx, f, _ := adjudicationFixture()
	for _, kind := range []string{"changed", "missing", "partial"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "missing" {
				idx.Lineups = nil
			}
			if kind == "partial" {
				idx.Lineups = map[string]*LineupRecord{f.RootSourceLineupKey: {Country: "USA"}}
			}
			report := ApplyEvidenceQuarantine(&idx)
			if usableFact(idx.Stations[f.RootSourceStationID].Facts[0]) {
				t.Fatal("context mutation rehabilitated assignment")
			}
			if report.Adjudications[0].ContextStatus == "verified" {
				t.Fatal("synthetic context falsely verified")
			}
		})
	}
}
func TestAdjudicationFreshEPGSeedsAndPartialLineage(t *testing.T) {
	idx, f, r := adjudicationFixture()
	sid := f.RootSourceStationID
	pf := ProviderFact{StationID: sid, Kind: FactAlias, Value: f.Value, SourceID: f.SourceID, SourceRowID: f.SourceRowID, RootSourceStationID: sid, RootSourceLineupKey: f.RootSourceLineupKey, EvidencePolicy: ProviderIdentityPolicy, Method: "identity-policy-v2"}
	scan := &postalLineupScan{Lineup: idx.Lineups[f.RootSourceLineupKey], Grids: map[string]*web.GridResponse{"b": {Channels: []web.JSONChannel{{ChannelID: sid, CallSign: "RAW"}}}}, Facts: epgIdentityFacts(ProviderEvidenceResult{Facts: []ProviderFact{pf}, IdentityFacts: []ProviderFact{pf}}), Relations: []ProviderCategoryRelation{r}}
	for _, p := range scan.Facts {
		if scanFactBindingValid(scan, p) {
			t.Fatal("rejected transient fact admitted")
		}
	}
	// A malformed claimed provider copy must not become an independent raw pair.
	c := EvidenceContext{Country: "USA", PostalCode: "synthetic", LineupID: "synthetic", Device: "X", Fingerprint: "source", RawCallSigns: map[string][]string{sid: {"RAW"}, "peer": {"PEER"}}}
	p := StationFact{Kind: FactAlias, Value: "PEER", SourceID: weekdayEPGSourcePrefix + "synthetic", PeerStationID: "peer", EvidencePolicy: ProviderIdentityPolicy, RootSourceID: f.SourceID, PairProof: &StationPairProof{TargetStationID: sid, TargetCallSigns: []string{"RAW"}, PeerStationID: "peer", PeerCallSign: "PEER", Timezone: "UTC", WindowStarts: []int64{0, 86400}, ProgrammeRevision: strings.Repeat("a", 64)}}
	if reusablePairFact(sid, p, c.RawCallSigns) {
		t.Fatal("partial provider lineage upgraded to independent proof")
	}
	p.RootSourceID = p.SourceID
	if !reusablePairFact(sid, p, c.RawCallSigns) {
		t.Fatal("independent raw pair lost")
	}
	// Existing fresh candidate builder consumes filtered Facts and Relations.
	scan.Facts = []ProviderFact{pf}
	stations, _ := buildEPGCandidates([]*postalLineupScan{scan}, "b")
	if len(stations[sid].ProviderNames) != 0 || len(stations[sid].Categories) != 0 {
		t.Fatal("fresh rejected identity/category seeded EPG candidates")
	}
}
func TestAdjudicationRestartReingestAndSnapshotRetention(t *testing.T) {
	idx, f, r := adjudicationFixture()
	dir := t.TempDir()
	path := filepath.Join(dir, "index.json")
	data, _ := json.Marshal(idx)
	os.WriteFile(path, data, 0600)
	svc, err := NewService(ServiceConfig{Path: path, Providers: &fakeProviders{}, Grids: &fakeGrids{}})
	if err != nil {
		t.Fatal(err)
	}
	pf := ProviderFact{StationID: f.RootSourceStationID, Kind: f.Kind, Value: f.Value, SourceID: f.SourceID, SourceRowID: f.SourceRowID, SourceRevision: "new-revision", RootSourceStationID: f.RootSourceStationID, RootSourceLineupKey: f.RootSourceLineupKey, EvidencePolicy: ProviderIdentityPolicy, Method: "rewritten"}
	evidence := ProviderEvidenceResult{Facts: []ProviderFact{pf}, CategoryRelations: []ProviderCategoryRelation{r}, Sources: []EvidenceSourceRecord{{ID: f.SourceID, Status: StatusComplete}}}
	if _, _, err = svc.ingestProviderEvidence(f.RootSourceLineupKey, evidence); err != nil {
		t.Fatal(err)
	}
	if usableFact(svc.index.Stations[f.RootSourceStationID].Facts[0]) {
		t.Fatal("reingest reactivated assignment")
	}
	// Stored raw snapshots keep the assertion; replay remains filtered.
	if err = svc.writeLineupSnapshot(*idx.Lineups[f.RootSourceLineupKey], &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: f.RootSourceStationID, CallSign: "RAW"}}}, evidence); err != nil {
		t.Fatal(err)
	}
	l := idx.Lineups[f.RootSourceLineupKey]
	rawSnapshot, err := os.ReadFile(lineupSnapshotPath(svc.snapshotDir, l.Country, l.PostalCode, l.Key))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot LineupSnapshot
	if err = json.Unmarshal(rawSnapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Channels) != 1 || len(snapshot.Channels[0].Aliases) != 1 {
		t.Fatal("raw audit assertion erased from snapshot")
	}
	old := snapshot.Channels[0].Aliases[0]
	replay := pf
	replay.SourceRowID = old.SourceRowID
	replay.SourceRevision = old.SourceRevision
	replay.RootSourceID = old.RootSourceID
	replay.RootSourceStationID = old.RootSourceStationID
	replay.RootSourceLineupKey = old.RootSourceLineupKey
	replay.Method = old.Method
	if _, _, err = svc.ingestProviderEvidence(f.RootSourceLineupKey, ProviderEvidenceResult{Facts: []ProviderFact{replay}}); err != nil {
		t.Fatal(err)
	}
	if usableFact(svc.index.Stations[f.RootSourceStationID].Facts[0]) {
		t.Fatal("raw snapshot replay reactivated assignment")
	}
	reloaded, err := loadIndex(path, SeedCatalog{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ApplyEvidenceQuarantine(&reloaded)
	if usableFact(reloaded.Stations[f.RootSourceStationID].Facts[0]) {
		t.Fatal("restart reactivated row")
	}
	if _, _, err = svc.ingestProviderEvidence(f.RootSourceLineupKey, ProviderEvidenceResult{Sources: evidence.Sources}); err != nil {
		t.Fatal(err)
	}
	if len(ApplyEvidenceQuarantine(&svc.index).Adjudications[0].OriginalAssertions) != 2 {
		t.Fatal("successful empty reconciliation erased policy audit")
	}
}
