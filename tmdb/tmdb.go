package tmdb

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/daniel-widrick/GraceNoteScraper/internal/applog"
)

const (
	baseURL      = "https://api.themoviedb.org"
	imageBase    = "https://image.tmdb.org/t/p/w500"
	backdropBase = "https://image.tmdb.org/t/p/w780"
	rateDelay    = 250 * time.Millisecond // ~4 req/sec
)

type Client struct {
	http  *http.Client
	cache *Cache
	mu    sync.Mutex // guards rate limiting
	last  time.Time
}

type bearerTransport struct {
	token string
	rt    http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.rt.RoundTrip(req)
}

func NewClient(token, cachePath string) *Client {
	if token == "" {
		return nil
	}
	return &Client{
		http: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &bearerTransport{
				token: token,
				rt:    http.DefaultTransport,
			},
		},
		cache: LoadCache(cachePath),
	}
}

func (c *Client) Close() {
	if c == nil {
		return
	}
	c.cache.Save()
}

// Lookup checks the cache, validates TMDB search results against the requested
// title, and then retrieves the selected title's detail record. It deliberately
// rejects weak title matches instead of silently accepting the first result.
func (c *Client) Lookup(title string, isMovie bool) CacheEntry {
	if c == nil {
		return CacheEntry{}
	}

	key := cacheKey(title, isMovie)
	if entry, ok := c.cache.Get(key); ok {
		return entry
	}

	entry := c.search(title, isMovie)
	c.cache.Set(key, entry)
	return entry
}

type searchResponse struct {
	Results []searchResult `json:"results"`
}

type searchResult struct {
	GenreIDs         []int   `json:"genre_ids"`
	ID               int     `json:"id"`
	Title            string  `json:"title"`
	OriginalTitle    string  `json:"original_title"`
	Name             string  `json:"name"`
	OriginalName     string  `json:"original_name"`
	PosterPath       *string `json:"poster_path"`
	BackdropPath     *string `json:"backdrop_path"`
	VoteAverage      float64 `json:"vote_average"`
	VoteCount        int     `json:"vote_count"`
	Popularity       float64 `json:"popularity"`
	Overview         string  `json:"overview"`
	OriginalLanguage string  `json:"original_language"`
	FirstAirDate     string  `json:"first_air_date"`
	ReleaseDate      string  `json:"release_date"`
}

type genre struct {
	Name string `json:"name"`
}

type namedValue struct {
	Name string `json:"name"`
}

