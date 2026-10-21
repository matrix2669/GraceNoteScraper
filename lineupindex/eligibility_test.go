package lineupindex

import (
	"encoding/json"
	"github.com/daniel-widrick/GraceNoteScraper/web"
	"strings"
	"testing"
	"time"
)

func eligibilityFixture() (*Service, EvidenceContext) {
	c := EvidenceContext{Country: "USA", PostalCode: "12345", LineupID: "A", Device: "X", Fingerprint: "source-A", RawCallSigns: map[string][]string{"S": {"RAW"}}}
	f := StationFact{Kind: FactAlias, Value: "Scoped alias", Normalized: "SCOPEDALIAS", SourceID: "directv-official-lineup", SourceRowID: "row", SourceRevision: "revision", RootSourceStationID: "S", RootSourceLineupKey: "a", LineupKeys: []string{"a"}, EvidencePolicy: ProviderIdentityPolicy, Method: "identity-policy-v2; provider-source-alignment-v1"}
	idx := Index{Stations: map[string]*Station{"S": {StationID: "S", Facts: []StationFact{f}, Names: []StationName{{Kind: NameCallSign, Value: "OLD"}, {Kind: NameEventCallSign, Value: "EVENT"}}}}, Lineups: map[string]*LineupRecord{"a": {Country: "USA", PostalCode: "12345", LineupID: "A", Device: "X", ProviderName: "DIRECTV"}, "b": {Country: "USA", PostalCode: "12345", LineupID: "B", Device: "X", ProviderName: "DIRECTV"}}}
	return &Service{index: idx}, c
}
func hasEvidence(v EvidenceView, value string) bool {
	for _, a := range v.AliasesForStations([]string{"S"})["S"] {
		if a.Value == value {
			return true
		}
	}
	return false
}
func TestEvidenceContextIsExactAndDoesNotRewriteRecords(t *testing.T) {
	s, c := eligibilityFixture()
	before, _ := json.Marshal(s.index)
	if !hasEvidence(s.EvidenceForLineup(c), "Scoped alias") {
		t.Fatal("current row missing")
	}
	for _, change := range []func(*EvidenceContext){func(c *EvidenceContext) { c.LineupID = "B" }, func(c *EvidenceContext) { c.Device = "L" }, func(c *EvidenceContext) { c.Country = "CAN" }, func(c *EvidenceContext) { c.PostalCode = "99999" }, func(c *EvidenceContext) { c.Fingerprint = "" }} {
		other := c
		change(&other)
		if hasEvidence(s.EvidenceForLineup(other), "Scoped alias") {
			t.Fatalf("old assertion leaked into %+v", other)
		}
	}
	after, _ := json.Marshal(s.index)
	if string(before) != string(after) {
		t.Fatal("view changed retained data")
	}
}
func TestReusableCatalogSurvivesSourceSwitchButUnprovenScheduleDoesNot(t *testing.T) {
	s, c := eligibilityFixture()
	f := StationFact{Kind: FactAlias, Value: "Reusable", SourceID: "spectrum-official-lineup", SourceRowID: "catalog-row", SourceRevision: "catalog-revision", StationBound: true, RootSourceStationID: "S", Method: "exact Gracenote station ID from Spectrum TMSID"}
	s.index.Stations["S"].Facts = append(s.index.Stations["S"].Facts, f, StationFact{Kind: FactAlias, Value: "Wrong", SourceID: weekdayEPGSourcePrefix + "old", Method: "pair-level identity (RAW); identity-policy-v2", LineupKeys: []string{"a", "b"}})
	c.LineupID = "B"
	v := s.EvidenceForLineup(c)
	if !hasEvidence(v, "Reusable") || hasEvidence(v, "Wrong") || hasEvidence(v, "EVENT") || hasEvidence(v, "OLD") {
		t.Fatalf("invalid reusable projection: %+v", v.aliases)
	}
	for _, change := range []func(*StationFact){func(f *StationFact) { f.SourceID = "unknown" }, func(f *StationFact) { f.RootSourceStationID = "OTHER" }, func(f *StationFact) { f.SourceRevision = "" }, func(f *StationFact) { f.SourceRowID = "" }} {
		bad := f
		change(&bad)
		s.index.Stations["S"].Facts = []StationFact{bad}
		if hasEvidence(s.EvidenceForLineup(c), "Reusable") {
			t.Fatal("malformed catalog proof accepted")
		}
	}
}
func TestExactIdentityRequiresCurrentRawCorroborationNotEventOrMethod(t *testing.T) {
	s, c := eligibilityFixture()
	f := s.index.Stations["S"].Facts[0]
	f.Value = "RAW"
	f.Normalized = "RAW"
	f.Method = "identity-policy-v2"
	s.index.Stations["S"].Facts = []StationFact{f}
	c.LineupID = "B"
	if !hasEvidence(s.EvidenceForLineup(c), "RAW") {
		t.Fatal("corroborated raw identity missing")
	}
	c.RawCallSigns = map[string][]string{"S": {"OTHER"}}
	if hasEvidence(s.EvidenceForLineup(c), "RAW") {
		t.Fatal("stale raw identity corroborated itself")
	}
}
func TestCategoryBindingsUseSameEligibleView(t *testing.T) {
	s, c := eligibilityFixture()
	s.index.CategoryRelations = []ProviderCategoryRelation{{StationID: "S", AliasValue: "Scoped alias", AliasNormalized: "SCOPEDALIAS", Category: "News & Weather", SourceID: "directv-official-lineup", SourceRowID: "row", SourceLineupKey: "a", LineupKeys: []string{"a"}, EvidencePolicy: ProviderIdentityPolicy, Method: "identity-policy-v2; provider-source-alignment-v1"}}
	if s.EvidenceForLineup(c).CategoriesForStations([]string{"S"})["S"].Value == "" {
		t.Fatal("current scoped category missing")
	}
	c.LineupID = "B"
	if s.EvidenceForLineup(c).CategoriesForStations([]string{"S"})["S"].Value != "" {
		t.Fatal("old category survived through relation")
	}
}
func TestMultipleUnsafeOriginScopesArePreservedForQuarantine(t *testing.T) {
	s, _ := eligibilityFixture()
	s.index.Lineups["a"].ProviderName = "Frontier Fios"
	s.index.Lineups["b"].ProviderName = "Frontier Fios"
	bad := StationFact{Kind: FactAlias, Value: "Wrong", SourceID: "verizon-fios-official-lineup", Method: "number-policy-provider-alias-v3; provider-source-alignment-v1", LineupKeys: []string{"a", "b"}}
	s.index.Stations["S"].Names = []StationName{{Kind: NameCallSign, Value: "RAWSD"}}
	s.index.Stations["S"].Facts = []StationFact{bad}
	s.index.Stations["T"] = &Station{Names: []StationName{{Kind: NameCallSign, Value: "RAWHD"}}, Facts: []StationFact{{Kind: FactAlias, Value: "Wrong", SourceID: weekdayEPGSourcePrefix + "old", Method: "identity-policy-v2", LineupKeys: []string{"b"}}}}
	ApplyEvidenceQuarantine(&s.index)
	if !strings.Contains(s.index.Stations["T"].Facts[0].quarantineReason, "derived") {
		t.Fatal("second scope legacy copy escaped")
	}
}

