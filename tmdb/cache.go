package tmdb

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/internal/applog"
)

const (
	cacheTTL          = 7 * 24 * time.Hour
	cacheSaveEvery    = 250
	cacheSaveInterval = time.Minute
)

type Credit struct {
	Name string `json:"name"`
	Role string `json:"role,omitempty"`
}

type CacheEntry struct {
	GenreIDs       []int    `json:"genre_ids,omitempty"`
	MediaType      string   `json:"media_type,omitempty"`
	GenresCaptured bool     `json:"genres_captured,omitempty"`
	ImageURL       string   `json:"image_url"`
	BackdropURL    string   `json:"backdrop_url,omitempty"`
	Rating         float64  `json:"rating,omitempty"`
	VoteCount      int      `json:"vote_count,omitempty"`
	Year           string   `json:"year,omitempty"`
	ReleaseDate    string   `json:"release_date,omitempty"`
	Overview       string   `json:"overview,omitempty"`
	TMDBID         int      `json:"tmdb_id,omitempty"`
	IMDbID         string   `json:"imdb_id,omitempty"`
	TVDBID         int      `json:"tvdb_id,omitempty"`
	OrigLanguage   string   `json:"orig_language,omitempty"`
	Genres         []string `json:"genres,omitempty"`
	Keywords       []string `json:"keywords,omitempty"`
	Runtime        int      `json:"runtime,omitempty"`
	Certification  string   `json:"certification,omitempty"`
	Credits        []Credit `json:"credits,omitempty"`
	MatchedTitle   string   `json:"matched_title,omitempty"`
	MatchScore     int      `json:"match_score,omitempty"`
	FetchedAt      int64    `json:"fetched_at"`
}

type Cache struct {
	mu           sync.Mutex
	saveMu       sync.Mutex
	entries      map[string]CacheEntry
	path         string
	version      uint64
	savedVersion uint64
	lastSave     time.Time
}

func LoadCache(path string) *Cache {
	c := &Cache{
		entries:  make(map[string]CacheEntry),
		path:     path,
		lastSave: time.Now(),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}

	if err := json.Unmarshal(data, &c.entries); err != nil {
		applog.Warnf("tmdb cache file is corrupt, starting fresh: %v", err)
		c.entries = make(map[string]CacheEntry)
	}
	return c
}

func (c *Cache) Get(key string) (CacheEntry, bool) {
	// Treat obvious news, shopping, religious, sports, filler, and excluded
	// channel/category titles as negative cache hits. This prevents a TMDB HTTP
	// request without removing the programme from the generated guide.
	if tmdbSkipReason(key) != "" {
		recordLookupProgress(key)
		c.flushCompletedScan()
		return CacheEntry{}, true
	}

	c.mu.Lock()
	entry, ok := c.entries[key]
	if !ok {
		c.mu.Unlock()
		return CacheEntry{}, false
	}
	if time.Since(time.Unix(entry.FetchedAt, 0)) > cacheTTL {
		delete(c.entries, key)
		c.version++
		c.mu.Unlock()
		return CacheEntry{}, false
	}
	entry.GenreIDs = append([]int(nil), entry.GenreIDs...)
	entry.Genres = append([]string(nil), entry.Genres...)
	c.mu.Unlock()

	// Successful entries and empty negative-cache entries both count as a
	// completed title in the current scan.
	recordLookupProgress(key)
	registerCompletedLookup(key, entry)
	c.flushCompletedScan()
	return entry, true
}

// Set stores a lookup result. An empty entry is valid and acts as a negative
// cache for cacheTTL, so titles with no sufficiently close match are not queried
// again on every scrape. During long scrapes the cache is flushed approximately
// once per minute or every cacheSaveEvery completed lookups, whichever happens
// first. The final title in every scan also forces a flush.
func (c *Cache) Set(key string, entry CacheEntry) {
	entry.FetchedAt = time.Now().Unix()
	entry.GenreIDs = append([]int(nil), entry.GenreIDs...)
	entry.Genres = append([]string(nil), entry.Genres...)

	c.mu.Lock()
	c.entries[key] = entry
	c.version++
	pending := c.version - c.savedVersion
	saveDue := pending >= cacheSaveEvery || time.Since(c.lastSave) >= cacheSaveInterval
	c.mu.Unlock()

	recordLookupProgress(key)
	registerCompletedLookup(key, entry)

	if c.flushCompletedScan() {
		return
	}
	if saveDue {
		c.save(false)
	}
}

// flushCompletedScan performs a final forced save as soon as every registered
// unique title has been processed. It returns true when the scan was complete,
// even if there were no pending cache changes to write.
func (c *Cache) flushCompletedScan() bool {
	done, total, active := lookupScanProgress()
	if !active || done < total {
		return false
	}
	c.save(true)
	return true
}

// Save flushes all unsaved cache changes. It remains safe to call during or at
// the end of a scrape because writes use a snapshot and atomic rename.
func (c *Cache) Save() {
	c.save(true)
}

func (c *Cache) save(force bool) {
	c.saveMu.Lock()
	defer c.saveMu.Unlock()

	c.mu.Lock()
	version := c.version
	pending := version - c.savedVersion
	if pending == 0 {
		c.mu.Unlock()
		return
	}
	if !force && pending < cacheSaveEvery && time.Since(c.lastSave) < cacheSaveInterval {
		c.mu.Unlock()
		return
	}

	snapshot := make(map[string]CacheEntry, len(c.entries))
	for key, entry := range c.entries {
		snapshot[key] = entry
	}
	c.mu.Unlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		applog.Errorf("tmdb failed to marshal cache: %v", err)
		return
	}
	if err := writeFileAtomic(c.path, data, 0644); err != nil {
		applog.Errorf("tmdb failed to write cache file: %v", err)
		return
	}

	c.mu.Lock()
	if version > c.savedVersion {
		c.savedVersion = version
	}
	c.lastSave = time.Now()
	remaining := c.version - c.savedVersion
	entryCount := len(c.entries)
	c.mu.Unlock()

	done, total, active := lookupScanProgress()
	if active {
		percent := 100 * float64(done) / float64(total)
		log.Printf("tmdb: saved cache (%d entries, %d new, %d pending); scan progress %d/%d titles (%.1f%%)",
			entryCount, pending, remaining, done, total, percent)
		return
	}
	log.Printf("tmdb: saved cache (%d entries, %d new, %d pending)", entryCount, pending, remaining)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	tmp, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary cache file: %w", err)
	}
	tmpName := tmp.Name()
	removeTemp := true
	defer func() {
		_ = tmp.Close()
		if removeTemp {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write temporary cache file: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		return fmt.Errorf("set temporary cache permissions: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temporary cache file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary cache file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace cache file: %w", err)
	}

	removeTemp = false
	return nil
}
