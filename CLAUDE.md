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
3. `guide.ConvertChannel` / `guide.ConvertEvent` translate the raw JSON into `guide.TVGuide` (internal canonical types). `TVGuide.LineupChannels` separately retains every raw provider position for the Lineuparr builder; the XMLTV `Channels` list may still collapse repeated Gracenote station IDs. The `guide.tmpl` template renders XMLTV. `index.html`, `setup.html`, `lineuparr.html`, and `guide.tmpl` are embedded at build time via `//go:embed`.
4. `tmdb.Client.Lookup` enriches programs (poster images, ratings, overview, year) via TMDB search API. Deduplicates by `(title, isMovie)` before hitting the API. Rate-limited to ~4 req/sec.
5. `tvlogo.Client.Resolve` replaces Gracenote channel icons with verified PNGs from `github.com/tv-logo/tv-logos`. Generates candidate URL slugs from callsign/affiliate name and HEAD-checks each (rate-limited to ~5 req/sec).
6. `fixDeadImageURLs` rewrites `zap2it.tmsimg.com` → `tmsimg.com` for broken Gracenote image URLs.
7. If `BASE_URL` is set, all image URLs are rewritten to route through the local `/img` proxy endpoint.
8. `/lineuparr` builds a source-aware draft from the active lineup. `lineuparr/` derives exact Gracenote aliases, merges unique exact catalog/iptv-org/network identities, applies source-fingerprint-scoped user choices, suggests only quality-marked SD/HD duplicates, and emits the Lineuparr `categories` JSON shape. The primary on-demand discovery pass scans every unique Gracenote provider lineup returned for the configured ZIP, joins each public official provider source to its own grid, and reuses facts in the selected lineup only through an identical Gracenote station ID. `providersource/` owns focused public/no-login provider adapters; generated PrismCast and PBS catalogs provide additional exact-ID evidence. Broad provider group headings such as Optimum `Networks` are not categories by themselves; explicit PEG/public-access identities and attributable broadcast identities map to `Local & Public`. Conflicting exact categories and aliases owned by multiple station IDs are not auto-applied, and Dispatcharr groups are not category evidence. The ranked major-market scanner is intentionally absent; only configured-ZIP scans are supported. Exact sources precede conservative Gracenote schedule-profile category hints. Provider-source routing may use the active postal code and Gracenote location before deciding whether an address is required. Address-gated official sources use explicit, ZIP-restricted OpenStreetMap/Nominatim search through the server; the public endpoint is rate-limited and is never used for autocomplete. The selected address remains browser-only until an immediate provider-adapter request uses it. Enrichment source statuses are consolidated by provider family and include safe selected-lineup match evidence for the browser review dialog; direct PDF URLs remain external document links, while generic provider pages do not replace captured evidence. Remote enrichment is optional and cannot block the guide.

**Caching layers:**

| Cache | File | TTL |
|---|---|---|
| Guide (in-memory + disk) | `guide_cache.json` | 4h (startup skip) / 24h (rescrape) |
| TMDB lookups | `tmdb_cache.json` | 7 days |
| TV logo HEAD checks | `tvlogo_cache.json` | persisted, no expiry |
| Image proxy | `image_cache/` dir | indefinite (per-URL SHA256 key) |
| Lineuparr source data | `lineuparr_source_cache/` dir | 24h fresh; stale fallback on refresh failure |

`lineuparr_state.json` is not an enrichment cache. It stores only explicit inclusion/category choices and is ignored automatically when the active Gracenote source fingerprint changes.

**Server mode startup logic:** The HTTP server starts even without a provider so `/setup` is always recoverable. `/` redirects to setup until a valid source exists. If `xmlguide.xmltv` and a source-matching `guide_cache.json` both exist and the cache is under 4 hours old, the initial scrape is skipped. Otherwise a background scrape is queued. A `sync.RWMutex`-guarded `GuideState` holds the live guide, and `appconfig.Store.WhileCurrent` prevents an old scrape from publishing after a provider change.

**Jellyfin integration:** Optional. When `JELLYFIN_URL` + `JELLYFIN_API_KEY` are set, three extra routes are registered (`/api/livetv/channels`, `/api/livetv/tune`, `/api/livetv/stop`). The tune flow does a 3-step Jellyfin handshake (PlaybackInfo → LiveStreams/Open → build master.m3u8 URL) with a hardcoded 4-second delay before returning the HLS URL. Channel filter (`JELLYFIN_CHANNEL_FILTER`) reduces the guide to only channels Jellyfin has in its live TV lineup, matched by channel number.

**Image proxy allowlist:** Only `image.tmdb.org`, `tmsimg.com`, and `raw.githubusercontent.com/tv-logo/tv-logos/` paths are proxied. All other URLs return 403.