func TestScheduleProofTransfersRawIdentityOnlyAndRejectsLegacyCopies(t *testing.T) {
	s, c := eligibilityFixture()
	s.index.Stations["P"] = &Station{StationID: "P"}
	c.RawCallSigns["P"] = []string{"PEER"}
	a := ensureEPGIdentityStation(map[string]*epgIdentityStation{}, "S")
	a.CallSigns["RAW"] = "RAW"
	b := ensureEPGIdentityStation(map[string]*epgIdentityStation{}, "P")
	b.CallSigns["PEER"] = "PEER"
	b.ProviderNames["WRONG"] = "Wrong network"
	b.Categories = []ProviderFact{{Kind: FactCategory, Value: "News"}}
	proof := StationPairProof{Timezone: "America/New_York", ProgrammeRevision: strings.Repeat("a", 64), WindowStarts: []int64{testEPGBlocks()[0].Start.Unix(), testEPGBlocks()[1].Start.Unix()}}
	facts := buildEPGDerivedFacts(map[string]*epgIdentityStation{"S": a, "P": b}, []epgPairResult{{Pair: epgCandidatePair{LeftID: "S", RightID: "P"}, Status: "confirmed"}}, weekdayEPGSourcePrefix+"fixture", "America/New_York", proof)
	if len(facts) != 2 {
		t.Fatalf("raw facts %+v", facts)
	}
	for _, d := range facts {
		if d.Kind != FactAlias || d.Value == "Wrong network" {
			t.Fatalf("laundered peer evidence %+v", d)
		}
		f := StationFact{Kind: d.Kind, Value: d.Value, PairProof: d.PairProof, PeerStationID: d.PeerStationID, SourceID: d.SourceID, EvidencePolicy: d.EvidencePolicy}
		s.index.Stations[d.StationID].Facts = append(s.index.Stations[d.StationID].Facts, f)
	}
	c.LineupID = "B"
	if !hasEvidence(s.EvidenceForLineup(c), "PEER") {
		t.Fatal("valid independently confirmed raw identity lost")
	}
	s.index.Stations["S"].Facts[1].PairProof = nil
	if hasEvidence(s.EvidenceForLineup(c), "PEER") {
		t.Fatal("missing pair proof became reusable")
	}
}

