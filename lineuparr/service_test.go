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
	if channel := channelByID(t, draft, "one"); channel.Included || channel.Category != "News" {
		t.Fatalf("source-one override = %+v", channel)
	}
	draft, _ = service.Build(context.Background(), testContext("source-two"), inputs)
	if channel := channelByID(t, draft, "one"); !channel.Included || channel.Category != uncategorized {
		t.Fatalf("old override leaked into source-two: %+v", channel)
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
