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
		seen[normalizeCategoryName(category.Name)] = true
	}

	var out []Category
	appendCategory := func(name string) {
		name = strings.TrimSpace(name)
		key := normalizeCategoryName(name)
		if name == "" || key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, Category{Name: xmlEscape(name), Lang: p.Lang})
	}

	// TMDB genres are already suitable as XMLTV categories.
	for _, genre := range entry.Genres {
		appendCategory(genre)
	}

	// Channels DVR does not expose XMLTV <keyword> values, so promote only a
	// conservative, curated subset to <category>. The original <keyword>
	// elements are still emitted for XMLTV consumers that support them.
	for _, keyword := range entry.Keywords {
		if category, ok := promotedKeywordCategory(keyword); ok {
			appendCategory(category)
		}
	}

	return out
}

func normalizeCategoryName(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(html.UnescapeString(value))), " ")
}

func promotedKeywordCategory(keyword string) (string, bool) {
	normalized := normalizeCategoryName(keyword)
	category, ok := tmdbKeywordCategories[normalized]
	return category, ok
}

var tmdbKeywordCategories = map[string]string{
	"sitcom":                       "Sitcom",
	"situation comedy":             "Sitcom",
	"true crime":                   "True Crime",
	"police":                       "Police",
	"police investigation":         "Police",
	"courtroom":                    "Courtroom",
	"courtroom drama":              "Courtroom",
	"medical":                      "Medical",
	"medical drama":                "Medical",
	"cooking":                      "Cooking",
	"cooking show":                 "Cooking",
	"travel":                       "Travel",
	"travel show":                  "Travel",
	"history":                      "History",
	"historical documentary":       "History",
	"military":                     "Military",
	"espionage":                    "Espionage",
	"spy":                          "Espionage",
	"superhero":                    "Superhero",
	"holiday":                      "Holiday",
	"christmas":                    "Christmas",
	"western":                      "Western",
	"documentary":                  "Documentary",
	"reality":                      "Reality",
	"reality competition":          "Competition",
	"competition":                  "Competition",
	"home improvement":             "Home Improvement",
	"home renovation":              "Home Improvement",
	"automotive":                   "Automotive",
	"cars":                         "Automotive",
	"nature":                       "Nature",
	"wildlife":                     "Nature",
	"science":                      "Science",
	"science documentary":          "Science",
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
