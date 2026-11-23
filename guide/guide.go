package guide

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/daniel-widrick/GraceNoteScraper/tmdb"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

type TVGuide struct {
	Lineup         []LineupPosition
	Source         Source
	Channels       []Channel
	Programs       []Program
	LineupChannels []Channel
	// Persisted only to resume interrupted initial enrichment without new grids.
	TMDBPending      bool      `json:",omitempty"`
	TMDBPendingSince time.Time `json:",omitempty"`
}

// Source identifies the Gracenote lineup a guide was built from.
type Source struct {
	Country     string
	PostalCode  string
	HeadendID   string
	LineupID    string
	Device      string
	Language    string
	GeneratedAt time.Time
}

// SourceFromPreferences copies the request preferences into a Source.
func SourceFromPreferences(p web.Preferences, generatedAt time.Time) Source {
	return Source{
		Country:     p.Country,
		PostalCode:  p.ZipCode,
		HeadendID:   p.Headend,
		LineupID:    p.LineupId,
		Device:      p.Device,
		Language:    p.Language,
		GeneratedAt: generatedAt,
	}
}

// LineupPosition is one channel number in a provider lineup.
type LineupPosition struct {
	ChannelNo         string
	StationID         string
	PlacementID       string // Gracenote row id; carried for fidelity, not a stable key
	CallSign          string
	Affiliate         string
	AffiliateCallSign string
	Filters           []string
	LogoURL           string
}

// Key identifies a position across grid slices: the same station at the same
// number is one position no matter how many responses it appears in.
func (p LineupPosition) Key() string {
	return p.ChannelNo + "|" + p.StationID
}

// ConvertLineupPosition converts a JSON channel row to a lineup position.
func ConvertLineupPosition(ch web.JSONChannel) LineupPosition {
	return LineupPosition{
		ChannelNo:         ch.ChannelNo,
		StationID:         ch.ChannelID,
		PlacementID:       ch.ID,
		CallSign:          ch.CallSign,
		Affiliate:         ch.AffiliateName,
		AffiliateCallSign: normalizeNull(ch.AffiliateCallSign),
		Filters:           stripFilterPrefixes(ch.StationFilters),
		LogoURL:           gracenoteIconURL(ch.Thumbnail),
	}
}

// ChannelNumberLess orders channel numbers numerically where both parse
// (so "2.1" < "10" < "100"), places numeric numbers before non-numeric ones,
// and falls back to string order. Equal numbers compare as strings so the
// ordering is strict.
func ChannelNumberLess(a, b string) bool {
	af, errA := strconv.ParseFloat(strings.TrimSpace(a), 64)
	bf, errB := strconv.ParseFloat(strings.TrimSpace(b), 64)
	switch {
	case errA == nil && errB == nil:
		if af != bf {
			return af < bf
		}
		return a < b
	case errA == nil:
		return true
	case errB == nil:
		return false
	default:
		return a < b
	}
}

// SortLineup orders positions by channel number, then station ID, in place.
func SortLineup(positions []LineupPosition) {
	sort.SliceStable(positions, func(i, j int) bool {
		if positions[i].ChannelNo != positions[j].ChannelNo {
			return ChannelNumberLess(positions[i].ChannelNo, positions[j].ChannelNo)
		}
		return positions[i].StationID < positions[j].StationID
	})
}

type Channel struct {
	AffiliateCallSign string
	Filters           []string
	ID                string
	DisplayNames      []DisplayName
	IconURL           string
	CallSign          string   // internal, not in template
	Affiliate         string   // internal, not in template
	ChannelNo         string   // internal, not in template
	PlacementID       string   // lineup position id, internal and not in template
	EventCallSigns    []string // observed event callsigns, internal and not in template
}

type DisplayName struct {
	Name string
}

type Program struct {
	Filters     []string
	TMSID       string
	ReleaseYear string
	Generic     bool
	// Keep optional TMDB evidence separate from Gracenote categories.
	TMDBGenreIDs       []int    `json:"TMDBGenreIDs,omitempty"`
	TMDBGenreNames     []string `json:"TMDBGenreNames,omitempty"`
	TMDBMediaType      string   `json:"TMDBMediaType,omitempty"`
	TMDBGenresCaptured bool     `json:"TMDBGenresCaptured,omitempty"`
	Start              string
	Stop               string
	Channel            string
	Lang               string
	Title              string
	SubTitle           string
	Description        string
	LengthUnits        string
	Length             string
	IconSrc            string
	Images             []Image
	URL                string
	Language           string
	OrigLanguage       string
	Country            string
	EpisodeNumbers     []EpisodeNumber
	Categories         []Category
	// RawFilters preserves response-level Gracenote filters before channel and
	// TMDB-derived categories are injected into Categories. Schedule inference
	// must use this field; legacy mixed Categories are not safe to reinterpret.
	RawFilters      []string `json:"RawFilters,omitempty"`
	New             bool
	Premiere        bool
	PreviouslyShown bool
	Subtitles       []Subtitle
	Rating          string
	RatingSystem    string
	StarRating      string
	Date            string
}

type Image struct {
	URL    string
	Type   string
	Size   string
	Orient string
	System string
}