type detailsResponse struct {
	ID               int     `json:"id"`
	Title            string  `json:"title"`
	Name             string  `json:"name"`
	PosterPath       *string `json:"poster_path"`
	BackdropPath     *string `json:"backdrop_path"`
	VoteAverage      float64 `json:"vote_average"`
	VoteCount        int     `json:"vote_count"`
	Overview         string  `json:"overview"`
	OriginalLanguage string  `json:"original_language"`
	ReleaseDate      string  `json:"release_date"`
	FirstAirDate     string  `json:"first_air_date"`
	Runtime          int     `json:"runtime"`
	EpisodeRunTime   []int   `json:"episode_run_time"`
	Genres           []genre `json:"genres"`
	ExternalIDs      struct {
		IMDbID string `json:"imdb_id"`
		TVDBID int    `json:"tvdb_id"`
	} `json:"external_ids"`
	Keywords struct {
		Keywords []namedValue `json:"keywords"`
		Results  []namedValue `json:"results"`
	} `json:"keywords"`
	Credits struct {
		Cast []struct {
			Name  string `json:"name"`
			Order int    `json:"order"`
		} `json:"cast"`
		Crew []struct {
			Name string `json:"name"`
			Job  string `json:"job"`
		} `json:"crew"`
	} `json:"credits"`
	ReleaseDates struct {
		Results []struct {
			ISO31661     string `json:"iso_3166_1"`
			ReleaseDates []struct {
				Certification string `json:"certification"`
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
	ContentRatings struct {
		Results []struct {
			ISO31661 string `json:"iso_3166_1"`
			Rating   string `json:"rating"`
		} `json:"results"`
	} `json:"content_ratings"`
}

type scoredResult struct {
	result searchResult
	score  int
}

func (c *Client) search(title string, isMovie bool) CacheEntry {
	path := "/3/search/tv"
	if isMovie {
		path = "/3/search/movie"
	}

	var sr searchResponse
	if err := c.getJSON(path, url.Values{"query": {title}}, &sr); err != nil {
		applog.Errorf("tmdb search failed for %q: %v", title, err)
		return CacheEntry{}
	}

	selected, score, ok := selectBestResult(title, sr.Results, isMovie)
	if !ok {
		log.Printf("tmdb: no sufficiently close title match for %q", title)
		return CacheEntry{}
	}

	entry := cacheEntryFromSearch(selected, isMovie)
	entry.MatchScore = score
	entry.MatchedTitle = resultTitle(selected, isMovie)

	detailPath := fmt.Sprintf("/3/%s/%d", map[bool]string{true: "movie", false: "tv"}[isMovie], selected.ID)
	params := url.Values{"append_to_response": {"credits,external_ids,keywords,release_dates,content_ratings"}}
	var details detailsResponse
	if err := c.getJSON(detailPath, params, &details); err != nil {
		applog.Warnf("tmdb detail lookup failed for %q (id=%d): %v", title, selected.ID, err)
		return entry
	}

	mergeDetails(&entry, details, isMovie)
	return entry
}

func (c *Client) getJSON(path string, params url.Values, dst any) error {
	c.rateWait()
	u := baseURL + path
	if encoded := params.Encode(); encoded != "" {
		u += "?" + encoded
	}

	resp, err := c.http.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

func selectBestResult(query string, results []searchResult, isMovie bool) (searchResult, int, bool) {
	if len(results) == 0 {
		return searchResult{}, 0, false
	}

	scored := make([]scoredResult, 0, len(results))
	for _, result := range results {
		best := 0
		for _, candidate := range resultTitles(result, isMovie) {
			if score := titleSimilarity(query, candidate); score > best {
				best = score
			}
		}
		if best > 0 {
			scored = append(scored, scoredResult{result: result, score: best})
		}
	}
	if len(scored) == 0 {
		return searchResult{}, 0, false
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		if scored[i].result.VoteCount != scored[j].result.VoteCount {
			return scored[i].result.VoteCount > scored[j].result.VoteCount
		}
		return scored[i].result.Popularity > scored[j].result.Popularity
	})

	best := scored[0]
	if best.score < 85 {
		return searchResult{}, best.score, false
	}
	return best.result, best.score, true
}

func resultTitles(result searchResult, isMovie bool) []string {
	if isMovie {
		return []string{result.Title, result.OriginalTitle}
	}
	return []string{result.Name, result.OriginalName}
}

func resultTitle(result searchResult, isMovie bool) string {
	for _, title := range resultTitles(result, isMovie) {
		if strings.TrimSpace(title) != "" {
			return title
		}
	}
	return ""
}

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

func normalizeTitle(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(nonAlphanumeric.ReplaceAllString(b.String(), " ")), " ")
}

func titleSimilarity(a, b string) int {
	a = normalizeTitle(a)
	b = normalizeTitle(b)
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 100
	}
	if strings.Contains(a, b) || strings.Contains(b, a) {
		shorter, longer := len(a), len(b)
		if shorter > longer {
			shorter, longer = longer, shorter
		}
		return 85 + (15 * shorter / longer)
	}

	aTokens := tokenSet(a)
	bTokens := tokenSet(b)
	intersection := 0
	for token := range aTokens {
		if bTokens[token] {
			intersection++
		}
	}
	if intersection == 0 {
		return 0
	}
	return 100 * (2 * intersection) / (len(aTokens) + len(bTokens))
}

func tokenSet(value string) map[string]bool {
	set := make(map[string]bool)
	for _, token := range strings.Fields(value) {
		set[token] = true
	}
	return set
}

func cacheEntryFromSearch(result searchResult, isMovie bool) CacheEntry {
	entry := CacheEntry{
		GenreIDs:       append([]int(nil), result.GenreIDs...),
		GenresCaptured: true,
		MediaType:      "tv",
		Rating:         result.VoteAverage,
		VoteCount:      result.VoteCount,
		Overview:       result.Overview,
		TMDBID:         result.ID,
		OrigLanguage:   result.OriginalLanguage,
	}
	if result.PosterPath != nil {
		entry.ImageURL = imageBase + *result.PosterPath
	}
	if result.BackdropPath != nil {
		entry.BackdropURL = backdropBase + *result.BackdropPath
	}
	date := result.FirstAirDate
	if isMovie {
		entry.MediaType = "movie"
		date = result.ReleaseDate
	}
	entry.ReleaseDate = date
	if len(date) >= 4 {
		entry.Year = date[:4]
	}
	return entry
}

func mergeDetails(entry *CacheEntry, details detailsResponse, isMovie bool) {
	entry.TMDBID = details.ID
	entry.IMDbID = details.ExternalIDs.IMDbID
	entry.TVDBID = details.ExternalIDs.TVDBID
	entry.Rating = details.VoteAverage
	entry.VoteCount = details.VoteCount
	entry.Overview = firstNonEmpty(details.Overview, entry.Overview)
	entry.OrigLanguage = firstNonEmpty(details.OriginalLanguage, entry.OrigLanguage)

	if details.PosterPath != nil {
		entry.ImageURL = imageBase + *details.PosterPath
	}
	if details.BackdropPath != nil {
		entry.BackdropURL = backdropBase + *details.BackdropPath
	}

	date := details.FirstAirDate
	if isMovie {
		date = details.ReleaseDate
	}
	if date != "" {
		entry.ReleaseDate = date
		if len(date) >= 4 {
			entry.Year = date[:4]
		}
	}

	entry.Runtime = details.Runtime
	if !isMovie && entry.Runtime == 0 && len(details.EpisodeRunTime) > 0 {
		entry.Runtime = details.EpisodeRunTime[0]
	}
	for _, item := range details.Genres {
		appendUnique(&entry.Genres, item.Name)
	}
	for _, item := range details.Keywords.Keywords {
		appendUnique(&entry.Keywords, item.Name)
	}
	for _, item := range details.Keywords.Results {
		appendUnique(&entry.Keywords, item.Name)
	}

	for _, crew := range details.Credits.Crew {
		if crew.Job == "Director" || crew.Job == "Creator" {
			entry.Credits = append(entry.Credits, Credit{Name: crew.Name, Role: crew.Job})
		}
	}
	sort.SliceStable(details.Credits.Cast, func(i, j int) bool {
		return details.Credits.Cast[i].Order < details.Credits.Cast[j].Order
	})
	for i, cast := range details.Credits.Cast {
		if i >= 5 {
			break
		}
		entry.Credits = append(entry.Credits, Credit{Name: cast.Name, Role: "Actor"})
	}

	if isMovie {
		for _, country := range details.ReleaseDates.Results {
			if country.ISO31661 != "US" {
				continue
			}
			for _, release := range country.ReleaseDates {
				if release.Certification != "" {
					entry.Certification = release.Certification
					return
				}
			}
		}
	} else {
		for _, rating := range details.ContentRatings.Results {
			if rating.ISO31661 == "US" && rating.Rating != "" {
				entry.Certification = rating.Rating
				return
			}
		}
	}
}

func appendUnique(values *[]string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	for _, existing := range *values {
		if strings.EqualFold(existing, value) {
			return
		}
	}
	*values = append(*values, value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// rateWait enforces a minimum delay between API requests.
func (c *Client) rateWait() {
	c.mu.Lock()
	defer c.mu.Unlock()

	since := time.Since(c.last)
	if since < rateDelay {
		time.Sleep(rateDelay - since)
	}
	c.last = time.Now()
}

func cacheKey(title string, isMovie bool) string {
	prefix := "tv"
	if isMovie {
		prefix = "movie"
	}
	return prefix + ":" + normalizeTitle(title)
}
