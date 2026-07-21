package tmdb

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestEmptyNoMatchEntryIsNegativeCached(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tmdb_cache.json")
	key := cacheKey("Unmatched Programme", false)

	cache := LoadCache(path)
	cache.Set(key, CacheEntry{})
	cache.Save()

	reloaded := LoadCache(path)
	entry, ok := reloaded.Get(key)
	if !ok {
		t.Fatal("empty no-match result was not returned as a negative cache hit")
	}
	if entry.FetchedAt == 0 {
		t.Fatal("negative cache entry is missing fetched_at")
	}
	if entry.TMDBID != 0 || entry.ImageURL != "" {
		t.Fatalf("negative cache entry unexpectedly contains match data: %+v", entry)
	}
}

func TestExpiredNegativeCacheEntryIsRetried(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tmdb_cache.json")
	key := cacheKey("Old Unmatched Programme", false)
	entries := map[string]CacheEntry{
		key: {FetchedAt: time.Now().Add(-cacheTTL - time.Hour).Unix()},
	}
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal expired cache fixture: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write expired cache fixture: %v", err)
	}

	cache := LoadCache(path)
	if _, ok := cache.Get(key); ok {
		t.Fatal("expired negative cache entry should be retried")
	}
}

func TestLookupProgressCountsMatchesNoMatchesAndSkips(t *testing.T) {
	t.Setenv("TMDB_EXCLUDE_PROGRAM_CATEGORIES", "faith")
	resetChannelEligibilityRegistryForTest()
	defer resetChannelEligibilityRegistryForTest()

	RegisterChannelEligibility("normal", "FXHD", "FX", "553")
	RegisterProgramEligibility("Matched Programme", false, "normal", nil)
	RegisterProgramEligibility("Unmatched Programme", false, "normal", nil)
	RegisterProgramEligibility("Faith Programme", false, "normal", []string{"filter-faith"})

	cache := LoadCache(filepath.Join(t.TempDir(), "tmdb_cache.json"))
	cache.Set(cacheKey("Matched Programme", false), CacheEntry{TMDBID: 123})
	cache.Set(cacheKey("Unmatched Programme", false), CacheEntry{})
	if _, ok := cache.Get(cacheKey("Faith Programme", false)); !ok {
		t.Fatal("category skip should behave as a negative cache hit")
	}

	done, total, active := lookupScanProgress()
	if !active {
		t.Fatal("lookup scan should be active")
	}
	if done != 3 || total != 3 {
		t.Fatalf("lookup progress = %d/%d, want 3/3", done, total)
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
