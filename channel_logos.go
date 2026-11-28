package main

import (
	"log"
	"net/url"
	"strings"

	"github.com/daniel-widrick/GraceNoteScraper/guide"
	"github.com/daniel-widrick/GraceNoteScraper/tvlogo"
)

// The provider image host is deliberately not added to the image proxy
// allowlist. Existing verified TV logos keep their normal proxy behavior.
func stationLogoForOutput(baseURL, stationID, raw string) string {
	if fallback := guide.StationThumbnailURL(stationID, raw); fallback != "" {
		return fallback
	}
	if baseURL == "" || raw == "" {
		return raw
	}
	proxy := strings.TrimRight(baseURL, "/") + "/img?url="
	if strings.HasPrefix(raw, proxy) {
		return raw
	}
	return proxy + url.QueryEscape(raw)
}

// propagateChannelLogos copies the same resolution to every provider placement
// of that station, including deliberately blank logo results.
func propagateChannelLogos(channels []guide.Channel, lineup []guide.LineupPosition) {
	byStation := make(map[string]string, len(channels))
	for _, ch := range channels {
		byStation[ch.ID] = ch.IconURL
	}
	for i := range lineup {
		if logo, ok := byStation[lineup[i].StationID]; ok {
			lineup[i].LogoURL = logo
		}
	}
}

func enrichChannelIcons(client *tvlogo.Client, channels []guide.Channel) {
	verified, fallback := 0, 0
	for i := range channels {
		ch := &channels[i]
		// Keep the source image out of the tv-logos cache; a later lookup
		// that finds the preferred asset can still replace the fallback.
		if logo := client.Resolve(ch.ID, ch.CallSign, ch.Affiliate, ch.ChannelNo); logo != "" {
			ch.IconURL = logo
			verified++
			continue
		}
		ch.IconURL = guide.StationThumbnailURL(ch.ID, ch.IconURL)
		if ch.IconURL != "" {
			fallback++
		}
	}
	log.Printf("TV logos: resolved %d/%d from tv-logos; %d Gracenote fallbacks", verified, len(channels), fallback)
}
