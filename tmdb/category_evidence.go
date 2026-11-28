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

// CategoryEvidenceRevision changes whenever retained lookup evidence changes.
func (c *Client) CategoryEvidenceRevision() uint64 {
	if c == nil || c.cache == nil {
		return 0
	}
	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()
	return c.cache.version
}
func (c *Client) WhileCategoryEvidenceRevision(revision uint64, action func() error) (bool, error) {
	if c == nil || c.cache == nil {
		return revision == 0, func() error {
			if revision == 0 {
				return action()
			}
			return nil
		}()
	}
	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()
	if c.cache.version != revision {
		return false, nil
	}
	return true, action()
}
