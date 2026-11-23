package lineuparr

import (
	"path/filepath"
	"testing"
)

func TestCategoryReviewRetainsOriginalProposal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := LoadStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	channel := DraftChannel{Category: "Movies", CategorySource: "gracenote-schedule", CategoryPriority: 3, NeedsCategoryReview: true}
	chosen := "Entertainment"
	if err = store.Update("provider", "channel", ChannelUpdate{Category: &chosen, Review: ReviewCategory(channel, chosen)}); err != nil {
		t.Fatal(err)
	}
	store, err = LoadStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got := store.Snapshot("provider")["channel"]
	if got.CategoryReview == nil || got.CategoryReview.Proposed != "Movies" || got.CategoryReview.Chosen != "Entertainment" {
		t.Fatal(got)
	}
	chosen = "Other"
	if err = store.Update("provider", "channel", ChannelUpdate{Category: &chosen}); err != nil {
		t.Fatal(err)
	}
	got = store.Snapshot("provider")["channel"]
	if got.CategoryReview.Proposed != "Movies" || got.CategoryReview.Chosen != "Other" {
		t.Fatal(got)
	}
	got.CategoryReview.Proposed = "changed copy"
	if store.Snapshot("provider")["channel"].CategoryReview.Proposed != "Movies" {
		t.Fatal("review snapshot leaked mutable pointer")
	}
}
