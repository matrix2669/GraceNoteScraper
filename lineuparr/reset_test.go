package lineuparr

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBuilderResetBackupRestoreAndPreservedFiles(t *testing.T) {
	for _, withdraw := range []bool{false, true} {
		t.Run(fmt.Sprintf("withdraw-%v", withdraw), func(t *testing.T) { testBuilderResetBackupRestoreAndPreservedFiles(t, withdraw) })
	}
}

func testBuilderResetBackupRestoreAndPreservedFiles(t *testing.T, withdraw bool) {
	dir := t.TempDir()
	store, err := LoadStateStore(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(store, ServiceOptions{})
	yes := true
	if err = store.Update("source", "a", ChannelUpdate{Included: &yes}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"guide_cache.json", "tmdb_cache.json", "market-index.json", "config.json", "dispatcharr_config.json"} {
		if err = os.WriteFile(filepath.Join(dir, name), []byte(name+" unchanged"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	original, _ := os.ReadFile(store.path)
	token := svc.ReviewStateToken()
	backup, err := svc.ResetBuilder("source", token, "", withdraw)
	if err != nil {
		t.Fatal(err)
	}
	if svc.ResetGeneration() != 1 || len(store.Snapshot("source")) != 0 {
		t.Fatal("choices were not cleared")
	}
	if _, err = svc.ResetBuilder("source", token, "", withdraw); err != ErrStaleReview {
		t.Fatalf("stale reset accepted: %v", err)
	}
	for _, name := range []string{"guide_cache.json", "tmdb_cache.json", "market-index.json", "config.json", "dispatcharr_config.json"} {
		data, _ := os.ReadFile(filepath.Join(dir, name))
		if string(data) != name+" unchanged" {
			t.Fatalf("shared data changed: %s", name)
		}
	}
	store.mu.Lock()
	store.state.Workflow.TMDBDisposition = "skipped"
	store.state.Workflow.TMDBCompletedAt = time.Unix(1234, 0)
	store.mu.Unlock()
	if _, err = svc.ResetBuilder("source", svc.ReviewStateToken(), backup, withdraw); err != nil {
		t.Fatal(err)
	}
	if store.state.Workflow.TMDBDisposition != "skipped" || !store.state.Workflow.TMDBCompletedAt.Equal(time.Unix(1234, 0)) {
		t.Fatal("restore replaced current TMDB settings")
	}
	if svc.ResetGeneration() != 2 || len(store.Snapshot("source")) != 1 {
		t.Fatal("restore did not preserve choices and fence old clients")
	}
	info, _ := os.Stat(filepath.Join(dir, "builder-reset-backups", backup))
	if info.Mode().Perm() != 0600 {
		t.Fatal("backup permissions")
	}
	_ = original
	reloaded, err := LoadStateStore(store.path)
	if err != nil || reloaded.state.ResetGeneration != 2 {
		t.Fatalf("restart lost reset fence: %v", err)
	}
}

func TestBuilderResetBackupFailurePreservesState(t *testing.T) {
	dir := t.TempDir()
	store, _ := LoadStateStore(filepath.Join(dir, "state.json"))
	svc := NewService(store, ServiceOptions{})
	yes := true
	_ = store.Update("source", "a", ChannelUpdate{Included: &yes})
	before, _ := os.ReadFile(store.path)
	token := svc.ReviewStateToken()
	_ = os.WriteFile(filepath.Join(dir, "builder-reset-backups"), []byte("not a directory"), 0600)
	if _, err := svc.ResetBuilder("source", token, "", true); err == nil {
		t.Fatal("backup failure ignored")
	}
	after, _ := os.ReadFile(store.path)
	if string(before) != string(after) || token != svc.ReviewStateToken() {
		t.Fatal("failed reset changed retained or in-memory choices")
	}
}

func TestBuilderResetDoesNotPromoteOldSourceCategoryCache(t *testing.T) {
	store, _ := LoadStateStore(filepath.Join(t.TempDir(), "state.json"))
	svc := NewService(store, ServiceOptions{})
	yes := true
	_ = store.Update("old-source", "same-position", ChannelUpdate{Included: &yes})
	store.mu.Lock()
	store.state.TMDBCategoryScan = TMDBCategoryScan{Revision: "old-source-schedules"}
	store.state.Workflow.TMDBDisposition = "complete"
	store.mu.Unlock()
	if _, err := svc.ResetBuilder("new-source", svc.ReviewStateToken(), "", false); err != nil {
		t.Fatal(err)
	}
	if svc.TMDBCategoryScan("new-source").Revision != "" || store.state.Workflow.TMDBDisposition != "" {
		t.Fatal("old lineup-derived categories promoted to new source")
	}
}

func TestResetRecoverySurvivesRestartAndIsSourceScoped(t *testing.T) {
	dir := t.TempDir()
	store, _ := LoadStateStore(filepath.Join(dir, "state.json"))
	svc := NewService(store, ServiceOptions{})
	yes := true
	if err := store.Update("source", "a", ChannelUpdate{Included: &yes}); err != nil {
		t.Fatal(err)
	}
	backup, err := svc.ResetBuilder("source", svc.ReviewStateToken(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := LoadStateStore(store.path)
	if err != nil {
		t.Fatal(err)
	}
	recovered := NewService(restarted, ServiceOptions{})
	if got := recovered.LatestResetRecovery("source"); got == nil || got.ID != backup {
		t.Fatalf("missing durable recovery: %+v", got)
	}
	if got := recovered.LatestResetRecovery("other"); got != nil {
		t.Fatalf("cross-source recovery: %+v", got)
	}
	// Returning to the saved source must not copy categories from the intervening source.
	if err = restarted.Update("other", "b", ChannelUpdate{Included: &yes}); err != nil {
		t.Fatal(err)
	}
	restarted.state.TMDBCategoryScan = TMDBCategoryScan{Revision: "other-source"}
	restarted.state.Workflow.TMDBDisposition = "complete"
	if _, err = recovered.ResetBuilder("source", recovered.ReviewStateToken(), backup, false); err != nil {
		t.Fatal(err)
	}
	if restarted.state.TMDBCategoryScan.Revision != "" || restarted.state.Workflow.TMDBDisposition != "" {
		t.Fatal("restore promoted other-source enrichment")
	}
	if len(restarted.Snapshot("source")) != 1 {
		t.Fatal("original choices not restored")
	}
}

func TestResetSaveFailureRollsBackMemoryAndRetainsRecovery(t *testing.T) {
	dir := t.TempDir()
	store, _ := LoadStateStore(filepath.Join(dir, "state.json"))
	svc := NewService(store, ServiceOptions{})
	yes := true
	if err := store.Update("source", "a", ChannelUpdate{Included: &yes}); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(store.path)
	// An existing directory prevents the atomic final rename, after backup succeeds.
	if err := os.Remove(store.path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(store.path, 0700); err != nil {
		t.Fatal(err)
	}
	token := svc.ReviewStateToken()
	if _, err := svc.ResetBuilder("source", token, "", true); err == nil {
		t.Fatal("state write unexpectedly succeeded")
	}
	if svc.ReviewStateToken() != token || svc.ResetGeneration() != 0 || svc.PublicationGeneration() != 0 {
		t.Fatal("failed persistence altered in-memory state")
	}
	recovery := svc.LatestResetRecovery("source")
	if recovery == nil {
		t.Fatal("backup missing after failed rename")
	}
	bytes, err := os.ReadFile(filepath.Join(dir, "builder-reset-backups", recovery.ID))
	if err != nil || string(bytes) != string(original) {
		t.Fatal("recovery does not preserve exact original state")
	}
}

func TestPublicationWithdrawalSurvivesRestartAndSourceChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, _ := LoadStateStore(path)
	svc := NewService(store, ServiceOptions{})
	yes := true
	if err := store.Update("source", "a", ChannelUpdate{Included: &yes}); err != nil {
		t.Fatal(err)
	}
	backup, err := svc.ResetBuilder("source", svc.ReviewStateToken(), "", true)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := LoadStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	svc = NewService(restarted, ServiceOptions{})
	if svc.PublicationGeneration() != 1 {
		t.Fatal("restart lost withdrawal")
	}
	if err = restarted.Update("other", "a", ChannelUpdate{Included: &yes}); err != nil {
		t.Fatal(err)
	}
	if svc.PublicationGeneration() != 1 {
		t.Fatal("source change rolled back withdrawal")
	}
	if _, err = svc.ResetBuilder("source", svc.ReviewStateToken(), backup, false); err != nil {
		t.Fatal(err)
	}
	if svc.PublicationGeneration() != 1 {
		t.Fatal("restore resurrected old published JSON")
	}
}
