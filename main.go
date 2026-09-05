package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	neturl "net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"github.com/daniel-widrick/GraceNoteScraper/geocode"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	"github.com/daniel-widrick/GraceNoteScraper/internal/applog"
	lineuparrbuilder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/providersource"
	"github.com/daniel-widrick/GraceNoteScraper/tmdb"
	"github.com/daniel-widrick/GraceNoteScraper/tvlogo"
	"github.com/daniel-widrick/GraceNoteScraper/util"
	"github.com/daniel-widrick/GraceNoteScraper/web"
	"github.com/joho/godotenv"
)

//go:embed guide.tmpl
var guideTmplFS embed.FS

//go:embed index.html
var indexHTML []byte

//go:embed favicon.svg
var faviconSVG []byte

// ---------- GuideState ----------

// GuideState holds the current guide data, safe for concurrent access.
type GuideState struct {
	mu                sync.RWMutex
	guide             *guide.TVGuide
	sourceFingerprint string
}

func (s *GuideState) UpdateForSource(g *guide.TVGuide, sourceFingerprint string) {
	s.mu.Lock()
	s.guide = g
	s.sourceFingerprint = sourceFingerprint
	s.mu.Unlock()
}

func (s *GuideState) Get() *guide.TVGuide {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.guide
}

func (s *GuideState) GetForSource(sourceFingerprint string) *guide.TVGuide {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.guide == nil || s.sourceFingerprint != sourceFingerprint {
		return nil
	}
	return s.guide
}

// ---------- JSON API types ----------

type APIGuide struct {
	Generated string       `json:"generated"`
	Channels  []APIChannel `json:"channels"`
}

type APIChannel struct {
	ID       string       `json:"id"`
	Number   string       `json:"number"`
	Name     string       `json:"name"`
	LogoURL  string       `json:"logoUrl"`
	Programs []APIProgram `json:"programs"`
}

type APIProgram struct {
	Title       string `json:"title"`
	SubTitle    string `json:"subTitle,omitempty"`
	Start       string `json:"start"`
	End         string `json:"end"`
	Category    string `json:"category,omitempty"`
	IsNew       bool   `json:"isNew,omitempty"`
	Rating      string `json:"rating,omitempty"`
	IconURL     string `json:"iconUrl,omitempty"`
	Description string `json:"description,omitempty"`
}

func currentStationNames(g *guide.TVGuide) map[string][]string {
	stations := make(map[string][]string)
	if g == nil {
		return stations
	}
	for _, channel := range g.Channels {
		if strings.TrimSpace(channel.ID) == "" {
			continue
		}
		names := make([]string, 0, 2)
		if strings.TrimSpace(channel.CallSign) != "" {
			names = append(names, channel.CallSign)
		}
		if strings.TrimSpace(channel.Affiliate) != "" {
			names = append(names, channel.Affiliate)
		}
		stations[channel.ID] = names
	}
	return stations
}

// ---------- Conversion ----------

func apiProgram(p guide.Program) APIProgram {
	category := ""
	if len(p.Categories) > 0 {
		category = p.Categories[0].Name
	}
	description := html.UnescapeString(p.Description)
	if description == "Unavailable" {
		description = ""
	}
	return APIProgram{
		Title: html.UnescapeString(p.Title), SubTitle: html.UnescapeString(p.SubTitle),
		Start: xmltvTimeToISO(p.Start), End: xmltvTimeToISO(p.Stop), Category: category,
		IsNew: p.New, Rating: p.Rating, IconURL: p.IconSrc, Description: description,
	}
}

// guideToJSON converts a TVGuide into the simplified JSON API format.
func guideToJSON(g *guide.TVGuide) APIGuide {
	// Build channel-id -> programs map
	chanProgs := make(map[string][]APIProgram)
	for _, p := range g.Programs {
		chanProgs[p.Channel] = append(chanProgs[p.Channel], apiProgram(p))
	}

	// Sort programs by start time within each channel
	for id := range chanProgs {
		progs := chanProgs[id]
		sort.Slice(progs, func(i, j int) bool {
			return progs[i].Start < progs[j].Start
		})
	}

	var channels []APIChannel
	for _, ch := range g.Channels {
		number := ""
		name := ""
		if len(ch.DisplayNames) >= 3 {
			number = ch.DisplayNames[1].Name // just the number
			name = ch.DisplayNames[2].Name   // just the callsign
		} else if len(ch.DisplayNames) >= 1 {
			name = ch.DisplayNames[0].Name
		}
		channels = append(channels, APIChannel{
			ID:       ch.ID,
			Number:   html.UnescapeString(number),
			Name:     html.UnescapeString(name),
			LogoURL:  ch.IconURL,
			Programs: chanProgs[ch.ID],
		})
	}

	// Sort channels by number (numeric sort)
	sort.Slice(channels, func(i, j int) bool {
		return channelNumberLess(channels[i].Number, channels[j].Number)
	})

	return APIGuide{
		Generated: time.Now().UTC().Format(time.RFC3339),
		Channels:  channels,
	}
}

// channelNumberLess compares channel numbers numerically where possible.
func channelNumberLess(a, b string) bool {
	// Try to parse as float for numeric comparison (handles "5.1", "12", etc.)
	var ai, bi float64
	_, errA := fmt.Sscanf(a, "%f", &ai)
	_, errB := fmt.Sscanf(b, "%f", &bi)
	if errA == nil && errB == nil {
		return ai < bi
	}
	return a < b
}

func mergeLineupChannel(existing, observed guide.Channel) guide.Channel {
	if existing.ID == "" {
		existing.ID = observed.ID
	}
	if existing.PlacementID == "" {
		existing.PlacementID = observed.PlacementID
	}
	if existing.ChannelNo == "" {
		existing.ChannelNo = observed.ChannelNo
	}
	if existing.CallSign == "" {
		existing.CallSign = observed.CallSign
	}
	if existing.Affiliate == "" {
		existing.Affiliate = observed.Affiliate
	}
	if existing.IconURL == "" {
		existing.IconURL = observed.IconURL
	}
	if len(existing.DisplayNames) == 0 {
		existing.DisplayNames = observed.DisplayNames
	}
	seen := make(map[string]bool, len(existing.EventCallSigns)+len(observed.EventCallSigns))
	merged := make([]string, 0, len(existing.EventCallSigns)+len(observed.EventCallSigns))
	for _, values := range [][]string{existing.EventCallSigns, observed.EventCallSigns} {
		for _, value := range values {
			key := strings.ToLower(strings.TrimSpace(value))
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, strings.TrimSpace(value))
		}
	}
	existing.EventCallSigns = merged
	return existing
}

// xmltvTimeToISO converts "20250225200000 +0000" → "2025-02-25T20:00:00Z"
func xmltvTimeToISO(xmltvTime string) string {
	xmltvTime = strings.TrimSpace(xmltvTime)
	// Strip the timezone suffix — we assume +0000 (UTC)
	if idx := strings.Index(xmltvTime, " "); idx >= 0 {
		xmltvTime = xmltvTime[:idx]
	}
	if len(xmltvTime) < 14 {
		return xmltvTime
	}
	return xmltvTime[0:4] + "-" + xmltvTime[4:6] + "-" + xmltvTime[6:8] +
		"T" + xmltvTime[8:10] + ":" + xmltvTime[10:12] + ":" + xmltvTime[12:14] + "Z"
}

// ---------- Scraping ----------

var errScrapeSourceChanged = errors.New("active lineup changed during scrape")

type guidePersister func(*guide.TVGuide) (bool, error)

