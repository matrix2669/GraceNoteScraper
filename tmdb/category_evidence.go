package tmdb

// CachedCategoryEvidence reads only retained metadata for the exact TMDB
// identity already attached to a programme. It never refreshes, expires, saves,
// or records lookup progress. Cache TTL continues to govern normal Lookup.
func (c *Client) CachedCategoryEvidence(title string, movie bool, expectedID int) ([]int, []string, bool) {
	if c == nil || c.cache == nil || expectedID <= 0 {
		return nil, nil, false
	}
	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()
	entry, ok := c.cache.entries[cacheKey(title, movie)]
	if !ok || entry.TMDBID != expectedID || (len(entry.GenreIDs) == 0 && len(entry.Genres) == 0) {
		return nil, nil, false
	}
	return append([]int(nil), entry.GenreIDs...), append([]string(nil), entry.Genres...), true
}
