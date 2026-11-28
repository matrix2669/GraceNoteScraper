package main

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/guide"
)

func usableGuide(g *guide.TVGuide) bool {
	return g != nil && len(g.Channels) > 0 && len(g.Programs) > 0
}

func resumableTMDB(g *guide.TVGuide, now time.Time) bool {
	return usableGuide(g) && g.TMDBPending && !g.TMDBPendingSince.IsZero() && now.Sub(g.TMDBPendingSince) >= 0 && now.Sub(g.TMDBPendingSince) < guideRefreshInterval
}

// runGuideCycle is serialized by startScraper. Early publication makes the
// guide available to HTTP readers without spawning an overlapping scrape job.
func runGuideCycle(existing *guide.TVGuide, tmdbEnabled bool, now time.Time,
	scrape func(bool, guidePersister) (*guide.TVGuide, error),
	enrich func(*guide.TVGuide) error, persist guidePersister, current func() bool,
) (*guide.TVGuide, error) {
	if !tmdbEnabled || (usableGuide(existing) && !resumableTMDB(existing, now)) {
		return scrape(tmdbEnabled, persist)
	}
	base := existing
	if !resumableTMDB(base, now) {
		var err error
		base, err = scrape(false, func(g *guide.TVGuide) (bool, error) {
			if !usableGuide(g) {
				return false, errors.New("no usable Gracenote guide was downloaded")
			}
			g.TMDBPending = true
			g.TMDBPendingSince = now
			return persist(g)
		})
		if err != nil {
			return nil, err
		}
	}
	if current != nil && !current() {
		return base, errScrapeSourceChanged
	}
	if !resumableTMDB(base, now) {
		return base, errors.New("initial guide was not published for enrichment")
	}
	// The published graph must remain immutable, including nested program slices.
	result := copyGuideForTMDB(base)
	if err := enrich(result); err != nil {
		return base, err
	}
	if current != nil && !current() {
		return base, errScrapeSourceChanged
	}
	result.TMDBPending = false
	result.TMDBPendingSince = time.Time{}
	saved, err := persist(result)
	if err != nil {
		return base, err
	}
	if !saved {
		return base, errScrapeSourceChanged
	}
	return result, nil
}

func copyGuideForTMDB(g *guide.TVGuide) *guide.TVGuide {
	copy := *g
	copy.Channels = append([]guide.Channel(nil), g.Channels...)
	for i := range copy.Channels {
		copy.Channels[i].DisplayNames = append([]guide.DisplayName(nil), g.Channels[i].DisplayNames...)
	}
	copy.Programs = append([]guide.Program(nil), g.Programs...)
	for i := range copy.Programs {
		p := &copy.Programs[i]
		p.Images = append([]guide.Image(nil), p.Images...)
		p.EpisodeNumbers = append([]guide.EpisodeNumber(nil), p.EpisodeNumbers...)
		p.Categories = append([]guide.Category(nil), p.Categories...)
		p.Subtitles = append([]guide.Subtitle(nil), p.Subtitles...)
	}
	return &copy
}

// Initial guides may already contain proxy URLs when enrichment resumes.
// Only newly added artwork needs wrapping; never nest the same proxy URL.
func rewriteGuideImageURLs(g *guide.TVGuide, baseURL string) {
	if baseURL == "" {
		return
	}
	proxy := strings.TrimRight(baseURL, "/") + "/img?url="
	rewrite := func(value string) string {
		if value == "" || strings.HasPrefix(value, proxy) {
			return value
		}
		return proxy + url.QueryEscape(value)
	}
	for i := range g.Channels {
		g.Channels[i].IconURL = rewrite(g.Channels[i].IconURL)
	}
	for i := range g.Programs {
		g.Programs[i].IconSrc = rewrite(g.Programs[i].IconSrc)
		for j := range g.Programs[i].Images {
			g.Programs[i].Images[j].URL = rewrite(g.Programs[i].Images[j].URL)
		}
	}
}
