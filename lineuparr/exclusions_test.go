package lineuparr

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
)

func TestDeniedFullNamesPersistenceScopingAndUndo(t *testing.T) {
	service := newTestService(t, "", "")
	lineup := testContext("source")
	inputs := []InputChannel{{Key: "one", StationID: "1", CallSign: "ONE"}, {Key: "two", StationID: "2", CallSign: "TWO"}}
	names := []string{"US-ReelzChannel", "GO| Reelz Channel HD", "Reelz Channel", "ReelzChannel", "US: Reelz Channel 4K", "US-ReelzChannel", " us-reelzchannel ", " GO|\tReelz   Channel HD ", "M*A*S*H", `US\M*A*S\*H`, "Example ? [HD]!", "null"}
	decisions := make([]MatchDecision, 0, len(names))
	for i, name := range names {
		key := fmt.Sprintf("%02d", i)
		decisions = append(decisions, MatchDecision{Key: key, Decision: "denied", DispatcharrFingerprint: "dispatch", StreamFingerprint: key, StreamKey: key, ChannelID: "one", StreamName: name, NormalizedAlias: "same-review-group", Score: 100, NameScore: 95})
	}
	if err := service.SetMatchDecisions(lineup.SourceFingerprint, decisions); err != nil {
		t.Fatal(err)
	}
	store, err := LoadStateStore(service.store.path)
	if err != nil {
		t.Fatal(err)
	}
	service = NewService(store, ServiceOptions{})
	stored := service.MatchDecisions(lineup.SourceFingerprint)
	if len(stored) != len(names) {
		t.Fatalf("lost constituents: %d", len(stored))
	}
	for _, d := range decisions {
		if stored[d.Key].StreamName != cleanText(d.StreamName) || stored[d.Key].NameScore != 95 {
			t.Fatalf("changed decision: %+v", stored[d.Key])
		}
	}
	build := func(source LineupContext) *Draft {
		t.Helper()
		draft, err := service.Build(context.Background(), source, inputs)
		if err != nil {
			t.Fatal(err)
		}
		return draft
	}
	want := []string{"US-ReelzChannel", "GO| Reelz Channel HD", "Reelz Channel", "ReelzChannel", "US: Reelz Channel 4K", "M*A*S*H", `US\M*A*S\*H`, "Example ? [HD]!", "null"}
	assertNames := func(got []string) {
		t.Helper()
		a, b := append([]string(nil), got...), append([]string(nil), want...)
		sort.Strings(a)
		sort.Strings(b)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("full exclusions = %q, want %q", a, b)
		}
	}
	draft := build(lineup)
	assertNames(channelByID(t, draft, "one").ExcludedAliases)
	if len(channelByID(t, draft, "two").ExcludedAliases) != 0 {
		t.Fatal("exclusions leaked to other channel")
	}
	if len(channelByID(t, build(testContext("different-source")), "one").ExcludedAliases) != 0 {
		t.Fatal("exclusions leaked to other lineup")
	}
	// Building another source does not discard the stored source's decisions.
	assertNames(channelByID(t, build(lineup), "one").ExcludedAliases)
	for _, status := range draft.Sources {
		if status.ID == "dispatcharr-denied" && status.Matched != len(want) {
			t.Fatalf("count is not distinct full names: %+v", status)
		}
	}
	keys := make([]string, 0, len(stored))
	for key := range stored {
		keys = append(keys, key)
	}
	if err := service.ClearMatchDecisions(lineup.SourceFingerprint, keys); err != nil {
		t.Fatal(err)
	}
	store, err = LoadStateStore(service.store.path)
	if err != nil {
		t.Fatal(err)
	}
	service = NewService(store, ServiceOptions{})
	if len(channelByID(t, build(lineup), "one").ExcludedAliases) != 0 {
		t.Fatal("undo failed after restart")
	}
}

