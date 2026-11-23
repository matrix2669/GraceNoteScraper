package main

import (
	"github.com/daniel-widrick/GraceNoteScraper/tmdb"
	"testing"
)

// Setup's progress callback must observe completed concurrent TMDB work.
func TestRebuiltTMDBWorkersReportMonotonicProgress(t *testing.T) {
	keys := []tmdbTitleKey{{title: "one"}, {title: "two"}, {title: "three"}}
	completed := 0
	results := lookupTMDBTitles(keys, 2, func(title string, movie bool) tmdb.CacheEntry {
		return tmdb.CacheEntry{Overview: title}
	}, func(done, total int) {
		completed++
		if done != completed || total != len(keys) {
			t.Errorf("progress %d/%d, expected %d/%d", done, total, completed, len(keys))
		}
	})
	if completed != len(keys) || len(results) != len(keys) {
		t.Fatal("incomplete work or progress")
	}
}
