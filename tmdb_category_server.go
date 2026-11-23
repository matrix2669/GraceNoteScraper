package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/daniel-widrick/GraceNoteScraper/channelcategory"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	lineuparrbuilder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func tmdbGenreFilters(p guide.Program) []string {
	if !p.TMDBGenresCaptured {
		return nil
	}
	if p.TMDBMediaType == "movie" {
		return []string{"movie"}
	}
	if p.TMDBMediaType != "tv" {
		return nil
	}
	var result []string
	for _, name := range p.TMDBGenreNames {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "kids", "family":
			result = append(result, "family")
		case "news":
			result = append(result, "news")
		case "comedy", "drama", "crime", "mystery", "action & adventure", "sci-fi & fantasy", "reality", "documentary":
			result = append(result, "entertainment")
		}
	}
	for _, id := range p.TMDBGenreIDs {
		switch id {
		case 10762, 10751:
			result = append(result, "family")
		case 10763:
			result = append(result, "news")
		case 35, 18, 80, 9648, 10759, 10765, 10764, 99:
			result = append(result, "entertainment")
		}
	}
	return result
}

func tmdbGuideRevision(g *guide.TVGuide) (string, int) {
	h := sha256.New()
	n := 0
	for _, p := range g.Programs {
		if !p.TMDBGenresCaptured {
			continue
		}
		n++
		// Include schedule boundaries so a new guide invalidates the scan too.
		_ = json.NewEncoder(h).Encode([]any{p.Channel, p.Start, p.Stop, p.Title, p.TMDBMediaType, p.TMDBGenreIDs, p.TMDBGenreNames})
	}
	return hex.EncodeToString(h.Sum(nil)), n
}

// Adapt older published guides without mutating them or triggering a scrape.
// The cache key alone is insufficient: require the same explicit TMDB ID and
// movie/series identity previously attached by enrichment.
func (s *lineuparrServer) categoryEvidenceGuide(g *guide.TVGuide) *guide.TVGuide {
	if g == nil || s.tmdbCachedEvidence == nil {
		return g
	}
	copy := *g
	copy.Programs = append([]guide.Program(nil), g.Programs...)
	for i, p := range copy.Programs {
		if p.TMDBGenresCaptured {
			continue
		}
		var identity string
		conflict := false
		for _, number := range p.EpisodeNumbers {
			if number.System != "themoviedb.org" {
				continue
			}
			if identity != "" && identity != number.EpisodeNumber {
				conflict = true
			}
			identity = number.EpisodeNumber
		}
		if conflict || identity == "" {
			continue
		}
		movie := !strings.HasPrefix(identity, "series/")
		id, err := strconv.Atoi(strings.TrimPrefix(identity, "series/"))
		if err != nil || id <= 0 {
			continue
		}
		ids, names, ok := s.tmdbCachedEvidence(strings.ToLower(html.UnescapeString(p.Title)), movie, id)
		if !ok {
			continue
		}
		copy.Programs[i].TMDBGenreIDs, copy.Programs[i].TMDBGenreNames = ids, names
		copy.Programs[i].TMDBMediaType = "tv"
		if movie {
			copy.Programs[i].TMDBMediaType = "movie"
		}
		copy.Programs[i].TMDBGenresCaptured = true
	}
	return &copy
}