func TestDenialEligibilityUsesEachIndependentNameScore(t *testing.T) {
	tests := []struct {
		name             string
		score, nameScore int
		reason           string
		want             bool
	}{
		{"below", 100, 94, "Exact EPG ID", false},
		{"boundary", 100, 95, "Exact EPG ID", true},
		{"above", 100, 96, "Exact EPG ID", true},
		{"zero EPG", 100, 0, "Exact EPG ID", false},
		{"legacy number below", 98, 0, "Fuzzy name 94% + channel number", false},
		{"legacy number boundary", 99, 0, "Fuzzy name 95% + channel number", true},
		{"legacy name", 98, 0, "Exact normalized name or alias", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newTestService(t, "", "")
			lineup := testContext("source")
			for i, d := range []MatchDecision{
				{StreamName: "US: Strong HD", NameScore: 100, Score: 100},
				{StreamName: tt.name, NameScore: tt.nameScore, Score: tt.score, Reason: tt.reason},
			} {
				d.Key = fmt.Sprint(i)
				d.Decision = "denied"
				d.DispatcharrFingerprint = "dispatch"
				d.StreamFingerprint = d.Key
				d.StreamKey = d.Key
				d.ChannelID = "one"
				d.NormalizedAlias = "same"
				if err := service.SetMatchDecision(lineup.SourceFingerprint, d); err != nil {
					t.Fatal(err)
				}
			}
			draft, err := service.Build(context.Background(), lineup, []InputChannel{{Key: "one", CallSign: "ONE"}})
			if err != nil {
				t.Fatal(err)
			}
			if got := contains(draft.Channels[0].ExcludedAliases, tt.name); got != tt.want {
				t.Fatalf("eligibility %v, want %v: %q", got, tt.want, draft.Channels[0].ExcludedAliases)
			}
		})
	}
}

func TestLiteralExclusionJSONRoundTrip(t *testing.T) {
	names := []string{"M*A*S*H", `US\M*A*S\*H`, `\`, "*", "**", `end\`, `A\\B`, "US: Example ? [HD]!", "A & B", "A &amp; B"}
	want := []string{`M\*A\*S\*H`, `US\\M\*A\*S\\\*H`, `\\`, `\*`, `\*\*`, `end\\`, `A\\\\B`, "US: Example ? [HD]!", "A & B", "A &amp; B"}
	draft := &Draft{Channels: []DraftChannel{{Name: "ONE", Included: true, Category: "Other", ExcludedAliases: append([]string(nil), names...)}}}
	for attempt := 0; attempt < 2; attempt++ {
		exported := ExportFromDraft(draft)
		got := exported.Categories["Other"][0].ExcludedAliases
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("escaped = %q want %q", got, want)
		}
		data, err := json.Marshal(exported)
		if err != nil {
			t.Fatal(err)
		}
		var decoded ExportFile
		if err = json.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(decoded.Categories["Other"][0].ExcludedAliases, want) {
			t.Fatalf("JSON round trip: %s", data)
		}
		var field map[string]json.RawMessage
		channelJSON, _ := json.Marshal(exported.Categories["Other"][0])
		if err = json.Unmarshal(channelJSON, &field); err != nil {
			t.Fatal(err)
		}
		if _, ok := field["excluded_aliases"]; !ok {
			t.Fatal("missing consumer field")
		}
		literalJSON, _ := json.Marshal(got[0])
		if string(literalJSON) != `"M\\*A\\*S\\*H"` {
			t.Fatalf("literal JSON = %s", literalJSON)
		}
	}
	if !reflect.DeepEqual(draft.Channels[0].ExcludedAliases, names) {
		t.Fatal("export mutated raw draft names")
	}
}

func TestConfirmedPositiveGroupingUnchanged(t *testing.T) {
	service := newTestService(t, "", "")
	lineup := testContext("source")
	for i, name := range []string{"US: Group Name HD", "Group Name", "GO| Group Name 4K"} {
		key := fmt.Sprint(i)
		if err := service.SetMatchDecision(lineup.SourceFingerprint, MatchDecision{Key: key, Decision: "confirmed", DispatcharrFingerprint: "d", StreamFingerprint: key, StreamKey: key, ChannelID: "one", StreamName: name, NormalizedAlias: "groupname", Score: 94, NameScore: 94}); err != nil {
			t.Fatal(err)
		}
	}
	draft, err := service.Build(context.Background(), lineup, []InputChannel{{Key: "one", CallSign: "ONE"}})
	if err != nil {
		t.Fatal(err)
	}
	c := draft.Channels[0]
	if !contains(c.Aliases, "Group Name") || contains(c.Aliases, "US: Group Name HD") || contains(c.Aliases, "GO| Group Name 4K") || len(c.ExcludedAliases) > 0 {
		t.Fatalf("positive grouping changed: %+v", c)
	}
}
