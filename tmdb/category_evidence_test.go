package tmdb

import "testing"

func TestLegacyCategoryEvidenceNeedsSameIdentity(t *testing.T) {
	c := &Client{cache: &Cache{entries: map[string]CacheEntry{"tv:example": {TMDBID: 42, Genres: []string{"Comedy"}, FetchedAt: 1}}}}
	_, names, ok := c.CachedCategoryEvidence("example", false, 42)
	if !ok || len(names) != 1 || names[0] != "Comedy" {
		t.Fatal(names, ok)
	}
	names[0] = "changed"
	_, names, _ = c.CachedCategoryEvidence("example", false, 42)
	if names[0] != "Comedy" {
		t.Fatal("cache mutated through result")
	}
	for _, test := range []struct {
		movie bool
		id    int
	}{{true, 42}, {false, 43}, {false, 0}} {
		if _, _, ok := c.CachedCategoryEvidence("example", test.movie, test.id); ok {
			t.Fatal("accepted different identity", test)
		}
	}
	if c.cache.entries["tv:example"].FetchedAt != 1 {
		t.Fatal("read changed cache freshness")
	}
}