func TestActualScannerWindowsProduceEligibleRawPair(t *testing.T) {
	blocks, zone, err := weekdayEPGBlocks(time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC), []web.Provider{{Timezone: "Eastern"}}, &web.ProviderResponse{})
	if err != nil {
		t.Fatal(err)
	}
	scan := testEPGScan("source", "DIRECTV", map[string]*web.GridResponse{blocks[0].ID: {}, blocks[1].ID: {}})
	p := scheduleProof([]*postalLineupScan{scan}, blocks, zone)
	p.TargetStationID = "S"
	p.TargetCallSigns = []string{"RAW"}
	p.PeerStationID = "P"
	p.PeerCallSign = "PEER"
	f := StationFact{PairProof: &p, Kind: FactAlias, Value: "PEER", PeerStationID: "P", SourceID: weekdayEPGSourcePrefix + "fixture", EvidencePolicy: ProviderIdentityPolicy}
	if !reusablePairFact("S", f, map[string][]string{"S": {"RAW"}, "P": {"PEER"}}) {
		t.Fatalf("actual18-hour scanner windows rejected %+v", p)
	}
	p.PeerCallSign = "Wrong provider alias"
	if reusablePairFact("S", f, map[string][]string{"S": {"RAW"}}) {
		t.Fatal("pair proof laundered unrelated alias")
	}
}
func TestEvidenceViewStaysDetachedAcrossIndexUpdates(t *testing.T) {
	s, c := eligibilityFixture()
	v := s.EvidenceForLineup(c)
	s.mu.Lock()
	s.index.Stations["S"].Facts[0].Value = "Changed"
	s.index.Lineups["a"].ProviderName = "OTHER"
	s.mu.Unlock()
	if !hasEvidence(v, "Scoped alias") {
		t.Fatal("view changed after creation")
	}
	out := v.AliasesForStations([]string{"S"})
	out["S"][0].Value = "mutated"
	if hasEvidence(v, "mutated") {
		t.Fatal("caller mutated view")
	}
}

func TestCurrentNumberOnlyRowIsAuditOnlyWithoutChangingRetainedData(t *testing.T) {
	s, c := eligibilityFixture()
	s.index.Stations["S"].Facts[0].Method = "number-policy-provider-alias-v3; provider-source-alignment-v1"
	before, _ := json.Marshal(s.index)
	if hasEvidence(s.EvidenceForLineup(c), "Scoped alias") {
		t.Fatal("position promoted to station identity")
	}
	after, _ := json.Marshal(s.index)
	if string(before) != string(after) {
		t.Fatal("retained evidence changed")
	}
}
