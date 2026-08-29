package lineuparr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func newTestService(t *testing.T, catalogJSON, iptvJSON string) *Service {
	t.Helper()
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := ""
		switch request.URL.Path {
		case "/catalog.json":
			body = catalogJSON
		case "/iptv.json":
			body = iptvJSON
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}
	store, err := LoadStateStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("LoadStateStore() error = %v", err)
	}
	options := ServiceOptions{CacheDir: filepath.Join(t.TempDir(), "cache"), HTTPClient: client}
	if catalogJSON != "" {
		options.CatalogURLs = []string{"https://sources.test/catalog.json"}
	}
	if iptvJSON != "" {
		options.IPTVOrgURL = "https://sources.test/iptv.json"
	}
	return NewService(store, options)
}

func testContext(fingerprint string) LineupContext {
	return LineupContext{
		SourceFingerprint: fingerprint,
		Country:           "USA",
		PostalCode:        "11743",
		ProviderName:      "Test Cable",
		LineupID:          "USA-TEST",
	}
}

func TestBuildRetainsEveryProviderPosition(t *testing.T) {
	service := newTestService(t, "", "")
	inputs := []InputChannel{
		{Key: "placement-1", StationID: "12345", PlacementID: "123450", Number: "50", CallSign: "USA", Affiliate: "USA Network"},
		{Key: "placement-2", StationID: "12345", PlacementID: "123451", Number: "550", CallSign: "USAHD", Affiliate: "USA Network"},
	}
	draft, err := service.Build(context.Background(), testContext("source-one"), inputs)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if draft.Total != 2 || draft.Included != 2 || draft.Excluded != 0 {
		t.Fatalf("draft counts = total %d included %d excluded %d", draft.Total, draft.Included, draft.Excluded)
	}
	if draft.Channels[0].ID == draft.Channels[1].ID {
		t.Fatal("provider positions were collapsed")
	}
	for _, channel := range draft.Channels {
		if !contains(channel.EPGIDs, "12345") {
			t.Fatalf("channel %q EPG IDs = %v", channel.ID, channel.EPGIDs)
		}
		if !contains(channel.Aliases, channel.PlacementID) {
			t.Fatalf("channel %q aliases do not contain placement ID %q: %v", channel.ID, channel.PlacementID, channel.Aliases)
		}
		if !channel.Included {
			t.Fatalf("channel %q was not retained by default", channel.ID)
		}
	}
}

func TestBuildAppliesOnlyUniqueExactCatalogMatches(t *testing.T) {
	catalog := `{
      "package":"Curated test",
      "categories":{
        "Sports":[{"name":"ESPN","number":206,"aliases":["ESPNHD","ESPN US"],"epg_ids":["20001"]}],
        "News":[{"name":"Alpha News","number":1,"aliases":["AMB"]},{"name":"Another News","number":2,"aliases":["AMB"]}]
      }
    }`
	service := newTestService(t, catalog, "")
	inputs := []InputChannel{
		{Key: "espn", StationID: "20001", Number: "570", CallSign: "ESPNHD"},
		{Key: "ambiguous", StationID: "30001", Number: "99", CallSign: "AMB"},
	}
	draft, err := service.Build(context.Background(), testContext("source-one"), inputs)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	espn := channelByID(t, draft, "espn")
	if espn.Name != "ESPN" || espn.Category != "Sports" {
		t.Fatalf("ESPN enrichment = name %q category %q", espn.Name, espn.Category)
	}
	if !contains(espn.Aliases, "ESPN US") || !contains(espn.EPGIDs, "20001") {
		t.Fatalf("ESPN aliases/EPG IDs = %v / %v", espn.Aliases, espn.EPGIDs)
	}
	if espn.NameMethod != "exact catalog identity" || espn.CategoryMethod != "exact catalog identity" || !evidenceHasMethod(espn.EPGIDEvidence, "20001", "curated catalog EPG ID") {
		t.Fatalf("ESPN provenance = name %q category %q EPG %+v", espn.NameMethod, espn.CategoryMethod, espn.EPGIDEvidence)
	}
	ambiguous := channelByID(t, draft, "ambiguous")
	if ambiguous.Name != "AMB" || ambiguous.Category != uncategorized {
		t.Fatalf("ambiguous channel was applied: %+v", ambiguous)
	}
	status := draft.Sources[1]
	if status.Matched != 1 || status.Ambiguous != 1 {
		t.Fatalf("catalog status = %+v", status)
	}
}

