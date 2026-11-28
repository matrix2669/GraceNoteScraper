package lineupindex

import (
	"fmt"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/web"
)

func TestNormalizeEPGCallSignPreservesDigitalSubchannels(t *testing.T) {
	tests := map[string]string{
		"WCBSDT":  "WCBS",
		"WCBS-HD": "WCBS",
		"WCBS SD": "WCBS",
		"WCBSDT2": "WCBSDT2",
		"WCBSDT3": "WCBSDT3",
	}
	for input, want := range tests {
		if got := normalizeEPGCallSign(input); got != want {
			t.Errorf("normalizeEPGCallSign(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestWeekdayEPGBlocksUseProviderLocalTime(t *testing.T) {
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	blocks, timezone, err := weekdayEPGBlocks(now, []web.Provider{{Timezone: "Eastern"}}, &web.ProviderResponse{})
	if err != nil {
		t.Fatal(err)
	}
	if timezone != "America/New_York" || len(blocks) != 4 {
		t.Fatalf("timezone=%q blocks=%+v", timezone, blocks)
	}
	location, _ := time.LoadLocation("America/New_York")
	if got := blocks[0].Start.In(location); got.Weekday() != time.Tuesday || got.Hour() != 20 {
		t.Fatalf("primary prime block = %s", got)
	}
	if got := blocks[1].Start.In(location); got.Weekday() != time.Wednesday || got.Hour() != 14 {
		t.Fatalf("primary secondary block = %s", got)
	}
}

func TestWeekdayEPGBlocksUseGracenoteMinuteOffsets(t *testing.T) {
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	blocks, timezone, err := weekdayEPGBlocks(now, nil, &web.ProviderResponse{
		StdUTCOffset: "-300",
		DSTUTCOffset: "-240",
	})
	if err != nil {
		t.Fatal(err)
	}
	if timezone != "America/New_York" || len(blocks) != 4 {
		t.Fatalf("timezone=%q blocks=%+v", timezone, blocks)
	}
	location, _ := time.LoadLocation("America/New_York")
	if got := blocks[0].Start.In(location); got.Weekday() != time.Tuesday || got.Hour() != 20 {
		t.Fatalf("primary prime block = %s", got)
	}
}

func TestParseUTCOffsetSupportsGracenoteMinutesAndConventionalHours(t *testing.T) {
	tests := []struct {
		value string
		want  int
		ok    bool
	}{
		{value: "-300", want: -300, ok: true},
		{value: "-240", want: -240, ok: true},
		{value: "-5", want: -300, ok: true},
		{value: "+05:30", want: 330, ok: true},
		{value: "840", want: 840, ok: true},
		{value: "841", ok: false},
		{value: "15:00", ok: false},
		{value: "05:30:00", ok: false},
	}
	for _, test := range tests {
		got, ok := parseUTCOffset(test.value)
		if got != test.want || ok != test.ok {
			t.Errorf("parseUTCOffset(%q) = %d, %v; want %d, %v", test.value, got, ok, test.want, test.ok)
		}
	}
}

func TestEPGMatchingRequiresPairLevelIdentityAndConfirmsSemanticSchedules(t *testing.T) {
	blocks := testEPGBlocks()
	leftPrime := testEPGChannel("LEFT", "WCBSDT", "CBS", blocks[0], []string{"News", "Prime 1", "Prime 2", "Prime 3", "Prime 4", "Late News"}, "LEFT")
	rightPrime := testEPGChannel("RIGHT", "WCBSHD", "CBS", blocks[0], []string{"News", "Prime 1", "Prime 2", "Prime 3", "Prime 4", "Late News"}, "RIGHT")
	leftSecondary := testEPGChannel("LEFT", "WCBSDT", "CBS", blocks[1], []string{"Talk 1", "Talk 2", "Drama 1", "Drama 2", "News 1", "News 2"}, "LEFT")
	rightSecondary := testEPGChannel("RIGHT", "WCBSHD", "CBS", blocks[1], []string{"Talk 1", "Talk 2", "Drama 1", "Drama 2", "News 1", "News 2"}, "RIGHT")
	scans := []*postalLineupScan{
		testEPGScan("L1", "Optimum", map[string]*web.GridResponse{blocks[0].ID: {Channels: []web.JSONChannel{leftPrime}}, blocks[1].ID: {Channels: []web.JSONChannel{leftSecondary}}}),
		testEPGScan("L2", "DIRECTV", map[string]*web.GridResponse{blocks[0].ID: {Channels: []web.JSONChannel{rightPrime}}, blocks[1].ID: {Channels: []web.JSONChannel{rightSecondary}}}),
	}
	stations, pairs := buildEPGCandidates(scans, blocks[0].ID)
	if len(pairs) != 1 {
		t.Fatalf("candidate pairs = %+v", pairs)
	}
	results := evaluateEPGPairs(scans, pairs, blocks)
	if len(results) != 1 || results[0].Status != "confirmed" || results[0].Occurrences != 12 {
		t.Fatalf("results = %+v", results)
	}
	facts := buildEPGDerivedFacts(stations, results, "test-epg", "America/New_York")
	if !hasEPGAlias(facts, "LEFT", "WCBSHD") || !hasEPGAlias(facts, "LEFT", "WCBS") || !hasEPGAlias(facts, "RIGHT", "WCBSDT") {
		t.Fatalf("derived facts = %+v", facts)
	}

	noIdentity := []*postalLineupScan{
		testEPGScan("L1", "Provider One", map[string]*web.GridResponse{blocks[0].ID: {Channels: []web.JSONChannel{testEPGChannel("A", "ALPHA", "Network A", blocks[0], []string{"One", "Two", "Three", "Four", "Five", "Six"}, "A")}}}),
		testEPGScan("L2", "Provider Two", map[string]*web.GridResponse{blocks[0].ID: {Channels: []web.JSONChannel{testEPGChannel("B", "BRAVO", "Network B", blocks[0], []string{"One", "Two", "Three", "Four", "Five", "Six"}, "B")}}}),
	}
	_, scheduleOnlyPairs := buildEPGCandidates(noIdentity, blocks[0].ID)
	if len(scheduleOnlyPairs) != 0 {
		t.Fatalf("schedule-only pairs = %+v", scheduleOnlyPairs)
	}

	sameLineupPositionCollision := []*postalLineupScan{
		testEPGScan("COLLISION", "DIRECTV New York", map[string]*web.GridResponse{blocks[0].ID: {Channels: []web.JSONChannel{
			testEPGChannel("A", "FILLER-A", "", blocks[0], []string{"One", "Two", "Three", "Four", "Five", "Six"}, "A"),
			testEPGChannel("B", "FILLER-B", "", blocks[0], []string{"One", "Two", "Three", "Four", "Five", "Six"}, "B"),
		}}}),
	}
	_, collisionPairs := buildEPGCandidates(sameLineupPositionCollision, blocks[0].ID)
	if len(collisionPairs) != 0 {
		t.Fatalf("same-lineup position collision became identity evidence: %+v", collisionPairs)
	}

	providerNameBridge := []*postalLineupScan{
		testEPGScan("L1", "Optimum", map[string]*web.GridResponse{blocks[0].ID: {Channels: []web.JSONChannel{{ChannelID: "PROVIDER", CallSign: "ALT"}}}}),
		testEPGScan("L2", "DIRECTV", map[string]*web.GridResponse{blocks[0].ID: {Channels: []web.JSONChannel{{ChannelID: "GRID", CallSign: "WCBSHD"}}}}),
	}
	providerNameBridge[0].Facts = []ProviderFact{{StationID: "PROVIDER", Kind: FactAlias, Value: "WCBS"}}
	_, providerNamePairs := buildEPGCandidates(providerNameBridge, blocks[0].ID)
	if len(providerNamePairs) != 1 || providerNamePairs[0].Evidence[0] != "identity-name:WCBS" {
		t.Fatalf("provider-name bridge = %+v", providerNamePairs)
	}
}

func TestEPGMatchingRejectsAffiliateOnlyIdentity(t *testing.T) {
	blocks := testEPGBlocks()
	scans := []*postalLineupScan{
		testEPGScan("L1", "Optimum", map[string]*web.GridResponse{blocks[0].ID: {Channels: []web.JSONChannel{
			testEPGChannel("WPXN", "WPXN", "ION: INDEPENDENT TELEVISION", blocks[0], []string{"One", "Two", "Three", "Four", "Five", "Six"}, "WPXN"),
		}}}),
		testEPGScan("L2", "DIRECTV", map[string]*web.GridResponse{blocks[0].ID: {Channels: []web.JSONChannel{
			testEPGChannel("IOND", "IONDHD", "ION: INDEPENDENT TELEVISION", blocks[0], []string{"One", "Two", "Three", "Four", "Five", "Six"}, "IOND"),
		}}}),
	}
	_, pairs := buildEPGCandidates(scans, blocks[0].ID)
	if len(pairs) != 0 {
		t.Fatalf("affiliate-only identity created candidate pairs: %+v", pairs)
	}
}

func TestEPGDerivedFactsExcludeTemporaryEventAliases(t *testing.T) {
	stations := map[string]*epgIdentityStation{
		"WPXN": {
			StationID: "WPXN", CallSigns: map[string]string{"WPXN": "WPXN"}, Affiliates: map[string]string{},
			ProviderNames: map[string]string{}, Positions: map[string]map[string]bool{}, LineupKeys: map[string]bool{"L1": true},
		},
		"IOND": {
			StationID: "IOND", CallSigns: map[string]string{"IOND": "IONDHD"}, Affiliates: map[string]string{},
			ProviderNames: map[string]string{"WNBAONION1": "WNBA on ION 1", "ION": "ION"},
			Positions:     map[string]map[string]bool{}, LineupKeys: map[string]bool{"L2": true},
		},
	}
	facts := buildEPGDerivedFacts(stations, []epgPairResult{{
		Pair:   epgCandidatePair{LeftID: "WPXN", RightID: "IOND", Evidence: []string{"identity-name:ION"}},
		Status: "confirmed", Occurrences: 12, MatchedMinutes: 720,
	}}, "test-epg", "America/New_York")
	if hasEPGAlias(facts, "WPXN", "WNBA on ION 1") {
		t.Fatalf("temporary event alias was transferred: %+v", facts)
	}
	if !hasEPGAlias(facts, "WPXN", "ION") {
		t.Fatalf("permanent alias was not transferred: %+v", facts)
	}
}

func TestEPGMatchingRejectsPlaceholderGuides(t *testing.T) {
	blocks := testEPGBlocks()
	placeholder := func(stationID string, block epgBlock) web.JSONChannel {
		channel := testEPGChannel(stationID, "ACCESSHD", "Local", block, []string{"Paid Programming", "Paid Programming", "Paid Programming", "Paid Programming", "Paid Programming", "Paid Programming"}, stationID)
		for index := range channel.Events {
			channel.Events[index].Program.ID = "SH000000010000"
		}
		return channel
	}
	scans := []*postalLineupScan{
		testEPGScan("L1", "One", map[string]*web.GridResponse{blocks[0].ID: {Channels: []web.JSONChannel{placeholder("A", blocks[0])}}, blocks[1].ID: {Channels: []web.JSONChannel{placeholder("A", blocks[1])}}}),
		testEPGScan("L2", "Two", map[string]*web.GridResponse{blocks[0].ID: {Channels: []web.JSONChannel{placeholder("B", blocks[0])}}, blocks[1].ID: {Channels: []web.JSONChannel{placeholder("B", blocks[1])}}}),
	}
	_, pairs := buildEPGCandidates(scans, blocks[0].ID)
	results := evaluateEPGPairs(scans, pairs, blocks)
	if len(results) != 1 || results[0].Status != "rejected" || results[0].Reason != "placeholder-only" {
		t.Fatalf("results = %+v", results)
	}
}

func TestEPGMatchingAcceptsLongFormCoverageAndUsesFallback(t *testing.T) {
	blocks := testEPGBlocks()
	longTitles := []string{"Movie One", "Movie Two"}
	weak := []string{"Paid Programming", "Paid Programming"}
	scans := []*postalLineupScan{
		testEPGScan("L1", "One", map[string]*web.GridResponse{
			blocks[0].ID: {Channels: []web.JSONChannel{testEPGChannel("A", "FX", "FX", blocks[0], weak, "A")}},
			blocks[1].ID: {Channels: []web.JSONChannel{testEPGChannel("A", "FX", "FX", blocks[1], longTitles, "A")}},
			blocks[2].ID: {Channels: []web.JSONChannel{testEPGChannel("A", "FX", "FX", blocks[2], longTitles, "A")}},
		}),
		testEPGScan("L2", "Two", map[string]*web.GridResponse{
			blocks[0].ID: {Channels: []web.JSONChannel{testEPGChannel("B", "FXHD", "FX", blocks[0], weak, "B")}},
			blocks[1].ID: {Channels: []web.JSONChannel{testEPGChannel("B", "FXHD", "FX", blocks[1], longTitles, "B")}},
			blocks[2].ID: {Channels: []web.JSONChannel{testEPGChannel("B", "FXHD", "FX", blocks[2], longTitles, "B")}},
		}),
	}
	for _, scan := range scans {
		for _, channel := range scan.Grids[blocks[0].ID].Channels {
			for index := range channel.Events {
				channel.Events[index].Program.ID = "SH000000010000"
			}
		}
	}
	_, pairs := buildEPGCandidates(scans, blocks[0].ID)
	results := evaluateEPGPairs(scans, pairs, blocks)
	if len(results) != 1 || results[0].Status != "confirmed" || results[0].Reason != "long-form-coverage" || results[0].MatchedMinutes != 720 {
		t.Fatalf("results = %+v", results)
	}
}

func TestEPGMatchingDoesNotReplaceStrongDivergentPrimaryWithFallback(t *testing.T) {
	blocks := testEPGBlocks()
	leftPrimary := []string{"A", "B", "C", "D", "E", "F"}
	rightPrimary := []string{"G", "H", "I", "J", "K", "L"}
	fallback := []string{"One", "Two", "Three", "Four", "Five", "Six"}
	scans := []*postalLineupScan{
		testEPGScan("L1", "One", map[string]*web.GridResponse{
			blocks[0].ID: {Channels: []web.JSONChannel{testEPGChannel("A", "MATCH", "", blocks[0], leftPrimary, "A")}},
			blocks[1].ID: {Channels: []web.JSONChannel{testEPGChannel("A", "MATCH", "", blocks[1], fallback, "A")}},
			blocks[2].ID: {Channels: []web.JSONChannel{testEPGChannel("A", "MATCH", "", blocks[2], fallback, "A")}},
		}),
		testEPGScan("L2", "Two", map[string]*web.GridResponse{
			blocks[0].ID: {Channels: []web.JSONChannel{testEPGChannel("B", "MATCHHD", "", blocks[0], rightPrimary, "B")}},
			blocks[1].ID: {Channels: []web.JSONChannel{testEPGChannel("B", "MATCHHD", "", blocks[1], fallback, "B")}},
			blocks[2].ID: {Channels: []web.JSONChannel{testEPGChannel("B", "MATCHHD", "", blocks[2], fallback, "B")}},
		}),
	}
	_, pairs := buildEPGCandidates(scans, blocks[0].ID)
	results := evaluateEPGPairs(scans, pairs, blocks)
	if len(results) != 1 || results[0].Status != "rejected" || results[0].Reason != "weekday-schedules-diverged" {
		t.Fatalf("results = %+v", results)
	}
}

func testEPGBlocks() []epgBlock {
	start := time.Date(2026, time.September, 2, 0, 0, 0, 0, time.UTC)
	return []epgBlock{
		{ID: "primary-prime", Role: epgPrimeRole, Start: start},
		{ID: "primary-secondary", Role: epgSecondaryRole, Start: start.Add(18 * time.Hour)},
		{ID: "fallback-prime", Role: epgPrimeRole, Fallback: true, Start: start.Add(48 * time.Hour)},
		{ID: "fallback-secondary", Role: epgSecondaryRole, Fallback: true, Start: start.Add(66 * time.Hour)},
	}
}

func testEPGScan(lineupID, provider string, grids map[string]*web.GridResponse) *postalLineupScan {
	return &postalLineupScan{
		Lineup: &LineupRecord{Key: lineupID, LineupID: lineupID}, Provider: web.Provider{Name: provider, LineupID: lineupID}, Grids: grids,
	}
}

func testEPGChannel(stationID, callSign, affiliate string, block epgBlock, titles []string, programPrefix string) web.JSONChannel {
	duration := epgBlockHours * time.Hour / time.Duration(len(titles))
	events := make([]web.JSONEvent, 0, len(titles))
	for index, title := range titles {
		start := block.Start.Add(time.Duration(index) * duration)
		end := start.Add(duration)
		events = append(events, web.JSONEvent{
			StartTime: start.Format(time.RFC3339), EndTime: end.Format(time.RFC3339),
			Program: web.JSONProgram{ID: fmt.Sprintf("%s-%d", programPrefix, index), Title: title},
		})
	}
	return web.JSONChannel{ChannelID: stationID, ChannelNo: "2", CallSign: callSign, AffiliateName: affiliate, Events: events}
}

func hasEPGAlias(facts []epgDerivedFact, stationID, value string) bool {
	for _, fact := range facts {
		if fact.StationID == stationID && fact.Kind == FactAlias && fact.Value == value {
			return true
		}
	}
	return false
}
