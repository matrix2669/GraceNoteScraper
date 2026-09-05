# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```sh
# Build
go build -o gracenotescraper .

# Run (server mode, port 8080)
./gracenotescraper

# Run (scrape once and exit — no server)
./gracenotescraper --guide-only

# Docker
docker compose up -d
docker compose logs -f
docker compose up -d --build

# Release builds (CGO disabled, trimmed)
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o gracenotescraper .
```

Run `go test ./...` for the setup, configuration, and provider-client tests.

## Architecture

The binary is a single Go process that scrapes GraceNote/TMS for 14 days of TV listings and serves the data as XMLTV over HTTP. Runtime orchestration lives in `main.go`; setup handlers live in `setup.go` and persisted configuration lives in `appconfig/`.

**Data flow:**

1. `/setup` uses `web.ProviderClient` to discover Gracenote lineups by country and postal code. `appconfig.Store` persists the selected non-secret source in `config.json`; complete legacy `GN_*` settings can bootstrap it.
2. `web.Client.GetDataByTime` fetches 6-hour grid slices from the GraceNote API (`tvlistings.gracenote.com/api/grid`) — 56 slots for 14 days. A 5-second sleep separates requests. Raw JSON types live in `web/web.go`.
3. `guide.ConvertChannel` / `guide.ConvertEvent` translate the raw JSON into `guide.TVGuide` (internal canonical types). The `guide.tmpl` template renders these to XMLTV. `index.html`, `setup.html`, and `guide.tmpl` are embedded at build time via `//go:embed`.
4. `tmdb.Client.Lookup` enriches programs (poster images, ratings, overview, year) via TMDB search API. Deduplicates by `(title, isMovie)` before hitting the API. Rate-limited to ~4 req/sec.
5. `tvlogo.Client.Resolve` replaces Gracenote channel icons with verified PNGs from `github.com/tv-logo/tv-logos`. Generates candidate URL slugs from callsign/affiliate name and HEAD-checks each (rate-limited to ~5 req/sec).
6. `fixDeadImageURLs` rewrites `zap2it.tmsimg.com` → `tmsimg.com` for broken Gracenote image URLs.
7. If `BASE_URL` is set, all image URLs are rewritten to route through the local `/img` proxy endpoint.

**Caching layers:**

Initial server guides are published before TMDB in `runGuideCycle`; regular refreshes and guide-only mode retain full-cycle publication. `TVGuide.TMDBPending` and `TMDBPendingSince` survive guide-cache persistence and channel filtering. Fresh pending guides resume from cached programmes; expired pending guides use normal refresh. Only the serialized scraper writes guides. TMDB mutates a private copy (including nested programme slices), then the source-guarded persister swaps it. Setup `guideReady` is independent of `running`. The background-aware alias coordinator may permit local provider/source work during `tmdb_background` with nonzero channel/program counts, without clearing the scraper's running state; ordinary guide stages and final saving remain gated. Source changes/shutdown stop new worker lookups; in-flight calls may finish but cannot publish obsolete data. Image proxy rewriting must remain idempotent for resumed guides.

| Cache | File | TTL |
|---|---|---|
| Guide (in-memory + disk) | `guide_cache.json` | 24h freshness; stale source-matching fallback during refresh |
| TMDB lookups | `tmdb_cache.json` | 7 days |
| TV logo HEAD checks | `tvlogo_cache.json` | persisted, no expiry |
| Image proxy | `image_cache/` dir | indefinite (per-URL SHA256 key) |

**Server mode startup logic:** The HTTP server starts even without a provider so `/setup` is always recoverable. `/` redirects to setup until a valid source exists. A source-matching guide cache younger than 24 hours is loaded and schedules its next scrape for the remainder of that interval. An older source-matching cache remains available while an immediate background refresh runs; failed refreshes retain that stale guide for the existing 15-minute retry. If the cache is usable but `xmlguide.xmltv` is missing, the XMLTV file is rebuilt locally from the cache. Missing, unreadable, corrupt, and source-mismatched cache states are logged explicitly; only source changes invalidate the old lineup's artifacts. Cache writes and XMLTV writes replace their respective files atomically. A `sync.RWMutex`-guarded `GuideState` holds the live guide, and `appconfig.Store.WhileCurrent` prevents an old scrape from publishing after a provider change. Routine progress uses stdout while classified warnings and errors use stderr.

**Jellyfin integration:** Optional. When `JELLYFIN_URL` + `JELLYFIN_API_KEY` are set, three extra routes are registered (`/api/livetv/channels`, `/api/livetv/tune`, `/api/livetv/stop`). The tune flow does a 3-step Jellyfin handshake (PlaybackInfo → LiveStreams/Open → build master.m3u8 URL) with a hardcoded 4-second delay before returning the HLS URL. Channel filter (`JELLYFIN_CHANNEL_FILTER`) reduces the guide to only channels Jellyfin has in its live TV lineup, matched by channel number.

**Image proxy allowlist:** Only `image.tmdb.org`, `tmsimg.com`, and `raw.githubusercontent.com/tv-logo/tv-logos/` paths are proxied. All other URLs return 403.