type scrapeProgressUpdate struct {
	Stage     string
	Message   string
	Completed int
	Total     int
	Channels  int
	Programs  int
}

type scrapeProgressReporter func(scrapeProgressUpdate)

// runScrape performs the full scrape cycle and returns the populated TVGuide.
// It also writes xmlguide.xmltv atomically.
func runScrape(pref web.Preferences, tmdbClient *tmdb.Client, baseURL string, channelFilter map[string]bool, sourceFingerprint string, sourceCurrent func() bool, persister guidePersister, reporters ...scrapeProgressReporter) (*guide.TVGuide, error) {
	report := func(update scrapeProgressUpdate) {
		for _, reporter := range reporters {
			if reporter != nil {
				reporter(update)
			}
		}
	}
	client := web.NewClient(pref)

	now := time.Now().UTC()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	endTime := midnight.Add(14 * 24 * time.Hour)

	channelMap := make(map[string]guide.Channel)
	lineupMap := make(map[string]guide.Channel)
	eventMap := make(map[string]bool)
	var programs []guide.Program

	totalSlots := int(endTime.Sub(midnight) / (6 * time.Hour))
	slot := 0
	for t := midnight; t.Before(endTime); t = t.Add(6 * time.Hour) {
		if sourceCurrent != nil && !sourceCurrent() {
			return nil, errScrapeSourceChanged
		}
		slot++
		ts := t.Unix()
		report(scrapeProgressUpdate{Stage: "gracenote", Message: fmt.Sprintf("Downloading guide data (%d of %d)", slot, totalSlots), Completed: slot - 1, Total: totalSlots, Channels: len(channelMap), Programs: len(programs)})
		log.Printf("Fetching grid %d/%d for time=%d (%s)", slot, totalSlots, ts, t.Format(time.RFC3339))

		grid, err := client.GetDataByTime(ts)
		if err != nil {
			applog.Errorf("fetching grid at %d: %v", ts, err)
			continue
		}

		for _, ch := range grid.Channels {
			converted := guide.ConvertChannel(ch)
			lineupKey := converted.PlacementID
			if lineupKey == "" {
				lineupKey = strings.Join([]string{converted.ID, converted.ChannelNo, converted.CallSign}, "|")
			}
			if existing, exists := lineupMap[lineupKey]; exists {
				lineupMap[lineupKey] = mergeLineupChannel(existing, converted)
			} else {
				lineupMap[lineupKey] = converted
			}
			if _, exists := channelMap[ch.ChannelID]; !exists {
				channelMap[ch.ChannelID] = converted
			}

			for _, ev := range ch.Events {
				dedupKey := ch.ChannelID + "|" + ev.StartTime + "|" + ev.EndTime
				if eventMap[dedupKey] {
					continue
				}
				eventMap[dedupKey] = true
				programs = append(programs, guide.ConvertEvent(ev, ch.ChannelID, pref.Language, pref.Country))
			}
		}

		log.Printf("Channels so far: %d, Events so far: %d", len(channelMap), len(programs))
		report(scrapeProgressUpdate{Stage: "gracenote", Message: fmt.Sprintf("Downloaded guide data (%d of %d)", slot, totalSlots), Completed: slot, Total: totalSlots, Channels: len(channelMap), Programs: len(programs)})

		if t.Add(6 * time.Hour).Before(endTime) {
			time.Sleep(5 * time.Second)
		}
	}

	var channels []guide.Channel
	for _, ch := range channelMap {
		channels = append(channels, ch)
	}
	var lineupChannels []guide.Channel
	for _, ch := range lineupMap {
		lineupChannels = append(lineupChannels, ch)
	}
	sort.Slice(lineupChannels, func(i, j int) bool {
		if lineupChannels[i].ChannelNo != lineupChannels[j].ChannelNo {
			return channelNumberLess(lineupChannels[i].ChannelNo, lineupChannels[j].ChannelNo)
		}
		return lineupChannels[i].PlacementID < lineupChannels[j].PlacementID
	})

	logoClient := tvlogo.NewClient(pref.Country, "tvlogo_cache.json")
	if logoClient != nil {
		defer logoClient.Close()
	}
	report(scrapeProgressUpdate{Stage: "logos", Message: "Matching channel logos", Channels: len(channels), Programs: len(programs)})
	enrichChannelIcons(logoClient, channels)
	enrichProgramThumbnails(tmdbClient, programs, func(completed, total int) {
		report(scrapeProgressUpdate{Stage: "tmdb", Message: fmt.Sprintf("Enriching program titles (%d of %d)", completed, total), Completed: completed, Total: total, Channels: len(channels), Programs: len(programs)})
	})
	fixDeadImageURLs(programs)

	tvGuide := &guide.TVGuide{
		Channels:       channels,
		Programs:       programs,
		LineupChannels: lineupChannels,
	}
	rewriteGuideImageURLs(tvGuide, baseURL)

	if channelFilter != nil {
		before := len(tvGuide.Channels)
		tvGuide = filterGuideChannels(tvGuide, channelFilter)
		log.Printf("Channel filter: %d → %d channels (Jellyfin has %d)", before, len(tvGuide.Channels), len(channelFilter))
	}

	if persister != nil {
		report(scrapeProgressUpdate{Stage: "saving", Message: "Rendering and saving the guide", Channels: len(tvGuide.Channels), Programs: len(tvGuide.Programs)})
		persisted, err := persister(tvGuide)
		if err != nil {
			return nil, err
		}
		if !persisted {
			return nil, errScrapeSourceChanged
		}
	} else if err := persistGuideFiles(tvGuide, sourceFingerprint); err != nil {
		return nil, err
	}
	return tvGuide, nil
}

func persistGuideFiles(tvGuide *guide.TVGuide, sourceFingerprint string) error {
	if err := writeGuideFile(tvGuide); err != nil {
		return err
	}
	if err := saveGuideCache(tvGuide, sourceFingerprint); err != nil {
		applog.Errorf("failed to save guide cache: %v", err)
	}
	return nil
}

func writeGuideFile(tvGuide *guide.TVGuide) error {
	log.Printf("Rendering XMLTV: %d channels, %d programs", len(tvGuide.Channels), len(tvGuide.Programs))

	// Parse embedded template
	tmpl, err := template.ParseFS(guideTmplFS, "guide.tmpl")
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	// Atomic write: write to temp file, then rename
	tmpFile, err := os.CreateTemp(".", "xmlguide-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpName := tmpFile.Name()

	if err := tmpl.Execute(tmpFile, tvGuide); err != nil {
		tmpFile.Close()
		os.Remove(tmpName)
		return fmt.Errorf("failed to execute template: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to close temporary guide: %w", err)
	}

	if err := os.Rename(tmpName, "xmlguide.xmltv"); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("failed to rename output file: %w", err)
	}
	if err := os.Chmod("xmlguide.xmltv", 0644); err != nil {
		return fmt.Errorf("failed to set guide permissions: %w", err)
	}

	log.Printf("Wrote guide to xmlguide.xmltv")
	return nil
}

// ---------- Guide cache ----------

const (
	guideCachePath            = "guide_cache.json"
	guideRefreshInterval      = 24 * time.Hour
	immediateGuideRefreshWait = 100 * time.Millisecond
	guideCacheVersion         = 2
)

type guideCache struct {
	Version           int           `json:"version"`
	SavedAt           time.Time     `json:"saved_at"`
	SourceFingerprint string        `json:"source_fingerprint"`
	Guide             guide.TVGuide `json:"guide"`
}