func TestBuildUsesScheduleCategoryOnlyWhenExactSourcesDoNotCategorize(t *testing.T) {
	catalog := `{"package":"Curated test","categories":{"Sports":[{"name":"ESPN","aliases":["ESPNHD"]}]}}`
	service := newTestService(t, catalog, "")
	inputs := []InputChannel{
		{
			Key: "catalog", CallSign: "ESPNHD",
			CategoryHint: &AttributedCategory{Value: "News", Source: "gracenote-schedule", Label: "Gracenote schedule profile", Method: "80% of scheduled minutes use Gracenote news filter"},
		},
		{
			Key: "schedule", CallSign: "MOVIES",
			CategoryHint: &AttributedCategory{Value: "Movies", Source: "gracenote-schedule", Label: "Gracenote schedule profile", Method: "90% of scheduled minutes use Gracenote movie filter"},
		},
	}
	draft, err := service.Build(context.Background(), testContext("source-one"), inputs)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	catalogChannel := channelByID(t, draft, "catalog")
	if catalogChannel.Category != "Sports" || catalogChannel.CategoryMethod != "exact catalog identity" {
		t.Fatalf("catalog category = %+v", catalogChannel)
	}
	scheduleChannel := channelByID(t, draft, "schedule")
	if scheduleChannel.Category != "Movies" || scheduleChannel.CategorySource != "gracenote-schedule" || !strings.Contains(scheduleChannel.CategoryMethod, "90%") {
		t.Fatalf("schedule category = %+v", scheduleChannel)
	}
}

func TestBuildUsesIPTVOrgExactNamesAndCategories(t *testing.T) {
	iptv := `[
      {"id":"Example.us","name":"Example Network","alt_names":["EXNET"],"country":"US","categories":["documentary"],"closed":null,"replaced_by":null},
      {"id":"Closed.us","name":"Closed Network","alt_names":["CLOSED"],"country":"US","categories":["news"],"closed":"2025-01-01","replaced_by":"Example.us"},
      {"id":"Foreign.ca","name":"Foreign Network","alt_names":["FOREIGN"],"country":"CA","categories":["sports"],"closed":null,"replaced_by":null}
    ]`
	service := newTestService(t, "", iptv)
	inputs := []InputChannel{
		{Key: "example", StationID: "1", Number: "1", CallSign: "EXNET"},
		{Key: "closed", StationID: "2", Number: "2", CallSign: "CLOSED"},
		{Key: "foreign", StationID: "3", Number: "3", CallSign: "FOREIGN"},
	}
	draft, err := service.Build(context.Background(), testContext("source-one"), inputs)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	example := channelByID(t, draft, "example")
	if example.Name != "Example Network" || example.Category != "Discovery" {
		t.Fatalf("iptv-org enrichment = %+v", example)
	}
	if !contains(example.EPGIDs, "Example.us") || !evidenceHasMethod(example.EPGIDEvidence, "Example.us", "public database channel ID") {
		t.Fatalf("iptv-org EPG provenance = IDs %v evidence %+v", example.EPGIDs, example.EPGIDEvidence)
	}
	if channelByID(t, draft, "closed").Category != uncategorized {
		t.Fatal("closed iptv-org entry was applied")
	}
	if channelByID(t, draft, "foreign").Category != uncategorized {
		t.Fatal("foreign iptv-org entry was applied")
	}
}

