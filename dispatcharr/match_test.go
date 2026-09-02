package dispatcharr

import (
	"strings"
	"testing"
)

func TestMatchStreamsNormalizesProviderPrefixesAndQuality(t *testing.T) {
	channels := []MatchChannel{
		{ID: "espn", Number: "570", Name: "ESPN", Aliases: []string{"ESPNHD"}, EPGIDs: []string{"ESPN.us"}},
		{ID: "usa", Number: "550", Name: "USA Network", Aliases: []string{"USAHD"}},
	}
	number := 570.0
	streams := []Stream{
		{ID: 1, M3UAccountID: 3, Name: "US| ESPN FHD", StreamChannelNo: &number},
		{ID: 2, M3UAccountID: 3, Name: "USA Network HD"},
	}
	got := MatchStreams("source", channels, streams, nil)
	if len(got) != 2 {
		t.Fatalf("candidates = %+v", got)
	}
	byStream := map[int64]Candidate{got[0].StreamID: got[0], got[1].StreamID: got[1]}
	if byStream[1].ChannelID != "espn" || byStream[1].Score < 95 {
		t.Fatalf("ESPN candidate = %+v", byStream[1])
	}
	if byStream[2].ChannelID != "usa" {
		t.Fatalf("USA Network candidate = %+v", byStream[2])
	}
}

func TestMatchStreamsNormalizesStylizedProviderQualityMarkers(t *testing.T) {
	got := MatchStreams("source",
		[]MatchChannel{{ID: "bounce", Number: "494", Name: "Bounce TV", Aliases: []string{"US: BOUNCE TV HD"}}},
		[]Stream{{ID: 1, M3UAccountID: 3, Name: "US: BOUNCE TV ᴿᴬᵂ ⁶⁰ᶠᵖˢ"}},
		nil,
	)
	if len(got) != 1 || got[0].ChannelID != "bounce" || got[0].Score < 98 {
		t.Fatalf("stylized-quality candidate = %+v", got)
	}
}

func TestMatchStreamsNormalizesDelimitedProviderPrefixesAndSpacing(t *testing.T) {
	channels := []MatchChannel{
		{ID: "espn2", Number: "574", Name: "ESPN 2"},
		{ID: "reelz", Number: "692", Name: "REELZ"},
		{ID: "roku", Number: "800", Name: "The Roku Channel"},
	}
	streams := []Stream{
		{ID: 1, M3UAccountID: 3, Name: "GO: ESPN2 HD"},
		{ID: 2, M3UAccountID: 3, Name: "Prime| REELZ FHD"},
		{ID: 3, M3UAccountID: 3, Name: "ROKU: The Roku Channel"},
	}
	got := MatchStreams("source", channels, streams, nil)
	if len(got) != 3 {
		t.Fatalf("provider-prefix candidates = %+v", got)
	}
	for _, candidate := range got {
		if candidate.Score < 98 {
			t.Fatalf("provider-prefix candidate = %+v", candidate)
		}
	}
}

func TestMatchStreamsStripsOnlyMatchingDispatcharrChannelNumber(t *testing.T) {
	number := 502.0
	got := MatchStreams("source",
		[]MatchChannel{{ID: "wcbs", Number: "2", Name: "WCBS-DT"}},
		[]Stream{{ID: 1, M3UAccountID: 12, Name: "502 WCBS-DT", StreamChannelNo: &number}},
		nil,
	)
	if len(got) != 1 || got[0].ChannelID != "wcbs" || got[0].NormalizedAlias != "wcbsdt" || got[0].Score < 98 {
		t.Fatalf("number-prefixed candidate = %+v", got)
	}

	wrongNumber := 999.0
	if got := MatchStreams("source",
		[]MatchChannel{{ID: "usa", Number: "550", Name: "USA Network"}},
		[]Stream{{ID: 2, M3UAccountID: 12, Name: "2026 USA BMX Red River National", StreamChannelNo: &wrongNumber}},
		nil,
	); len(got) != 0 {
		t.Fatalf("unverified leading number was stripped: %+v", got)
	}
}