type EpisodeNumber struct {
	System        string
	EpisodeNumber string
}

type Category struct {
	Name string
	Lang string
}

type Subtitle struct {
	Type string
}

// escapes &, <, > for safe XML text content.
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// converts "2025-08-06T02:00:00Z" to "20250806020000 +0000"
func formatXMLTVTime(iso string) string {
	s := iso
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "T", "")
	s = strings.ReplaceAll(s, ":", "")
	s = strings.ReplaceAll(s, "Z", " +0000")
	return s
}

// appendCanonicalCategory adds a category case-insensitively. When the same
// category already came from Gracenote, its spelling is normalized to the
// requested canonical form instead of adding a duplicate.
func appendCanonicalCategory(categories []Category, name, lang string) []Category {
	name = strings.TrimSpace(name)
	if name == "" {
		return categories
	}
	for i := range categories {
		if strings.EqualFold(strings.TrimSpace(categories[i].Name), name) {
			categories[i].Name = name
			if categories[i].Lang == "" {
				categories[i].Lang = lang
			}
			return categories
		}
	}
	return append(categories, Category{Name: name, Lang: lang})
}

// converts a JSON channel to a template Channel struct.
func ConvertChannel(ch web.JSONChannel) Channel {
	// Register whether this channel remains in the guide but should be excluded
	// from TMDB enrichment. This does not alter the returned channel or events.
	tmdb.RegisterChannelEligibility(ch.ChannelID, ch.CallSign, ch.AffiliateName, ch.ChannelNo)

	// Dedicated news and sports networks add an XMLTV category to each of their
	// programmes. This classification is independent of enrichment eligibility.
	tmdb.RegisterChannelProgramCategories(ch.ChannelID, ch.CallSign, ch.AffiliateName, ch.ChannelNo)

	// Build icon URL: strip leading slashes, strip query params, prepend http://
	iconURL := ""
	if ch.Thumbnail != "" {
		raw := ch.Thumbnail
		// Strip query string
		if idx := strings.Index(raw, "?"); idx >= 0 {
			raw = raw[:idx]
		}
		raw = strings.TrimLeft(raw, "/")
		if raw != "" {
			iconURL = "http://" + raw
		}
	}

	eventCallSigns := make([]string, 0, 1)
	seenCallSigns := make(map[string]bool)
	for _, event := range ch.Events {
		callSign := strings.TrimSpace(event.CallSign)
		if callSign == "" || seenCallSigns[callSign] {
			continue
		}
		seenCallSigns[callSign] = true
		eventCallSigns = append(eventCallSigns, callSign)
	}

	return Channel{
		AffiliateCallSign: normalizeNull(ch.AffiliateCallSign),
		Filters:           stripFilterPrefixes(ch.StationFilters),
		ID:                ch.ChannelID,
		DisplayNames: []DisplayName{
			{Name: xmlEscape(ch.ChannelNo + " " + ch.CallSign)},
			{Name: xmlEscape(ch.ChannelNo)},
			{Name: xmlEscape(ch.CallSign)},
			{Name: xmlEscape(titleCase(ch.AffiliateName))},
		},
		IconURL:        iconURL,
		CallSign:       ch.CallSign,
		Affiliate:      ch.AffiliateName,
		ChannelNo:      ch.ChannelNo,
		PlacementID:    ch.ID,
		EventCallSigns: eventCallSigns,
	}
}

