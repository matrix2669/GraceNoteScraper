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

Run `go test ./...` for setup, configuration, provider-client, and Lineuparr builder tests.

## Architecture

The binary is a single Go process that scrapes GraceNote/TMS for 14 days of TV listings and serves the data as XMLTV over HTTP. Runtime orchestration lives in `main.go`; setup handlers live in `setup.go` and persisted configuration lives in `appconfig/`.

**Data flow:**

1. `/setup` uses `web.ProviderClient` to discover Gracenote lineups by country and postal code. `appconfig.Store` persists the selected non-secret source in `config.json`; complete legacy `GN_*` settings can bootstrap it.
2. `web.Client.GetDataByTime` fetches 6-hour grid slices from the GraceNote API (`tvlistings.gracenote.com/api/grid`) — 56 slots for 14 days. A 5-second sleep separates requests. Raw JSON types live in `web/web.go`.
3. `guide.ConvertChannel` / `guide.ConvertEvent` translate the raw JSON into `guide.TVGuide` (internal canonical types). `TVGuide.LineupChannels` separately retains every raw provider position for the Lineuparr builder; the XMLTV `Channels` list may still collapse repeated Gracenote station IDs. The `guide.tmpl` template renders XMLTV. `index.html`, `setup.html`, `lineuparr.html`, `favicon.svg`, and `guide.tmpl` are embedded at build time via `//go:embed`.
4. `tmdb.Client.Lookup` enriches programs (poster images, ratings, overview, year) via TMDB search API. Deduplicates by `(title, isMovie)` before hitting the API. Rate-limited to ~4 req/sec.
5. `tvlogo.Client.Resolve` replaces Gracenote channel icons with verified PNGs from `github.com/tv-logo/tv-logos`. Generates candidate URL slugs from callsign/affiliate name and HEAD-checks each (rate-limited to ~5 req/sec).
6. `fixDeadImageURLs` rewrites `zap2it.tmsimg.com` → `tmsimg.com` for broken Gracenote image URLs.
7. If `BASE_URL` is set, all image URLs are rewritten to route through the local `/img` proxy endpoint.
8. `/lineuparr` builds a source-aware draft from the active lineup. `lineuparr/` derives exact Gracenote aliases, merges unique exact catalog/iptv-org identities, applies source-fingerprint-scoped user choices, suggests only quality-marked SD/HD duplicates, and emits the Lineuparr `categories` JSON shape. Duplicate cleanup recognizes exact terminal `HD`, `SD`, and unnumbered broadcast `DT` pairs while preserving numbered subchannels such as `DT2` and `DT3`. It may bridge different callsigns only when exactly two positions share one normalized nonnumeric alias, both positions have attributable evidence other than raw `gracenote`, one position has a unique stronger quality rank, and no competing keep candidate exists. Explicit-SD removal requires a source shared by both positions; unmarked-to-HD removal requires a shared non-schedule source or confirmed schedule evidence whose provider-position token names the opposite position's actual channel number. It counts only suggested channels that remain included and requires a browser review of remove/keep pairs. Categorized and uncategorized summary totals likewise count only included channels. The API accepts an optional validated `channelIds` subset while preserving the older all-suggestions request. Detailed and compact rows lead with callsign and choose one distinct attributable descriptive name. The browser defaults to included positions and supports stable number/name sorting without changing draft order. Category batches validate every selected position before one atomic state save. The channel-program endpoint reads only the active selected guide and returns at most 24 current/upcoming programmes. Remote enrichment is optional and cannot block the guide.

**Caching layers:**

| Cache | File | TTL |
|---|---|---|
| Guide (in-memory + disk) | `guide_cache.json` | 4h (startup skip) / 24h (rescrape) |
| TMDB lookups | `tmdb_cache.json` | 7 days |
| TV logo HEAD checks | `tvlogo_cache.json` | persisted, no expiry |
| Image proxy | `image_cache/` dir | indefinite (per-URL SHA256 key) |
| Lineuparr source data | `lineuparr_source_cache/` dir | 24h fresh; stale fallback on refresh failure |

`lineuparr_state.json` is not an enrichment cache. It stores only explicit inclusion/category choices and is ignored automatically when the active Gracenote source fingerprint changes.

Published Lineuparr JSON is separate from draft state. `POST /api/lineuparr/publish` validates the browser's source fingerprint, builds through the same draft/export path as direct downloads, and atomically replaces one versioned record per source under `LINEUPARR_EXPORT_DIR` (default: `lineuparr_exports` beside the state file). The record includes filename, publication time, and the exported JSON only. `GET`/`HEAD /lineuparr/exports/{fingerprint}/lineup.json` reads that saved record without the current configuration, guide, builder, or external requests; the fixed basename and hexadecimal fingerprint validation prevent arbitrary file access. The URL remains stable when the provider's display name changes; the response supplies the current Lineuparr filename in `Content-Disposition`, is not cached, and uses `inline` unless `download=1`. Draft edits never republish implicitly. Earlier lineups' snapshots remain available until manually removed. The export dialog publishes only after Download or Copy URL is selected and reuses the result within that dialog. Export waits for pending browser channel saves. The legacy direct-download endpoint remains live-draft-only.

**Server mode startup logic:** The HTTP server starts even without a provider so `/setup` is always recoverable. `/` redirects to setup until a valid source exists. If `xmlguide.xmltv` and a source-matching `guide_cache.json` both exist and the cache is under 4 hours old, the initial scrape is skipped. Otherwise a background scrape is queued. A `sync.RWMutex`-guarded `GuideState` holds the live guide, and `appconfig.Store.WhileCurrent` prevents an old scrape from publishing after a provider change.

**Jellyfin integration:** Optional. When `JELLYFIN_URL` + `JELLYFIN_API_KEY` are set, three extra routes are registered (`/api/livetv/channels`, `/api/livetv/tune`, `/api/livetv/stop`). The tune flow does a 3-step Jellyfin handshake (PlaybackInfo → LiveStreams/Open → build master.m3u8 URL) with a hardcoded 4-second delay before returning the HLS URL. Channel filter (`JELLYFIN_CHANNEL_FILTER`) reduces the guide to only channels Jellyfin has in its live TV lineup, matched by channel number.

**Image proxy allowlist:** Only `image.tmdb.org`, `tmsimg.com`, and `raw.githubusercontent.com/tv-logo/tv-logos/` paths are proxied. All other URLs return 403.
