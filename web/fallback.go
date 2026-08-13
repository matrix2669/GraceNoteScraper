package web

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const gridFallbackWindow = 6 * time.Hour

// The guide cache uses the exported guide package field names. Local mirror
// structs avoid an import cycle because guide itself imports this web package.
type cachedGuideFile struct {
	SavedAt time.Time     `json:"saved_at"`
	Source  GuideSource   `json:"source"`
	Guide   cachedTVGuide `json:"guide"`
}

type cachedTVGuide struct {
	Channels []cachedChannel `json:"Channels"`
	Programs []cachedProgram `json:"Programs"`
}

type cachedChannel struct {
	ID           string              `json:"ID"`
	DisplayNames []cachedDisplayName `json:"DisplayNames"`
	IconURL      string              `json:"IconURL"`
	CallSign     string              `json:"CallSign"`
	Affiliate    string              `json:"Affiliate"`
	ChannelNo    string              `json:"ChannelNo"`
}

type cachedDisplayName struct {
	Name string `json:"Name"`
}

type cachedProgram struct {
	Start          string                `json:"Start"`
	Stop           string                `json:"Stop"`
	Channel        string                `json:"Channel"`
	Title          string                `json:"Title"`
	SubTitle       string                `json:"SubTitle"`
	Description    string                `json:"Description"`
	Length         string                `json:"Length"`
	IconSrc        string                `json:"IconSrc"`
	URL            string                `json:"URL"`
	EpisodeNumbers []cachedEpisodeNumber `json:"EpisodeNumbers"`
	Categories     []cachedCategory      `json:"Categories"`
	New            bool                  `json:"New"`
	Premiere       bool                  `json:"Premiere"`
	Subtitles      []cachedSubtitle      `json:"Subtitles"`
	Rating         string                `json:"Rating"`
}

type cachedEpisodeNumber struct {
	System        string `json:"System"`
	EpisodeNumber string `json:"EpisodeNumber"`
}

type cachedCategory struct {
	Name string `json:"Name"`
}

type cachedSubtitle struct {
	Type string `json:"Type"`
}

func loadFallbackGrid(path string, unixTime int64, expectedSource GuideSource) (*GridResponse, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read guide cache: %w", err)
	}
	var cached cachedGuideFile
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, fmt.Errorf("decode guide cache: %w", err)
	}
	if cached.Source != expectedSource {
		if cached.Source == (GuideSource{}) {
			return nil, fmt.Errorf("guide cache has no Gracenote source fingerprint")
		}
		return nil, fmt.Errorf(
			"guide cache source mismatch: cached lineup=%s zip=%s, current lineup=%s zip=%s",
			cached.Source.LineupID, cached.Source.ZipCode, expectedSource.LineupID, expectedSource.ZipCode,
		)
	}

	windowStart := time.Unix(unixTime, 0).UTC()
	windowEnd := windowStart.Add(gridFallbackWindow)
	channelsByID := make(map[string]cachedChannel, len(cached.Guide.Channels))
	for _, channel := range cached.Guide.Channels {
		channelsByID[channel.ID] = channel
	}

	eventsByChannel := make(map[string][]JSONEvent)
	eventCount := 0
	for _, program := range cached.Guide.Programs {
		start, startErr := parseXMLTVTime(program.Start)
		stop, stopErr := parseXMLTVTime(program.Stop)
		if startErr != nil || stopErr != nil || !start.Before(windowEnd) || !stop.After(windowStart) {
			continue
		}
		eventsByChannel[program.Channel] = append(eventsByChannel[program.Channel], cachedProgramToEvent(program))
		eventCount++
	}
	if eventCount == 0 {
		return nil, fmt.Errorf("guide cache contains no programmes overlapping %s to %s",
			windowStart.Format(time.RFC3339), windowEnd.Format(time.RFC3339))
	}

	grid := &GridResponse{Channels: make([]JSONChannel, 0, len(eventsByChannel))}
	added := make(map[string]bool)
	for _, channel := range cached.Guide.Channels {
		events := eventsByChannel[channel.ID]
		if len(events) == 0 {
			continue
		}
		grid.Channels = append(grid.Channels, cachedChannelToJSON(channel, events))
		added[channel.ID] = true
	}

	var unknownIDs []string
	for channelID := range eventsByChannel {
		if !added[channelID] {
			unknownIDs = append(unknownIDs, channelID)
		}
	}
	sort.Strings(unknownIDs)
	for _, channelID := range unknownIDs {
		channel := channelsByID[channelID]
		channel.ID = channelID
		grid.Channels = append(grid.Channels, cachedChannelToJSON(channel, eventsByChannel[channelID]))
	}

	age := time.Since(cached.SavedAt).Round(time.Second)
	fmt.Printf("Gracenote cached fallback: %d events across %d channels for %s to %s (cache age %s)\n",
		eventCount, len(grid.Channels), windowStart.Format(time.RFC3339), windowEnd.Format(time.RFC3339), age)
	return grid, nil
}

