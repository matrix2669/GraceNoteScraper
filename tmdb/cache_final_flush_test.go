package tmdb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompletedScanFlushesPendingCacheWhenFinalTitleIsSkip(t *testing.T) {
	t.Setenv("TMDB_EXCLUDE_PROGRAM_CATEGORIES", "faith")
	resetChannelEligibilityRegistryForTest()
	defer resetChannelEligibilityRegistryForTest()

	RegisterChannelEligibility("normal", "FXHD", "FX", "553")
	RegisterProgramEligibility("Matched Programme", false, "normal", nil)
	RegisterProgramEligibility("Faith Programme", false, "normal", []string{"filter-faith"})

	path := filepath.Join(t.TempDir(), "tmdb_cache.json")
	cache := LoadCache(path)
	cache.Set(cacheKey("Matched Programme", false), CacheEntry{TMDBID: 123})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cache should remain pending before scan completion; stat error = %v", err)
	}

	if _, ok := cache.Get(cacheKey("Faith Programme", false)); !ok {
		t.Fatal("faith programme should be a deterministic skip")
	}

	entries := readCacheFileForTest(t, path)
	entry, ok := entries[cacheKey("Matched Programme", false)]
	if !ok || entry.TMDBID != 123 {
		t.Fatalf("completed scan did not flush pending match: %+v", entry)
	}
}
