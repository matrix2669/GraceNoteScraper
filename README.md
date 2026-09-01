# GraceNote XMLTV Scraper

Generate XMLTV guide data from GraceNote/TMS listings for use with Jellyfin, Plex, Emby, TVHeadend, and other DVR/IPTV software.

## Features

- Scrapes 14 days of GraceNote/TMS program listings and outputs standard XMLTV format
- Enriches programs with TMDB poster images, ratings, descriptions, and release dates
- Enriches channel icons via the [tv-logo/tv-logos](https://github.com/tv-logo/tv-logos) project
- Runs as a long-lived server with automatic 24-hour refresh, or as a one-shot scrape for cron jobs
- First-run ZIP/postal-code setup with cable, satellite, and over-the-air lineup selection
- Lineuparr JSON builder for the active provider, with attributable aliases, category review, per-channel inclusion, and optional duplicate-SD cleanup
- Optional Dispatcharr M3U matching with explicit confirm/deny review and reversible alias cleanup
- Guide data cached on disk — fast restarts without re-scraping
- Automatic XMLTV file rotation with 7-day retention
- Optional Jellyfin Live TV integration with in-browser streaming
- Optional channel filter to limit guide output to Jellyfin-available channels
- Bonus: built-in retro TV guide web UI ("The Grid")

## Jellyfin / Plex Setup

1. Run the scraper in server mode (see below).
2. Open `http://<your-host>:8080/setup` and choose your provider lineup.
3. When the first guide finishes building, add an XMLTV guide source in your DVR software pointing to:
   ```
   http://<your-host>:8080/xmlguide.xmltv
   ```
4. Guide data refreshes automatically every 24 hours.

Alternatively, use `--guide-only` mode with a cron job and point your DVR software at the local `xmlguide.xmltv` file.

## Quick Start (Docker Compose)

1. Clone the repo:
   ```sh
   git clone https://github.com/daniel-widrick/GraceNoteScraper.git
   cd GraceNoteScraper
   ```

2. Copy the environment file for optional integrations:
   ```sh
   cp .env.example .env
   # Optionally add a TMDB token, Jellyfin settings, or legacy GN_* settings
   ```

3. Start the container:
   ```sh
   docker compose up -d
   ```

4. Open `http://<your-host>:8080/setup`, enter your ZIP or postal code, and choose a provider lineup.

5. After the first guide finishes building, point your DVR software at `http://<your-host>:8080/xmlguide.xmltv`.

6. Open `http://<your-host>:8080/lineuparr` to review and export a Lineuparr-compatible JSON file for the same active lineup.

Setup, guide data, caches, and images are persisted in a Docker volume. The container restarts automatically and refreshes guide data every 24 hours.

To view logs:
```sh
docker compose logs -f
```

To rebuild after pulling updates:
```sh
docker compose up -d --build
```

## Requirements

- Docker and Docker Compose, **or** Go 1.25+ for building from source
- (Optional) A [TMDB API read access token](https://www.themoviedb.org/settings/api) for poster images and metadata

## Building from Source

If you prefer to run without Docker:

```sh
go build -o gracenotescraper .
cp .env.example .env
# Edit .env
./gracenotescraper
```

### Scrape-only mode

Scrapes once, writes `xmlguide.xmltv` to the working directory, and exits. Useful for cron-based setups where you don't need the server running.

Run server mode once to save a provider through `/setup`, or provide complete legacy `GN_*` settings, before using this mode.

```sh
./gracenotescraper --guide-only
```

## Environment Variables

| Variable | Description | Default |
|---|---|---|
| `CONFIG_PATH` | Saved non-secret setup configuration | `config.json` |
| `LINEUPARR_STATE_PATH` | Saved channel, alias, and Dispatcharr match-review choices for the current lineup | `lineuparr_state.json` |
| `LINEUPARR_CACHE_DIR` | Cache for public Lineuparr and iptv-org enrichment sources | `lineuparr_source_cache` |
| `LINEUPARR_CATALOG_URLS` | Comma-separated Lineuparr JSON source override. Blank uses the matching built-in source list; `off` disables catalogs. | — |
| `LINEUPARR_IPTV_ORG_URL` | Public channel database URL. Set to `off` to disable. | `https://iptv-org.github.io/api/channels.json` |
| `DISPATCHARR_CONFIG_PATH` | Separate owner-only Dispatcharr connection file saved from the builder | `dispatcharr_config.json` |
| `GN_HEADEND` | Legacy/bootstrap GraceNote headend ID; use with `GN_LINEUP` and `GN_ZIPCODE` | — |
| `GN_LINEUP` | Legacy/bootstrap full lineup string | — |
| `GN_COUNTRY` | Country code | `USA` |
| `GN_ZIPCODE` | Legacy/bootstrap ZIP or postal code | — |
| `GN_LANGUAGE` | Language code | `en-us` |
| `GN_DEVICE` | Device identifier | `-` |
| `TMDB_TOKEN` | TMDB read access token (optional) | — |
| `BASE_URL` | Server base URL — rewrites XMLTV image URLs to use the built-in proxy cache (e.g. `http://192.168.1.50:8080`) | — |
| `PORT` | HTTP server port | `8080` |
| `JELLYFIN_URL` | Jellyfin server URL (optional — enables live TV integration) | — |
| `JELLYFIN_API_KEY` | Jellyfin API key | — |
| `JELLYFIN_CHANNEL_FILTER` | Set to any non-empty value to filter guide to only Jellyfin-available channels. Requires `JELLYFIN_URL` and `JELLYFIN_API_KEY`. | — |

A saved `CONFIG_PATH` selection takes precedence over legacy `GN_*` settings. Delete or move that file if you intentionally want to bootstrap from environment settings again.

## Lineuparr JSON Builder

The builder at `/lineuparr` is an extension of the active scraper lineup rather than a second provider configuration. Gracenote remains authoritative for provider membership and channel numbers. Every raw provider position starts included, even when two positions point to the same station, so SD removal is an explicit and reversible choice.

Aliases derived directly from Gracenote include callsigns, station IDs, lineup-position IDs, number-plus-callsign names, safe affiliate names, and event callsigns. The corresponding Gracenote station ID is exported as `epg_ids`; exact catalog and iptv-org matches may add their attributable EPG identifiers. The builder then applies only unique exact matches from:

- Matching provider and country catalogs from [Dispatcharr Lineuparr Plugin](https://github.com/matrix2669/Dispatcharr-Lineuparr-Plugin). US defaults select a Verizon FiOS, DIRECTV, or DISH provider catalog when applicable and also use the combined US catalog; other currently mapped catalogs cover the UK, Canada, Australia, Spain, France, and the Netherlands.
- The public-domain [iptv-org channel database](https://github.com/iptv-org/database), restricted to the active lineup country and active channel records.

Ambiguous source identities are counted but never applied. Channels without an attributable category remain in an honest `Uncategorized` group and are highlighted for review. Program genres and Gracenote's station filters are not used as channel-category guesses.

The optional **Remove suggested SD** action is conservative: it appears only when two provider positions map to the same exact sourced identity and one has a stronger HD, UHD, 4K, or digital marker. The affected channels remain individually reversible, and **Restore all** puts every provider position back into the export.

Source failures do not interrupt guide generation or prevent a Gracenote-only export. Successful public-source downloads are cached for 24 hours, and an older cache is used when a refresh fails. Source URLs are server configuration; credentials and stream URLs are never part of the exported JSON.

### Dispatcharr match review

The optional Dispatcharr panel compares the active lineup with every non-stale stream from active M3U accounts. Choose either a normal Dispatcharr username/password or an API key; only the fields for the selected method are shown and enabled. Password authentication uses Dispatcharr's JWT API and keeps access and refresh tokens in memory only. The saved connection settings live in the separate `DISPATCHARR_CONFIG_PATH` file, created with owner-only (`0600`) permissions on POSIX systems. The default file is excluded from Git and Docker build context. Use HTTPS unless both applications communicate only over a trusted private network.

Matching prioritizes exact EPG IDs, direct channel names, and attributable aliases before offering bounded fuzzy-name candidates. Delimited `US`, `GO`, `Prime`, `Tubi`, and `ROKU` provider prefixes, common HD/UHD markers, punctuation, and spacing are normalized. A leading HDHomeRun-style number is removed only when it exactly equals Dispatcharr's channel-number metadata, so event years and unrelated numeric names remain intact. A score is never accepted automatically:

- **Confirm** adds the stream name and, when present, its `tvg_id` to that specific lineup position with Dispatcharr provenance.
- **Deny** records only that stream/channel pairing as rejected and reveals the next candidate when one exists.
- **Undo** reverses either decision. Confirmed aliases can also be removed from or restored to the export with the same alias controls used for other sources.

Confirm and deny actions remove their row immediately without locking the remaining queue. Decisions retain the safe normalized stream identity as well as the active lineup, stream fingerprint, and target channel, so equivalent account variants, authentication changes, and container restarts do not restore reviewed rows. The confirmed counter opens reviewed matches. TVG-ID choices distinguish an ID already known to the selected channel from an additional provider-reported ID and show the contributing raw stream names and M3U accounts.

Only the metadata needed for review—stream ID, name, `tvg_id`, M3U account/group IDs, and provider channel number—is retained. Dispatcharr stream URLs, logos, tokens, and statistics are discarded as the API response is decoded and are never returned to the browser, saved in Lineuparr state, or exported. Stream lists are cached in memory for five minutes; if a refresh fails, a visible warning identifies the older list being used.

## HTTP Endpoints

| Endpoint | Description |
|---|---|
| `GET /setup` | Choose or change the active provider lineup |
| `GET /api/setup/config` | Read the current non-secret lineup selection |
| `GET /api/setup/providers?postalCode=...` | Find Gracenote lineups for an area |
| `POST /api/setup/provider` | Save the selected provider and queue a fresh guide |
| `GET /lineuparr` | Review the current lineup and export Lineuparr JSON |
| `GET /api/lineuparr/draft` | Current builder draft with aliases, provenance, and duplicate suggestions |
| `POST /api/lineuparr/channel` | Include/exclude one channel or update its category |
| `POST /api/lineuparr/alias` | Remove or restore one attributable alias for the active lineup |
| `POST /api/lineuparr/remove-duplicates` | Exclude all current duplicate-SD suggestions |
| `POST /api/lineuparr/restore-all` | Restore every provider channel to the export |
| `GET /api/lineuparr/export` | Download the current Lineuparr-compatible JSON file |
| `GET, POST, DELETE /api/lineuparr/dispatcharr/config` | Read, test/save, or remove the Dispatcharr connection; saved credentials are never returned |
| `GET /api/lineuparr/dispatcharr/review` | Fetch the current safe M3U match-review queue; add `?refresh=true` to refresh streams |
| `POST, DELETE /api/lineuparr/dispatcharr/decision` | Confirm/deny a current candidate, or undo one reviewed decision |
| `GET /xmlguide.xmltv` | XMLTV guide data (point your DVR here) |
| `GET /api/guide.json` | Guide data as JSON |
| `GET /` | The Grid — built-in web UI |
| `GET /img?url=...` | Image proxy with local cache |
| `GET /api/livetv/config` | Returns `{"enabled":true/false}` — whether Jellyfin live TV is configured |
| `GET /api/livetv/channels` | Proxies Jellyfin channel list (requires `JELLYFIN_URL` and `JELLYFIN_API_KEY`) |
| `GET /api/livetv/tune?id=<channelId>` | Starts a live stream for the given channel and returns an HLS URL |
| `POST /api/livetv/stop` | Forwards a playback-stop notification to Jellyfin to end a live stream |

## The Grid

The server includes a built-in retro-styled TV guide web UI at the root URL. If no provider is configured, `/` redirects to `/setup`. Once configured, the guide auto-scrolls through your channel lineup and shows program details, posters, and metadata. Handy for a quick glance at what's on without opening your DVR app.

![The Grid](https://gist.githubusercontent.com/daniel-widrick/2c52c4d023ffe75d163b4eff58263c77/raw/demo.gif)

## Project Structure

```
appconfig/       Persisted non-secret provider configuration
lineuparr/        Source-aware Lineuparr draft, state, duplicate review, and export
dispatcharr/      Private connection store, safe stream client, and manual-review matcher
main.go          Entry point, HTTP server, scraper, image proxy
guide/           GraceNote data types and XMLTV conversion
web/             HTTP client for GraceNote API
tmdb/            TMDB client and cache
tvlogo/          TV logo resolver and cache
util/            Shared helpers
index.html       The Grid web UI (embedded at build time)
setup.html       Provider-selection UI (embedded at build time)
lineuparr.html   Lineuparr review/export UI (embedded at build time)
guide.tmpl       XMLTV output template (embedded at build time)
```

---

<sub>Portions of this project were developed with the assistance of generative AI ([Claude](https://claude.ai)).</sub>
