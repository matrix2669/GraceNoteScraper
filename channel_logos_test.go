package main

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	"github.com/daniel-widrick/GraceNoteScraper/scrape"
	"github.com/daniel-widrick/GraceNoteScraper/tvlogo"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

const (
	providerAntenna     = "https://zpmc.tmsimg.com/h3/NowShowing/50777/GNLZZGG0028Y4HS.png"
	providerMilitary    = "https://zpmc.tmsimg.com/h3/NowShowing/48999/s48999_ll_h15_ac.png"
	tvMilitary          = "https://raw.githubusercontent.com/tv-logo/tv-logos/main/countries/united-states/military-history-us.png"
	providerPlaceholder = "https://zpmc.tmsimg.com/sources/generic/generic_sources_h3.png"
)

func TestGracenoteLogoFallbackStrictOriginAndStation(t *testing.T) {
	for _, tt := range []struct{ label, id, input, want string }{
		{"migrate HTTP from guide", "50777", strings.Replace(providerAntenna, "https:", "http:", 1), providerAntenna},
		{"HTTPS unchanged", "50777", providerAntenna, providerAntenna},
		{"provider protocol relative", "50777", strings.TrimPrefix(providerAntenna, "https:"), providerAntenna},
		{"uses station directory not internal image name", "70260", "https://zpmc.tmsimg.com/h3/NowShowing/70260/s119814_ll_h15_ab.png", "https://zpmc.tmsimg.com/h3/NowShowing/70260/s119814_ll_h15_ab.png"},
		{"never use parent or other station", "20373", providerAntenna, ""},
		{"generic placeholder", "50777", providerPlaceholder, ""},
		{"untrusted host", "50777", strings.Replace(providerAntenna, "zpmc.tmsimg.com", "zpmc.tmsimg.com.example.net", 1), ""},
		{"credentials", "50777", strings.Replace(providerAntenna, "zpmc.tmsimg.com", "other@zpmc.tmsimg.com", 1), ""},
		{"port", "50777", strings.Replace(providerAntenna, "zpmc.tmsimg.com", "zpmc.tmsimg.com:443", 1), ""},
		{"encoded path", "50777", strings.Replace(providerAntenna, "50777", "%350777", 1), ""},
		{"path traversal", "50777", strings.Replace(providerAntenna, "50777", "50777/../50777", 1), ""},
		{"query", "50777", providerAntenna + "?redirect=elsewhere", ""},
		{"fragment", "50777", providerAntenna + "#not-the-image", ""},
		{"non HTTP input", "50777", strings.Replace(providerAntenna, "https:", "ftp:", 1), ""},
	} {
		t.Run(tt.label, func(t *testing.T) {
			if got := guide.StationThumbnailURL(tt.id, tt.input); got != tt.want {
				t.Errorf("fallback = %q, want %q", got, tt.want)
			}
		})
	}
}

type logoFallbackGrid struct {
	grid    *web.GridResponse
	fetches int
}

func (f *logoFallbackGrid) GetDataByTimeContext(_ context.Context, _ int64) (*web.GridResponse, error) {
	f.fetches++
	if f.fetches == 1 {
		return f.grid, nil
	}
	grid := *f.grid
	grid.Channels = append([]web.JSONChannel(nil), grid.Channels...)
	// Same placement and station, a later slice now supplies its logo.
	grid.Channels[1].Thumbnail = strings.TrimPrefix(providerAntenna, "https:") + "?w=55"
	return &grid, nil
}

