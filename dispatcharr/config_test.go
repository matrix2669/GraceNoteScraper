package dispatcharr

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigStorePersistsCredentialsPrivately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "dispatcharr.json")
	store, err := LoadConfigStore(path)
	if err != nil {
		t.Fatal(err)
	}
	config := Config{BaseURL: " https://dispatcharr.example.test/root/ ", Username: " admin ", Password: "secret"}
	if err := store.Save(config); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("config mode = %o, want 600", got)
	}
	reloaded, err := LoadConfigStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, configured := reloaded.Get()
	if !configured || got.BaseURL != "https://dispatcharr.example.test/root" || got.Username != "admin" || got.Password != "secret" {
		t.Fatalf("reloaded config = %+v configured=%v", got, configured)
	}
	if err := reloaded.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("config still exists after Clear(): %v", err)
	}
}

func TestConfigRejectsEmbeddedCredentials(t *testing.T) {
	_, err := (Config{BaseURL: "https://admin:secret@example.test", Username: "admin", Password: "secret"}).Normalized()
	if err == nil {
		t.Fatal("embedded URL credentials were accepted")
	}
}

func TestConfigFingerprintPreservesCaseSensitivePath(t *testing.T) {
	upper, _ := (Config{BaseURL: "https://EXAMPLE.test/Dispatcharr", Username: "admin", Password: "one"}).Normalized()
	lower, _ := (Config{BaseURL: "https://example.test/dispatcharr", Username: "admin", Password: "two"}).Normalized()
	if upper.Fingerprint() == lower.Fingerprint() {
		t.Fatal("case-sensitive base paths shared a fingerprint")
	}
}
