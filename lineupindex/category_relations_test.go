package lineupindex

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/web"
)

func TestProviderCategoryRelationsPersistReplaceAndPreserveFailures(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "market-index.json")
	service, err := NewService(ServiceConfig{
		Path: path, Providers: &fakeProviders{responses: map[string][]web.Provider{}},
		Grids: &fakeGrids{responses: map[string]*web.GridResponse{}, failures: map[string]int{}, calls: map[string]int{}},
		Now:   func() time.Time { return time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	service.index.Stations["S1"] = &Station{StationID: "S1", Names: []StationName{{Value: "HLN", Normalized: "HLN", Kind: NameCallSign}}}
	old := ProviderCategoryRelation{StationID: "S1", AliasValue: "HLN", Category: "News & Weather", SourceID: "verizon-fios-official-lineup", SourceLabel: "Verizon", SourceRowID: "row-old", SourceRevision: "r1", Method: "aligned number", LineupKeys: []string{"L1"}}
	if _, _, err := service.ingestProviderEvidence("L1", ProviderEvidenceResult{
		Sources: []EvidenceSourceRecord{{ID: old.SourceID, Status: StatusComplete}}, CategoryRelations: []ProviderCategoryRelation{old},
	}); err != nil {
		t.Fatal(err)
	}
	if got := service.EligibleProviderCategoryFacts([]string{"S1"}, ""); len(got["S1"]) != 1 || got["S1"][0].Value != "News & Weather" {
		t.Fatalf("accepted alias did not unlock relation category: %+v", got)
	}
	// A dependent EPG copy must disappear when its source row is removed.
	service.index.Stations["S1"].Facts = append(service.index.Stations["S1"].Facts, StationFact{Kind: FactCategory, Value: old.Category, Normalized: normalizeName(old.Category), SourceID: "gracenote-weekday-epg-usa-1", RootSourceID: old.SourceID, SourceRowID: old.SourceRowID, LineupKeys: []string{"L1"}, Method: "pair-level identity; identity-policy-v2"})
	newRelation := old
	newRelation.AliasValue = "HLN NEWS"
	newRelation.AliasNormalized = "HLNNEWS"
	newRelation.SourceRowID = "row-new"
	newRelation.SourceRevision = "r2"
	if _, _, err := service.ingestProviderEvidence("L1", ProviderEvidenceResult{
		Sources: []EvidenceSourceRecord{{ID: old.SourceID, Status: StatusComplete}}, CategoryRelations: []ProviderCategoryRelation{newRelation},
	}); err != nil {
		t.Fatal(err)
	}
	if len(service.index.CategoryRelations) != 1 || service.index.CategoryRelations[0].SourceRowID != "row-new" {
		t.Fatalf("replaced relations = %+v", service.index.CategoryRelations)
	}
	if len(service.index.Stations["S1"].Facts) != 0 {
		t.Fatalf("dependent category survived source-row removal: %+v", service.index.Stations["S1"].Facts)
	}
	if got := service.EligibleProviderCategoryFacts([]string{"S1"}, ""); len(got["S1"]) != 0 {
		t.Fatalf("unbound replacement relation became eligible: %+v", got)
	}
	if got := service.CategoriesForStations([]string{"S1"}); got["S1"].Value != "" {
		t.Fatalf("removed relation still produced a category candidate: %+v", got["S1"])
	}
	// A failed source pass preserves the last good replacement.
	failed := newRelation
	failed.AliasValue = "STALE"
	failed.SourceRowID = "row-failed"
	if _, _, err := service.ingestProviderEvidence("L1", ProviderEvidenceResult{
		Sources: []EvidenceSourceRecord{{ID: old.SourceID, Status: StatusError}}, CategoryRelations: []ProviderCategoryRelation{failed},
	}); err != nil {
		t.Fatal(err)
	}
	if len(service.index.CategoryRelations) != 1 || service.index.CategoryRelations[0].SourceRowID != "row-new" {
		t.Fatalf("failed source removed last good relation = %+v", service.index.CategoryRelations)
	}
	reopened, err := NewService(ServiceConfig{
		Path: path, Providers: &fakeProviders{responses: map[string][]web.Provider{}},
		Grids: &fakeGrids{responses: map[string]*web.GridResponse{}, failures: map[string]int{}, calls: map[string]int{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.index.CategoryRelations) != 1 || reopened.index.CategoryRelations[0].SourceRowID != "row-new" {
		t.Fatalf("reopened relations = %+v", reopened.index.CategoryRelations)
	}
}

func TestProviderCategoryRelationsReplaceOneScopePreservesOtherScope(t *testing.T) {
	service, err := NewService(ServiceConfig{
		Path:      filepath.Join(t.TempDir(), "market-index.json"),
		Providers: &fakeProviders{responses: map[string][]web.Provider{}},
		Grids:     &fakeGrids{responses: map[string]*web.GridResponse{}, failures: map[string]int{}, calls: map[string]int{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service.index.Stations["S1"] = &Station{StationID: "S1", Names: []StationName{{Value: "HLN", Normalized: "HLN", Kind: NameCallSign}}}
	base := ProviderCategoryRelation{
		StationID: "S1", AliasValue: "HLN", AliasNormalized: "HLN", Category: "News & Weather",
		SourceID: "verizon-fios-official-lineup", SourceRowID: "row-1", SourceRevision: "rev-1",
		Method: "aligned number; provider-source-alignment-v1",
	}
	for _, lineupKey := range []string{"L1", "L2"} {
		relation := base
		relation.LineupKeys = []string{lineupKey}
		if _, _, err := service.ingestProviderEvidence(lineupKey, ProviderEvidenceResult{
			Sources:           []EvidenceSourceRecord{{ID: base.SourceID, Status: StatusComplete}},
			CategoryRelations: []ProviderCategoryRelation{relation},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if len(service.index.CategoryRelations) != 2 {
		t.Fatalf("relations before scoped replacement = %+v", service.index.CategoryRelations)
	}
	if _, _, err := service.ingestProviderEvidence("L1", ProviderEvidenceResult{
		Sources: []EvidenceSourceRecord{{ID: base.SourceID, Status: StatusComplete}},
	}); err != nil {
		t.Fatal(err)
	}
	if len(service.index.CategoryRelations) != 1 || service.index.CategoryRelations[0].LineupKeys[0] != "L2" {
		t.Fatalf("L1 replacement removed L2 relation = %+v", service.index.CategoryRelations)
	}
	// An identified dependent copy follows the same scope lifecycle.
	service.index.Stations["S1"].Facts = append(service.index.Stations["S1"].Facts, StationFact{
		Kind: FactCategory, Value: "News & Weather", Normalized: normalizeName("News & Weather"),
		SourceID: "gracenote-weekday-epg-usa-1", RootSourceID: base.SourceID, SourceRowID: base.SourceRowID,
		LineupKeys: []string{"L2"}, Method: "pair-level identity (HLN); identity-policy-v2",
	})
	if _, _, err := service.ingestProviderEvidence("L2", ProviderEvidenceResult{
		Sources: []EvidenceSourceRecord{{ID: base.SourceID, Status: StatusComplete}},
	}); err != nil {
		t.Fatal(err)
	}
	if len(service.index.CategoryRelations) != 0 || len(service.index.Stations["S1"].Facts) != 0 {
		t.Fatalf("valid empty L2 replacement left stale evidence: relations=%+v facts=%+v", service.index.CategoryRelations, service.index.Stations["S1"].Facts)
	}
}

func TestReplaceEPGFactsPreservesDistinctProviderRootsForSameCategory(t *testing.T) {
	service, err := NewService(ServiceConfig{
		Path:      filepath.Join(t.TempDir(), "market-index.json"),
		Providers: &fakeProviders{responses: map[string][]web.Provider{}},
		Grids:     &fakeGrids{responses: map[string]*web.GridResponse{}, failures: map[string]int{}, calls: map[string]int{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service.index.Stations["S1"] = &Station{StationID: "S1"}
	const epgSource = "gracenote-weekday-epg-usa-1"
	peer := ensureEPGIdentityStation(map[string]*epgIdentityStation{}, "P")
	peer.Categories = []ProviderFact{
		{StationID: "P", Kind: FactCategory, Value: "News", SourceID: "directv-official-lineup", SourceRowID: "directv-row", Method: "provider category"},
		{StationID: "P", Kind: FactCategory, Value: "News", SourceID: "verizon-fios-official-lineup", SourceRowID: "verizon-row", Method: "provider category"},
	}
	stations := map[string]*epgIdentityStation{"S1": ensureEPGIdentityStation(map[string]*epgIdentityStation{}, "S1"), "P": peer}
	facts := buildEPGDerivedFacts(stations, []epgPairResult{{Pair: epgCandidatePair{LeftID: "S1", RightID: "P", Evidence: []string{"shared alias"}}, Status: "confirmed", Occurrences: 2, MatchedMinutes: 120}}, epgSource, "America/New_York")
	if _, categories, _, err := service.replaceEPGFacts(epgSource, facts); err != nil || categories != 2 {
		t.Fatalf("EPG root facts = categories %d err %v", categories, err)
	}
	if got := service.index.Stations["S1"].Facts; len(got) != 2 || got[0].RootSourceID == got[1].RootSourceID {
		t.Fatalf("same-category provider roots collapsed: %+v", got)
	}
}

func TestSpectrumSnapshotRetiresStationBoundRelationsAcrossLineups(t *testing.T) {
	service, err := NewService(ServiceConfig{
		Path:      filepath.Join(t.TempDir(), "market-index.json"),
		Providers: &fakeProviders{responses: map[string][]web.Provider{}},
		Grids:     &fakeGrids{responses: map[string]*web.GridResponse{}, failures: map[string]int{}, calls: map[string]int{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service.index.Stations["S1"] = &Station{StationID: "S1", Names: []StationName{{Value: "HLN", Normalized: "HLN", Kind: NameCallSign}}}
	const sourceID = "spectrum-official-lineup"
	old := ProviderCategoryRelation{
		StationID: "S1", AliasValue: "HLN", AliasNormalized: "HLN", Category: "News & Weather",
		SourceID: sourceID, SourceRevision: "rev-1", SourceRowID: "row-1", LineupKeys: []string{"L1"},
		Method: "exact Spectrum station ID", StationBound: true,
	}
	initial := ProviderEvidenceResult{
		Sources:           []EvidenceSourceRecord{{ID: sourceID, Status: StatusComplete}},
		CategoryRelations: []ProviderCategoryRelation{old},
		SnapshotComplete:  true, SnapshotSourceID: sourceID, SnapshotRevision: "rev-1", SnapshotStationIDs: []string{"S1"},
		SnapshotFacts: []ProviderFact{
			{StationID: "S1", Kind: FactAlias, Value: "HLN", SourceID: sourceID},
			{StationID: "S1", Kind: FactCategory, Value: "News & Weather", SourceID: sourceID},
		},
	}
	if _, _, err := service.ingestProviderEvidence("L1", initial); err != nil {
		t.Fatal(err)
	}
	if len(service.index.CategoryRelations) != 1 {
		t.Fatalf("initial Spectrum relation missing: %+v", service.index.CategoryRelations)
	}
	// The next complete national snapshot is from another lineup scope and
	// omits the old row. Snapshot reconciliation must retire the old relation
	// globally, not leave it eligible through L1.
	revised := ProviderEvidenceResult{
		Sources:          []EvidenceSourceRecord{{ID: sourceID, Status: StatusComplete}},
		SnapshotComplete: true, SnapshotSourceID: sourceID, SnapshotRevision: "rev-2", SnapshotStationIDs: []string{"S1"},
		SnapshotFacts: []ProviderFact{
			{StationID: "S1", Kind: FactAlias, Value: "OTHER", SourceID: sourceID},
			{StationID: "S1", Kind: FactCategory, Value: "Sports", SourceID: sourceID},
		},
	}
	if _, _, err := service.ingestProviderEvidence("L2", revised); err != nil {
		t.Fatal(err)
	}
	if len(service.index.CategoryRelations) != 0 {
		t.Fatalf("retired Spectrum relation survived revised snapshot: %+v", service.index.CategoryRelations)
	}
	if got := service.CategoriesForStations([]string{"S1"}); got["S1"].Value != "" {
		t.Fatalf("retired Spectrum relation resurrected category: %+v", got["S1"])
	}
}

func TestProviderRefreshReplacesLegacyDirectCategoryWithinScope(t *testing.T) {
	service, err := NewService(ServiceConfig{
		Path:      filepath.Join(t.TempDir(), "market-index.json"),
		Providers: &fakeProviders{responses: map[string][]web.Provider{}},
		Grids:     &fakeGrids{responses: map[string]*web.GridResponse{}, failures: map[string]int{}, calls: map[string]int{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service.index.Stations["S1"] = &Station{StationID: "S1"}
	const sourceID = "verizon-fios-official-lineup"
	if _, _, err := service.ingestProviderEvidence("L1", ProviderEvidenceResult{
		Sources: []EvidenceSourceRecord{{ID: sourceID, Status: StatusComplete}},
		Facts:   []ProviderFact{{StationID: "S1", Kind: FactCategory, Value: "News", SourceID: sourceID, Method: "legacy direct provider category"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ingestProviderEvidence("L1", ProviderEvidenceResult{
		Sources: []EvidenceSourceRecord{{ID: sourceID, Status: StatusComplete}},
		Facts:   []ProviderFact{{StationID: "S1", Kind: FactCategory, Value: "Entertainment", SourceID: sourceID, Method: "current direct provider category"}},
	}); err != nil {
		t.Fatal(err)
	}
	facts := service.index.Stations["S1"].Facts
	if len(facts) != 1 || facts[0].Value != "Entertainment" || len(facts[0].LineupKeys) != 1 || facts[0].LineupKeys[0] != "L1" {
		t.Fatalf("legacy direct category was not replaced in scope: %+v", facts)
	}
}

func TestProviderFactsRetainDistinctSourceRowsWithSameCategory(t *testing.T) {
	service, err := NewService(ServiceConfig{
		Path:      filepath.Join(t.TempDir(), "market-index.json"),
		Providers: &fakeProviders{responses: map[string][]web.Provider{}},
		Grids:     &fakeGrids{responses: map[string]*web.GridResponse{}, failures: map[string]int{}, calls: map[string]int{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service.index.Stations["S1"] = &Station{StationID: "S1"}
	const sourceID = "directv-official-lineup"
	_, _, err = service.ingestProviderEvidence("L1", ProviderEvidenceResult{
		Sources: []EvidenceSourceRecord{{ID: sourceID, Status: StatusComplete}},
		Facts: []ProviderFact{
			{StationID: "S1", Kind: FactCategory, Value: "News", SourceID: sourceID, SourceRowID: "row-a", Method: "row a"},
			{StationID: "S1", Kind: FactCategory, Value: "News", SourceID: sourceID, SourceRowID: "row-b", Method: "row b"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := service.index.Stations["S1"].Facts; len(got) != 2 || got[0].SourceRowID == got[1].SourceRowID {
		t.Fatalf("same-category source rows collapsed: %+v", got)
	}
}

func TestEPGReplacementPreservesFailedProviderRootUntilSuccessfulEmptyPass(t *testing.T) {
	service, err := NewService(ServiceConfig{
		Path:      filepath.Join(t.TempDir(), "market-index.json"),
		Providers: &fakeProviders{responses: map[string][]web.Provider{}},
		Grids:     &fakeGrids{responses: map[string]*web.GridResponse{}, failures: map[string]int{}, calls: map[string]int{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service.index.Stations["S1"] = &Station{StationID: "S1"}
	const epgSource = "gracenote-weekday-epg-usa-1"
	old := epgDerivedFact{ProviderFact: ProviderFact{
		StationID: "S1", Kind: FactCategory, Value: "News", SourceID: epgSource,
		RootSourceID: "verizon-fios-official-lineup", RootSourceLineupKey: "L1", RootSourceStationID: "S1", SourceRowID: "row-1",
		Method: "pair-level identity (HLN); identity-policy-v2",
	}, LineupKeys: []string{"L1", "L2"}}
	if _, _, _, err := service.replaceEPGFacts(epgSource, []epgDerivedFact{old}); err != nil {
		t.Fatal(err)
	}
	newFact := old
	newFact.ProviderFact.RootSourceID = "directv-official-lineup"
	newFact.ProviderFact.RootSourceLineupKey = "L3"
	newFact.ProviderFact.SourceRowID = "row-2"
	if _, _, _, err := service.replaceEPGFactsForRun(epgSource, []epgDerivedFact{newFact}, map[string]bool{"verizon-fios-official-lineup\x00L1": true}); err != nil {
		t.Fatal(err)
	}
	if len(service.index.Stations["S1"].Facts) != 2 {
		t.Fatalf("failed provider root was erased during EPG replacement: %+v", service.index.Stations["S1"].Facts)
	}
	if _, _, _, err := service.replaceEPGFactsForRun(epgSource, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(service.index.Stations["S1"].Facts) != 0 {
		t.Fatalf("successful empty EPG pass retained stale provider root: %+v", service.index.Stations["S1"].Facts)
	}
}

func TestEPGReplacementPreservesUnscannedProviderRootsByDefault(t *testing.T) {
	service, err := NewService(ServiceConfig{
		Path: filepath.Join(t.TempDir(), "market-index.json"), Providers: &fakeProviders{responses: map[string][]web.Provider{}},
		Grids: &fakeGrids{responses: map[string]*web.GridResponse{}, failures: map[string]int{}, calls: map[string]int{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service.index.Stations["S1"] = &Station{StationID: "S1"}
	const epgSource = "gracenote-weekday-epg-usa-1"
	facts := []epgDerivedFact{
		{ProviderFact: ProviderFact{StationID: "S1", Kind: FactCategory, Value: "News", SourceID: epgSource, RootSourceID: "verizon-fios-official-lineup", RootSourceLineupKey: "L3", RootSourceStationID: "S1", SourceRowID: "row-3", Method: "pair-level identity (HLN); identity-policy-v2"}, LineupKeys: []string{"L3"}},
	}
	if _, _, _, err := service.replaceEPGFacts(epgSource, facts); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := service.replaceEPGFactsForRun(epgSource, nil, failedEPGProviderRoots([]*postalLineupScan{{Lineup: &LineupRecord{Key: "L1"}}})); err != nil {
		t.Fatal(err)
	}
	if len(service.index.Stations["S1"].Facts) != 1 {
		t.Fatalf("unscanned provider root was erased: %+v", service.index.Stations["S1"].Facts)
	}
	if _, _, _, err := service.replaceEPGFactsForRun(epgSource, nil, failedEPGProviderRoots([]*postalLineupScan{{Lineup: &LineupRecord{Key: "L3"}, Sources: []EvidenceSourceRecord{{ID: "verizon-fios-official-lineup", Status: StatusComplete}}}})); err != nil {
		t.Fatal(err)
	}
	if len(service.index.Stations["S1"].Facts) != 0 {
		t.Fatalf("explicit complete empty scope retained provider root: %+v", service.index.Stations["S1"].Facts)
	}
}

func TestRunPostalEPGPreservesFailedProviderCategoryThroughConfirmedPair(t *testing.T) {
	service, err := NewService(ServiceConfig{
		Path:      filepath.Join(t.TempDir(), "market-index.json"),
		Providers: &fakeProviders{responses: map[string][]web.Provider{}},
		Grids:     &fakeGrids{responses: map[string]*web.GridResponse{}, responsesAt: map[string]map[int64]*web.GridResponse{}, failures: map[string]int{}, calls: map[string]int{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service.index.Stations["A"] = &Station{StationID: "A"}
	service.index.Stations["B"] = &Station{StationID: "B"}
	blocks := testEPGBlocks()
	grids := &fakeGrids{responses: map[string]*web.GridResponse{}, responsesAt: map[string]map[int64]*web.GridResponse{"L1": {}}, failures: map[string]int{}, calls: map[string]int{}}
	service.grids = grids
	primary := &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: "A", CallSign: "LOCAL-A"}, {ChannelID: "B", CallSign: "LOCAL-B"}}}
	for _, block := range blocks {
		grids.responsesAt["L1"][block.Start.Unix()] = &web.GridResponse{Channels: []web.JSONChannel{
			testEPGChannel("A", "LOCAL-A", "", block, []string{"Morning News", "Market News", "World News", "Evening News"}, "same-a"),
			testEPGChannel("B", "LOCAL-B", "", block, []string{"Morning News", "Market News", "World News", "Evening News"}, "same-b"),
		}}
	}
	baseFacts := []ProviderFact{
		{StationID: "A", Kind: FactAlias, Value: "SHARED NETWORK", SourceID: "verizon-fios-official-lineup", SourceRowID: "row-a", RootSourceLineupKey: "L1", RootSourceStationID: "A"},
		{StationID: "B", Kind: FactAlias, Value: "SHARED NETWORK", SourceID: "verizon-fios-official-lineup", SourceRowID: "row-b", RootSourceLineupKey: "L1", RootSourceStationID: "B"},
	}
	scan := testEPGScan("L1", "Verizon FiOS", map[string]*web.GridResponse{blocks[0].ID: primary})
	scan.Facts = baseFacts
	scan.Relations = []ProviderCategoryRelation{{StationID: "A", AliasValue: "SHARED NETWORK", AliasNormalized: "SHAREDNETWORK", Category: "News", SourceID: "verizon-fios-official-lineup", SourceRowID: "row-a", SourceLineupKey: "L1", LineupKeys: []string{"L1"}, Method: "number-policy-provider-alias-v3; provider-source-alignment-v1"}}
	scan.Sources = []EvidenceSourceRecord{{ID: "verizon-fios-official-lineup", Status: StatusComplete}}
	lastGridRequest := time.Time{}
	if _, err := service.runPostalEPG(context.Background(), "USA:11743", "USA", "11743", "America/New_York", []*postalLineupScan{scan}, blocks, &lastGridRequest); err != nil {
		t.Fatal(err)
	}
	if len(service.index.Stations["B"].Facts) == 0 {
		t.Fatal("first confirmed pair did not create EPG evidence")
	}
	foundTransferred := false
	for _, fact := range service.index.Stations["B"].Facts {
		if fact.Kind == FactCategory && fact.Value == "News & Weather" && strings.Contains(fact.Method, "provider-category-relation") && !strings.Contains(fact.Method, "number-policy-provider-alias-v3") {
			foundTransferred = true
		}
	}
	if !foundTransferred {
		t.Fatalf("confirmed two-block pair did not transfer aligned relation category safely: %+v", service.index.Stations["B"].Facts)
	}
	if candidate, ok := service.CategoriesForStations([]string{"B"})["B"]; !ok || !strings.Contains(strings.Join(candidate.Methods, " "), "requires review") {
		t.Fatalf("transferred relation category was not reviewable: %+v, %v", candidate, ok)
	}
	if _, _, err := service.ingestProviderEvidence("L1", ProviderEvidenceResult{Sources: []EvidenceSourceRecord{{ID: "verizon-fios-official-lineup", Status: StatusComplete}}, Facts: baseFacts, CategoryRelations: scan.Relations}); err != nil {
		t.Fatal(err)
	}
	checkUncategorizedPeerAlias := func() {
		t.Helper()
		for _, fact := range service.index.Stations["A"].Facts {
			if fact.Kind == FactAlias && fact.SourceID == weekdayEPGSourceID("USA", "11743") && fact.RootSourceID == "verizon-fios-official-lineup" && fact.RootSourceStationID == "B" && fact.SourceRowID == "row-b" {
				return
			}
		}
		t.Fatalf("unchanged uncategorized row's confirmed peer alias was removed: %+v", service.index.Stations["A"].Facts)
	}
	checkUncategorizedPeerAlias()
	// A provider row refresh has succeeded, but the next EPG verification is
	// interrupted. Its direct alias being re-added is not sufficient: the
	// previously verified cross-GNID copy must survive too.
	interrupted, stop := context.WithCancel(context.Background())
	stop()
	if _, err := service.runPostalEPG(interrupted, "USA:11743", "USA", "11743", "America/New_York", []*postalLineupScan{scan}, blocks, &lastGridRequest); err == nil {
		t.Fatal("interrupted verification unexpectedly succeeded")
	}
	checkUncategorizedPeerAlias()
	scan.Facts = []ProviderFact{
		{StationID: "A", Kind: FactAlias, Value: "SHARED NETWORK", SourceID: "directv-official-lineup", SourceRowID: "directv-a", RootSourceLineupKey: "L1", RootSourceStationID: "A"},
		{StationID: "B", Kind: FactAlias, Value: "SHARED NETWORK", SourceID: "directv-official-lineup", SourceRowID: "directv-b", RootSourceLineupKey: "L1", RootSourceStationID: "B"},
	}
	scan.Relations = nil
	scan.Sources = []EvidenceSourceRecord{{ID: "verizon-fios-official-lineup", Status: StatusError}}
	if _, err := service.runPostalEPG(context.Background(), "USA:11743", "USA", "11743", "America/New_York", []*postalLineupScan{scan}, blocks, &lastGridRequest); err != nil {
		t.Fatal(err)
	}
	foundRetainedCategory := false
	for _, fact := range service.index.Stations["B"].Facts {
		if fact.Kind == FactCategory && fact.RootSourceID == "verizon-fios-official-lineup" && fact.Value == "News & Weather" {
			foundRetainedCategory = true
		}
	}
	if !foundRetainedCategory {
		t.Fatalf("provider failure erased prior EPG category: %+v", service.index.Stations["B"].Facts)
	}
	// NationalOnly/address-required passes may omit the local provider record;
	// that omission is not an authoritative replacement.
	scan.Sources = []EvidenceSourceRecord{{ID: "directv-official-lineup", Status: StatusComplete}}
	if _, err := service.runPostalEPG(context.Background(), "USA:11743", "USA", "11743", "America/New_York", []*postalLineupScan{scan}, blocks, &lastGridRequest); err != nil {
		t.Fatal(err)
	}
	foundRetainedCategory = false
	for _, fact := range service.index.Stations["B"].Facts {
		if fact.Kind == FactCategory && fact.RootSourceID == "verizon-fios-official-lineup" && fact.Value == "News & Weather" {
			foundRetainedCategory = true
		}
	}
	if !foundRetainedCategory {
		t.Fatalf("omitted provider source erased prior EPG category: %+v", service.index.Stations["B"].Facts)
	}
	// A canceled two-block pass is not an authoritative EPG replacement either.
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.runPostalEPG(canceled, "USA:11743", "USA", "11743", "America/New_York", []*postalLineupScan{scan}, blocks, &lastGridRequest); err == nil {
		t.Fatal("canceled EPG pass unexpectedly succeeded")
	}
	foundRetainedCategory = false
	for _, fact := range service.index.Stations["B"].Facts {
		if fact.Kind == FactCategory && fact.RootSourceID == "verizon-fios-official-lineup" && fact.Value == "News & Weather" {
			foundRetainedCategory = true
		}
	}
	if !foundRetainedCategory {
		t.Fatalf("canceled EPG pass erased prior provider category: %+v", service.index.Stations["B"].Facts)
	}
	scan.Facts = nil
	scan.Sources = []EvidenceSourceRecord{{ID: "verizon-fios-official-lineup", Status: StatusComplete}}
	scan.Grids[blocks[0].ID] = &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: "A", CallSign: "LOCAL-A"}}}
	if _, err := service.runPostalEPG(context.Background(), "USA:11743", "USA", "11743", "America/New_York", []*postalLineupScan{scan}, blocks, &lastGridRequest); err != nil {
		t.Fatal(err)
	}
	for _, fact := range service.index.Stations["B"].Facts {
		if fact.Kind == FactCategory && fact.RootSourceID == "verizon-fios-official-lineup" {
			t.Fatalf("successful empty pass retained Verizon EPG category: %+v", service.index.Stations["B"].Facts)
		}
	}
}

func TestSourceScopeRetirementRemovesCopiedL1FactButPreservesIndependentL3(t *testing.T) {
	service, err := NewService(ServiceConfig{
		Path:      filepath.Join(t.TempDir(), "market-index.json"),
		Providers: &fakeProviders{responses: map[string][]web.Provider{}},
		Grids:     &fakeGrids{responses: map[string]*web.GridResponse{}, failures: map[string]int{}, calls: map[string]int{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service.index.Stations["S1"] = &Station{StationID: "S1", Names: []StationName{{Value: "HLN", Normalized: "HLN", Kind: NameCallSign}}}
	const sourceID = "verizon-fios-official-lineup"
	for _, lineupKey := range []string{"L1", "L3"} {
		relation := ProviderCategoryRelation{StationID: "S1", AliasValue: "HLN", AliasNormalized: "HLN", Category: "News", SourceID: sourceID, SourceRowID: "row-" + lineupKey, SourceLineupKey: lineupKey, LineupKeys: []string{lineupKey}, Method: "aligned; provider-source-alignment-v1"}
		if _, _, err := service.ingestProviderEvidence(lineupKey, ProviderEvidenceResult{Sources: []EvidenceSourceRecord{{ID: sourceID, Status: StatusComplete}}, CategoryRelations: []ProviderCategoryRelation{relation}}); err != nil {
			t.Fatal(err)
		}
	}
	oldCopied := epgDerivedFact{ProviderFact: ProviderFact{StationID: "S1", Kind: FactCategory, Value: "News", SourceID: weekdayEPGSourceID("USA", "11743"), RootSourceID: sourceID, RootSourceLineupKey: "L1", RootSourceStationID: "S1", SourceRowID: "row-L1", Method: "pair-level identity (HLN); identity-policy-v2"}, LineupKeys: []string{"L1", "L2"}}
	independent := oldCopied
	independent.ProviderFact.RootSourceLineupKey = "L3"
	independent.ProviderFact.SourceRowID = "row-L3"
	if _, _, _, err := service.replaceEPGFacts(oldCopied.SourceID, []epgDerivedFact{oldCopied, independent}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ingestProviderEvidence("L1", ProviderEvidenceResult{Sources: []EvidenceSourceRecord{{ID: sourceID, Status: StatusComplete}}}); err != nil {
		t.Fatal(err)
	}
	facts := service.index.Stations["S1"].Facts
	if len(facts) != 1 || facts[0].RootSourceLineupKey != "L3" {
		t.Fatalf("withdrawn L1 copy was retained or independent L3 lost: %+v", facts)
	}
}

func TestProviderRowMoveRetiresOldStationBindingWithSameRowID(t *testing.T) {
	service, err := NewService(ServiceConfig{
		Path:      filepath.Join(t.TempDir(), "market-index.json"),
		Providers: &fakeProviders{responses: map[string][]web.Provider{}},
		Grids:     &fakeGrids{responses: map[string]*web.GridResponse{}, failures: map[string]int{}, calls: map[string]int{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service.index.Stations["A"] = &Station{StationID: "A", Names: []StationName{{Value: "HLN", Normalized: "HLN", Kind: NameCallSign}}}
	service.index.Stations["B"] = &Station{StationID: "B", Names: []StationName{{Value: "HLN", Normalized: "HLN", Kind: NameCallSign}}}
	const sourceID = "verizon-fios-official-lineup"
	old := ProviderCategoryRelation{StationID: "A", AliasValue: "HLN", AliasNormalized: "HLN", Category: "News", SourceID: sourceID, SourceRowID: "stable-row", SourceLineupKey: "L1", LineupKeys: []string{"L1"}, Method: "aligned; provider-source-alignment-v1"}
	if _, _, err := service.ingestProviderEvidence("L1", ProviderEvidenceResult{Sources: []EvidenceSourceRecord{{ID: sourceID, Status: StatusComplete}}, CategoryRelations: []ProviderCategoryRelation{old}}); err != nil {
		t.Fatal(err)
	}
	service.index.Stations["A"].Facts = append(service.index.Stations["A"].Facts, StationFact{Kind: FactCategory, Value: "News", Normalized: "NEWS", SourceID: sourceID, SourceRowID: "stable-row", RootSourceLineupKey: "L1", RootSourceStationID: "A", LineupKeys: []string{"L1"}})
	moved := old
	moved.StationID = "B"
	if _, _, err := service.ingestProviderEvidence("L1", ProviderEvidenceResult{Sources: []EvidenceSourceRecord{{ID: sourceID, Status: StatusComplete}}, CategoryRelations: []ProviderCategoryRelation{moved}}); err != nil {
		t.Fatal(err)
	}
	if len(service.index.Stations["A"].Facts) != 0 || len(service.index.CategoryRelations) != 1 || service.index.CategoryRelations[0].StationID != "B" {
		t.Fatalf("same-row station move retained old A binding: relations=%+v factsA=%+v", service.index.CategoryRelations, service.index.Stations["A"].Facts)
	}
}

func TestCategoryOnlyRefreshPreservesUnchangedConfirmedAliasRow(t *testing.T) {
	service, err := NewService(ServiceConfig{
		Path:      filepath.Join(t.TempDir(), "market-index.json"),
		Providers: &fakeProviders{responses: map[string][]web.Provider{}},
		Grids:     &fakeGrids{responses: map[string]*web.GridResponse{}, failures: map[string]int{}, calls: map[string]int{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service.index.Stations["S1"] = &Station{StationID: "S1"}
	const sourceID = "verizon-fios-official-lineup"
	relation := ProviderCategoryRelation{StationID: "S1", AliasValue: "HLN", AliasNormalized: "HLN", Category: "News", SourceID: sourceID, SourceRowID: "row-1", SourceLineupKey: "L1", LineupKeys: []string{"L1"}, Method: "aligned; provider-source-alignment-v1"}
	alias := ProviderFact{StationID: "S1", Kind: FactAlias, Value: "HLN", SourceID: sourceID, SourceRowID: "row-1", RootSourceLineupKey: "L1", RootSourceStationID: "S1"}
	if _, _, err := service.ingestProviderEvidence("L1", ProviderEvidenceResult{Sources: []EvidenceSourceRecord{{ID: sourceID, Status: StatusComplete}}, CategoryRelations: []ProviderCategoryRelation{relation}, Facts: []ProviderFact{alias}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ingestProviderEvidence("L1", ProviderEvidenceResult{Sources: []EvidenceSourceRecord{{ID: sourceID, Status: StatusComplete}}, CategoryRelations: []ProviderCategoryRelation{relation}}); err != nil {
		t.Fatal(err)
	}
	if got := service.index.Stations["S1"].Facts; len(got) != 1 || got[0].Value != "HLN" {
		t.Fatalf("unchanged confirmed alias was removed by category-only refresh: %+v", got)
	}
	if _, _, err := service.ingestProviderEvidence("L1", ProviderEvidenceResult{Sources: []EvidenceSourceRecord{{ID: sourceID, Status: StatusComplete}}}); err != nil {
		t.Fatal(err)
	}
	if got := service.index.Stations["S1"].Facts; len(got) != 0 {
		t.Fatalf("withdrawn category relation retained alias row: %+v", got)
	}
}