func TestProviderLogosReachScrapeOutputsWithoutProxyExpansion(t *testing.T) {
	f := &logoFallbackGrid{grid: &web.GridResponse{Channels: []web.JSONChannel{
		{ChannelID: "50777", ID: "row1", ChannelNo: "2.2", CallSign: "WPIXDT2", Thumbnail: strings.TrimPrefix(providerPlaceholder, "https:")},
		{ChannelID: "50777", ID: "row2", ChannelNo: "465", CallSign: "WPIXDT2"},
		{ChannelID: "48999", ID: "row3", ChannelNo: "126", CallSign: "MILH", Thumbnail: strings.TrimPrefix(providerMilitary, "https:")},
		{ChannelID: "141774", ID: "row4", ChannelNo: "239", CallSign: "HEREHD", Thumbnail: strings.TrimPrefix(providerPlaceholder, "https:")},
		{ChannelID: "20373", ID: "row5", ChannelNo: "11", CallSign: "WPIXDT", Thumbnail: strings.TrimPrefix(providerAntenna, "https:")},
	}}}
	t.Chdir(t.TempDir())
	originalScrapeOptions := newScrapeOptions
	t.Cleanup(func() { newScrapeOptions = originalScrapeOptions })
	newScrapeOptions = func(web.Preferences) scrape.Options {
		return scrape.Options{Fetcher: f, Days: 1, SlotDelay: scrape.NoDelay, Logger: log.New(io.Discard, "", 0)}
	}
	pref := web.Preferences{Country: "USA", ZipCode: "00000", Headend: "h", LineupId: "L", Device: "-", Language: "en-us"}
	path := filepath.Join(t.TempDir(), "tvlogo-seed.json")
	c := tvlogo.LoadCache(path)
	for _, id := range []string{"50777", "48999", "141774", "20373"} {
		entry := tvlogo.CacheEntry{}
		if id == "48999" {
			entry.LogoURL = tvMilitary
		}
		c.Set("channelId:"+id, entry)
	}
	c.Save()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("tvlogo_cache.json", b, 0600); err != nil {
		t.Fatal(err)
	}
	// Do not allow external lookup in this fixture; each station has an authoritative
	// current cache entry. The fallback must not change a repo miss into a cache hit.
	g, err := runScrape(pref, nil, "http://guide:8080", nil, "same-source", func() bool { return true }, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if g == nil || len(g.Channels) != 4 || len(g.Lineup) != 5 {
		t.Fatalf("guide shape: %+v", g)
	}
	for _, ch := range g.Channels {
		switch ch.ID {
		case "50777":
			if ch.IconURL != providerAntenna {
				t.Errorf("provider output %q", ch.IconURL)
			}
		case "48999":
			if !strings.HasPrefix(ch.IconURL, "http://guide:8080/img?url=https") || !strings.Contains(ch.IconURL, "military-history-us.png") {
				t.Errorf("verified TV-logo output %q", ch.IconURL)
			}
		default:
			if ch.IconURL != "" {
				t.Errorf("untrusted logo escaped to XMLTV for %s: %q", ch.ID, ch.IconURL)
			}
		}
	}
	for _, p := range g.Lineup {
		for _, ch := range g.Channels {
			if p.StationID == ch.ID && p.LogoURL != ch.IconURL {
				t.Errorf("lineup %s logo %q, channel icon %q", p.ChannelNo, p.LogoURL, ch.IconURL)
			}
		}
	}
	rawLineup := lineupToJSON(g, appconfig.Config{}, false)
	providerPositions := 0
	for _, p := range rawLineup.Positions {
		if p.StationID == "50777" {
			providerPositions++
			if p.LogoURL != providerAntenna {
				t.Errorf("raw lineup response logo = %q", p.LogoURL)
			}
		}
		if p.StationID == "141774" && p.LogoURL != "" {
			t.Errorf("raw lineup response contained placeholder %q", p.LogoURL)
		}
	}
	if providerPositions != 2 {
		t.Errorf("raw lineup has %d provider positions, want 2", providerPositions)
	}
	xmltv, err := os.ReadFile("xmlguide.xmltv")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(xmltv), providerAntenna) || strings.Contains(string(xmltv), "generic_sources_h3") {
		t.Fatalf("bad XMLTV logo output: %s", xmltv)
	}
	if imageURLAllowed(providerAntenna) {
		t.Fatal("provider host was added to image proxy")
	}
	after := tvlogo.LoadCache("tvlogo_cache.json")
	entry, ok := after.Get("channelId:50777")
	if !ok || entry.LogoURL != "" || time.Since(time.Unix(entry.FetchedAt, 0)) > time.Minute {
		t.Fatalf("repo miss replaced: %+v", entry)
	}
}
