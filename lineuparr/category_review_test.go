package lineuparr

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestBuildKeepsProvisionalWinnerVisibleWhenProviderConflictNeedsReview(t *testing.T) {
	store, err := LoadStateStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, ServiceOptions{})
	draft, err := service.Build(context.Background(), LineupContext{SourceFingerprint: "source", ProviderName: "Test"}, []InputChannel{{
		Key: "unknown", CallSign: "UNKNOWN", CategoryConflict: true,
		CategoryHint: &AttributedCategory{Value: "News & Weather", Source: "provider", Method: "provider counts: News & Weather = 2; category review: disagreement"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Channels) != 1 || draft.Channels[0].Category != "News & Weather" || !draft.Channels[0].NeedsCategoryReview {
		t.Fatalf("provisional category = %+v", draft.Channels)
	}
}

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
	choices := []CategoryReviewChoice{{Channel: rows[0], Category: "Entertainment"}, {Channel: rows[1], Category: "Sports"}}
	bad := append([]CategoryReviewChoice(nil), choices...)
	bad[1].Channel.Included = false
	if err := service.ApproveReviewedCategories("test", bad); err == nil {
		t.Fatal("invalid batch accepted")
	}
	if len(store.Snapshot("test")) != 0 {
		t.Fatal("partial batch persisted")
	}
	invalidCategory := append([]CategoryReviewChoice(nil), choices...)
	invalidCategory[1].Category = "Not a master category"
	if err := service.ApproveReviewedCategories("test", invalidCategory); err == nil {
		t.Fatal("invalid category accepted")
	}
	if len(store.Snapshot("test")) != 0 {
		t.Fatal("invalid category partially persisted")
	}
	if err := service.CompleteCustomization("test", "customized", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := service.ApproveReviewedCategories("test", choices); err != nil {
		t.Fatal(err)
	}
	if progress := service.WorkflowProgress("test"); progress.CustomizationSignature != "" {
		t.Fatalf("category approval did not invalidate customization: %+v", progress)
	}
	reopened, err := LoadStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.Snapshot("test")
	if got["a"].Category != "Entertainment" || got["a"].CategoryReview.Proposed != "Movies" || got["a"].CategoryReview.Chosen != "Entertainment" || got["b"].Category != "Sports" || got["b"].CategoryReview.Proposed != "Sports" || got["b"].CategoryReview.Chosen != "Sports" {
		t.Fatal(got)
	}
	if err := service.ApproveReviewedCategories("test", choices); err == nil {
		t.Fatal("stale batch overwrote manual choices")
	}
}
