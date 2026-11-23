package lineupindex

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHistoricalQuarantineIsNonDestructiveAndSourceScoped(t *testing.T) {
	bad := StationFact{Kind: FactAlias, Value: "Wrong Alias", Normalized: "WRONGALIAS", SourceID: "xfinity-official-lineup", Method: "number-policy-provider-alias-v3; provider-source-alignment-v1", LineupKeys: []string{"bad-scope"}, SourceRowID: "bad-row", RootSourceLineupKey: "bad-scope", RootSourceStationID: "sd"}
	derived := StationFact{Kind: FactAlias, Value: "Wrong Alias", Normalized: "WRONGALIAS", SourceID: "gracenote-weekday-epg-fixture", Method: "pair-level identity (identity-name:FOO); identity-policy-v2", SourceRowID: "bad-row", RootSourceID: "xfinity-official-lineup", RootSourceLineupKey: "bad-scope", RootSourceStationID: "sd"}
	good := derived
	good.Kind = FactCategory
	good.Value = "Sports"
	good.SourceRowID = "good-row"
	good.RootSourceID = "optimum-official-lineup"
	good.RootSourceLineupKey = "good-scope"
	idx := Index{SchemaVersion: CurrentIndexVersion, Stations: map[string]*Station{"sd": {StationID: "sd", Names: []StationName{{Kind: NameCallSign, Value: "FOO"}}, Facts: []StationFact{bad}}, "hd": {StationID: "hd", Names: []StationName{{Kind: NameCallSign, Value: "FOOHD"}}, Facts: []StationFact{derived, good}}}, Lineups: map[string]*LineupRecord{}, CategoryRelations: []ProviderCategoryRelation{{StationID: "sd", SourceID: "xfinity-official-lineup", Method: bad.Method, LineupKeys: []string{"bad-scope"}}}}
	before, e := json.Marshal(idx)
	if e != nil {
		t.Fatal(e)
	}
	report := ApplyEvidenceQuarantine(&idx)
	if len(report.Facts) != 3 {
		t.Fatalf("quarantine=%+v", report)
	}
	if usableFact(idx.Stations["sd"].Facts[0]) || usableFact(idx.Stations["hd"].Facts[0]) {
		t.Fatal("suspect evidence remained active")
	}
	if !usableFact(idx.Stations["hd"].Facts[1]) {
		t.Fatal("independent valid provider category quarantined")
	}
	after, _ := json.Marshal(idx)
	if !bytes.Equal(before, after) {
		t.Fatal("policy changed serialized retained data")
	}
	path := filepath.Join(t.TempDir(), "copy.json")
	if e = os.WriteFile(path, before, 0600); e != nil {
		t.Fatal(e)
	}
	r, e := AuditIndexFile(path)
	if e != nil || len(r.Facts) != 3 {
		t.Fatalf("dry run %v %+v", e, r)
	}
	disk, _ := os.ReadFile(path)
	if !bytes.Equal(before, disk) {
		t.Fatal("dry run wrote input")
	}
}
func TestLegacyDerivedCopyWithoutRootMetadataQuarantinedOnlyForSameServiceAndScope(t *testing.T) {
	bad := StationFact{Kind: FactAlias, Value: "FX", SourceID: "xfinity-official-lineup", Method: "number-policy-provider-alias-v3", LineupKeys: []string{"scope"}}
	legacy := StationFact{Kind: FactAlias, Value: "FX", SourceID: "gracenote-weekday-epg-fixture", Method: "identity-policy-v2", LineupKeys: []string{"scope"}}
	idx := Index{Stations: map[string]*Station{"sd": {Names: []StationName{{Kind: NameCallSign, Value: "ESPN2"}}, Facts: []StationFact{bad}}, "hd": {Names: []StationName{{Kind: NameCallSign, Value: "ESPN2HD"}}, Facts: []StationFact{legacy}}, "real": {Names: []StationName{{Kind: NameCallSign, Value: "FXHD"}}, Facts: []StationFact{legacy}}}}
	ApplyEvidenceQuarantine(&idx)
	if usableFact(idx.Stations["hd"].Facts[0]) {
		t.Fatal("legacy contaminated copy active")
	}
	if !usableFact(idx.Stations["real"].Facts[0]) {
		t.Fatal("real service alias removed by text alone")
	}
}
func TestSafeHistoricalAliasRetainsTypedOriginWithoutDiskMigration(t *testing.T) {
	f := StationFact{Kind: FactAlias, Value: "Seasonal Sports", SourceID: "optimum-official-lineup", SourceRowID: "row", RootSourceStationID: "A", RootSourceLineupKey: "own", Method: "number-policy-provider-alias-v3; provider-source-alignment-v1"}
	s := &Service{index: Index{Stations: map[string]*Station{"A": {StationID: "A", Facts: []StationFact{f}}}}}
	ApplyEvidenceQuarantine(&s.index)
	got := s.AliasesForStations([]string{"A"})["A"]
	if len(got) != 1 || got[0].EvidencePolicy != EvidenceQuarantinePolicy+"-reevaluated" || got[0].SourceRowID != "row" {
		t.Fatalf("safe legacy proof missing: %+v", got)
	}
}
func TestFrontierRelationsAndTypedDerivedEvidenceQuarantined(t *testing.T) {
	idx := Index{Lineups: map[string]*LineupRecord{"frontier": {ProviderName: "Frontier Fios"}}, Stations: map[string]*Station{"A": {Facts: []StationFact{{Kind: FactAlias, Value: "Wrong Network", SourceID: "verizon-fios-official-lineup", LineupKeys: []string{"frontier"}}}}}, CategoryRelations: []ProviderCategoryRelation{{SourceID: "verizon-fios-official-lineup", SourceLineupKey: "frontier"}}}
	r := ApplyEvidenceQuarantine(&idx)
	if len(r.Facts) != 2 || idx.CategoryRelations[0].quarantineReason == "" {
		t.Fatal("Frontier source scope not quarantined", r)
	}
}