// saveGuideCache persists the TVGuide atomically so an interrupted write does
// not replace the last usable cache.
func saveGuideCache(g *guide.TVGuide, sourceFingerprint string) error {
	data, err := json.Marshal(guideCache{Version: guideCacheVersion, SavedAt: time.Now(), SourceFingerprint: sourceFingerprint, Guide: *g})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	tmp, err := os.CreateTemp(".", "guide-cache-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		return fmt.Errorf("set temporary file permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Rename(tmpName, guideCachePath); err != nil {
		return fmt.Errorf("replace cache: %w", err)
	}
	log.Println("Saved guide cache")
	return nil
}

type guideCacheStatus string

const (
	guideCacheMissing       guideCacheStatus = "missing"
	guideCacheUnreadable    guideCacheStatus = "unreadable"
	guideCacheCorrupt       guideCacheStatus = "corrupt"
	guideCacheSourceChanged guideCacheStatus = "source-changed"
	guideCacheReady         guideCacheStatus = "ready"
)

type guideCacheLoadResult struct {
	Guide  *guide.TVGuide
	Age    time.Duration
	Status guideCacheStatus
	Err    error
}

// loadGuideCache validates the source-bound JSON cache without rejecting it
// solely because it is old. Startup decides whether to schedule an immediate
// refresh while continuing to serve stale data.
func loadGuideCache(sourceFingerprint string) guideCacheLoadResult {
	data, err := os.ReadFile(guideCachePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return guideCacheLoadResult{Status: guideCacheMissing}
		}
		return guideCacheLoadResult{Status: guideCacheUnreadable, Err: err}
	}
	var c guideCache
	if err := json.Unmarshal(data, &c); err != nil {
		return guideCacheLoadResult{Status: guideCacheCorrupt, Err: err}
	}
	if c.Version != guideCacheVersion || len(c.Guide.LineupChannels) == 0 {
		log.Println("guide cache: missing full provider lineup data, ignoring cached guide")
		return guideCacheLoadResult{Status: guideCacheCorrupt, Err: errors.New("missing full provider lineup data")}
	}
	if c.SourceFingerprint != sourceFingerprint {
		return guideCacheLoadResult{Status: guideCacheSourceChanged}
	}
	age := time.Since(c.SavedAt)
	if age < 0 {
		age = 0
	}
	return guideCacheLoadResult{Guide: &c.Guide, Age: age, Status: guideCacheReady}
}

type guideStartupPlan struct {
	Guide               *guide.TVGuide
	NextScrapeIn        time.Duration
	Message             string
	Warn                bool
	InvalidateArtifacts bool
}

func planGuideStartup(cache guideCacheLoadResult, xmltvErr error) guideStartupPlan {
	refreshPlan := guideStartupPlan{NextScrapeIn: immediateGuideRefreshWait}

	switch cache.Status {
	case guideCacheMissing:
		refreshPlan.Message = "Guide cache is missing; a fresh guide will be generated in the background"
		return refreshPlan
	case guideCacheUnreadable:
		refreshPlan.Message = fmt.Sprintf("Guide cache is unreadable (%v); a fresh guide will be generated in the background", cache.Err)
		refreshPlan.Warn = true
		return refreshPlan
	case guideCacheCorrupt:
		refreshPlan.Message = fmt.Sprintf("Guide cache is corrupt (%v); a fresh guide will be generated in the background", cache.Err)
		refreshPlan.Warn = true
		return refreshPlan
	case guideCacheSourceChanged:
		refreshPlan.Message = "Guide cache belongs to a different lineup; stale guide artifacts will be removed before refresh"
		refreshPlan.InvalidateArtifacts = true
		return refreshPlan
	case guideCacheReady:
		if xmltvErr != nil {
			if errors.Is(xmltvErr, os.ErrNotExist) {
				refreshPlan.Message = "Guide cache is valid but xmlguide.xmltv is missing; a fresh guide will be generated in the background"
			} else {
				refreshPlan.Message = fmt.Sprintf("Guide cache is valid but xmlguide.xmltv cannot be inspected (%v); a fresh guide will be generated in the background", xmltvErr)
			}
			refreshPlan.Warn = true
			return refreshPlan
		}
	default:
		refreshPlan.Message = fmt.Sprintf("Guide cache has unknown status %q; a fresh guide will be generated in the background", cache.Status)
		refreshPlan.Warn = true
		return refreshPlan
	}

	plan := guideStartupPlan{Guide: cache.Guide}
	if cache.Age >= guideRefreshInterval {
		plan.NextScrapeIn = immediateGuideRefreshWait
		plan.Message = fmt.Sprintf("Loaded stale guide cache (%s old); serving it while an immediate background refresh runs", cache.Age.Round(time.Second))
		return plan
	}

	plan.NextScrapeIn = guideRefreshInterval - cache.Age
	if plan.NextScrapeIn < time.Second {
		plan.NextScrapeIn = time.Second
	}
	plan.Message = fmt.Sprintf("Loaded fresh guide cache (%s old); next refresh in %s", cache.Age.Round(time.Second), plan.NextScrapeIn.Round(time.Second))
	return plan
}

func restoreMissingGuideFile(cache guideCacheLoadResult, xmltvErr error) (error, bool) {
	if cache.Status != guideCacheReady || !errors.Is(xmltvErr, os.ErrNotExist) {
		return xmltvErr, false
	}
	if err := writeGuideFile(cache.Guide); err != nil {
		return fmt.Errorf("rebuild XMLTV from guide cache: %w", err), false
	}
	return nil, true
}

func invalidateCurrentGuideArtifacts() {
	for _, path := range []string{"xmlguide.xmltv", guideCachePath} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			applog.Warnf("setup could not remove stale %s: %v", path, err)
		}
	}
}

// ---------- File rotation ----------

// rotateFiles links the current xmlguide.xmltv to a dated file when possible,
// falls back to a streamed copy across filesystems, and prunes old rotations.
func rotateFiles() {
	dated := fmt.Sprintf("xmlguide.%s.xmltv", time.Now().UTC().Format("20060102"))
	linked, err := rotateGuideFile("xmlguide.xmltv", dated, os.Link)
	if err != nil {
		applog.Errorf("rotate failed to create %s: %v", dated, err)
		return
	}
	if linked {
		log.Printf("Rotated guide to %s without duplicating file data", dated)
	} else {
		log.Printf("Rotated guide to %s with a cross-filesystem copy", dated)
	}

	// Prune: keep only the 7 most recent dated files
	matches, _ := filepath.Glob("xmlguide.*.xmltv")
	if len(matches) <= 7 {
		return
	}

	// Sort descending (newest first)
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	for _, old := range matches[7:] {
		log.Printf("Pruning old guide: %s", old)
		os.Remove(old)
	}
}

func rotateGuideFile(source, destination string, link func(string, string) error) (bool, error) {
	if _, err := os.Stat(source); err != nil {
		return false, err
	}
	directory := filepath.Dir(destination)
	temporary, err := os.CreateTemp(directory, ".xmlguide-rotation-*.tmp")
	if err != nil {
		return false, err
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return false, err
	}
	if err := os.Remove(temporaryPath); err != nil {
		return false, err
	}
	keepTemporary := false
	defer func() {
		if !keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	linked := false
	if err := link(source, temporaryPath); err == nil {
		linked = true
	} else {
		if copyErr := copyGuideFile(source, temporaryPath); copyErr != nil {
			return false, copyErr
		}
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return false, err
	}
	keepTemporary = true
	return linked, nil
}

func copyGuideFile(source, destination string) error {
	sourceFile, err := os.Open(source)
	if err != nil {
		return err
	}
	defer sourceFile.Close()
	destinationFile, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = destinationFile.Close()
		}
	}()
	if _, err := io.Copy(destinationFile, sourceFile); err != nil {
		return err
	}
	if err := destinationFile.Sync(); err != nil {
		return err
	}
	if err := destinationFile.Close(); err != nil {
		return err
	}
	closed = true
	return nil
}

