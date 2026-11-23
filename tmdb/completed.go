package tmdb

import "sync"

var completedLookups = struct {
	sync.RWMutex
	entries map[string]CacheEntry
}{entries: make(map[string]CacheEntry)}

func registerCompletedLookup(key string, entry CacheEntry) {
	if entry.TMDBID == 0 {
		return
	}
	completedLookups.Lock()
	completedLookups.entries[key] = entry
	completedLookups.Unlock()
}

// LookupCompleted returns metadata already resolved by the current scrape or
// returned from the TMDB cache. It never performs a network request.
func LookupCompleted(title string, isMovie bool) (CacheEntry, bool) {
	key := cacheKey(title, isMovie)
	completedLookups.RLock()
	entry, ok := completedLookups.entries[key]
	completedLookups.RUnlock()
	return entry, ok
}
