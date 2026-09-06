package lineuparr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/channelcategory"
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

func TestBuildConsolidatesProviderSourceRowsAndListsMatchedEvidence(t *testing.T) {
	service := newTestService(t, "", "")
	lineup := testContext("source-one")
	lineup.AdditionalSources = []SourceStatus{
		{ID: "provider-guide-dish", Label: "DISH official lineup", URL: "https://example.test/dish", Status: "registered"},
		{ID: "dish-official-lineup", Label: "DISH official lineup", URL: "https://example.test/dish", Status: "complete", Matched: 10, Message: "provider rows captured"},
		{ID: "dish-official-lineup", Label: "DISH official lineup", Status: "derived", Matched: 1, Message: "category applied"},
	}
	inputs := []InputChannel{{
		Key: "espn", StationID: "20001", Number: "570", CallSign: "ESPNHD",
		ExternalAliases: []AttributedAlias{{Value: "ESPN", Source: "dish-official-lineup", Method: "exact provider identity"}},
		CategoryHint:    &AttributedCategory{Value: "Sports", Source: "dish-official-lineup", Label: "DISH official lineup", Method: "provider category Sports"},
	}}
	draft, err := service.Build(context.Background(), lineup, inputs)
	if err != nil {
		t.Fatal(err)
	}
	dishRows := 0
	for _, status := range draft.Sources {
		if sourceStatusFamily(status.ID) != "provider:dish" {
			continue
		}
		dishRows++
		if status.ID != "dish-official-lineup" || status.Matched != 1 || len(status.RelatedIDs) != 2 || len(status.Matches) != 1 {
			t.Fatalf("consolidated DISH source = %+v", status)
		}
		match := status.Matches[0]
		if match.Number != "570" || !contains(match.Aliases, "ESPN") || match.Category != "Sports" {
			t.Fatalf("DISH matched evidence = %+v", match)
		}
	}
	if dishRows != 1 {
		t.Fatalf("DISH source rows = %d; sources = %+v", dishRows, draft.Sources)
	}
}

