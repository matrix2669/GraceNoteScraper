package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/channelcategory"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	lineuparrbuilder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
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

func independentProgrammeFilters(p guide.Program) []string {
	filters := append([]string(nil), p.RawFilters...)
	filters = append(filters, tmdbGenreFilters(p)...)
	seen := make(map[string]bool, len(filters))
	result := make([]string, 0, len(filters))
	for _, filter := range filters {
		filter = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(filter)), "filter-")
		if filter == "" || seen[filter] {
			continue
		}
		seen[filter] = true
		result = append(result, filter)
	}
	return result
}

func independentTargetShare(a channelcategory.ScheduleAssessment) float64 {
	switch a.Category {
	case channelcategory.Movies:
		return a.Shares["movie"]
	case channelcategory.Sports:
		return a.Shares["sports"]
	case channelcategory.NewsWeather:
		return a.Shares["news"]
	case channelcategory.KidsFamily:
		return a.Shares["family"]
	case channelcategory.Entertainment:
		return a.Shares["entertainment"]
	default:
		return 0
	}
}

func containsCategory(categories []string, wanted string) bool {
	for _, category := range categories {
		if strings.EqualFold(strings.TrimSpace(category), strings.TrimSpace(wanted)) {
			return true
		}
	}
	return false
}

func tmdbGuideRevision(g *guide.TVGuide) (string, int) {
	h := sha256.New()
	n := 0
	for _, p := range g.Programs {
		if p.TMDBGenresCaptured || strings.TrimSpace(p.OrigLanguage) != "" {
			n++
		}
		// Unclassified intervals still affect coverage, window selection and
		// overlap rejection. Evidence availability is part of the revision too.
		_ = json.NewEncoder(h).Encode([]any{p.Channel, p.Start, p.Stop, p.Title, p.RawFilters, p.TMDBGenresCaptured, p.TMDBMediaType, p.TMDBGenreIDs, p.TMDBGenreNames, p.OrigLanguage})
	}
	return hex.EncodeToString(h.Sum(nil)), n
}

// A scan records the effective timezone verified during assessment. Discovery
// can establish it before any lineup record is retained; in that case the
// source-scoped scan remains last-known provenance, without a draft-time fetch.
// A later known conflicting timezone invalidates confirmation, as does legacy
// scan data that never recorded its assessment timezone.
func (s *lineuparrServer) tmdbScanTimezoneCurrent(c appconfig.Config, scan lineuparrbuilder.TMDBCategoryScan) bool {
	if strings.TrimSpace(scan.Timezone) == "" {
		return false
	}
	if s.marketIndex != nil {
		if current := s.marketIndex.LineupTimezone(c.Gracenote.Country, c.Gracenote.PostalCode, c.Gracenote.LineupID, c.Gracenote.Device); current != nil {
			return current.String() == scan.Timezone
		}
	}
	return true
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
	genreCount := 0
	if g != nil {
		revision, count = tmdbGuideRevision(g)
		for _, programme := range g.Programs {
			if programme.TMDBGenresCaptured {
				genreCount++
			}
		}
	}
	if s.tmdbConfigured {
		switch {
		case s.tmdbEnriching != nil && s.tmdbEnriching():
			status = "enriching"
			message = "TMDB enrichment is running. Category scanning will be available after publication."
		case count == 0:
			status = "waiting-for-evidence"
			message = "No usable TMDB genre or original-language evidence is linked to this guide yet. Existing cached genre names are reused when their TMDB identity matches; otherwise normal enrichment must capture the missing evidence. Do not clear your guide or saved lineup choices."
		case revision != previous.Revision || !s.tmdbScanTimezoneCurrent(c, previous):
			status = "ready"
			message = "Programme evidence or its lineup timezone needs a category scan. Scan the cached data to propose categories; this does not request TMDB lookups."
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
			http.Error(w, "Lineup timezone service unavailable", 503)
			return
		}
		loc, err := s.marketIndex.ResolveLineupTimezone(r.Context(), c.Gracenote.Country, c.Gracenote.PostalCode, c.Gracenote.LineupID, c.Gracenote.Device, c.Gracenote.Language)
		if err != nil {
			http.Error(w, "Could not establish the selected lineup timezone: "+err.Error(), 409)
			return
		}
		rows := map[string][]channelcategory.ScheduleEvent{}
		independentRows := map[string][]channelcategory.ScheduleEvent{}
		languageRows := map[string][]channelcategory.LanguageEvent{}
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
			independentRows[p.Channel] = append(independentRows[p.Channel], channelcategory.ScheduleEvent{Start: a, Stop: b, Title: p.Title, Filters: independentProgrammeFilters(p)})
			languageRows[p.Channel] = append(languageRows[p.Channel], channelcategory.LanguageEvent{Start: a, Stop: b, Title: p.Title, OriginalLanguage: p.OrigLanguage})
		}
		if first.IsZero() {
			http.Error(w, "No valid programme intervals available", 409)
			return
		}
		scan := lineuparrbuilder.TMDBCategoryScan{Revision: revision, Timezone: loc.String(), ScannedAt: time.Now().UTC(), Categories: map[string]lineuparrbuilder.AttributedCategory{}, IndependentCategories: map[string][]string{}}
		for id, events := range rows {
			independent := channelcategory.AssessSchedule(independentRows[id], first, loc)
			independentCategories := independentCategoryNames(independent)
			if len(independentCategories) > 0 {
				scan.IndependentCategories[id] = independentCategories
			}
			language := channelcategory.AssessLanguage(languageRows[id], first, loc)
			if language.Category != "" {
				scan.Categories[id] = lineuparrbuilder.AttributedCategory{
					Value: language.Category, Source: "tmdb-language-schedule", Label: "TMDB original-language profile", Priority: language.Priority,
					Method: fmt.Sprintf("priority-%d; optional TMDB search-result original_language; 14-day weekdays in %s, %.1f%% language coverage across %d days and %d distinct titles; %.1f%% non-English airtime; category-quality-v2; requires review", language.Priority, language.Timezone, language.Coverage*100, language.Days, language.DistinctTitles, language.NonEnglishShare*100),
				}
				continue
			}
			a := channelcategory.AssessSchedule(events, first, loc)
			if a.Category == "" {
				continue
			}
			method := fmt.Sprintf("priority-4; optional TMDB search-result genres; 14-day weekday airtime, %.1f%% usable coverage; mean %.1f minutes; category-quality-v1; requires review", a.Coverage*100, a.AverageMinutes)
			confirmed := containsCategory(independentCategories, a.Category)
			if confirmed {
				method += fmt.Sprintf("; independent schedule confirmation: %s with >=80%% usable weekday coverage", strings.Join(independentCategories, ", "))
			}
			scan.Categories[id] = lineuparrbuilder.AttributedCategory{Value: a.Category, Source: "tmdb-schedule", Label: "TMDB programme genres", Priority: 4, IndependentConfirmation: confirmed, IndependentCategories: independentCategories, Method: method}
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
		message = fmt.Sprintf("Scanned cached TMDB genre and original-language evidence: %d provisional channel categories. Manual choices remain unchanged.", len(scan.Categories))
		previous = scan
	}
	writeLineuparrJSON(w, 200, map[string]any{"state": status, "message": message, "programmesWithEvidence": count, "programmesWithGenres": genreCount, "lastScan": previous.ScannedAt, "categoryCount": len(previous.Categories)})
}