func TestMatchStreamsUsesExactEPGID(t *testing.T) {
	got := MatchStreams("source",
		[]MatchChannel{{ID: "cnn", Number: "600", Name: "Cable News Network", EPGIDs: []string{"CNN.us"}}},
		[]Stream{{ID: 5, M3UAccountID: 3, Name: "Unhelpful provider label", TVGID: "cnn.US"}},
		nil,
	)
	if len(got) != 1 || got[0].ChannelID != "cnn" || got[0].Score != 100 || got[0].Reason != "Exact EPG ID" {
		t.Fatalf("EPG candidate = %+v", got)
	}
}

func TestDeniedCandidateExposesNextChoiceAndConfirmedHidesStream(t *testing.T) {
	channels := []MatchChannel{
		{ID: "sd", Number: "70", Name: "ESPN"},
		{ID: "hd", Number: "570", Name: "ESPN"},
	}
	streams := []Stream{{ID: 1, M3UAccountID: 3, Name: "ESPN HD"}}
	first := MatchStreams("source", channels, streams, nil)
	if len(first) != 1 || first[0].ChannelID != "sd" {
		t.Fatalf("first candidate = %+v", first)
	}
	denied := map[string]Decision{
		first[0].Key: {Key: first[0].Key, Decision: "denied", Source: "source", StreamHash: first[0].StreamHash, ChannelID: "sd"},
	}
	second := MatchStreams("source", channels, streams, denied)
	if len(second) != 1 || second[0].ChannelID != "hd" {
		t.Fatalf("second candidate = %+v", second)
	}
	confirmed := map[string]Decision{
		first[0].Key: {Key: first[0].Key, Decision: "confirmed", Source: "source", StreamHash: first[0].StreamHash, ChannelID: "sd"},
	}
	if got := MatchStreams("source", channels, streams, confirmed); len(got) != 0 {
		t.Fatalf("confirmed stream was proposed again: %+v", got)
	}
}

func TestMatchStreamCandidatesRetainsAlreadyScoredAlternatives(t *testing.T) {
	channels := []MatchChannel{
		{ID: "sd", Number: "70", Name: "ESPN"},
		{ID: "hd", Number: "570", Name: "ESPN"},
	}
	result := MatchStreamCandidates("source", channels, []Stream{{ID: 1, M3UAccountID: 3, Name: "ESPN HD"}}, nil)
	if len(result.Primary) != 1 || result.Primary[0].ChannelID != "sd" {
		t.Fatalf("primary candidates = %+v", result.Primary)
	}
	if len(result.All) != 2 || result.All[0].ChannelID != "sd" || result.All[1].ChannelID != "hd" {
		t.Fatalf("all candidates = %+v", result.All)
	}
}

func TestUnrelatedStreamHasNoCandidate(t *testing.T) {
	got := MatchStreams("source",
		[]MatchChannel{{ID: "espn", Number: "570", Name: "ESPN"}},
		[]Stream{{ID: 1, M3UAccountID: 3, Name: "Completely Unrelated Feed"}},
		nil,
	)
	if len(got) != 0 {
		t.Fatalf("unrelated candidates = %+v", got)
	}
}

func TestMatchStreamsFindsSingleTokenTypo(t *testing.T) {
	got := MatchStreams("source",
		[]MatchChannel{{ID: "discovery", Number: "620", Name: "Discovery"}},
		[]Stream{{ID: 1, M3UAccountID: 3, Name: "Discvery HD"}},
		nil,
	)
	if len(got) != 1 || got[0].ChannelID != "discovery" || got[0].Score < minimumCandidateScore || !strings.HasPrefix(got[0].Reason, "Fuzzy name") {
		t.Fatalf("single-token typo candidate = %+v", got)
	}
}

