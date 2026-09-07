package lineuparr

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateStorePersistsSecurely(t *testing.T) {
	path := filepath.Join(t.TempDir(), "builder", "state.json")
	store, err := LoadStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	included := false
	category := "Sports"
	if err := store.Update("fingerprint", "channel", ChannelUpdate{Included: &included, Category: &category}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("state mode = %o, want 600", got)
	}
	reloaded, err := LoadStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	override := reloaded.Snapshot("fingerprint")["channel"]
	if override.Included == nil || *override.Included || override.Category != "Sports" {
		t.Fatalf("reloaded override = %+v", override)
	}
	if got := reloaded.Snapshot("another"); len(got) != 0 {
		t.Fatalf("state leaked to another fingerprint: %+v", got)
	}
}

func TestWorkflowProgressPersistsAndResetsWithSourceAndCustomizationChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "builder", "state.json")
	store, err := LoadStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, time.September, 7, 15, 4, 5, 0, time.UTC)
	if err := store.SetTMDBDisposition("source-a", "skipped", when); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteCustomization("source-a", "signature-a", when); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	progress := reloaded.WorkflowProgress("source-a")
	if progress.TMDBDisposition != "skipped" || progress.CustomizationSignature != "signature-a" || !progress.TMDBCompletedAt.Equal(when) {
		t.Fatalf("persisted workflow = %+v", progress)
	}
	category := "Sports"
	if err := reloaded.Update("source-a", "channel-a", ChannelUpdate{Category: &category}); err != nil {
		t.Fatal(err)
	}
	progress = reloaded.WorkflowProgress("source-a")
	if progress.TMDBDisposition != "skipped" || progress.CustomizationSignature != "" || !progress.CustomizationCompletedAt.IsZero() {
		t.Fatalf("customization change reset the wrong progress = %+v", progress)
	}
	if progress := reloaded.WorkflowProgress("source-b"); progress != (WorkflowProgress{}) {
		t.Fatalf("workflow leaked across sources = %+v", progress)
	}
	if err := reloaded.SetTMDBDisposition("source-b", "complete", when); err != nil {
		t.Fatal(err)
	}
	if progress := reloaded.WorkflowProgress("source-a"); progress != (WorkflowProgress{}) {
		t.Fatalf("old source survived provider change = %+v", progress)
	}
}

func TestWorkflowProgressRejectsInvalidValues(t *testing.T) {
	store, err := LoadStateStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetTMDBDisposition("source", "later", time.Now()); err == nil {
		t.Fatal("invalid TMDB disposition was accepted")
	}
	if err := store.CompleteCustomization("source", "", time.Now()); err == nil {
		t.Fatal("empty customization signature was accepted")
	}
}