func (s *lineuparrServer) handleTMDBCategories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	if s.store == nil || s.state == nil || s.builder == nil {
		http.Error(w, "Category scan unavailable", 503)
		return
	}
	c, configured, _ := s.store.Get()
	if !configured {
		writeLineuparrJSON(w, 200, map[string]any{"state": "no-provider", "message": "Choose a lineup first."})
		return
	}
	status := "not-configured"
	message := "Add TMDB_TOKEN to the container environment and restart the container to enable optional programme metadata and category evidence. Guide and Lineuparr features work without it."
	previous := s.builder.TMDBCategoryScan(c.Fingerprint())
	g := s.categoryEvidenceGuide(s.state.GetForSource(c.Fingerprint()))
	revision := ""
	count := 0
	if g != nil {
		revision, count = tmdbGuideRevision(g)
	}
	if s.tmdbConfigured {
		switch {
		case s.tmdbEnriching != nil && s.tmdbEnriching():
			status = "enriching"
			message = "TMDB enrichment is running. Category scanning will be available after publication."
		case count == 0:
			status = "waiting-for-genres"
			message = "No usable TMDB genre evidence is linked to this guide yet. Existing cached genre names are reused when their TMDB identity matches; otherwise normal enrichment must capture the missing evidence. Do not clear your guide or saved lineup choices."
		case revision != previous.Revision:
			status = "ready"
			message = "New TMDB programme evidence is available. Scan the cached data to propose categories; this does not request TMDB lookups."
		default:
			status = "current"
			message = "The available TMDB programme evidence has been scanned."
		}
	}
	if r.Method == http.MethodPost {
		if !requireJSONContentType(w, r) {
			return
		}
		var body struct{}
		if !decodeLineuparrRequest(w, r, &body) {
			return
		}
		if status != "ready" && status != "current" {
			http.Error(w, message, 409)
			return
		}
		if s.marketIndex == nil {
			http.Error(w, "Scan the local providers first to establish the lineup timezone", 409)
			return
		}
		loc := s.marketIndex.LineupTimezone(c.Gracenote.Country, c.Gracenote.PostalCode, c.Gracenote.LineupID, c.Gracenote.Device)
		if loc == nil {
			http.Error(w, "Selected lineup timezone unavailable; scan local providers first", 409)
			return
		}
		rows := map[string][]channelcategory.ScheduleEvent{}
		var first time.Time
		for _, p := range g.Programs {
			a, err := time.Parse("20060102150405 -0700", p.Start)
			if err != nil {
				continue
			}
			b, err := time.Parse("20060102150405 -0700", p.Stop)
			if err != nil || !b.After(a) {
				continue
			}
			if first.IsZero() || a.Before(first) {
				first = a
			}
			rows[p.Channel] = append(rows[p.Channel], channelcategory.ScheduleEvent{Start: a, Stop: b, Title: p.Title, Filters: tmdbGenreFilters(p)})
		}
		if first.IsZero() {
			http.Error(w, "No valid programme intervals available", 409)
			return
		}
		scan := lineuparrbuilder.TMDBCategoryScan{Revision: revision, ScannedAt: time.Now().UTC(), Categories: map[string]lineuparrbuilder.AttributedCategory{}}
		for id, events := range rows {
			a := channelcategory.AssessSchedule(events, first, loc)
			if a.Category == "" {
				continue
			}
			scan.Categories[id] = lineuparrbuilder.AttributedCategory{Value: a.Category, Source: "tmdb-schedule", Label: "TMDB programme genres", Priority: 4, Method: fmt.Sprintf("priority-4; optional TMDB search-result genres; 14-day weekday airtime, %.1f%% usable coverage; mean %.1f minutes; category-quality-v1; requires review", a.Coverage*100, a.AverageMinutes)}
		}
		current, err := s.store.WhileCurrent(c.Fingerprint(), func() error { return s.builder.SaveTMDBCategoryScan(c.Fingerprint(), scan) })
		if err != nil {
			http.Error(w, "Could not save category scan", 500)
			return
		}
		if !current {
			http.Error(w, "Provider changed; scan again", 409)
			return
		}
		status = "current"
		message = fmt.Sprintf("Scanned cached TMDB genres: %d provisional channel categories. Manual choices remain unchanged.", len(scan.Categories))
		previous = scan
	}
	writeLineuparrJSON(w, 200, map[string]any{"state": status, "message": message, "programmesWithGenres": count, "lastScan": previous.ScannedAt, "categoryCount": len(previous.Categories)})
}