func TestIPTVOrgCategoryMappingCoversPublishedCategories(t *testing.T) {
	tests := map[string]string{
		"animation": "Kids", "auto": "Outdoors", "business": "News",
		"culture": "Discovery", "family": "Kids", "interactive": "Entertainment",
		"series": "Entertainment", "shop": "Shopping", "xxx": "Adult & PPV",
	}
	for input, want := range tests {
		if got := mapIPTVOrgCategory(input); got != want {
			t.Errorf("mapIPTVOrgCategory(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDuplicateSuggestionsAreExplicitAndReversible(t *testing.T) {
	catalog := `{"package":"Curated test","categories":{"Sports":[{"name":"ESPN","number":206,"aliases":["ESPNSD","ESPNHD"]}]}}`
	service := newTestService(t, catalog, "")
	inputs := []InputChannel{
		{Key: "sd", StationID: "10", Number: "70", CallSign: "ESPNSD"},
		{Key: "hd", StationID: "11", Number: "570", CallSign: "ESPNHD"},
	}
	lineup := testContext("source-one")
	draft, err := service.Build(context.Background(), lineup, inputs)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(draft.DuplicateSuggestions) != 1 || draft.DuplicateSuggestions[0].RemoveID != "sd" || draft.DuplicateSuggestions[0].KeepID != "hd" {
		t.Fatalf("duplicate suggestions = %+v; channels = %+v; sources = %+v", draft.DuplicateSuggestions, draft.Channels, draft.Sources)
	}
	if err := service.RemoveSuggestedDuplicates(lineup.SourceFingerprint, draft); err != nil {
		t.Fatalf("RemoveSuggestedDuplicates() error = %v", err)
	}
	draft, _ = service.Build(context.Background(), lineup, inputs)
	if channelByID(t, draft, "sd").Included || !channelByID(t, draft, "hd").Included {
		t.Fatalf("post-removal inclusion = sd %v hd %v", channelByID(t, draft, "sd").Included, channelByID(t, draft, "hd").Included)
	}
	if err := service.RestoreAll(lineup.SourceFingerprint); err != nil {
		t.Fatalf("RestoreAll() error = %v", err)
	}
	draft, _ = service.Build(context.Background(), lineup, inputs)
	if !channelByID(t, draft, "sd").Included || !channelByID(t, draft, "hd").Included {
		t.Fatal("RestoreAll() did not restore both positions")
	}
}

func TestDuplicateSuggestionRecognizesLocalDigitalCallsign(t *testing.T) {
	catalog := `{"package":"Local test","categories":{"Local":[{"name":"CBS 2 New York","number":502,"aliases":["WCBS","WCBSDT"]}]}}`
	service := newTestService(t, catalog, "")
	draft, err := service.Build(context.Background(), testContext("source-one"), []InputChannel{
		{Key: "sd", StationID: "10", Number: "2", CallSign: "WCBS"},
		{Key: "hd", StationID: "11", Number: "502", CallSign: "WCBSDT"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.DuplicateSuggestions) != 1 || draft.DuplicateSuggestions[0].RemoveID != "sd" || draft.DuplicateSuggestions[0].KeepID != "hd" {
		t.Fatalf("local duplicate suggestions = %+v", draft.DuplicateSuggestions)
	}
}

func TestDuplicateSuggestionRequiresSharedAttributableSource(t *testing.T) {
	suggestions := findDuplicateSuggestions([]DraftChannel{
		{ID: "sd", Number: "2", Name: "Example", OriginalName: "EXAMPLE", CallSign: "EXAMPLE", NameSource: "source one", MatchedSources: []string{"gracenote", "source-one"}},
		{ID: "hd", Number: "502", Name: "Example", OriginalName: "EXAMPLEHD", CallSign: "EXAMPLEHD", NameSource: "source two", MatchedSources: []string{"gracenote", "source-two"}},
	})
	if len(suggestions) != 0 {
		t.Fatalf("cross-source duplicate suggestion = %+v", suggestions)
	}
}

func TestOverridesAreScopedToActiveSource(t *testing.T) {
	service := newTestService(t, "", "")
	inputs := []InputChannel{{Key: "one", StationID: "1", Number: "1", CallSign: "ONE"}}
	category := "News"
	included := false
	if err := service.UpdateChannel("source-one", "one", ChannelUpdate{Included: &included, Category: &category}); err != nil {
		t.Fatalf("UpdateChannel() error = %v", err)
	}
	draft, _ := service.Build(context.Background(), testContext("source-one"), inputs)
	if channel := channelByID(t, draft, "one"); channel.Included || channel.Category != "News" {
		t.Fatalf("source-one override = %+v", channel)
	}
	draft, _ = service.Build(context.Background(), testContext("source-two"), inputs)
	if channel := channelByID(t, draft, "one"); !channel.Included || channel.Category != uncategorized {
		t.Fatalf("old override leaked into source-two: %+v", channel)
	}
}

func TestConfirmedDispatcharrMatchAndAliasSuppressionAreReversible(t *testing.T) {
	service := newTestService(t, "", "")
	lineup := testContext("source-one")
	decision := MatchDecision{
		Key: "candidate", Decision: "confirmed", DispatcharrFingerprint: "dispatcharr-source",
		StreamFingerprint: "stream-hash", StreamKey: "3:10", StreamID: 10, M3UAccountID: 3,
		ChannelID: "one", ChannelNumber: "5", ChannelName: "ONE", StreamName: "US| One Network HD",
		TVGID: "OneNetwork.us", Score: 96, Reason: "Exact normalized name or alias",
	}
	if err := service.SetMatchDecision(lineup.SourceFingerprint, decision); err != nil {
		t.Fatal(err)
	}
	inputs := []InputChannel{{Key: "one", StationID: "1", Number: "5", CallSign: "ONE"}}
	draft, err := service.Build(context.Background(), lineup, inputs)
	if err != nil {
		t.Fatal(err)
	}
	channel := channelByID(t, draft, "one")
	if !contains(channel.Aliases, decision.StreamName) || !contains(channel.EPGIDs, decision.TVGID) {
		t.Fatalf("confirmed aliases/EPG IDs = %v / %v", channel.Aliases, channel.EPGIDs)
	}
	if err := service.SetAliasSuppressed(lineup.SourceFingerprint, "one", decision.StreamName, true); err != nil {
		t.Fatal(err)
	}
	draft, _ = service.Build(context.Background(), lineup, inputs)
	channel = channelByID(t, draft, "one")
	if contains(channel.Aliases, decision.StreamName) || len(channel.SuppressedAliasEvidence) != 1 {
		t.Fatalf("suppressed channel = %+v", channel)
	}
	if err := service.SetAliasSuppressed(lineup.SourceFingerprint, "one", decision.StreamName, false); err != nil {
		t.Fatal(err)
	}
	if err := service.ClearMatchDecision(lineup.SourceFingerprint, decision.Key); err != nil {
		t.Fatal(err)
	}
	draft, _ = service.Build(context.Background(), lineup, inputs)
	channel = channelByID(t, draft, "one")
	if contains(channel.Aliases, decision.StreamName) || contains(channel.EPGIDs, decision.TVGID) {
		t.Fatalf("cleared decision still enriched channel: %+v", channel)
	}
}

func TestExportIsLineuparrCompatibleAndExcludesRemovedChannels(t *testing.T) {
	service := newTestService(t, "", "")
	inputs := []InputChannel{
		{Key: "keep", StationID: "1", PlacementID: "11", Number: "5.1", CallSign: "KEEP"},
		{Key: "remove", StationID: "2", PlacementID: "22", Number: "6", CallSign: "REMOVE"},
	}
	lineup := testContext("source-one")
	category := "Local"
	included := false
	if err := service.UpdateChannel(lineup.SourceFingerprint, "keep", ChannelUpdate{Category: &category}); err != nil {
		t.Fatal(err)
	}
	if err := service.UpdateChannel(lineup.SourceFingerprint, "remove", ChannelUpdate{Included: &included}); err != nil {
		t.Fatal(err)
	}
	draft, _ := service.Build(context.Background(), lineup, inputs)
	export := ExportFromDraft(draft)
	if len(export.Categories) != 1 || len(export.Categories["Local"]) != 1 {
		t.Fatalf("export categories = %+v", export.Categories)
	}
	entry := export.Categories["Local"][0]
	if entry.Name != "KEEP" || entry.Number != 5.1 || !contains(entry.EPGIDs, "1") {
		t.Fatalf("export entry = %+v", entry)
	}
	encoded, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var shape map[string]any
	if err := json.Unmarshal(encoded, &shape); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if _, ok := shape["categories"]; !ok {
		t.Fatal("export is missing required categories key")
	}
	if got := ExportFilename(draft); got != "US_Test-Cable-11743_lineup.json" {
		t.Fatalf("ExportFilename() = %q", got)
	}
}

func channelByID(t *testing.T, draft *Draft, id string) DraftChannel {
	t.Helper()
	for _, channel := range draft.Channels {
		if channel.ID == id {
			return channel
		}
	}
	t.Fatalf("channel %q not found", id)
	return DraftChannel{}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func evidenceHasMethod(values []IdentifierEvidence, wantedValue, wantedMethod string) bool {
	for _, evidence := range values {
		if evidence.Value == wantedValue && contains(evidence.Methods, wantedMethod) {
			return true
		}
	}
	return false
}