func cachedChannelToJSON(channel cachedChannel, events []JSONEvent) JSONChannel {
	channelNo := channel.ChannelNo
	callSign := channel.CallSign
	if channelNo == "" && len(channel.DisplayNames) > 1 {
		channelNo = html.UnescapeString(channel.DisplayNames[1].Name)
	}
	if callSign == "" && len(channel.DisplayNames) > 2 {
		callSign = html.UnescapeString(channel.DisplayNames[2].Name)
	}
	return JSONChannel{
		ChannelID:     channel.ID,
		ChannelNo:     channelNo,
		CallSign:      callSign,
		AffiliateName: html.UnescapeString(channel.Affiliate),
		Thumbnail:     cachedChannelThumbnail(channel.IconURL),
		Events:        events,
	}
}

func cachedProgramToEvent(program cachedProgram) JSONEvent {
	seriesID, programID := cachedProgramIDs(program)
	season, episode := cachedSeasonEpisode(program.EpisodeNumbers)

	var episodeTitle *string
	if value := strings.TrimSpace(html.UnescapeString(program.SubTitle)); value != "" {
		episodeTitle = &value
	}
	var description *string
	if value := strings.TrimSpace(html.UnescapeString(program.Description)); value != "" && value != "Unavailable" {
		description = &value
	}
	var rating *string
	if value := strings.TrimSpace(program.Rating); value != "" {
		rating = &value
	}

	flags := make([]string, 0, 2)
	if program.New {
		flags = append(flags, "New")
	}
	if program.Premiere {
		flags = append(flags, "Premiere")
	}

	var tags []string
	for _, subtitle := range program.Subtitles {
		if strings.EqualFold(subtitle.Type, "teletext") {
			tags = append(tags, "CC")
			break
		}
	}

	seenFilters := make(map[string]bool)
	var filters []string
	for _, category := range program.Categories {
		name := strings.ToLower(strings.TrimSpace(html.UnescapeString(category.Name)))
		if name == "" || name == "series" {
			continue
		}
		filter := "filter-" + name
		if !seenFilters[filter] {
			seenFilters[filter] = true
			filters = append(filters, filter)
		}
	}

	return JSONEvent{
		StartTime: xmltvToISO(program.Start),
		EndTime:   xmltvToISO(program.Stop),
		Duration:  program.Length,
		Thumbnail: cachedAssetThumbnailID(program.IconSrc),
		SeriesID:  seriesID,
		Rating:    rating,
		Flag:      flags,
		Tags:      tags,
		Filter:    filters,
		Program: JSONProgram{
			ID:           programID,
			Title:        html.UnescapeString(program.Title),
			EpisodeTitle: episodeTitle,
			ShortDesc:    description,
			Season:       season,
			Episode:      episode,
		},
	}
}

func parseXMLTVTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if len(value) < 14 {
		return time.Time{}, fmt.Errorf("invalid XMLTV time %q", value)
	}
	return time.Parse("20060102150405", value[:14])
}

func xmltvToISO(value string) string {
	parsed, err := parseXMLTVTime(value)
	if err != nil {
		return value
	}
	return parsed.UTC().Format(time.RFC3339)
}

func cachedProgramIDs(program cachedProgram) (seriesID, programID string) {
	if parsed, err := url.Parse(html.UnescapeString(program.URL)); err == nil {
		seriesID = parsed.Query().Get("programSeriesId")
		programID = parsed.Query().Get("tmsId")
	}
	for _, number := range program.EpisodeNumbers {
		if number.System != "dd_progid" {
			continue
		}
		value := strings.TrimSpace(number.EpisodeNumber)
		if seriesID == "" {
			seriesID = strings.SplitN(value, ".", 2)[0]
		}
		if programID == "" {
			programID = strings.ReplaceAll(value, ".", "")
		}
		break
	}
	if programID == "" && seriesID != "" {
		programID = seriesID + "0000"
	}
	return seriesID, programID
}

func cachedSeasonEpisode(numbers []cachedEpisodeNumber) (season, episode *string) {
	for _, number := range numbers {
		if number.System != "onscreen" {
			continue
		}
		var seasonValue, episodeValue int
		if _, err := fmt.Sscanf(strings.ToUpper(number.EpisodeNumber), "S%dE%d", &seasonValue, &episodeValue); err == nil {
			seasonText := fmt.Sprintf("%d", seasonValue)
			episodeText := fmt.Sprintf("%d", episodeValue)
			return &seasonText, &episodeText
		}
	}
	return nil, nil
}

func unwrapCachedURL(raw string) string {
	raw = html.UnescapeString(strings.TrimSpace(raw))
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if target := parsed.Query().Get("url"); target != "" {
		if decoded, err := url.QueryUnescape(target); err == nil {
			return decoded
		}
		return target
	}
	return raw
}

func cachedAssetThumbnailID(raw string) string {
	raw = unwrapCachedURL(raw)
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	marker := "/assets/"
	index := strings.Index(parsed.Path, marker)
	if index < 0 {
		return ""
	}
	name := strings.TrimPrefix(parsed.Path[index:], marker)
	name = strings.TrimSuffix(name, ".jpg")
	if strings.Contains(name, "/") {
		return ""
	}
	return name
}

func cachedChannelThumbnail(raw string) string {
	raw = unwrapCachedURL(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return "//" + parsed.Host + parsed.EscapedPath()
}
