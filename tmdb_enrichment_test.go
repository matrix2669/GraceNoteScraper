package main

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/tmdb"
)

func TestLookupTMDBTitlesBoundsWorkersAndPreservesResults(t *testing.T) {
	keys := make([]tmdbTitleKey, 12)
	for index := range keys {
		keys[index] = tmdbTitleKey{title: fmt.Sprintf("title-%02d", index), isMovie: index%2 == 0}
	}
	var mu sync.Mutex
	active, maximum := 0, 0
	var progress [][2]int
	results := lookupTMDBTitles(keys, 4, func(title string, _ bool) tmdb.CacheEntry {
		mu.Lock()
		active++
		if active > maximum {
			maximum = active
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		return tmdb.CacheEntry{Overview: title}
	}, func(completed, total int) {
		progress = append(progress, [2]int{completed, total})
	})
	if maximum != 4 {
		t.Fatalf("maximum workers = %d, want 4", maximum)
	}
	if len(results) != len(keys) {
		t.Fatalf("result count = %d, want %d", len(results), len(keys))
	}
	for _, key := range keys {
		if results[key].Overview != key.title {
			t.Fatalf("result for %+v = %+v", key, results[key])
		}
	}
	if len(progress) != len(keys) {
		t.Fatalf("progress updates = %d, want %d", len(progress), len(keys))
	}
	if got := progress[len(progress)-1]; got != [2]int{len(keys), len(keys)} {
		t.Fatalf("final progress = %v, want [%d %d]", got, len(keys), len(keys))
	}
}

func TestTMDBWorkerCountDefaultsAndClamps(t *testing.T) {
	t.Setenv("TMDB_WORKERS", "")
	if got := tmdbWorkerCount(); got != 4 {
		t.Fatalf("default workers = %d, want 4", got)
	}
	t.Setenv("TMDB_WORKERS", "99")
	if got := tmdbWorkerCount(); got != 16 {
		t.Fatalf("clamped workers = %d, want 16", got)
	}
	t.Setenv("TMDB_WORKERS", "1")
	if got := tmdbWorkerCount(); got != 1 {
		t.Fatalf("configured workers = %d, want 1", got)
	}
}
