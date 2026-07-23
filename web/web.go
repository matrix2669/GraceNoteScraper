package web

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/util"
)

const (
	userAgent          = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_9_3) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/35.0.1916.47 Safari/537.36"
	gridCachePath      = "guide_cache.json"
	gridFallbackWindow = 6 * time.Hour
	gridMaxAttempts    = 4 // initial request plus three retries
)

var gridRetryDelays = []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second}

// JSON response structs matching the Gracenote grid API
type GridResponse struct {
	Channels []JSONChannel `json:"channels"`
}

type JSONChannel struct {
	ChannelID     string      `json:"channelId"`
	ChannelNo     string      `json:"channelNo"`
	CallSign      string      `json:"callSign"`
	AffiliateName string      `json:"affiliateName"`
	Thumbnail     string      `json:"thumbnail"`
	Events        []JSONEvent `json:"events"`
}

type JSONEvent struct {
	StartTime string      `json:"startTime"`
	EndTime   string      `json:"endTime"`
	Duration  string      `json:"duration"`
	Thumbnail string      `json:"thumbnail"`
	SeriesID  string      `json:"seriesId"`
	Rating    *string     `json:"rating"`
	Flag      []string    `json:"flag"`
	Tags      []string    `json:"tags"`
	Filter    []string    `json:"filter"`
	Program   JSONProgram `json:"program"`
}

type JSONProgram struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	EpisodeTitle *string `json:"episodeTitle"`
	ShortDesc    *string `json:"shortDesc"`
	Season       *string `json:"season"`
	Episode      *string `json:"episode"`
}

type Preferences struct {
	Country  string
	ZipCode  string
	Headend  string
	LineupId string
	Device   string
	Language string
}

type Client struct {
	*http.Client
	pref Preferences
}

func (c *Client) GetDataByTime(t int64) (*GridResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= gridMaxAttempts; attempt++ {
		grid, err := c.getDataByTimeOnce(t)
		if err == nil {
			if attempt > 1 {
				log.Printf("Gracenote grid time=%d succeeded on attempt %d/%d", t, attempt, gridMaxAttempts)
			}
			return grid, nil
		}
		lastErr = err
		if attempt == gridMaxAttempts {
			break
		}
		delay := gridRetryDelays[attempt-1]
		log.Printf("Gracenote grid time=%d attempt %d/%d failed: %v; retrying in %s",
			t, attempt, gridMaxAttempts, err, delay)
		time.Sleep(delay)
	}

	fallback, err := loadFallbackGrid(gridCachePath, t)
	if err == nil {
		log.Printf("Gracenote grid time=%d exhausted %d attempts; using previous guide data for this six-hour window",
			t, gridMaxAttempts)
		return fallback, nil
	}
	return nil, fmt.Errorf("Gracenote grid time=%d failed after %d attempts (%v); cached fallback unavailable: %w",
		t, gridMaxAttempts, lastErr, err)
}

func (c *Client) getDataByTimeOnce(t int64) (*GridResponse, error) {
	log.Printf("headendId=%s lineupId=%s zipCode=%s", c.pref.Headend, c.pref.LineupId, c.pref.ZipCode)

	params := url.Values{
		"aid":          {"orbebb"},
		"lineupId":     {c.pref.LineupId},
		"timespan":     {"6"},
		"headendId":    {c.pref.Headend},
		"country":      {c.pref.Country},
		"device":       {c.pref.Device},
		"postalCode":   {c.pref.ZipCode},
		"isOverride":   {"true"},
		"time":         {fmt.Sprintf("%d", t)},
		"timezone":     {""},
		"pref":         {"16,256"},
		"userId":       {"-"},
		"languagecode": {c.pref.Language},
	}
	gridURL := "https://tvlistings.gracenote.com/api/grid?" + params.Encode()
	log.Printf("Fetching: %s", gridURL)
	req, err := http.NewRequest("GET", gridURL, nil)
	if err != nil {
		return nil, fmt.Errorf("GetDataByTime failed to build request: %w", err)
	}
	req.Header.Set("Referer", "https://tvlistings.gracenote.com/grid-affiliates.html?aid=orbebb")
	req.Header.Set("X-Requested-Width", "XMLHttpRequest")
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GetDataByTime request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("guide API returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read guide body: %w", err)
	}

	var grid GridResponse
	if err := json.Unmarshal(b, &grid); err != nil {
		return nil, fmt.Errorf("unable to parse guide JSON: %w", err)
	}
	return &grid, nil
}

// The guide cache uses the exported guide package field names. Local mirror
// structs avoid an import cycle because guide itself imports this web package.
type cachedGuideFile struct {
	SavedAt time.Time     `json:"saved_at"`
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
	Start           string                `json:"Start"`
	Stop            string                `json:"Stop"`
	Channel         string                `json:"Channel"`
	Title           string                `json:"Title"`
	SubTitle        string                `json:"SubTitle"`
	Description     string                `json:"Description"`
	Length          string                `json:"Length"`
	IconSrc         string                `json:"IconSrc"`
	URL             string                `json:"URL"`
	EpisodeNumbers  []cachedEpisodeNumber `json:"EpisodeNumbers"`
	Categories      []cachedCategory      `json:"Categories"`
	New             bool                  `json:"New"`
	Premiere        bool                  `json:"Premiere"`
	Subtitles       []cachedSubtitle      `json:"Subtitles"`
	Rating          string                `json:"Rating"`
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

func loadFallbackGrid(path string, unixTime int64) (*GridResponse, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read guide cache: %w", err)
	}
	var cached cachedGuideFile
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, fmt.Errorf("decode guide cache: %w", err)
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
		start, err := parseXMLTVTime(program.Start)
		if err != nil || start.Before(windowStart) || !start.Before(windowEnd) {
			continue
		}
		eventsByChannel[program.Channel] = append(eventsByChannel[program.Channel], cachedProgramToEvent(program))
		eventCount++
	}
	if eventCount == 0 {
		return nil, fmt.Errorf("guide cache contains no programmes starting in %s to %s",
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
	log.Printf("Gracenote cached fallback: %d events across %d channels for %s to %s (cache age %s)",
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
		if name == "" {
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

func NewClient() *Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		log.Fatalf("Unable to create cookie storage for http client: %v", err)
		return nil
	}
	return &Client{
		Client: &http.Client{
			Jar:     jar,
			Timeout: 15 * time.Second,
			Transport: &headerTransport{
				rt: http.DefaultTransport,
			},
		},
		pref: Preferences{
			Country:  util.GetEnv("GN_COUNTRY", "USA"),
			ZipCode:  util.GetEnv("GN_ZIPCODE", "13490"),
			Headend:  util.GetEnv("GN_HEADEND", "lineupId"),
			LineupId: util.GetEnv("GN_LINEUP", "USA-lineupId-DEFAULT"),
			Device:   util.GetEnv("GN_DEVICE", "-"),
			Language: util.GetEnv("GN_LANGUAGE", "en-us"),
		},
	}
}

type headerTransport struct {
	rt http.RoundTripper
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", userAgent)
	return t.rt.RoundTrip(req)
}
