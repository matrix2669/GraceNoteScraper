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
