package guide

import (
	"fmt"
	"html"
	"strings"

	"github.com/daniel-widrick/GraceNoteScraper/tmdb"
)

type Credits struct {
	Directors []string
	Actors    []string
}

func (p Program) isMovie() bool {
	for _, category := range p.Categories {
		if strings.EqualFold(strings.TrimSpace(category.Name), "movie") {
			return true
		}
	}
	return false
}

func (p Program) tmdbEntry() (tmdb.CacheEntry, bool) {
	title := strings.TrimSpace(html.UnescapeString(p.Title))
	if title == "" {
		return tmdb.CacheEntry{}, false
	}
	return tmdb.LookupEnrichment(title, p.isMovie())
}

func (p Program) EnrichedCategories() []Category {
	out := append([]Category(nil), p.Categories...)
	seen := make(map[string]bool, len(out))
	for _, category := range out {
		seen[strings.ToLower(strings.TrimSpace(category.Name))] = true
	}

	entry, ok := p.tmdbEntry()
	if !ok {
		return out
	}
	for _, genre := range entry.Genres {
		genre = strings.TrimSpace(genre)
		key := strings.ToLower(genre)
		if genre == "" || seen[key] {
			continue
		}
		out = append(out, Category{Name: xmlEscape(genre), Lang: p.Lang})
		seen[key] = true
	}
	return out
}

func (p Program) TMDBKeywords() []string {
	entry, ok := p.tmdbEntry()
	if !ok {
		return nil
	}
	out := make([]string, 0, len(entry.Keywords))
	seen := make(map[string]bool)
	for _, keyword := range entry.Keywords {
		keyword = strings.TrimSpace(keyword)
		key := strings.ToLower(keyword)
		if keyword == "" || seen[key] {
			continue
		}
		out = append(out, xmlEscape(keyword))
		seen[key] = true
	}
	return out
}

func (p Program) EnrichedEpisodeNumbers() []EpisodeNumber {
	out := append([]EpisodeNumber(nil), p.EpisodeNumbers...)
	seen := make(map[string]bool, len(out))
	for _, number := range out {
		seen[number.System+"\x00"+number.EpisodeNumber] = true
	}

	entry, ok := p.tmdbEntry()
	if !ok {
		return out
	}
	add := func(system, value string) {
		if value == "" {
			return
		}
		key := system + "\x00" + value
		if seen[key] {
			return
		}
		out = append(out, EpisodeNumber{System: system, EpisodeNumber: value})
		seen[key] = true
	}
	if entry.IMDbID != "" {
		add("imdb.com", entry.IMDbID)
	}
	if entry.TVDBID != 0 {
		value := fmt.Sprintf("%d", entry.TVDBID)
		if !p.isMovie() {
			value = "series/" + value
		}
		add("thetvdb.com", value)
	}
	return out
}

func (p Program) EnrichedImages() []Image {
	out := append([]Image(nil), p.Images...)
	entry, ok := p.tmdbEntry()
	if !ok || entry.BackdropURL == "" {
		return out
	}
	for _, image := range out {
		if image.URL == entry.BackdropURL {
			return out
		}
	}
	return append(out, Image{
		URL: entry.BackdropURL, Type: "backdrop", Size: "3", Orient: "L", System: "tmdb",
	})
}

func (p Program) TMDBCredits() Credits {
	entry, ok := p.tmdbEntry()
	if !ok {
		return Credits{}
	}
	credits := Credits{}
	if strings.TrimSpace(entry.Director) != "" {
		credits.Directors = []string{xmlEscape(entry.Director)}
	}
	for _, actor := range entry.Cast {
		if actor = strings.TrimSpace(actor); actor != "" {
			credits.Actors = append(credits.Actors, xmlEscape(actor))
		}
	}
	return credits
}

func (p Program) HasTMDBCredits() bool {
	credits := p.TMDBCredits()
	return len(credits.Directors) > 0 || len(credits.Actors) > 0
}

func (p Program) EffectiveDate() string {
	entry, ok := p.tmdbEntry()
	if ok && entry.ReleaseDate != "" {
		return entry.ReleaseDate
	}
	return p.Date
}

func (p Program) EffectiveRating() string {
	if p.Rating != "" {
		return p.Rating
	}
	entry, ok := p.tmdbEntry()
	if ok {
		return entry.Certification
	}
	return ""
}

func (p Program) EffectiveRatingSystem() string {
	if p.Rating != "" {
		return p.RatingSystem
	}
	entry, ok := p.tmdbEntry()
	if !ok || entry.Certification == "" {
		return ""
	}
	if p.isMovie() {
		return "MPAA"
	}
	return "USA Parental Rating"
}
