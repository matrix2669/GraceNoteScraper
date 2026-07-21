package tmdb

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCacheAutomaticallySavesAtLookupThreshold(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tmdb_cache.json")
	cache := LoadCache(path)

	for i := 0; i < cacheSaveEvery; i++ {
		cache.Set(fmt.Sprintf("tv:automatic-save-%d", i), CacheEntry{
			TMDBID: i + 1,
		})
	}

	entries := readCacheFileForTest(t, path)
	if len(entries) != cacheSaveEvery {
		t.Fatalf("saved cache contains %d entries, want %d", len(entries), cacheSaveEvery)
	}
}

func TestCacheSaveFlushesPendingLookups(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tmdb_cache.json")
	cache := LoadCache(path)
	cache.Set("tv:manual-flush", CacheEntry{TMDBID: 1234})

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cache should not be written before a threshold or final flush; stat error = %v", err)
	}

	cache.Save()
	entries := readCacheFileForTest(t, path)
	entry, ok := entries["tv:manual-flush"]
	if !ok {
		t.Fatal("final cache flush did not persist pending lookup")
	}
	if entry.TMDBID != 1234 {
		t.Fatalf("persisted TMDB ID = %d, want 1234", entry.TMDBID)
	}
}

func TestAtomicCacheWriteReplacesFileAndCleansTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tmdb_cache.json")
	if err := os.WriteFile(path, []byte(`{"old":true}`), 0644); err != nil {
		t.Fatalf("write initial cache: %v", err)
	}

	data := []byte(`{"new":true}`)
	if err := writeFileAtomic(path, data, 0644); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read replaced cache: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("cache contents = %q, want %q", got, data)
	}

	temps, err := filepath.Glob(filepath.Join(dir, ".tmdb_cache.json.tmp-*"))
	if err != nil {
		t.Fatalf("glob temporary files: %v", err)
	}
	if len(temps) != 0 {
		t.Fatalf("temporary cache files remain: %v", temps)
	}
}

func readCacheFileForTest(t *testing.T, path string) map[string]CacheEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	var entries map[string]CacheEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("decode cache: %v", err)
	}
	return entries
}