// converts a JSON event to a template Program struct.
func ConvertEvent(ev web.JSONEvent, channelID, lang, country string) Program {
	season := 0
	episode := 0

	if ev.Program.Season != nil {
		if v, err := strconv.Atoi(*ev.Program.Season); err == nil {
			season = v
		}
	}
	if ev.Program.Episode != nil {
		if v, err := strconv.Atoi(*ev.Program.Episode); err == nil {
			episode = v
		}
	}

	// SubTitle
	subTitle := ""
	if ev.Program.EpisodeTitle != nil {
		subTitle = xmlEscape(*ev.Program.EpisodeTitle)
	}

	// Description
	desc := "Unavailable"
	if ev.Program.ShortDesc != nil {
		desc = xmlEscape(*ev.Program.ShortDesc)
	}

	// Icon URL
	iconSrc := ""
	if ev.Thumbnail != "" {
		iconSrc = "http://zap2it.tmsimg.com/assets/" + ev.Thumbnail + ".jpg"
	}

	// URL
	programURL := "https://tvlistings.gracenote.com//overview.html?programSeriesId=" + ev.SeriesID + "&amp;tmsId=" + ev.Program.ID

	// Categories from filter array (strip "filter-" prefix)
	var categories []Category
	rawFilters := make([]string, 0, len(ev.Filter))
	isMovie := false
	for _, f := range ev.Filter {
		name := strings.TrimPrefix(strings.TrimSpace(f), "filter-")
		categories = append(categories, Category{Name: name, Lang: lang})
		if name != "" {
			rawFilters = append(rawFilters, name)
		}
		if name == "movie" {
			isMovie = true
		}
	}

	// XMLTV categories belong to programmes, so programmes on dedicated news or
	// sports networks inherit the channel's canonical category. The channel
	// remains eligible for TMDB enrichment and is never removed from the guide.
	for _, category := range tmdb.ChannelProgramCategories(channelID) {
		categories = appendCanonicalCategory(categories, category, lang)
	}

	// Record whether this title occurs in a TMDB-eligible context. Categories
	// affect only this programme/title lookup and never classify its channel.
	tmdb.RegisterProgramEligibility(ev.Program.Title, isMovie, channelID, ev.Filter)

	// Episode numbers
	var episodeNumbers []EpisodeNumber

	if episode != 0 {
		// Add "Series" category
		categories = append(categories, Category{Name: "Series", Lang: lang})

		// onscreen: S01E05
		onscreen := fmt.Sprintf("S%02dE%02d", season, episode)
		episodeNumbers = append(episodeNumbers, EpisodeNumber{
			System:        "onscreen",
			EpisodeNumber: onscreen,
		})

		// xmltv_ns: season-1.episode-1
		seasonStr := ""
		if season != 0 {
			seasonStr = fmt.Sprintf("%d", season-1)
		}
		xmltvNS := fmt.Sprintf("%s.%d", seasonStr, episode-1)
		episodeNumbers = append(episodeNumbers, EpisodeNumber{
			System:        "xmltv_ns",
			EpisodeNumber: xmltvNS,
		})
	}

	// dd_progid
	progID := ev.Program.ID
	suffix := ""
	if len(progID) >= 4 {
		suffix = progID[len(progID)-4:]
	}
	var ddProgID string
	if suffix == "0000" {
		ddProgID = ev.SeriesID + "." + suffix
	} else {
		ddProgID = strings.Replace(ev.SeriesID, "SH", "EP", 1) + "." + suffix
	}
	episodeNumbers = append(episodeNumbers, EpisodeNumber{
		System:        "dd_progid",
		EpisodeNumber: ddProgID,
	})

	// Flags
	isNew := false
	isPremiere := false
	for _, flag := range ev.Flag {
		switch flag {
		case "New":
			isNew = true
		case "Premiere":
			isPremiere = true
		case "Finale":
			// Map Finale to a category since it's not in the XMLTV DTD
			categories = append(categories, Category{Name: "Finale", Lang: lang})
		}
	}

	// Subtitles from tags
	var subtitles []Subtitle
	for _, tag := range ev.Tags {
		if tag == "CC" {
			subtitles = append(subtitles, Subtitle{Type: "teletext"})
		}
	}

	// Rating
	rating := ""
	if ev.Rating != nil {
		rating = *ev.Rating
	}

	// Rating system - detect from value format
	ratingSystem := ""
	if rating != "" {
		if strings.HasPrefix(rating, "TV-") {
			ratingSystem = "USA Parental Rating"
		}
	}

	p := Program{
		Filters:         stripFilterPrefixes(ev.Filter),
		TMSID:           ev.Program.TmsID,
		ReleaseYear:     string(ev.Program.ReleaseYear),
		Generic:         bool(ev.Program.IsGeneric),
		Start:           formatXMLTVTime(ev.StartTime),
		Stop:            formatXMLTVTime(ev.EndTime),
		Channel:         channelID,
		Lang:            lang,
		Title:           xmlEscape(ev.Program.Title),
		SubTitle:        subTitle,
		Description:     desc,
		LengthUnits:     "minutes",
		Length:          ev.Duration,
		IconSrc:         iconSrc,
		URL:             programURL,
		Language:        lang,
		Country:         country,
		EpisodeNumbers:  episodeNumbers,
		Categories:      categories,
		RawFilters:      rawFilters,
		New:             isNew,
		Premiere:        isPremiere,
		PreviouslyShown: !isNew,
		Subtitles:       subtitles,
		Rating:          rating,
		RatingSystem:    ratingSystem,
	}

	return p
}

// uppercases the first letter of each word (simple replacement for deprecated strings.Title).
func titleCase(s string) string {
	prev := ' '
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(rune(prev)) || prev == ' ' {
			prev = r
			return unicode.ToUpper(r)
		}
		prev = r
		return r
	}, s)
}

func gracenoteIconURL(thumbnail string) string {
	if thumbnail == "" {
		return ""
	}
	raw := thumbnail
	if idx := strings.Index(raw, "?"); idx >= 0 {
		raw = raw[:idx]
	}
	raw = strings.TrimLeft(raw, "/")
	if raw == "" {
		return ""
	}
	return "http://" + raw
}

// converts a JSON channel to a template Channel struct.

func normalizeNull(s string) string {
	if strings.EqualFold(strings.TrimSpace(s), "null") {
		return ""
	}
	return s
}

// stripFilterPrefixes turns Gracenote filter tags such as "filter-sports" into
// "sports". A nil input stays nil so callers can distinguish absent from empty.
func stripFilterPrefixes(filters []string) []string {
	if filters == nil {
		return nil
	}
	out := make([]string, 0, len(filters))
	for _, f := range filters {
		out = append(out, strings.TrimPrefix(f, "filter-"))
	}
	return out
}

// converts a JSON event to a template Program struct.
