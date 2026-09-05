package tmdb

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

const cacheTTL = 7 * 24 * time.Hour

type CacheEntry struct {
	Genres         []string `json:"genres,omitempty"`
	GenreIDs       []int    `json:"genre_ids,omitempty"`
	MediaType      string   `json:"media_type,omitempty"`
	GenresCaptured bool     `json:"genres_captured,omitempty"`
	ImageURL       string   `json:"image_url"`
	Rating         float64  `json:"rating,omitempty"`
	Year           string   `json:"year,omitempty"`
	Overview       string   `json:"overview,omitempty"`
	TMDBID         int      `json:"tmdb_id,omitempty"`
	OrigLanguage   string   `json:"orig_language,omitempty"`
	FetchedAt      int64    `json:"fetched_at"`
}

type Cache struct {
	mu      sync.Mutex
	entries map[string]CacheEntry
	path    string
}

func LoadCache(path string) *Cache {
	c := &Cache{
		entries: make(map[string]CacheEntry),
		path:    path,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}

	if err := json.Unmarshal(data, &c.entries); err != nil {
		log.Printf("tmdb: cache file corrupt, starting fresh: %v", err)
		c.entries = make(map[string]CacheEntry)
	}
	return c
}

func (c *Cache) Get(key string) (CacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		return CacheEntry{}, false
	}
	if time.Since(time.Unix(entry.FetchedAt, 0)) > cacheTTL {
		delete(c.entries, key)
		return CacheEntry{}, false
	}
	entry.GenreIDs = append([]int(nil), entry.GenreIDs...)
	entry.Genres = append([]string(nil), entry.Genres...)
	return entry, true
}

// Stores a lookup result. An empty entry is valid (negative cache).
func (c *Cache) Set(key string, entry CacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry.FetchedAt = time.Now().Unix()
	entry.GenreIDs = append([]int(nil), entry.GenreIDs...)
	entry.Genres = append([]string(nil), entry.Genres...)
	c.entries[key] = entry
}

func (c *Cache) Save() {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := json.MarshalIndent(c.entries, "", "  ")
	if err != nil {
		log.Printf("tmdb: failed to marshal cache: %v", err)
		return
	}
	if err := os.WriteFile(c.path, data, 0644); err != nil {
		log.Printf("tmdb: failed to write cache file: %v", err)
	}
}
