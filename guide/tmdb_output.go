package guide

import (
	"fmt"
	"html"
	"strings"

	"github.com/daniel-widrick/GraceNoteScraper/tmdb"
)

type XMLTVCredit struct {
	Name string
	Role string
}

type XMLTVRating struct {
	System string
	Value  string
}

func (p Program) tmdbEntry() (tmdb.CacheEntry, bool) {
	isMovie := false
	for _, category := range p.Categories {
		if strings.EqualFold(category.Name, "movie") {
			isMovie = true
			break
		}
	}
	return tmdb.LookupCompleted(strings.ToLower(html.UnescapeString(p.Title)), isMovie)
}

func (p Program) TMDBCategories() []Category {
	entry, ok := p.tmdbEntry()
	if !ok {
		return nil
	}
	seen := make(map[string]bool)
	for _, category := range p.Categories {
		seen[strings.ToLower(category.Name)] = true
	}
	var out []Category
	for _, genre := range entry.Genres {
		key := strings.ToLower(genre)
		if genre != "" && !seen[key] {
			seen[key] = true
			out = append(out, Category{Name: xmlEscape(genre), Lang: p.Lang})
		}
	}
	return out
}

func (p Program) TMDBKeywords() []string {
	entry, ok := p.tmdbEntry()
	if !ok {
		return nil
	}
	out := make([]string, 0, len(entry.Keywords))
	for _, keyword := range entry.Keywords {
		if strings.TrimSpace(keyword) != "" {
			out = append(out, xmlEscape(keyword))
		}
	}
	return out
}

func (p Program) TMDBCredits() []XMLTVCredit {
	entry, ok := p.tmdbEntry()
	if !ok {
		return nil
	}
	out := make([]XMLTVCredit, 0, len(entry.Credits))
	for _, credit := range entry.Credits {
		if strings.TrimSpace(credit.Name) != "" {
			out = append(out, XMLTVCredit{Name: xmlEscape(credit.Name), Role: strings.ToLower(credit.Role)})
		}
	}
	return out
}

func (p Program) TMDBEpisodeNumbers() []EpisodeNumber {
	entry, ok := p.tmdbEntry()
	if !ok {
		return nil
	}
	var out []EpisodeNumber
	if entry.IMDbID != "" {
		out = append(out, EpisodeNumber{System: "imdb.com", EpisodeNumber: entry.IMDbID})
	}
	if entry.TVDBID != 0 {
		out = append(out, EpisodeNumber{System: "thetvdb.com", EpisodeNumber: fmt.Sprintf("series/%d", entry.TVDBID)})
	}
	return out
}

func (p Program) TMDBImages() []Image {
	entry, ok := p.tmdbEntry()
	if !ok || entry.BackdropURL == "" {
		return nil
	}
	return []Image{{URL: entry.BackdropURL, Type: "backdrop", Size: "3", Orient: "L", System: "tmdb"}}
}

func (p Program) TMDBRating() *XMLTVRating {
	if p.Rating != "" {
		return nil
	}
	entry, ok := p.tmdbEntry()
	if !ok || entry.Certification == "" {
		return nil
	}
	return &XMLTVRating{System: "TMDB", Value: xmlEscape(entry.Certification)}
}

func (p Program) TMDBDate() string {
	entry, ok := p.tmdbEntry()
	if !ok {
		return ""
	}
	return entry.ReleaseDate
}