// ---------- Background scraper ----------

// startScraper runs the active lineup on a 24-hour timer and accepts immediate
// requests after setup changes.
func startScraper(ctx context.Context, state *GuideState, store *appconfig.Store, tmdbClient *tmdb.Client, baseURL string, initialDelay time.Duration, trigger <-chan struct{}, jellyfinURL, jellyfinAPIKey string, filterEnabled bool, status *scrapeStatus) {
	if initialDelay <= 0 {
		initialDelay = 24 * time.Hour
	}

	timer := time.NewTimer(initialDelay)
	defer timer.Stop()

	for {
		reason := "scheduled"
		select {
		case <-ctx.Done():
			log.Println("Scraper shutting down")
			return
		case <-timer.C:
		case <-trigger:
			reason = "setup-requested"
		}

		config, configured, _ := store.Get()
		if !configured {
			log.Println("Scraper is waiting for a provider selection at /setup")
			resetScrapeTimer(timer, 24*time.Hour)
			continue
		}

		log.Printf("Starting %s scrape for %s", reason, config.Gracenote.ProviderName)
		if status != nil {
			status.start("Starting guide download")
			if g := state.Get(); usableGuide(g) {
				status.available(len(g.Channels), len(g.Programs))
			}
		}
		var channelFilter map[string]bool
		if filterEnabled {
			cf, err := fetchJellyfinChannelNumbers(jellyfinURL, jellyfinAPIKey)
			if err != nil {
				applog.Warnf("could not fetch Jellyfin channels for filter, proceeding unfiltered: %v", err)
			} else {
				channelFilter = cf
			}
		}

		fingerprint := config.Fingerprint()
		persister := func(g *guide.TVGuide) (bool, error) {
			return store.WhileCurrent(fingerprint, func() error {
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := persistGuideFiles(g, fingerprint); err != nil {
					return err
				}
				state.UpdateForSource(g, fingerprint)
				if status != nil {
					status.available(len(g.Channels), len(g.Programs))
				}
				rotateFiles()
				return nil
			})
		}
		sourceCurrent := func() bool {
			current, ok, _ := store.Get()
			return ctx.Err() == nil && ok && current.Fingerprint() == fingerprint
		}
		var reporter scrapeProgressReporter
		if status != nil {
			reporter = func(update scrapeProgressUpdate) {
				if !sourceCurrent() {
					return
				}
				status.update(update.Stage, update.Message, update.Completed, update.Total, update.Channels, update.Programs)
			}
		}
		builtGuide, err := runGuideCycle(state.Get(), tmdbClient != nil, time.Now(),
			func(withTMDB bool, save guidePersister) (*guide.TVGuide, error) {
				client := tmdbClient
				if !withTMDB {
					client = nil
				}
				return runScrape(config.Preferences(), client, baseURL, channelFilter, fingerprint, sourceCurrent, save, reporter)
			}, func(g *guide.TVGuide) error {
				log.Println("Initial guide is available; continuing TMDB enrichment in the background")
				err := enrichProgramThumbnailsWhile(tmdbClient, g.Programs, sourceCurrent, func(done, total int) {
					if status != nil && sourceCurrent() {
						status.update("tmdb_background", fmt.Sprintf("Guide ready — TMDB enrichment in background (%d of %d titles)", done, total), done, total, len(g.Channels), len(g.Programs))
					}
				})
				if err != nil {
					return err
				}
				fixDeadImageURLs(g.Programs)
				rewriteGuideImageURLs(g, baseURL)
				if status != nil && sourceCurrent() {
					status.update("saving", "Guide ready — saving enriched version", 0, 0, len(g.Channels), len(g.Programs))
				}
				return nil
			}, persister, sourceCurrent)
		nextDelay := 24 * time.Hour
		if errors.Is(err, errScrapeSourceChanged) {
			log.Println("Discarded scrape because the active lineup changed")
			if status != nil {
				status.queue("Waiting to build the newly selected lineup")
			}
		} else if err != nil {
			applog.Errorf("scrape failed: %v", err)
			if status != nil {
				if usableGuide(state.Get()) {
					status.fail("Guide remains available; background work failed and will retry in 15 minutes: " + err.Error())
				} else {
					status.fail("Guide build failed: " + err.Error())
				}
			}
			nextDelay = 15 * time.Minute
		} else {
			log.Println("Scrape complete")
			if status != nil {
				status.ready(len(builtGuide.Channels), len(builtGuide.Programs))
			}
		}
		resetScrapeTimer(timer, nextDelay)
	}
}

func resetScrapeTimer(timer *time.Timer, delay time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(delay)
}

func queueScrape(trigger chan<- struct{}) {
	select {
	case trigger <- struct{}{}:
	default:
	}
}

// ---------- HTTP handlers ----------

func handleIndex(store *appconfig.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if _, configured, _ := store.Get(); !configured {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == http.MethodGet {
			_, _ = w.Write(indexHTML)
		}
	}
}

func handleFavicon(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if r.Method == http.MethodGet {
		_, _ = w.Write(faviconSVG)
	}
}

func handleXMLTV(state *GuideState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if state.Get() == nil {
			w.Header().Set("Retry-After", "30")
			http.Error(w, "Guide is being generated", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		http.ServeFile(w, r, "xmlguide.xmltv")
	}
}

func handleGuideJSON(state *GuideState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		g := state.Get()
		if g == nil {
			w.Header().Set("Retry-After", "30")
			http.Error(w, "Guide is being generated", http.StatusServiceUnavailable)
			return
		}

		apiGuide := guideToJSON(g)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		enc.Encode(apiGuide)
	}
}

// ---------- Image proxy ----------

const imageCacheDir = "image_cache"

// imageURLAllowed checks whether a URL is on the proxy allowlist.
func imageURLAllowed(rawURL string) bool {
	u, err := neturl.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "image.tmdb.org" {
		return true
	}
	if host == "tmsimg.com" {
		return true
	}
	if host == "raw.githubusercontent.com" && strings.HasPrefix(u.Path, "/tv-logo/tv-logos/") {
		return true
	}
	return false
}

func handleImage(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		http.Error(w, "missing url parameter", http.StatusBadRequest)
		return
	}
	if !imageURLAllowed(rawURL) {
		http.Error(w, "url not allowed", http.StatusForbidden)
		return
	}

	// Cache key
	h := sha256.Sum256([]byte(rawURL))
	key := hex.EncodeToString(h[:])
	datPath := filepath.Join(imageCacheDir, key+".dat")
	typePath := filepath.Join(imageCacheDir, key+".type")

	// Cache hit — verify both files exist and are readable
	ct, ctErr := os.ReadFile(typePath)
	_, datErr := os.Stat(datPath)
	if ctErr == nil && datErr == nil {
		w.Header().Set("Content-Type", string(ct))
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFile(w, r, datPath)
		return
	}

	// Cache miss or inconsistent — fetch upstream
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		http.Error(w, "upstream fetch failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "upstream returned "+resp.Status, http.StatusBadGateway)
		return
	}

	// Ensure cache dir
	os.MkdirAll(imageCacheDir, 0755)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "failed reading upstream", http.StatusBadGateway)
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Write cache files atomically (temp + rename)
	if tmpDat, err := os.CreateTemp(imageCacheDir, "img-*.tmp"); err == nil {
		if _, wErr := tmpDat.Write(body); wErr == nil {
			tmpDat.Close()
			os.Rename(tmpDat.Name(), datPath)
			os.WriteFile(typePath, []byte(contentType), 0644)
		} else {
			tmpDat.Close()
			os.Remove(tmpDat.Name())
		}
	}

	// Serve
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(body)
}

