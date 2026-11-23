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

func TestCategoryBatchRetainsReviewAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := LoadStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{store: store}
	rows := []DraftChannel{{ID: "one", Category: "Movies", CategorySource: "tmdb-schedule", CategoryPriority: 4, NeedsCategoryReview: true}, {ID: "two", Category: "Sports", CategorySource: "official", CategoryPriority: 2}}
	if err := service.UpdateReviewedCategories("provider", rows, "Entertainment"); err != nil {
		t.Fatal(err)
	}
	reopened, err := LoadStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.Snapshot("provider")
	if got["one"].CategoryReview == nil || got["one"].CategoryReview.Proposed != "Movies" || got["one"].CategoryReview.Chosen != "Entertainment" {
		t.Fatal(got)
	}
	if got["two"].CategoryReview != nil || got["two"].Category != "Entertainment" {
		t.Fatal(got)
	}
}

func TestApproveMixedCategoriesAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := LoadStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{store: store}
	rows := []DraftChannel{{ID: "a", Category: "Movies", Included: true, NeedsCategoryReview: true, CategoryPriority: 4}, {ID: "b", Category: "Sports", Included: true, NeedsCategoryReview: true, CategoryPriority: 3}}
	bad := append([]DraftChannel(nil), rows...)
	bad[1].Included = false
	if err := service.ApproveReviewedCategories("test", bad); err == nil {
		t.Fatal("invalid batch accepted")
	}
	if len(store.Snapshot("test")) != 0 {
		t.Fatal("partial batch persisted")
	}
	if err := service.ApproveReviewedCategories("test", rows); err != nil {
		t.Fatal(err)
	}
	reopened, err := LoadStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.Snapshot("test")
	if got["a"].Category != "Movies" || got["b"].Category != "Sports" || got["b"].CategoryReview.Proposed != "Sports" {
		t.Fatal(got)
	}
	if err := service.ApproveReviewedCategories("test", rows); err == nil {
		t.Fatal("stale batch overwrote manual choices")
	}
}