func TestDraftCategoryCountsIncludeOnlyIncludedChannels(t *testing.T) {
	service := newTestService(t, "", "")
	news := "News"
	excluded := false
	if err := service.UpdateChannel("source-one", "excluded-uncategorized", ChannelUpdate{Included: &excluded}); err != nil {
		t.Fatal(err)
	}
	if err := service.UpdateChannel("source-one", "included-categorized", ChannelUpdate{Category: &news}); err != nil {
		t.Fatal(err)
	}
	if err := service.UpdateChannel("source-one", "excluded-categorized", ChannelUpdate{Included: &excluded, Category: &news}); err != nil {
		t.Fatal(err)
	}
	draft, err := service.Build(context.Background(), testContext("source-one"), []InputChannel{
		{Key: "included-uncategorized", Number: "1", CallSign: "ONE"},
		{Key: "excluded-uncategorized", Number: "2", CallSign: "TWO"},
		{Key: "included-categorized", Number: "3", CallSign: "THREE"},
		{Key: "excluded-categorized", Number: "4", CallSign: "FOUR"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Included != 2 || draft.Excluded != 2 || draft.Categorized != 1 || draft.Uncategorized != 1 {
		t.Fatalf("included category counts = included %d excluded %d categorized %d uncategorized %d", draft.Included, draft.Excluded, draft.Categorized, draft.Uncategorized)
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
	if espn.NameMethod != "exact catalog identity" || espn.CategoryMethod != channelcategory.MaintainedIdentityMethod || !evidenceHasMethod(espn.EPGIDEvidence, "20001", "curated catalog EPG ID") {
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
	if catalogChannel.Category != "Sports" || catalogChannel.CategoryPriority != channelcategory.MaintainedIdentityPriority || catalogChannel.CategoryMethod != channelcategory.MaintainedIdentityMethod {
		t.Fatalf("catalog category = %+v", catalogChannel)
	}
	scheduleChannel := channelByID(t, draft, "schedule")
	if scheduleChannel.Category != "Movies" || scheduleChannel.CategorySource != "gracenote-schedule" || !strings.Contains(scheduleChannel.CategoryMethod, "90%") {
		t.Fatalf("schedule category = %+v", scheduleChannel)
	}
}

func TestBuildMaintainedIdentityOutranksScheduleInference(t *testing.T) {
	service := newTestService(t, "", "")
	inputs := []InputChannel{
		{
			Key: "freeform", CallSign: "FREEFRM",
			CategoryHint: &AttributedCategory{Priority: 3, Value: "Movies", Source: "gracenote-schedule", Label: "Weekday schedule inference", Method: "55% movie airtime"},
		},
		{
			Key: "adult", CallSign: "CHSTLRH",
			ExternalAliases: []AttributedAlias{{Value: "Hustler HD (Comcast)", Source: "xfinity-official-lineup", Method: "exact provider identity"}},
			CategoryHint:    &AttributedCategory{Priority: 3, Value: "Movies", Source: "gracenote-schedule", Label: "Weekday schedule inference", Method: "100% movie airtime"},
		},
		{
			Key: "international", CallSign: "VMEKIDS",
			CategoryHint: &AttributedCategory{Priority: 3, Value: "Kids & Family", Source: "gracenote-schedule", Label: "Weekday schedule inference", Method: "82% family airtime"},
		},
	}
	draft, err := service.Build(context.Background(), testContext("source-one"), inputs)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"freeform": "Entertainment", "adult": "Other", "international": "International"} {
		channel := channelByID(t, draft, id)
		if channel.Category != want || channel.CategoryPriority != 1 || channel.NeedsCategoryReview {
			t.Errorf("%s category = %+v; want %q priority 1 without review", id, channel, want)
		}
	}
	if got := channelByID(t, draft, "adult"); got.CategorySource != "gracenote" {
		t.Fatalf("adult category source = %q, want exact callsign source", got.CategorySource)
	}
}

func TestManualCategoryOverridesMaintainedIdentity(t *testing.T) {
	service := newTestService(t, "", "")
	lineup := testContext("source-one")
	category := "News & Weather"
	if err := service.UpdateChannel(lineup.SourceFingerprint, "usa", ChannelUpdate{Category: &category}); err != nil {
		t.Fatal(err)
	}
	draft, err := service.Build(context.Background(), lineup, []InputChannel{{Key: "usa", CallSign: "USAHD"}})
	if err != nil {
		t.Fatal(err)
	}
	channel := channelByID(t, draft, "usa")
	if channel.Category != "News & Weather" || channel.CategorySource != "user" || channel.CategoryPriority != 1 {
		t.Fatalf("manual category did not override maintained identity: %+v", channel)
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
	if example.Name != "Example Network" || example.Category != "Entertainment" {
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
		"animation": "Kids & Family", "auto": "Entertainment", "business": "News & Weather",
		"culture": "Entertainment", "family": "Kids & Family", "interactive": "Other",
		"series": "Entertainment", "shop": "Other", "xxx": "Other",
	}
	for input, want := range tests {
		if got := mapIPTVOrgCategory(input); got != want {
			t.Errorf("mapIPTVOrgCategory(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestProviderCategoryFuzzyMatchingIsAuditableAndConservative(t *testing.T) {
	catalog := `{"package":"Provider categories","categories":{"documentry":[{"name":"DOCS"}],"International Sports":[{"name":"AMB"}]}}`
	service := newTestService(t, catalog, "")
	draft, err := service.Build(context.Background(), testContext("source-one"), []InputChannel{
		{Key: "fuzzy", CallSign: "DOCS"},
		{Key: "ambiguous", CallSign: "AMB"},
	})
	if err != nil {
		t.Fatal(err)
	}
	fuzzy := channelByID(t, draft, "fuzzy")
	if fuzzy.Category != "Entertainment" || !strings.Contains(fuzzy.CategoryMethod, "fuzzy category alias") || !strings.Contains(fuzzy.CategoryMethod, "documentary") {
		t.Fatalf("fuzzy category = %+v", fuzzy)
	}
	if ambiguous := channelByID(t, draft, "ambiguous"); ambiguous.Category != uncategorized {
		t.Fatalf("ambiguous category was applied = %+v", ambiguous)
	}
}

func TestDraftUsesOnlyMasterCategoriesAndRejectsUnknownEdits(t *testing.T) {
	service := newTestService(t, "", "")
	draft, err := service.Build(context.Background(), testContext("source-one"), []InputChannel{{Key: "one", CallSign: "ONE"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Local & Public", "News & Weather", "Sports", "Movies", "Entertainment", "Kids & Family", "Music", "Faith", "International", "PPV & Events", "Other", "Uncategorized"}
	if len(draft.Categories) != len(want) {
		t.Fatalf("draft categories = %v", draft.Categories)
	}
	for index := range want {
		if draft.Categories[index] != want[index] {
			t.Fatalf("draft categories = %v", draft.Categories)
		}
	}
	unknown := "A New Category"
	if err := service.UpdateChannel("source-one", "one", ChannelUpdate{Category: &unknown}); err == nil {
		t.Fatal("unknown category edit was accepted")
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

func TestBuildCategorizesExactMaintainedAndLocalIdentities(t *testing.T) {
	service := newTestService(t, "", "")
	draft, err := service.Build(context.Background(), testContext("source-one"), []InputChannel{
		{Key: "peg", Number: "24", CallSign: "PEG024"},
		{Key: "pbs", Number: "23", CallSign: "WNJN", Affiliate: "PUBLIC BROADCASTING SERVICE"},
		{Key: "cable", Number: "46", CallSign: "AETVHD"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"peg", "pbs"} {
		channel := channelByID(t, draft, id)
		if channel.Category != "Local & Public" || channel.CategorySource != "gracenote" {
			t.Fatalf("local channel %s = %+v", id, channel)
		}
	}
	if channel := channelByID(t, draft, "cable"); channel.Category != "Entertainment" || channel.CategoryPriority != 1 {
		t.Fatalf("maintained cable identity was not classified = %+v", channel)
	}
}

func TestRemoveSuggestedDuplicateIDsAppliesOnlyReviewedSelections(t *testing.T) {
	service := newTestService(t, "", "")
	draft := &Draft{DuplicateSuggestions: []DuplicateSuggestion{
		{RemoveID: "sd-one", KeepID: "hd-one"},
		{RemoveID: "sd-two", KeepID: "hd-two"},
	}}
	if err := service.RemoveSuggestedDuplicateIDs("source-one", draft, []string{"sd-two"}); err != nil {
		t.Fatalf("RemoveSuggestedDuplicateIDs() error = %v", err)
	}
	overrides := service.store.Snapshot("source-one")
	if len(overrides) != 1 || overrides["sd-two"].Included == nil || *overrides["sd-two"].Included {
		t.Fatalf("selective duplicate overrides = %+v", overrides)
	}
	if _, ok := overrides["sd-one"]; ok {
		t.Fatalf("unselected duplicate was removed = %+v", overrides)
	}
	if err := service.RemoveSuggestedDuplicateIDs("source-one", draft, []string{"not-a-suggestion"}); err == nil {
		t.Fatal("unknown duplicate suggestion was accepted")
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
		{ID: "sd", Number: "2", Name: "Example", OriginalName: "EXAMPLEA", CallSign: "EXAMPLEA", NameSource: "source one", MatchedSources: []string{"gracenote", "source-one"}},
		{ID: "hd", Number: "502", Name: "Example", OriginalName: "EXAMPLEBHD", CallSign: "EXAMPLEBHD", NameSource: "source two", MatchedSources: []string{"gracenote", "source-two"}},
	})
	if len(suggestions) != 0 {
		t.Fatalf("cross-source duplicate suggestion = %+v", suggestions)
	}
}

func TestDuplicateSuggestionRecognizesExactQualitySuffixWithGracenoteNames(t *testing.T) {
	suggestions := findDuplicateSuggestions([]DraftChannel{
		{ID: "vice-sd", Number: "161", Name: "VICE", OriginalName: "VICE", CallSign: "VICE", NameSource: "gracenote", MatchedSources: []string{"gracenote"}},
		{ID: "vice-hd", Number: "661", Name: "VICEHD", OriginalName: "VICEHD", CallSign: "VICEHD", NameSource: "gracenote", MatchedSources: []string{"gracenote"}},
		{ID: "reelz-sd", Number: "128", Name: "REELZ", OriginalName: "REELZ", CallSign: "REELZ", NameSource: "gracenote", MatchedSources: []string{"gracenote"}},
		{ID: "reelz-hd", Number: "628", Name: "REELZHD", OriginalName: "REELZHD", CallSign: "REELZHD", NameSource: "gracenote", MatchedSources: []string{"gracenote"}},
	})
	if len(suggestions) != 2 {
		t.Fatalf("quality-suffix duplicate suggestions = %+v", suggestions)
	}
	want := map[string]string{"reelz-sd": "reelz-hd", "vice-sd": "vice-hd"}
	for _, suggestion := range suggestions {
		if want[suggestion.RemoveID] != suggestion.KeepID {
			t.Fatalf("quality-suffix duplicate suggestion = %+v", suggestion)
		}
		if !strings.Contains(suggestion.Reason, "quality suffix") {
			t.Fatalf("quality-suffix duplicate reason = %q", suggestion.Reason)
		}
	}
}

func TestDuplicateSuggestionRecognizesTerminalDigitalCallsignWithoutCatalog(t *testing.T) {
	suggestions := findDuplicateSuggestions([]DraftChannel{
		{ID: "wcbs-sd", Number: "2", Name: "WCBS", OriginalName: "WCBS", CallSign: "WCBS", NameSource: "gracenote", MatchedSources: []string{"gracenote"}},
		{ID: "wcbs-hd", Number: "502", Name: "WCBSDT", OriginalName: "WCBSDT", CallSign: "WCBSDT", NameSource: "gracenote", MatchedSources: []string{"gracenote"}},
	})
	if len(suggestions) != 1 || suggestions[0].RemoveID != "wcbs-sd" || suggestions[0].KeepID != "wcbs-hd" {
		t.Fatalf("terminal-DT duplicate suggestions = %+v", suggestions)
	}
	if !strings.Contains(suggestions[0].Reason, "HD/SD/DT quality suffix") {
		t.Fatalf("terminal-DT duplicate reason = %q", suggestions[0].Reason)
	}
}

func TestDuplicateSuggestionRecognizesSharedAttributedAliasWithExplicitSD(t *testing.T) {
	source := "gracenote-weekday-epg-usa-11743"
	suggestions := findDuplicateSuggestions([]DraftChannel{
		{
			ID: "newsnation-sd", Number: "82", Name: "NWSNTSD", OriginalName: "NWSNTSD", CallSign: "NWSNTSD", NameSource: "gracenote",
			AliasEvidence: []AliasEvidence{{Value: "NewsNation", Sources: []string{source}, Methods: []string{"pair-level identity"}}},
		},
		{
			ID: "newsnation", Number: "686", Name: "NEWSNTN", OriginalName: "NEWSNTN", CallSign: "NEWSNTN", NameSource: "gracenote",
			AliasEvidence: []AliasEvidence{{Value: "NewsNation", Sources: []string{source}, Methods: []string{"pair-level identity"}}},
		},
	})
	if len(suggestions) != 1 || suggestions[0].RemoveID != "newsnation-sd" || suggestions[0].KeepID != "newsnation" {
		t.Fatalf("attributed-alias duplicate suggestions = %+v", suggestions)
	}
	if !strings.Contains(suggestions[0].Reason, "explicitly SD") || !strings.Contains(suggestions[0].Reason, "NewsNation") {
		t.Fatalf("attributed-alias duplicate reason = %q", suggestions[0].Reason)
	}
}

func TestDuplicateSuggestionRecognizesAttributedAliasWithExplicitHD(t *testing.T) {
	suggestions := findDuplicateSuggestions([]DraftChannel{
		{
			ID: "i24-unmarked", Number: "14", Name: "I24NWEN", OriginalName: "I24NWEN", CallSign: "I24NWEN", NameSource: "gracenote",
			AliasEvidence: []AliasEvidence{{Value: "i24 News", Sources: []string{"directv-official-lineup"}, Methods: []string{"exact provider identity"}}},
		},
		{
			ID: "i24-hd", Number: "697", Name: "I24NEHD", OriginalName: "I24NEHD", CallSign: "I24NEHD", NameSource: "gracenote",
			AliasEvidence: []AliasEvidence{{Value: "i24NEWS", Sources: []string{"gracenote-weekday-epg-usa-11743"}, Methods: []string{"pair-level identity (identity-name:I24NEWS, provider-position:optimum|14)"}}},
		},
	})
	if len(suggestions) != 1 || suggestions[0].RemoveID != "i24-unmarked" || suggestions[0].KeepID != "i24-hd" {
		t.Fatalf("attributed-alias HD duplicate suggestions = %+v", suggestions)
	}
	if !strings.Contains(suggestions[0].Reason, "unique stronger quality rank") || !strings.Contains(suggestions[0].Reason, "i24 News") {
		t.Fatalf("attributed-alias HD duplicate reason = %q", suggestions[0].Reason)
	}
}

func TestSharedAliasDuplicateSuggestionRejectsWeakOrAmbiguousEvidence(t *testing.T) {
	tests := []struct {
		name     string
		channels []DraftChannel
	}{
		{
			name: "gracenote-only alias",
			channels: []DraftChannel{
				{ID: "sd", CallSign: "NWSNTSD", OriginalName: "NWSNTSD", AliasEvidence: []AliasEvidence{{Value: "NewsNation", Sources: []string{"gracenote"}}}},
				{ID: "other", CallSign: "NEWSNTN", OriginalName: "NEWSNTN", AliasEvidence: []AliasEvidence{{Value: "NewsNation", Sources: []string{"gracenote"}}}},
			},
		},
		{
			name: "only one position has attributable evidence",
			channels: []DraftChannel{
				{ID: "unmarked", CallSign: "I24NWEN", OriginalName: "I24NWEN", AliasEvidence: []AliasEvidence{{Value: "i24 News", Sources: []string{"gracenote"}}}},
				{ID: "hd", CallSign: "I24NEHD", OriginalName: "I24NEHD", AliasEvidence: []AliasEvidence{{Value: "i24NEWS", Sources: []string{"epg-confirmed"}}}},
			},
		},
		{
			name: "explicit SD aliases come from different sources",
			channels: []DraftChannel{
				{ID: "sd", CallSign: "NWSNTSD", OriginalName: "NWSNTSD", AliasEvidence: []AliasEvidence{{Value: "NewsNation", Sources: []string{"provider-source"}}}},
				{ID: "other", CallSign: "NEWSNTN", OriginalName: "NEWSNTN", AliasEvidence: []AliasEvidence{{Value: "NewsNation", Sources: []string{"epg-confirmed"}}}},
			},
		},
		{
			name: "unmarked and HD aliases have schedule evidence only",
			channels: []DraftChannel{
				{ID: "unmarked", Number: "714", CallSign: "SHOPLCH", OriginalName: "SHOPLCH", AliasEvidence: []AliasEvidence{{Value: "WRNNSD", Sources: []string{"gracenote-weekday-epg-usa-11743"}, Methods: []string{"pair-level identity (identity-name:SHOPLC)"}}}},
				{ID: "hd", Number: "785", CallSign: "WRNNDT", OriginalName: "WRNNDT", AliasEvidence: []AliasEvidence{{Value: "WRNNSD", Sources: []string{"gracenote-weekday-epg-usa-11743"}, Methods: []string{"pair-level identity (affiliate:SHOPLC, identity-name:WRNN, provider-position:optimum|48)"}}}},
			},
		},
		{
			name: "official alias and unlinked schedule alias",
			channels: []DraftChannel{
				{ID: "unmarked", Number: "1", CallSign: "IN2TV", OriginalName: "IN2TV", AliasEvidence: []AliasEvidence{{Value: "Cheddar News", Sources: []string{"optimum-official-lineup"}, Methods: []string{"exact provider channel number"}}}},
				{ID: "hd", Number: "100", CallSign: "CHDDRHD", OriginalName: "CHDDRHD", AliasEvidence: []AliasEvidence{{Value: "Cheddar News", Sources: []string{"gracenote-weekday-epg-usa-11743"}, Methods: []string{"pair-level identity (identity-name:CHDDR, provider-position:optimum|100)"}}}},
			},
		},
		{
			name: "multiple non-SD counterparts",
			channels: []DraftChannel{
				{ID: "sd", CallSign: "NWSNTSD", OriginalName: "NWSNTSD", AliasEvidence: []AliasEvidence{{Value: "NewsNation", Sources: []string{"epg-confirmed"}}}},
				{ID: "one", CallSign: "NEWSNTN", OriginalName: "NEWSNTN", AliasEvidence: []AliasEvidence{{Value: "NewsNation", Sources: []string{"epg-confirmed"}}}},
				{ID: "two", CallSign: "NEWSNTNALT", OriginalName: "NEWSNTNALT", AliasEvidence: []AliasEvidence{{Value: "NewsNation", Sources: []string{"epg-confirmed"}}}},
			},
		},
		{
			name: "competing counterparts across aliases",
			channels: []DraftChannel{
				{ID: "sd", CallSign: "EXAMPLESD", OriginalName: "EXAMPLESD", AliasEvidence: []AliasEvidence{{Value: "Example Network", Sources: []string{"provider-source"}}, {Value: "Example Alternate", Sources: []string{"provider-source"}}}},
				{ID: "one", CallSign: "EXAMPLE", OriginalName: "EXAMPLE", AliasEvidence: []AliasEvidence{{Value: "Example Network", Sources: []string{"provider-source"}}}},
				{ID: "two", CallSign: "EXAMPLEALT", OriginalName: "EXAMPLEALT", AliasEvidence: []AliasEvidence{{Value: "Example Alternate", Sources: []string{"provider-source"}}}},
			},
		},
		{
			name: "numbered digital subchannel",
			channels: []DraftChannel{
				{ID: "sd", CallSign: "WABCSD", OriginalName: "WABCSD", AliasEvidence: []AliasEvidence{{Value: "ABC New York", Sources: []string{"provider-source"}}}},
				{ID: "subchannel", CallSign: "WABCDT2", OriginalName: "WABCDT2", AliasEvidence: []AliasEvidence{{Value: "ABC New York", Sources: []string{"provider-source"}}}},
			},
		},
		{
			name: "no explicit quality marker",
			channels: []DraftChannel{
				{ID: "one", CallSign: "NEWSNTN", OriginalName: "NEWSNTN", AliasEvidence: []AliasEvidence{{Value: "NewsNation", Sources: []string{"provider-source"}}}},
				{ID: "two", CallSign: "NWSNT", OriginalName: "NWSNT", AliasEvidence: []AliasEvidence{{Value: "NewsNation", Sources: []string{"provider-source"}}}},
			},
		},
		{
			name: "equal explicit quality ranks",
			channels: []DraftChannel{
				{ID: "one", CallSign: "I24NEHD", OriginalName: "I24NEHD", AliasEvidence: []AliasEvidence{{Value: "i24 News", Sources: []string{"provider-source"}}}},
				{ID: "two", CallSign: "I24NEWSHD", OriginalName: "I24NEWSHD", AliasEvidence: []AliasEvidence{{Value: "i24NEWS", Sources: []string{"epg-confirmed"}}}},
			},
		},
		{
			name: "natural callsign ending in SD",
			channels: []DraftChannel{
				{ID: "kusd", CallSign: "KUSD", OriginalName: "KUSD", AliasEvidence: []AliasEvidence{{Value: "South Dakota PBS", Sources: []string{"provider-source"}}}},
				{ID: "peer", CallSign: "SDPBS", OriginalName: "SDPBS", AliasEvidence: []AliasEvidence{{Value: "South Dakota PBS", Sources: []string{"provider-source"}}}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if suggestions := findDuplicateSuggestions(test.channels); len(suggestions) != 0 {
				t.Fatalf("duplicate suggestions = %+v", suggestions)
			}
		})
	}
}

func TestQualitySuffixDuplicateSuggestionPreservesSubchannelsAndAmbiguity(t *testing.T) {
	tests := []struct {
		name     string
		channels []DraftChannel
	}{
		{
			name: "digital subchannel suffix",
			channels: []DraftChannel{
				{ID: "main", Number: "7", CallSign: "WABC", NameSource: "gracenote"},
				{ID: "subchannel", Number: "7.2", CallSign: "WABCDT2", NameSource: "gracenote"},
				{ID: "subchannel-three", Number: "7.3", CallSign: "WABCDT3", NameSource: "gracenote"},
			},
		},
		{
			name: "short base",
			channels: []DraftChannel{
				{ID: "short", Number: "1", CallSign: "MT", NameSource: "gracenote"},
				{ID: "short-hd", Number: "501", CallSign: "MTHD", NameSource: "gracenote"},
			},
		},
		{
			name: "equal strongest variants",
			channels: []DraftChannel{
				{ID: "base", Number: "161", CallSign: "VICE", NameSource: "gracenote"},
				{ID: "hd-one", Number: "661", CallSign: "VICEHD", NameSource: "gracenote"},
				{ID: "hd-two", Number: "1661", CallSign: "VICE HD", NameSource: "gracenote"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if suggestions := findDuplicateSuggestions(test.channels); len(suggestions) != 0 {
				t.Fatalf("duplicate suggestions = %+v", suggestions)
			}
		})
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
	if channel := channelByID(t, draft, "one"); channel.Included || channel.Category != "News & Weather" {
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
		TVGID: "OneNetwork.us", Score: 92, NameScore: 92, Reason: "Fuzzy name 92%",
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

func TestDispatcharrExactThresholdControlsAliasesAndExclusions(t *testing.T) {
	service := newTestService(t, "", "")
	lineup := testContext("source-one")
	decisions := []MatchDecision{
		{Key: "confirmed-high", Decision: "confirmed", DispatcharrFingerprint: "dispatcharr-source", StreamFingerprint: "stream-1", StreamKey: "3:1", StreamID: 1, M3UAccountID: 3, ChannelID: "confirmed-high", StreamName: "US| REELZ HD", NormalizedAlias: "reelz", Score: 95, NameScore: 95, Reason: "Fuzzy name 95%"},
		{Key: "confirmed-low", Decision: "confirmed", DispatcharrFingerprint: "dispatcharr-source", StreamFingerprint: "stream-2", StreamKey: "3:2", StreamID: 2, M3UAccountID: 3, ChannelID: "confirmed-low", StreamName: "Provider Movie Network", NormalizedAlias: "providermovienetwork", Score: 92, NameScore: 92, Reason: "Fuzzy name 92%"},
		{Key: "confirmed-epg", Decision: "confirmed", DispatcharrFingerprint: "dispatcharr-source", StreamFingerprint: "stream-3", StreamKey: "3:3", StreamID: 3, M3UAccountID: 3, ChannelID: "confirmed-epg", StreamName: "Unhelpful provider label", NormalizedAlias: "unhelpfulproviderlabel", Score: 100, NameScore: 31, Reason: "Exact EPG ID"},
		{Key: "denied-high-a", Decision: "denied", DispatcharrFingerprint: "dispatcharr-source", StreamFingerprint: "stream-4", StreamKey: "3:4", StreamID: 4, M3UAccountID: 3, ChannelID: "denied-high", StreamName: "US-ReelzChannel", NormalizedAlias: "reelzchannel", Score: 95, NameScore: 95, Reason: "Fuzzy name 95%"},
		{Key: "denied-high-b", Decision: "denied", DispatcharrFingerprint: "dispatcharr-source", StreamFingerprint: "stream-5", StreamKey: "7:5", StreamID: 5, M3UAccountID: 7, ChannelID: "denied-high", StreamName: "GO| Reelz Channel HD", NormalizedAlias: "reelzchannel", Score: 94, NameScore: 94, Reason: "Fuzzy name 94%"},
		{Key: "denied-low", Decision: "denied", DispatcharrFingerprint: "dispatcharr-source", StreamFingerprint: "stream-6", StreamKey: "3:6", StreamID: 6, M3UAccountID: 3, ChannelID: "denied-low", StreamName: "Similar but safe", NormalizedAlias: "similarbutsafe", Score: 94, NameScore: 94, Reason: "Fuzzy name 94%"},
	}
	if err := service.SetMatchDecisions(lineup.SourceFingerprint, decisions); err != nil {
		t.Fatal(err)
	}
	inputs := []InputChannel{
		{Key: "confirmed-high", StationID: "1", Number: "1", CallSign: "REELZ"},
		{Key: "confirmed-low", StationID: "2", Number: "2", CallSign: "MOVIES"},
		{Key: "confirmed-epg", StationID: "3", Number: "3", CallSign: "EPGONLY"},
		{Key: "denied-high", StationID: "4", Number: "4", CallSign: "REELZALT"},
		{Key: "denied-low", StationID: "5", Number: "5", CallSign: "SAFE"},
	}
	draft, err := service.Build(context.Background(), lineup, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if channel := channelByID(t, draft, "confirmed-high"); contains(channel.Aliases, "US| REELZ HD") {
		t.Fatalf("95%%+ confirmation was exported as an alias: %+v", channel)
	}
	if channel := channelByID(t, draft, "confirmed-low"); !contains(channel.Aliases, "Provider Movie Network") {
		t.Fatalf("sub-95%% confirmation was not exported as an alias: %+v", channel)
	}
	if channel := channelByID(t, draft, "confirmed-epg"); !contains(channel.Aliases, "Unhelpful provider label") {
		t.Fatalf("EPG-only confirmation lost its required name alias: %+v", channel)
	}
	deniedHigh := channelByID(t, draft, "denied-high")
	if len(deniedHigh.ExcludedAliases) != 1 || deniedHigh.ExcludedAliases[0] != "US-ReelzChannel" {
		t.Fatalf("high-score denial exclusions = %+v", deniedHigh.ExcludedAliases)
	}
	if channel := channelByID(t, draft, "denied-low"); len(channel.ExcludedAliases) != 0 {
		t.Fatalf("sub-95%% denial was exported: %+v", channel.ExcludedAliases)
	}

	export := ExportFromDraft(draft)
	var exported ExportChannel
	for _, channels := range export.Categories {
		for _, channel := range channels {
			if channel.Name == deniedHigh.Name {
				exported = channel
			}
		}
	}
	if len(exported.ExcludedAliases) != 1 || exported.ExcludedAliases[0] != "US-ReelzChannel" || !strings.Contains(export.Notes, "Exact match sensitivity") {
		t.Fatalf("threshold export = %+v notes=%q", exported, export.Notes)
	}
	encoded, err := json.Marshal(exported)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"excluded_aliases":["US-ReelzChannel"]`) || strings.Contains(string(encoded), "excludedAliases") {
		t.Fatalf("excluded alias JSON contract = %s", encoded)
	}
}

func TestLegacyDecisionNameScoreIsConservative(t *testing.T) {
	if got := decisionNameScore(MatchDecision{Score: 100, Reason: "Exact EPG ID"}); got != 0 {
		t.Fatalf("EPG-only legacy name score = %d", got)
	}
	if got := decisionNameScore(MatchDecision{Score: 96, Reason: "Fuzzy name 92% + channel number"}); got != 92 {
		t.Fatalf("channel-number legacy name score = %d", got)
	}
	if got := decisionNameScore(MatchDecision{Score: 98, Reason: "Exact normalized name or alias"}); got != 98 {
		t.Fatalf("name-based legacy score = %d", got)
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
	if len(export.Categories) != 1 || len(export.Categories["Local & Public"]) != 1 {
		t.Fatalf("export categories = %+v", export.Categories)
	}
	entry := export.Categories["Local & Public"][0]
	if entry.Name != "KEEP" || entry.Number != 5.1 || !contains(entry.EPGIDs, "1") {
		t.Fatalf("export entry = %+v", entry)
	}
	encoded, err := json.Marshal(export)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	entryJSON, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal(entry) error = %v", err)
	}
	if strings.Contains(string(entryJSON), "excluded_aliases") {
		t.Fatalf("empty excluded_aliases should be omitted: %s", entryJSON)
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
