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

Run `go test ./...` for setup, configuration, provider-client, Lineuparr builder, and Dispatcharr match-review tests.

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
8. `/lineuparr` builds a source-aware draft from the active lineup. `lineuparr/` derives exact Gracenote aliases, merges unique exact catalog/iptv-org identities, applies source-fingerprint-scoped user choices, suggests only quality-marked SD/HD duplicates, and emits the Lineuparr `categories` JSON shape. Remote enrichment is optional and cannot block the guide.
9. `dispatcharr/` runs the full pinned Lineuparr Python matcher only when Refresh streams is explicitly requested. One batched process runs at 70%, exits, and leaves an in-memory snapshot of every scored pair. Go renders and filters that snapshot for current inclusion and decisions; removals filter remaining pairs, while added/reincluded channels, changed names/base aliases/numbers, or a restart require a new explicit refresh. Failed scans retain the previous complete snapshot and overlapping scans return a conflict. The source and MIT license are embedded; native Dispatcharr users need Python 3, while Docker includes Python and pinned RapidFuzz. Returned Score and NameScore are identical and new decisions retain MatcherVersion. Keep the existing 95% export conditions: confirmed below 95 adds a representative alias; denied full names at or above 95 add literal escaped exclusions. No secondary threshold run or Go EPG score boost is applied. See lineuparr_matcher/README.md for pinned hashes, input profile, and the consumer's post-admission number-boost limitation. Historical scores are retained without automatic migration. The browser uses safe metadata only, with no URLs/credentials passed to Python or returned in review.

**Caching layers:**

| Cache | File | TTL |
|---|---|---|
| Guide (in-memory + disk) | `guide_cache.json` | 4h (startup skip) / 24h (rescrape) |
| TMDB lookups | `tmdb_cache.json` | 7 days |
| TV logo HEAD checks | `tvlogo_cache.json` | persisted, no expiry |
| Image proxy | `image_cache/` dir | indefinite (per-URL SHA256 key) |
| Lineuparr source data | `lineuparr_source_cache/` dir | 24h fresh; stale fallback on refresh failure |
| Dispatcharr scored pairs | memory only | explicit refresh; missing/changed inputs require Refresh streams; no timed refresh |

`lineuparr_state.json` is not an enrichment cache. It stores explicit inclusion/category choices, alias suppressions, and Dispatcharr match decisions, including the independent name score used for bounded positive and negative export rules, and is ignored automatically when the active Gracenote source fingerprint changes. `dispatcharr_config.json` is a separate connection file created with mode `0600` on POSIX systems and excluded from Git and Docker build context; JWTs and stream URLs are never persisted.

**Server mode startup logic:** The HTTP server starts even without a provider so `/setup` is always recoverable. `/` redirects to setup until a valid source exists. If `xmlguide.xmltv` and a source-matching `guide_cache.json` both exist and the cache is under 4 hours old, the initial scrape is skipped. Otherwise a background scrape is queued. A `sync.RWMutex`-guarded `GuideState` holds the live guide, and `appconfig.Store.WhileCurrent` prevents an old scrape from publishing after a provider change.

**Jellyfin integration:** Optional. When `JELLYFIN_URL` + `JELLYFIN_API_KEY` are set, three extra routes are registered (`/api/livetv/channels`, `/api/livetv/tune`, `/api/livetv/stop`). The tune flow does a 3-step Jellyfin handshake (PlaybackInfo → LiveStreams/Open → build master.m3u8 URL) with a hardcoded 4-second delay before returning the HLS URL. Channel filter (`JELLYFIN_CHANNEL_FILTER`) reduces the guide to only channels Jellyfin has in its live TV lineup, matched by channel number.

**Image proxy allowlist:** Only `image.tmdb.org`, `tmsimg.com`, and `raw.githubusercontent.com/tv-logo/tv-logos/` paths are proxied. All other URLs return 403.