func TestMatchStreamsFindsAdjacentTransposition(t *testing.T) {
	got := MatchStreams("source",
		[]MatchChannel{{ID: "discovery", Number: "620", Name: "Discovery"}},
		[]Stream{{ID: 1, M3UAccountID: 3, Name: "Dicsovery HD"}},
		nil,
	)
	if len(got) != 1 || got[0].ChannelID != "discovery" || got[0].Score < minimumCandidateScore {
		t.Fatalf("transposed-name candidate = %+v", got)
	}
}

func TestShortDistinctiveChannelNameCanBeProposed(t *testing.T) {
	got := MatchStreams("source",
		[]MatchChannel{{ID: "cnn", Number: "600", Name: "CNN"}},
		[]Stream{{ID: 1, M3UAccountID: 3, Name: "CNN USA East"}},
		nil,
	)
	if len(got) != 1 || got[0].ChannelID != "cnn" || got[0].Score < minimumCandidateScore {
		t.Fatalf("short-name candidate = %+v", got)
	}
}

func TestExactChannelNameBeatsConflictingAlias(t *testing.T) {
	got := MatchStreams("source",
		[]MatchChannel{
			{ID: "cspan", Number: "606", Name: "C-SPAN", Aliases: []string{"US: C-SPAN 2 HD"}},
			{ID: "cspan2", Number: "607", Name: "C-SPAN2"},
		},
		[]Stream{{ID: 1, M3UAccountID: 3, Name: "CSPAN2"}},
		nil,
	)
	if len(got) != 1 || got[0].ChannelID != "cspan2" || got[0].Score != 99 {
		t.Fatalf("direct-name candidate = %+v", got)
	}
}

func TestDoesNotMatchEventOrDecorativeHeadingByOneGenericToken(t *testing.T) {
	got := MatchStreams("source",
		[]MatchChannel{{ID: "usa", Number: "550", Name: "USA Network"}, {ID: "cbs", Number: "502", Name: "CBS 2 New York"}},
		[]Stream{
			{ID: 1, M3UAccountID: 3, Name: "2026 USA BMX Red River National"},
			{ID: 2, M3UAccountID: 3, Name: "##### CBS HD #####"},
		},
		nil,
	)
	if len(got) != 0 {
		t.Fatalf("event or heading candidates = %+v", got)
	}
}

func TestDecisionsSurviveAuthenticationSourceChange(t *testing.T) {
	streams := []Stream{
		{ID: 1, M3UAccountID: 3, Name: "CNN"},
		{ID: 2, M3UAccountID: 3, Name: "ESPN"},
	}
	channels := []MatchChannel{
		{ID: "cnn", Number: "100", Name: "CNN"},
		{ID: "espn", Number: "200", Name: "ESPN"},
	}
	decisions := map[string]Decision{
		"old-confirm": {
			Key: "old-confirm", Decision: "confirmed", Source: "old-auth",
			StreamHash: streams[0].Fingerprint(), ChannelID: "cnn",
		},
		"old-deny": {
			Key: "old-deny", Decision: "denied", Source: "old-auth",
			StreamHash: streams[1].Fingerprint(), ChannelID: "espn",
		},
	}

	if got := MatchStreams("new-auth", channels, streams, decisions); len(got) != 0 {
		t.Fatalf("authentication change restored reviewed candidates: %+v", got)
	}
}

