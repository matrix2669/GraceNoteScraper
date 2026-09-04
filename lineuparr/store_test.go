package lineuparr

import (
	"os"
	"path/filepath"
	"testing"
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

func TestStateStoreMigratesVersionOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{\"version\":1,\"sourceFingerprint\":\"source\",\"channels\":{\"one\":{\"category\":\"News\"}}}"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := LoadStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.Snapshot("source")["one"].Category; got != "News" {
		t.Fatalf("migrated category = %q", got)
	}
	if store.MatchDecisionSnapshot("source") == nil {
		t.Fatal("version-one state did not initialize match decisions")
	}
}

func TestStateStorePersistsAliasAndMatchReview(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := LoadStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetAliasSuppressed("source", "one", "Bad Alias", true); err != nil {
		t.Fatal(err)
	}
	decision := MatchDecision{
		Key: "candidate", Decision: "denied", DispatcharrFingerprint: "dispatcharr",
		StreamFingerprint: "stream", StreamKey: "3:10", ChannelID: "one", StreamName: "Provider Name", NameScore: 98,
	}
	if err := store.SetMatchDecision("source", decision); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Snapshot("source")["one"].SuppressedAliases; len(got) != 1 || got[0] != "Bad Alias" {
		t.Fatalf("suppressed aliases = %v", got)
	}
	if got := reloaded.MatchDecisionSnapshot("source")["candidate"]; got.Decision != "denied" || got.NameScore != 98 {
		t.Fatalf("match decision = %+v", got)
	}
	if err := reloaded.ClearMatchDecision("source", "candidate"); err != nil {
		t.Fatal(err)
	}
	if err := reloaded.SetAliasSuppressed("source", "one", "Bad Alias", false); err != nil {
		t.Fatal(err)
	}
	if len(reloaded.MatchDecisionSnapshot("source")) != 0 || len(reloaded.Snapshot("source")) != 0 {
		t.Fatal("review state was not cleared")
	}
}
