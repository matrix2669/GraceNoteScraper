package tmdb

import "sync"

var enrichmentRegistry = struct {
	sync.RWMutex
	entries map[string]CacheEntry
}{entries: make(map[string]CacheEntry)}

func registerEntry(key string, entry CacheEntry) {
	enrichmentRegistry.Lock()
	enrichmentRegistry.entries[key] = entry
	enrichmentRegistry.Unlock()
}

func unregisterEntry(key string) {
	enrichmentRegistry.Lock()
	delete(enrichmentRegistry.entries, key)
	enrichmentRegistry.Unlock()
}

// LookupEnrichment returns metadata already resolved during the current scrape
// or loaded from the on-disk TMDB cache. It never performs a network request.
func LookupEnrichment(title string, isMovie bool) (CacheEntry, bool) {
	key := cacheKey(title, isMovie)
	enrichmentRegistry.RLock()
	entry, ok := enrichmentRegistry.entries[key]
	enrichmentRegistry.RUnlock()
	if !ok || entry.TMDBID == 0 {
		return CacheEntry{}, false
	}
	return entry, true
}