// ---------- Jellyfin Live TV ----------

var validJellyfinID = regexp.MustCompile(`^[0-9a-fA-F-]+$`)

func handleLiveTVConfig(jellyfinURL, jellyfinAPIKey string) http.HandlerFunc {
	enabled := jellyfinURL != "" && jellyfinAPIKey != ""
	body := []byte(`{"enabled":false}`)
	if enabled {
		body = []byte(`{"enabled":true}`)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}
}

// handleLiveTVChannels proxies the Jellyfin channel list so the frontend
// doesn't need credentials.
func handleLiveTVChannels(jellyfinURL, jellyfinAPIKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if jellyfinURL == "" || jellyfinAPIKey == "" {
			http.Error(w, "Live TV not configured", http.StatusServiceUnavailable)
			return
		}
		url := fmt.Sprintf("%s/LiveTv/Channels?api_key=%s&SortBy=SortName&SortOrder=Ascending&AddCurrentProgram=true",
			jellyfinURL, jellyfinAPIKey)
		resp, err := http.Get(url)
		if err != nil {
			http.Error(w, "Failed to reach Jellyfin", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
	}
}

// handleLiveTVTune does the three-step Jellyfin live-stream handshake
// server-side and returns a ready-to-play HLS URL.
//
// Flow: GET PlaybackInfo → POST LiveStreams/Open → build master.m3u8 URL.
func handleLiveTVTune(jellyfinURL, jellyfinAPIKey string) http.HandlerFunc {
	type playbackInfoResponse struct {
		PlaySessionId string `json:"PlaySessionId"`
		MediaSources  []struct {
			Id        string `json:"Id"`
			OpenToken string `json:"OpenToken"`
		} `json:"MediaSources"`
	}
	type openStreamResponse struct {
		MediaSource struct {
			Id           string `json:"Id"`
			LiveStreamId string `json:"LiveStreamId"`
		} `json:"MediaSource"`
	}

	client := &http.Client{Timeout: 15 * time.Second}

	jfGet := func(path string) ([]byte, error) {
		url := fmt.Sprintf("%s%s?api_key=%s", jellyfinURL, path, jellyfinAPIKey)
		resp, err := client.Get(url)
		if err != nil {
			return nil, fmt.Errorf("GET %s: %w", path, err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("GET %s returned %d: %s", path, resp.StatusCode, string(body))
		}
		return body, nil
	}

	jfPost := func(path, jsonBody string) ([]byte, error) {
		url := fmt.Sprintf("%s%s&api_key=%s", jellyfinURL, path, jellyfinAPIKey)
		req, err := http.NewRequest("POST", url, strings.NewReader(jsonBody))
		if err != nil {
			return nil, fmt.Errorf("creating POST %s: %w", path, err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("POST %s: %w", path, err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("POST %s returned %d: %s", path, resp.StatusCode, string(body))
		}
		return body, nil
	}

	return func(w http.ResponseWriter, r *http.Request) {
		channelId := r.URL.Query().Get("id")
		if channelId == "" {
			http.Error(w, "missing id parameter", http.StatusBadRequest)
			return
		}
		if !validJellyfinID.MatchString(channelId) {
			http.Error(w, "invalid id parameter", http.StatusBadRequest)
			return
		}

		// Step 1: Get playback info → OpenToken, PlaySessionId, MediaSourceId
		path := fmt.Sprintf("/Items/%s/PlaybackInfo", channelId)
		body, err := jfGet(path)
		if err != nil {
			applog.Errorf("livetv tune step 1: %v", err)
			http.Error(w, "PlaybackInfo failed: "+err.Error(), http.StatusBadGateway)
			return
		}

		var info playbackInfoResponse
		if err := json.Unmarshal(body, &info); err != nil {
			applog.Errorf("livetv tune could not parse playback info: %v", err)
			http.Error(w, "Failed to parse PlaybackInfo", http.StatusBadGateway)
			return
		}
		if len(info.MediaSources) == 0 {
			http.Error(w, "No media sources for channel", http.StatusBadGateway)
			return
		}

		// Step 2: Open live stream → LiveStreamId
		openBody := fmt.Sprintf(
			`{"OpenToken":%q,"PlaySessionId":%q,"ItemId":%q}`,
			info.MediaSources[0].OpenToken, info.PlaySessionId, channelId,
		)
		openPath := fmt.Sprintf("/LiveStreams/Open?PlaySessionId=%s&ItemId=%s",
			info.PlaySessionId, channelId)
		respBody, err := jfPost(openPath, openBody)
		if err != nil {
			applog.Errorf("livetv tune step 2: %v", err)
			http.Error(w, "LiveStreams/Open failed: "+err.Error(), http.StatusBadGateway)
			return
		}

		var opened openStreamResponse
		if err := json.Unmarshal(respBody, &opened); err != nil {
			applog.Errorf("livetv tune could not parse open stream: %v", err)
			http.Error(w, "Failed to parse LiveStreams/Open", http.StatusBadGateway)
			return
		}

		// Give the transcoder time to produce initial segments before
		// handing the URL to the browser.  The working jellyfinapi
		// implementation has a natural ~4s gap here because the user
		// clicks "play" after the handshake completes.
		time.Sleep(4 * time.Second)

		// Step 3: Build master.m3u8 URL with all required parameters
		streamURL := fmt.Sprintf(
			"%s/Videos/%s/master.m3u8?api_key=%s&MediaSourceId=%s&PlaySessionId=%s&LiveStreamId=%s&VideoCodec=h264&AudioCodec=aac&SegmentContainer=ts&MinSegments=1&BreakOnNonKeyFrames=true&VideoBitrate=2000000&AudioBitrate=192000&MaxWidth=1920&MaxHeight=1080&AudioStreamIndex=-1&VideoStreamIndex=-1",
			jellyfinURL, channelId, jellyfinAPIKey,
			opened.MediaSource.Id, info.PlaySessionId, opened.MediaSource.LiveStreamId,
		)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"url":           streamURL,
			"playSessionId": info.PlaySessionId,
		})
	}
}

// handleLiveTVStop forwards a playback-stop notification to Jellyfin.
func handleLiveTVStop(jellyfinURL, jellyfinAPIKey string) http.HandlerFunc {
	client := &http.Client{Timeout: 5 * time.Second}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		url := fmt.Sprintf("%s/Sessions/Playing/Stopped?api_key=%s", jellyfinURL, jellyfinAPIKey)
		req, err := http.NewRequest("POST", url, strings.NewReader(string(body)))
		if err != nil {
			http.Error(w, "failed to build request", http.StatusInternalServerError)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "Jellyfin unreachable", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		io.ReadAll(resp.Body)
		w.WriteHeader(resp.StatusCode)
	}
}

// ---------- Jellyfin channel filter ----------

// fetchJellyfinChannelNumbers queries Jellyfin for available live TV channels
// and returns a set of their channel number strings.
func fetchJellyfinChannelNumbers(jellyfinURL, jellyfinAPIKey string) (map[string]bool, error) {
	url := fmt.Sprintf("%s/LiveTv/Channels?api_key=%s&SortBy=SortName", jellyfinURL, jellyfinAPIKey)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetching Jellyfin channels: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Jellyfin returned %d", resp.StatusCode)
	}

	var result struct {
		Items []struct {
			ChannelNumber string `json:"ChannelNumber"`
		} `json:"Items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding Jellyfin channels: %w", err)
	}

	allowed := make(map[string]bool, len(result.Items))
	for _, item := range result.Items {
		if item.ChannelNumber != "" {
			allowed[item.ChannelNumber] = true
		}
	}
	return allowed, nil
}

// filterGuideChannels returns a new TVGuide containing only channels whose
// number (DisplayNames[1]) is in the allowed set, along with their programs.
func filterGuideChannels(g *guide.TVGuide, allowed map[string]bool) *guide.TVGuide {
	allowedIDs := make(map[string]bool)
	var channels []guide.Channel
	for _, ch := range g.Channels {
		number := ""
		if len(ch.DisplayNames) >= 3 {
			number = ch.DisplayNames[1].Name
		}
		if allowed[number] {
			channels = append(channels, ch)
			allowedIDs[ch.ID] = true
		}
	}

	var programs []guide.Program
	for _, p := range g.Programs {
		if allowedIDs[p.Channel] {
			programs = append(programs, p)
		}
	}

	filtered := *g
	filtered.Channels = channels
	filtered.Programs = programs
	return &filtered
}

// ---------- Main ----------

func enabledSetting(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on", "enabled":
		return true
	default:
		return false
	}
}

func main() {
	// Go's standard logger writes to stderr by default. Keep routine progress on
	// stdout so container log viewers do not label informational messages as
	// errors; explicitly classified warnings and failures use applog.
	log.SetOutput(os.Stdout)

	guideOnly := flag.Bool("guide-only", false, "Scrape once and exit (no server)")
	flag.Parse()

	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	port := util.GetEnv("PORT", "8080")
	baseURL := util.GetEnv("BASE_URL", "")
	configPath := util.GetEnv("CONFIG_PATH", "config.json")
	configStore, configErr := appconfig.LoadStore(configPath)
	if configErr != nil {
		applog.Warnf("configuration could not be loaded; /setup will remain available: %v", configErr)
	}
	lineuparrStatePath := util.GetEnv("LINEUPARR_STATE_PATH", "lineuparr_state.json")
	lineuparrStateStore, lineuparrStateErr := lineuparrbuilder.LoadStateStore(lineuparrStatePath)
	if lineuparrStateErr != nil {
		log.Printf("Lineuparr builder state could not be loaded; choices will start clean: %v", lineuparrStateErr)
	}
	catalogSetting := strings.TrimSpace(util.GetEnv("LINEUPARR_CATALOG_URLS", ""))
	useDefaultCatalogs := strings.EqualFold(catalogSetting, "default")
	var catalogURLs []string
	if !useDefaultCatalogs && !strings.EqualFold(catalogSetting, "off") && !strings.EqualFold(catalogSetting, "none") {
		for _, rawURL := range strings.FieldsFunc(catalogSetting, func(r rune) bool { return r == ',' || r == '\n' }) {
			if rawURL = strings.TrimSpace(rawURL); rawURL != "" {
				catalogURLs = append(catalogURLs, rawURL)
			}
		}
	}
	iptvOrgURL := strings.TrimSpace(util.GetEnv("LINEUPARR_IPTV_ORG_URL", ""))
	if strings.EqualFold(iptvOrgURL, "off") || strings.EqualFold(iptvOrgURL, "none") {
		iptvOrgURL = ""
	}
	referenceCatalogsEnabled := enabledSetting(util.GetEnv("LINEUPARR_REFERENCE_CATALOGS", ""))
	lineuparrBuilder := lineuparrbuilder.NewService(lineuparrStateStore, lineuparrbuilder.ServiceOptions{
		CacheDir:            util.GetEnv("LINEUPARR_CACHE_DIR", "lineuparr_source_cache"),
		CatalogURLs:         catalogURLs,
		UseDefaultCatalogs:  useDefaultCatalogs,
		UseEmbeddedCatalogs: referenceCatalogsEnabled,
		IPTVOrgURL:          iptvOrgURL,
	})
	dispatcharrConfigPath := util.GetEnv("DISPATCHARR_CONFIG_PATH", "dispatcharr_config.json")
	dispatcharrConfigStore, dispatcharrConfigErr := dispatcharr.LoadConfigStore(dispatcharrConfigPath)
	if dispatcharrConfigErr != nil {
		log.Printf("Dispatcharr connection could not be loaded; reconnect through the Lineuparr builder: %v", dispatcharrConfigErr)
	}
	dispatcharrClient := dispatcharr.NewClient(nil)

	jellyfinURL := strings.TrimRight(util.GetEnv("JELLYFIN_URL", ""), "/")
	jellyfinAPIKey := util.GetEnv("JELLYFIN_API_KEY", "")
	jellyfinConfigured := jellyfinURL != "" && jellyfinAPIKey != ""
	if jellyfinConfigured {
		log.Printf("Jellyfin Live TV integration enabled (%s)", jellyfinURL)
	}

	// Channel filter: only show channels available in Jellyfin
	channelFilterEnabled := util.GetEnv("JELLYFIN_CHANNEL_FILTER", "") != "" && jellyfinConfigured
	var channelFilter map[string]bool
	if channelFilterEnabled {
		cf, err := fetchJellyfinChannelNumbers(jellyfinURL, jellyfinAPIKey)
		if err != nil {
			applog.Warnf("could not fetch Jellyfin channels for filter: %v", err)
			log.Println("Channel filter will be retried on next scheduled scrape")
		} else {
			channelFilter = cf
			log.Printf("Channel filter enabled: %d Jellyfin channels", len(channelFilter))
		}
	}

	tmdbToken := util.GetEnv("TMDB_TOKEN", "")
	tmdbClient := tmdb.NewClient(tmdbToken, "tmdb_cache.json")
	if tmdbClient != nil {
		log.Println("TMDB integration enabled")
	} else {
		log.Println("No TMDB token configured, skipping image enrichment")
	}
	defer tmdbClient.Close()

	// --guide-only: always scrape, write output, exit
	if *guideOnly {
		config, configured, _ := configStore.Get()
		if !configured {
			applog.Fatalf("no provider is configured. Run server mode and open /setup, or provide complete GN_* environment settings.")
		}
		log.Println("Starting scrape (guide-only mode)...")
		if _, err := runScrape(config.Preferences(), tmdbClient, baseURL, channelFilter, config.Fingerprint(), nil, nil); err != nil {
			applog.Fatalf("scrape failed: %v", err)
		}
		log.Println("--guide-only: done")
		return
	}

	// Server mode starts immediately so first-run setup remains available while
	// the initial guide is generated in the background.
	var g *guide.TVGuide
	nextScrapeIn := 24 * time.Hour
	config, configured, source := configStore.Get()
	if configured {
		log.Printf("Active lineup: %s (%s)", config.Gracenote.ProviderName, source)
		_, xmltvErr := os.Stat("xmlguide.xmltv")
		cache := loadGuideCache(config.Fingerprint())
		var rebuilt bool
		xmltvErr, rebuilt = restoreMissingGuideFile(cache, xmltvErr)
		if rebuilt {
			log.Println("Rebuilt missing xmlguide.xmltv from the source-matching guide cache")
		}
		plan := planGuideStartup(cache, xmltvErr)
		if plan.Warn {
			applog.Warnf("%s", plan.Message)
		} else {
			log.Println(plan.Message)
		}
		if plan.InvalidateArtifacts {
			invalidateCurrentGuideArtifacts()
		}
		g = plan.Guide
		nextScrapeIn = plan.NextScrapeIn
		if g != nil {
			if channelFilter != nil {
				before := len(g.Channels)
				g = filterGuideChannels(g, channelFilter)
				log.Printf("Channel filter: %d → %d channels (cached guide)", before, len(g.Channels))
			}
		}
	} else {
		log.Println("No provider configured; open /setup to choose a lineup")
	}

	if tmdbClient != nil && resumableTMDB(g, time.Now()) {
		nextScrapeIn = immediateGuideRefreshWait
		log.Println("Cached guide is usable; resuming pending TMDB enrichment without downloading grids")
	}
	state := &GuideState{}
	initialFingerprint := ""
	if configured {
		initialFingerprint = config.Fingerprint()
	}
	state.UpdateForSource(g, initialFingerprint)
	guideChannels, guidePrograms := 0, 0
	if g != nil {
		guideChannels = len(g.Channels)
		guidePrograms = len(g.Programs)
	}
	guideStatus := newScrapeStatus(g != nil, guideChannels, guidePrograms)

	// Signal context for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	scrapeTrigger := make(chan struct{}, 1)
	setupHandlers := &setupServer{
		store:          configStore,
		providers:      web.NewProviderClient(),
		channelCounter: webProviderChannelCounter{},
		scrapeStatus:   guideStatus,
		onProviderSaved: func(changed bool) {
			if changed {
				state.UpdateForSource(nil, "")
				invalidateCurrentGuideArtifacts()
			}
			guideStatus.queue("Guide build queued")
			queueScrape(scrapeTrigger)
		},
	}
	var marketService *lineupindex.Service
	service, serviceErr := lineupindex.NewService(lineupindex.ServiceConfig{
		Path:            util.GetEnv("MARKET_INDEX_PATH", "market_index.json"),
		SnapshotDir:     util.GetEnv("LINEUP_SNAPSHOT_DIR", ""),
		Providers:       setupHandlers.providers,
		Grids:           lineupindex.WebGridFetcher{},
		Evidence:        providersource.NewService(providersource.Options{UseEmbeddedCatalogs: referenceCatalogsEnabled}),
		CurrentStations: func() map[string][]string { return currentStationNames(state.Get()) },
		ProviderDelay:   500 * time.Millisecond,
		GridDelay:       5 * time.Second,
	})
	if serviceErr != nil {
		log.Printf("Local lineup index is unavailable: %v", serviceErr)
	} else {
		marketService = service
		log.Printf("Local lineup index ready")
	}
	nominatimURL := strings.TrimSpace(util.GetEnv("NOMINATIM_URL", geocode.DefaultNominatimURL))
	var addressSearcher providerAddressSearcher
	if !strings.EqualFold(nominatimURL, "off") && !strings.EqualFold(nominatimURL, "none") && nominatimURL != "" {
		addressSearcher = geocode.NewNominatimClient(nil, nominatimURL)
	}
	aliasQueue := newAliasJobQueue(guideStatus, marketService)
	lineuparrHandlers := &lineuparrServer{
		store: configStore, state: state, builder: lineuparrBuilder, marketIndex: marketService,
		addressSearcher: addressSearcher, aliasQueue: aliasQueue,
		addressTester: providersource.NewService(),
		exportDir:     util.GetEnv("LINEUPARR_EXPORT_DIR", filepath.Join(filepath.Dir(lineuparrStatePath), "lineuparr_exports")),
	}
	if aliasQueue != nil {
		go aliasQueue.Run(ctx)
	}
	dispatcharrHandlers := &dispatcharrServer{
		lineup: lineuparrHandlers, config: dispatcharrConfigStore, client: dispatcharrClient,
	}

	// Start background scraper
	if configured && nextScrapeIn < time.Second {
		log.Println("Initial scrape queued")
		guideStatus.queue("Initial guide build queued")
	} else {
		log.Printf("Next scrape in %s", nextScrapeIn.Round(time.Minute))
	}
	go startScraper(ctx, state, configStore, tmdbClient, baseURL, nextScrapeIn, scrapeTrigger, jellyfinURL, jellyfinAPIKey, channelFilterEnabled, guideStatus)

	// HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex(configStore))
	mux.HandleFunc("/favicon.svg", handleFavicon)
	mux.HandleFunc("/setup", setupHandlers.handlePage)
	mux.HandleFunc("/api/setup/config", setupHandlers.handleConfig)
	mux.HandleFunc("/api/setup/providers", setupHandlers.handleProviders)
	mux.HandleFunc("/api/setup/provider", setupHandlers.handleProvider)
	mux.HandleFunc("/api/setup/status", setupHandlers.handleScrapeStatus)
	mux.HandleFunc("/lineuparr", lineuparrHandlers.handlePage)
	mux.HandleFunc("/api/lineuparr/provider-address/config", lineuparrHandlers.handleProviderAddressConfig)
	mux.HandleFunc("/lineuparr/address-help.png", lineuparrHandlers.handleAddressHelpImage)
	mux.HandleFunc("/api/lineuparr/provider-address/search", lineuparrHandlers.handleProviderAddressSearch)
	mux.HandleFunc("/api/lineuparr/draft", lineuparrHandlers.handleDraft)
	mux.HandleFunc("/api/lineuparr/channel", lineuparrHandlers.handleChannel)
	mux.HandleFunc("/api/lineuparr/categories", lineuparrHandlers.handleCategories)
	mux.HandleFunc("/api/lineuparr/channel-programs", lineuparrHandlers.handleChannelPrograms)
	mux.HandleFunc("/api/lineuparr/remove-duplicates", lineuparrHandlers.handleRemoveDuplicates)
	mux.HandleFunc("/api/lineuparr/restore-all", lineuparrHandlers.handleRestoreAll)
	mux.HandleFunc("/api/lineuparr/export", lineuparrHandlers.handleExport)
	mux.HandleFunc("/api/lineuparr/alias-index", lineuparrHandlers.handleAliasIndex)
	mux.HandleFunc("/api/lineuparr/alias-index/run", lineuparrHandlers.handleAliasIndexRun)
	mux.HandleFunc("/api/lineuparr/alias-index/stop", lineuparrHandlers.handleAliasIndexStop)
	mux.HandleFunc("/api/lineuparr/publish", lineuparrHandlers.handlePublish)
	mux.HandleFunc(lineuparrPublishedPrefix, lineuparrHandlers.handlePublishedExport)
	mux.HandleFunc("/api/lineuparr/alias", lineuparrHandlers.handleAlias)
	mux.HandleFunc("/api/lineuparr/dispatcharr/config", dispatcharrHandlers.handleConfig)
	mux.HandleFunc("/api/lineuparr/dispatcharr/review", dispatcharrHandlers.handleReview)
	mux.HandleFunc("/api/lineuparr/dispatcharr/decision", dispatcharrHandlers.handleDecision)
	shareLinks := &shareLinksServer{path: configPath + ".links.json"}
	mux.HandleFunc("/api/setup/share-links", shareLinks.handle)
	mux.HandleFunc("/xmlguide.xmltv", handleXMLTV(state))
	mux.HandleFunc("/api/guide.json", handleGuideJSON(state))
	mux.HandleFunc("/img", handleImage)
	mux.HandleFunc("/api/livetv/config", handleLiveTVConfig(jellyfinURL, jellyfinAPIKey))
	if jellyfinURL != "" && jellyfinAPIKey != "" {
		mux.HandleFunc("/api/livetv/channels", handleLiveTVChannels(jellyfinURL, jellyfinAPIKey))
		mux.HandleFunc("/api/livetv/tune", handleLiveTVTune(jellyfinURL, jellyfinAPIKey))
		mux.HandleFunc("/api/livetv/stop", handleLiveTVStop(jellyfinURL, jellyfinAPIKey))
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	// Start server in background
	go func() {
		log.Printf("HTTP server listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			applog.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()
	log.Println("Shutting down...")
	if marketService != nil {
		marketService.Stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		applog.Errorf("HTTP server shutdown error: %v", err)
	}

	log.Println("Goodbye")
}

// ---------- Enrichment helpers ----------

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// replaces broken Gracenote thumbnail URLs with TMDB
// poster images, star ratings, dates, and descriptions.
type tmdbTitleKey struct {
	title   string
	isMovie bool
}

type tmdbLookupResult struct {
	key   tmdbTitleKey
	entry tmdb.CacheEntry
}

func enrichProgramThumbnails(client *tmdb.Client, programs []guide.Program, progress ...func(completed, total int)) {
	_ = enrichProgramThumbnailsWhile(client, programs, nil, progress...)
}

func enrichProgramThumbnailsWhile(client *tmdb.Client, programs []guide.Program, current func() bool, progress ...func(completed, total int)) error {
	if current != nil && !current() {
		return errScrapeSourceChanged
	}
	if client == nil {
		return nil
	}

	// Phase 1: collect unique {title, isMovie} pairs
	seen := make(map[tmdbTitleKey]bool)
	var unique []tmdbTitleKey

	for _, p := range programs {
		title := strings.ToLower(html.UnescapeString(p.Title))
		isMovie := false
		for _, cat := range p.Categories {
			if cat.Name == "movie" {
				isMovie = true
				break
			}
		}
		k := tmdbTitleKey{title: title, isMovie: isMovie}
		if !seen[k] {
			seen[k] = true
			unique = append(unique, k)
		}
	}

	log.Printf("TMDB: looking up %d unique titles", len(unique))
	for _, update := range progress {
		if update != nil {
			update(0, len(unique))
		}
	}

	// Phase 2: lookup each unique title
	workerCount := tmdbWorkerCount()
	log.Printf("TMDB: using %d lookup workers with the shared request limiter", workerCount)
	results := lookupTMDBTitlesWhile(unique, workerCount, client.Lookup, current, progress...)
	if current != nil && !current() {
		return errScrapeSourceChanged
	}

	// Phase 3: apply results back to programs
	enriched := 0
	for i := range programs {
		title := strings.ToLower(html.UnescapeString(programs[i].Title))
		isMovie := false
		for _, cat := range programs[i].Categories {
			if cat.Name == "movie" {
				isMovie = true
				break
			}
		}
		entry := results[tmdbTitleKey{title: title, isMovie: isMovie}]
		if entry.TMDBID == 0 && entry.ImageURL == "" && entry.Rating == 0 {
			continue
		}
		enriched++
		if entry.ImageURL != "" {
			programs[i].IconSrc = entry.ImageURL
			programs[i].Images = []guide.Image{{
				URL:    entry.ImageURL,
				Type:   "poster",
				Size:   "3",
				Orient: "P",
				System: "tmdb",
			}}
		}
		if entry.Rating > 0 {
			programs[i].StarRating = fmt.Sprintf("%.1f/10", entry.Rating)
		}
		if entry.Year != "" {
			programs[i].Date = entry.Year
		}
		if entry.Overview != "" && programs[i].Description == "Unavailable" {
			programs[i].Description = xmlEscape(entry.Overview)
		}
		if entry.OrigLanguage != "" {
			programs[i].OrigLanguage = entry.OrigLanguage
		}
		if entry.TMDBID != 0 {
			tmdbEpNum := fmt.Sprintf("%d", entry.TMDBID)
			if !isMovie {
				tmdbEpNum = fmt.Sprintf("series/%d", entry.TMDBID)
			}
			programs[i].EpisodeNumbers = append(programs[i].EpisodeNumbers, guide.EpisodeNumber{
				System:        "themoviedb.org",
				EpisodeNumber: tmdbEpNum,
			})
		}
	}

	log.Printf("TMDB: enriched %d/%d programs", enriched, len(programs))
	return nil
}

func tmdbWorkerCount() int {
	workers, err := strconv.Atoi(strings.TrimSpace(os.Getenv("TMDB_WORKERS")))
	if err != nil || workers < 1 {
		return 4
	}
	if workers > 16 {
		return 16
	}
	return workers
}

func lookupTMDBTitles(keys []tmdbTitleKey, workerCount int, lookup func(string, bool) tmdb.CacheEntry, progress ...func(completed, total int)) map[tmdbTitleKey]tmdb.CacheEntry {
	return lookupTMDBTitlesWhile(keys, workerCount, lookup, nil, progress...)
}

func lookupTMDBTitlesWhile(keys []tmdbTitleKey, workerCount int, lookup func(string, bool) tmdb.CacheEntry, current func() bool, progress ...func(completed, total int)) map[tmdbTitleKey]tmdb.CacheEntry {
	if workerCount < 1 {
		workerCount = 1
	}
	if workerCount > len(keys) {
		workerCount = len(keys)
	}
	results := make(map[tmdbTitleKey]tmdb.CacheEntry, len(keys))
	if workerCount == 0 {
		return results
	}
	jobs := make(chan tmdbTitleKey, len(keys))
	completed := make(chan tmdbLookupResult, len(keys))
	var workers sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for key := range jobs {
				if current != nil && !current() {
					return
				}
				completed <- tmdbLookupResult{key: key, entry: lookup(key.title, key.isMovie)}
			}
		}()
	}
	for _, key := range keys {
		jobs <- key
	}
	close(jobs)
	go func() {
		workers.Wait()
		close(completed)
	}()
	count := 0
	for result := range completed {
		results[result.key] = result.entry
		count++
		for _, update := range progress {
			if update != nil {
				update(count, len(keys))
			}
		}
		if count%50 == 0 || count == len(keys) {
			log.Printf("TMDB: scan progress %d/%d", count, len(keys))
		}
	}
	return results
}

// fixDeadImageURLs rewrites program image URLs pointing to the defunct
// zap2it.tmsimg.com host to use tmsimg.com directly, which still serves images.
func fixDeadImageURLs(programs []guide.Program) {
	fixed := 0
	for i := range programs {
		if programs[i].IconSrc != "" && strings.Contains(programs[i].IconSrc, "zap2it.tmsimg.com") {
			programs[i].IconSrc = strings.Replace(programs[i].IconSrc, "zap2it.tmsimg.com", "tmsimg.com", 1)
			fixed++
		}
		for j := range programs[i].Images {
			if strings.Contains(programs[i].Images[j].URL, "zap2it.tmsimg.com") {
				programs[i].Images[j].URL = strings.Replace(programs[i].Images[j].URL, "zap2it.tmsimg.com", "tmsimg.com", 1)
				fixed++
			}
		}
	}
	if fixed > 0 {
		log.Printf("Fixed %d zap2it image URLs to use tmsimg.com", fixed)
	}
}

// resolves channel logos from the tv-logo/tv-logos repo,
// replacing dead Gracenote icon URLs with verified GitHub-hosted PNGs.
func enrichChannelIcons(client *tvlogo.Client, channels []guide.Channel) {
	if client == nil {
		return
	}

	log.Printf("TV logos: resolving icons for %d channels", len(channels))

	enriched := 0
	for i := range channels {
		logoURL := client.Resolve(channels[i].ID, channels[i].CallSign, channels[i].Affiliate, channels[i].ChannelNo)
		if logoURL != "" {
			channels[i].IconURL = logoURL
			enriched++
		} else {
			channels[i].IconURL = ""
		}
	}

	log.Printf("TV logos: enriched %d/%d channels", enriched, len(channels))
}
