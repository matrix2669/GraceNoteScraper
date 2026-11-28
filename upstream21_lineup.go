package main

import (
	"context"
	"encoding/json"
	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	"github.com/daniel-widrick/GraceNoteScraper/scrape"
	"github.com/daniel-widrick/GraceNoteScraper/web"
	"net/http"
	"sync/atomic"
	"time"
)

// APILineup is the response shape of /api/lineup.json: every provider
// position for the active lineup, plus the source it came from.
type APILineup struct {
	Generated string              `json:"generated"`
	Source    APILineupSource     `json:"source"`
	Positions []APILineupPosition `json:"positions"`
}

type APILineupSource struct {
	ProviderName string `json:"providerName"`
	ProviderType string `json:"providerType"`
	Location     string `json:"location"`
	LineupID     string `json:"lineupId"`
	HeadendID    string `json:"headendId"`
	PostalCode   string `json:"postalCode"`
	Country      string `json:"country"`
	Device       string `json:"device"`
	Language     string `json:"language"`
}

type APILineupPosition struct {
	Number            string   `json:"number"`
	StationID         string   `json:"stationId"`
	PlacementID       string   `json:"placementId"`
	CallSign          string   `json:"callSign"`
	Affiliate         string   `json:"affiliate"`
	AffiliateCallSign string   `json:"affiliateCallSign"`
	Filters           []string `json:"filters,omitempty"`
	LogoURL           string   `json:"logoUrl"`
}

// lineupToJSON converts a guide's lineup into the API shape. Provider naming
// comes from the saved configuration only when it describes the same source
// the guide was built from.
func lineupToJSON(g *guide.TVGuide, config appconfig.Config, configured bool) APILineup {
	positions := make([]guide.LineupPosition, len(g.Lineup))
	copy(positions, g.Lineup)
	guide.SortLineup(positions)

	out := APILineup{
		Generated: g.Source.GeneratedAt.UTC().Format(time.RFC3339),
		Source: APILineupSource{
			LineupID:   g.Source.LineupID,
			HeadendID:  g.Source.HeadendID,
			PostalCode: g.Source.PostalCode,
			Country:    g.Source.Country,
			Device:     g.Source.Device,
			Language:   g.Source.Language,
		},
		Positions: make([]APILineupPosition, 0, len(positions)),
	}
	if g.Source.GeneratedAt.IsZero() {
		out.Generated = time.Now().UTC().Format(time.RFC3339)
	}
	if configured && sourceMatchesConfig(g.Source, config) {
		out.Source.ProviderName = config.Gracenote.ProviderName
		out.Source.ProviderType = config.Gracenote.ProviderType
		out.Source.Location = config.Gracenote.Location
	}
	for _, p := range positions {
		out.Positions = append(out.Positions, APILineupPosition{
			Number:            p.ChannelNo,
			StationID:         p.StationID,
			PlacementID:       p.PlacementID,
			CallSign:          p.CallSign,
			Affiliate:         p.Affiliate,
			AffiliateCallSign: p.AffiliateCallSign,
			Filters:           p.Filters,
			LogoURL:           p.LogoURL,
		})
	}
	return out
}

// sourceMatchesConfig reports whether a guide was built from the configured lineup.
func sourceMatchesConfig(src guide.Source, config appconfig.Config) bool {
	p := config.Preferences()
	return src.LineupID == p.LineupId && src.HeadendID == p.Headend && src.PostalCode == p.ZipCode &&
		src.Country == p.Country && src.Device == p.Device && src.Language == p.Language
}

// xmltvTimeToISO converts "20250225200000 +0000" → "2025-02-25T20:00:00Z"
// newScrapeOptions builds the production scrape configuration. Tests replace
// it to inject a fake grid fetcher and remove the inter-slot delay.
var newScrapeOptions = func(pref web.Preferences) scrape.Options {
	return scrape.Options{Fetcher: web.NewClient(pref)}
}

// watchSourceChange cancels ctx as soon as sourceCurrent reports false, so a
// lineup change during the grid download stops the scrape promptly. The
// returned func reports whether a change has been observed, re-checking live
// so a change between the last tick and the call is not missed.
func watchSourceChange(ctx context.Context, cancel context.CancelFunc, sourceCurrent func() bool) func() bool {
	if sourceCurrent == nil {
		return func() bool { return false }
	}
	var changed atomic.Bool
	check := func() bool {
		if changed.Load() {
			return true
		}
		if !sourceCurrent() {
			changed.Store(true)
			cancel()
			return true
		}
		return false
	}
	if check() {
		return check
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if check() {
					return
				}
			}
		}
	}()
	return check
}

func handleLineupJSON(state *GuideState, store *appconfig.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		config, configured, _ := store.Get()
		g := state.GetForSource(config.Fingerprint())
		if !configured || g == nil || !sourceMatchesConfig(g.Source, config) {
			w.Header().Set("Retry-After", "30")
			http.Error(w, "Current provider lineup unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lineupToJSON(g, config, true))
	}
}
func selectedRawPositions(in []guide.LineupPosition, allowed map[string]bool) []guide.LineupPosition {
	if allowed == nil {
		return in
	}
	var out []guide.LineupPosition
	for _, p := range in {
		if allowed[p.ChannelNo] {
			out = append(out, p)
		}
	}
	return out
}
func prepareRawLineupLogos(g *guide.TVGuide, baseURL string) {
	propagateChannelLogos(g.Channels, g.Lineup)
	for i := range g.Lineup {
		p := &g.Lineup[i]
		p.LogoURL = stationLogoForOutput(baseURL, p.StationID, p.LogoURL)
	}
}
