package tmdb

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestGenreMetadataRoundTripAndIsolation(t *testing.T) {
	var result searchResponse
	if err := json.Unmarshal([]byte(`{"results":[{"id":1,"genre_ids":[35,18]}]}`), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 1 || len(result.Results[0].GenreIDs) != 2 {
		t.Fatal(result)
	}
	path := filepath.Join(t.TempDir(), "cache.json")
	c := LoadCache(path)
	e := CacheEntry{TMDBID: 1, GenreIDs: result.Results[0].GenreIDs, MediaType: "tv", GenresCaptured: true}
	c.Set("example", e)
	e.GenreIDs[0] = 999
	got, ok := c.Get("example")
	if !ok || got.GenreIDs[0] != 35 {
		t.Fatal(got)
	}
	got.GenreIDs[0] = 999
	c.Save()
	got, ok = LoadCache(path).Get("example")
	if !ok || got.GenreIDs[0] != 35 || !got.GenresCaptured || got.MediaType != "tv" {
		t.Fatal(got)
	}
}