func TestGoodRowOfSameProviderAndScopeIsNotQuarantined(t *testing.T) {
	bad := StationFact{Kind: FactAlias, Value: "Wrong", SourceID: "xfinity-official-lineup", SourceRowID: "bad", RootSourceLineupKey: "scope", Method: "number-policy-provider-alias-v3"}
	verified := StationFact{Kind: FactAlias, Value: "Right", SourceID: "xfinity-official-lineup", SourceRowID: "good", Method: "identity-policy-v2; number-policy-provider-v2"}
	good := StationFact{Kind: FactCategory, Value: "News", SourceID: "gracenote-weekday-epg-fixture", SourceRowID: "good", RootSourceID: "xfinity-official-lineup", RootSourceStationID: "A", RootSourceLineupKey: "scope", Method: "identity-policy-v2"}
	idx := Index{Stations: map[string]*Station{"A": {Facts: []StationFact{bad, verified}}, "B": {Facts: []StationFact{good}}}}
	ApplyEvidenceQuarantine(&idx)
	if !usableFact(idx.Stations["B"].Facts[0]) {
		t.Fatal("unrelated verified row removed")
	}
}
func TestTransientXfinityRootMissingFromIndexIsNotUpgradedByEPG(t *testing.T) {
	idx := Index{Stations: map[string]*Station{"hd": {Facts: []StationFact{{Kind: FactAlias, Value: "Unrelated Network", SourceID: "gracenote-weekday-epg-fixture", RootSourceID: "xfinity-official-lineup", RootSourceStationID: "sd", RootSourceLineupKey: "scope", SourceRowID: "transient-row", Method: "identity-policy-v2"}}}}}
	ApplyEvidenceQuarantine(&idx)
	if usableFact(idx.Stations["hd"].Facts[0]) {
		t.Fatal("transient unsafe alias promoted by schedule confirmation")
	}
}
func TestLegacyPairReevaluationNeedsReciprocalRawCallsigns(t *testing.T) {
	f := func(v string) StationFact {
		return StationFact{Kind: FactAlias, Value: v, SourceID: "gracenote-weekday-epg-fixture", Method: "identity-policy-v2"}
	}
	idx := Index{Stations: map[string]*Station{"A": {Names: []StationName{{Kind: NameCallSign, Value: "FEEDA"}}, Facts: []StationFact{f("FEEDB")}}, "B": {Names: []StationName{{Kind: NameCallSign, Value: "FEEDB"}}}}}
	ApplyEvidenceQuarantine(&idx)
	if identityPeer(idx.Stations["A"].Facts[0], "A") != "" {
		t.Fatal("one-way legacy alias became pair")
	}
	idx.Stations["B"].Facts = []StationFact{f("FEEDA")}
	ApplyEvidenceQuarantine(&idx)
	if identityPeer(idx.Stations["A"].Facts[0], "A") != "B" {
		t.Fatal("reciprocal confirmed pair not restored")
	}
}
