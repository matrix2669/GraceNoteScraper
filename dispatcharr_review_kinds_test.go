package main

import (
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"testing"
)

func TestReviewKindsKeepPerConstituentThresholdAndFeedException(t *testing.T) {
	candidates := []dispatcharr.Candidate{
		{Key: "low", ChannelID: "a", NormalizedAlias: "network", Score: 94, StreamName: "Network HD"},
		{Key: "exact", ChannelID: "a", NormalizedAlias: "network", Score: 95, StreamName: "Network SD"},
		{Key: "perfect", ChannelID: "a", NormalizedAlias: "network", Score: 100, StreamName: "Network UHD"},
		{Key: "pending", ChannelID: "a", NormalizedAlias: "network", Score: 100, StreamName: "Network"},
	}
	classifyReviewCandidates(candidates, automaticMatchView{Rows: []automaticMatchRow{{Key: "pending", PendingReview: true}}})
	want := []string{"inclusion", "qualified", "qualified", "feed-review"}
	for i, c := range candidates {
		if c.ReviewKind != want[i] {
			t.Fatalf("%s: %s", c.Key, c.ReviewKind)
		}
	}
	groups := dispatcharr.GroupCandidates(candidates)
	if len(groups) != 3 {
		t.Fatalf("mixed threshold classes combined: %+v", groups)
	}
	// A saved low-score inclusion must not make a different qualified or feed-review
	// constituent disappear merely because normalization produced the same alias.
	d := dispatcharr.Decision{ChannelID: "a", NormalizedAlias: "network", ReviewKind: "inclusion", Decision: "confirmed", StreamName: "Network HD"}
	for i := range candidates {
		candidates[i].StreamHash = candidates[i].Key
	}
	d.StreamHash = "low"
	remaining := dispatcharr.FilterSnapshotCandidates(candidates, map[string]bool{"a": true}, map[string]dispatcharr.Decision{"low": d})
	if len(remaining) != 3 {
		t.Fatalf("decision suppressed other classes: %+v", remaining)
	}
}