func TestGroupCandidatesCombinesEquivalentProviderStreams(t *testing.T) {
	candidates := []Candidate{
		{Key: "one", Source: "dispatch", ChannelID: "ION", ChannelNumber: "3", ChannelName: "WPXN", StreamName: "US: ION", TVGID: "ION.us", KnownEPGID: true, M3UAccountID: 3, Score: 98, Reason: "Exact normalized name or alias"},
		{Key: "two", Source: "dispatch", ChannelID: "ION", ChannelNumber: "3", ChannelName: "WPXN", StreamName: "US| ION HD", TVGID: "ION.us", KnownEPGID: true, M3UAccountID: 7, Score: 98, Reason: "Exact normalized name or alias"},
		{Key: "three", Source: "dispatch", ChannelID: "ION", ChannelNumber: "3", ChannelName: "WPXN", StreamName: "ION", TVGID: "ION-East.us", M3UAccountID: 11, Score: 99, Reason: "Exact normalized channel name"},
	}
	groups := GroupCandidates(candidates)
	if len(groups) != 1 {
		t.Fatalf("groups = %+v", groups)
	}
	group := groups[0]
	if group.StreamCount != 3 || len(group.StreamNames) != 3 || len(group.TVGIDs) != 2 || len(group.M3UAccountIDs) != 3 {
		t.Fatalf("group evidence = %+v", group)
	}
	if group.NormalizedAlias != "ion" || group.Tier != "exact" || group.MinimumScore != 98 || group.MaximumScore != 99 {
		t.Fatalf("group identity = %+v", group)
	}
	knownByID := make(map[string]bool)
	for _, evidence := range group.TVGIDEvidence {
		knownByID[evidence.ID] = evidence.Known
	}
	if len(group.TVGIDEvidence) != 2 || !knownByID["ION.us"] || knownByID["ION-East.us"] {
		t.Fatalf("TVG-ID provenance = %+v", group.TVGIDEvidence)
	}
}

func TestAliasDecisionSuppressesEquivalentStreamsAcrossAccounts(t *testing.T) {
	channels := []MatchChannel{{ID: "ION", Number: "3", Name: "WPXN", Aliases: []string{"ION"}}}
	first := Stream{ID: 1, M3UAccountID: 3, Name: "US: ION"}
	decisions := map[string]Decision{
		"confirmed": {Decision: "confirmed", StreamHash: first.Fingerprint(), ChannelID: "ION", StreamName: first.Name},
	}
	got := MatchStreams("dispatch", channels, []Stream{
		first,
		{ID: 2, M3UAccountID: 7, Name: "US| ION HD"},
	}, decisions)
	if len(got) != 0 {
		t.Fatalf("equivalent confirmed alias returned candidates: %+v", got)
	}
}

func TestPersistedSafeNormalizedAliasSuppressesEquivalentNumberedStream(t *testing.T) {
	channel := MatchChannel{ID: "WCBS", Number: "2", Name: "WCBS-DT"}
	firstNumber := 502.0
	first := Stream{ID: 1, M3UAccountID: 12, Name: "502 WCBS-DT", StreamChannelNo: &firstNumber}
	decisions := map[string]Decision{
		"confirmed": {Decision: "confirmed", StreamHash: first.Fingerprint(), ChannelID: "WCBS", StreamName: first.Name, NormalizedAlias: NormalizeStreamAlias(first)},
	}
	secondNumber := 2.0
	got := MatchStreams("dispatch", []MatchChannel{channel}, []Stream{
		first,
		{ID: 2, M3UAccountID: 7, Name: "GO: WCBS-DT HD", StreamChannelNo: &secondNumber},
	}, decisions)
	if len(got) != 0 {
		t.Fatalf("persisted normalized alias returned candidate: %+v", got)
	}
}

func TestTemporaryEventStreamDoesNotMatchPermanentNetworkAlias(t *testing.T) {
	channels := []MatchChannel{{ID: "ION", Number: "3", Name: "WPXN", Category: "Entertainment", Aliases: []string{"ION", "WNBA on ION 1"}}}
	got := MatchStreams("dispatch", channels, []Stream{{ID: 1, M3UAccountID: 3, Name: "WNBA on ION 1"}}, nil)
	if len(got) != 0 {
		t.Fatalf("temporary event stream matched permanent channel: %+v", got)
	}
	channels[0].Category = "PPV & Events"
	if got := MatchStreams("dispatch", channels, []Stream{{ID: 1, M3UAccountID: 3, Name: "WNBA on ION 1"}}, nil); len(got) != 1 {
		t.Fatalf("event channel did not retain event candidate: %+v", got)
	}
}
