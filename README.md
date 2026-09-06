# GraceNote XMLTV Scraper

Generate XMLTV guide data from GraceNote/TMS listings for use with Jellyfin, Plex, Emby, TVHeadend, and other DVR/IPTV software.

## Features

Duplicate review presents whole connected groups of verified SD/HD pairs and exact repeated positions. Checked means Keep. Lower-quality removals are suggested, but all equivalent HD or exact duplicate positions start checked for a manual choice; a lower channel number does not prove better quality. At least one included position per group must remain. Exact repeats require the same nonempty Gracenote station ID and callsign; names alone never create these groups.

Channel programme popups close when clicking outside their box. Popups requiring choices, such as export and duplicate review, retain explicit controls. SD suggestions accept multiple HD positions only when those positions share one nonempty Gracenote station ID; the review link prefers an included position, then the lowest channel number. HD positions themselves are not removed by this rule.

- Scrapes 14 days of GraceNote/TMS program listings and outputs standard XMLTV format
- Enriches programs with TMDB poster images, ratings, descriptions, and release dates
- Enriches channel icons via the [tv-logo/tv-logos](https://github.com/tv-logo/tv-logos) project
- Runs as a long-lived server with automatic 24-hour refresh, or as a one-shot scrape for cron jobs
- First-run ZIP/postal-code setup with cable, satellite, and over-the-air lineup selection
- Lineuparr JSON builder for the active provider, with attributable aliases, category review, per-channel inclusion, and optional duplicate-SD cleanup
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
| `LINEUPARR_STATE_PATH` | Saved channel inclusion and category choices for the current lineup | `lineuparr_state.json` |
| `LINEUPARR_CACHE_DIR` | Cache for public Lineuparr and iptv-org enrichment sources | `lineuparr_source_cache` |
| `LINEUPARR_EXPORT_DIR` | Persistent last-exported JSON snapshots, keyed by export filename | `lineuparr_exports` beside `LINEUPARR_STATE_PATH` |
| `LINEUPARR_CATALOG_URLS` | Comma-separated Lineuparr JSON source override. Blank uses the matching built-in source list; `off` disables catalogs. | — |
| `LINEUPARR_IPTV_ORG_URL` | Public channel database URL. Set to `off` to disable. | `https://iptv-org.github.io/api/channels.json` |
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

The workflow places Enrichment sources, Alias discovery, Major market enrichment,
TMDB category evidence, Category review, Channels, Dispatcharr matching, and
Export JSON in that order when those features are available. Enrichment sources
remains collapsed by default in the combined application.

Export JSON is a separate section at the bottom. After publishing, it shows the
saved file's creation time, copyable browser and configured Docker-network URLs,
and a download link. This summary survives page reloads. Reading it or downloading
the saved file does not publish edits; use Export JSON again to update the snapshot.

Click **Export JSON** and choose **Download JSON** or **Copy URL**. Either option publishes a snapshot of your current included channels, categories, and aliases. Cancelling the dialog does not publish anything. The download and URL serve the same saved JSON. Copy URL leaves you on the builder page and shows a selectable URL if automatic clipboard access is unavailable.

When an Internal base URL is saved in Setup, **Copy Docker-network URL** is also available. It publishes the same snapshot using the configured internal hostname and listening port; it does not create a different lineup or change image URLs. Use this link only from containers sharing the scraper's Docker network. Downloads still use the browser-accessible address. If optional internal-link settings are unavailable, normal downloads and browser URLs remain usable.

The URL serves the **last explicitly exported version**, using the download's descriptive filename: `/lineuparr/exports/US_Optimum-of-Woodbury-Digital-11743_lineup.json`, for example. Editing channels, refreshing enrichment, or fetching the URL does not change it; reopen Export and choose either option to update that filename's snapshot. A different provider name or ZIP creates a different filename and URL, while exporting the same filename replaces its previous snapshot. Previously exported filenames remain available until removed from `LINEUPARR_EXPORT_DIR`. Keep this directory on persistent storage so snapshots survive container replacement and remain readable during guide rebuilds. Older fingerprint URLs are no longer supported; export again to create the descriptive URL.

Use a scraper hostname and port that the Lineuparr host can reach. A compatible Lineuparr URL-import action can fetch the JSON using its normal lineup filename from the `Content-Disposition` header. The URL grants read access to the exported lineup to anyone who can reach the scraper. It contains no provider credentials or stream URLs. The existing `/api/lineuparr/export` endpoint remains a direct download of the live draft for older clients; it does not update the published snapshot.

The builder at `/lineuparr` is an extension of the active scraper lineup rather than a second provider configuration. Gracenote remains authoritative for provider membership and channel numbers. Every raw provider position starts included, even when two positions point to the same station, so SD removal is an explicit and reversible choice.

Aliases derived directly from Gracenote include callsigns, station IDs, lineup-position IDs, number-plus-callsign names, safe affiliate names, and event callsigns. The corresponding Gracenote station ID is exported as `epg_ids`; exact catalog and iptv-org matches may add their attributable EPG identifiers. The builder then applies only unique exact matches from:

Both detailed and compact channel rows lead with the callsign and append the best distinct name from attributable affiliate, official-provider, or catalog evidence; punctuation-only, identifier, and channel-number duplicates are suppressed. The channel list opens with only included positions visible and can be sorted by channel number or name; excluded positions remain available through the visibility filter. Clicking the name opens the next 24 current or upcoming programmes from the selected guide to help identify an unfamiliar channel. **Batch categorize** can select every currently visible filtered row and apply one category atomically.

The same GN brand mark shown in the page header is served as the SVG favicon for the guide, setup, and Lineuparr pages.

- Matching provider and country catalogs from [Dispatcharr Lineuparr Plugin](https://github.com/matrix2669/Dispatcharr-Lineuparr-Plugin). US defaults select a Verizon FiOS, DIRECTV, or DISH provider catalog when applicable and also use the combined US catalog; other currently mapped catalogs cover the UK, Canada, Australia, Spain, France, and the Netherlands.
- The public-domain [iptv-org channel database](https://github.com/iptv-org/database), restricted to the active lineup country and active channel records.

Ambiguous source identities are counted but never applied. Channels without an attributable category remain in an honest `Uncategorized` group and are highlighted for review. Program genres and Gracenote's station filters are not used as channel-category guesses.

The optional SD-duplicate action is conservative: it appears when two provider positions map to the same exact sourced identity and one has a stronger HD, UHD, 4K, or digital marker, or when normalized callsigns differ only by a terminal `HD`, `SD`, or unnumbered broadcast `DT` suffix and have one unique strongest variant. It can also bridge different callsign spellings when exactly two positions share the same normalized, nonnumeric alias and both positions have attributable evidence. An explicitly marked SD position still requires the other position to share that alias through the same source. An unmarked lower-quality position may instead pair with an explicitly marked HD/digital position only when both share a non-schedule source or confirmed schedule evidence on one position references the opposite position's actual channel number. A pair such as `WCBS`/`WCBSDT` keeps the `DT` digital/HD position and proposes the otherwise identical base position for removal; `NWSNTSD`/`NEWSNTN` uses the shared exact `NewsNation` identity to propose the explicitly marked SD position; and `I24NWEN`/`I24NEHD` uses the exact normalized `i24 News` identity plus its cross-position evidence to keep the explicitly marked HD position. Quality-suffix and attributable-alias matching require a base of at least three alphanumeric characters where a suffix is interpreted, never strip numbered digital-subchannel suffixes such as `DT2` or `DT3`, reject schedule-only bridges to unrelated or self-referential positions, and suppress ambiguous groups or competing keep candidates. Clicking the action opens every proposed remove/keep pair for review; all safe proposals start selected, individual pairs can be unchecked, and only the confirmed subset is excluded. The affected channels remain individually reversible, and **Restore all** puts every provider position back into the export. The `Categorized` and `Needs category` header totals count only channels currently included in the draft.

Source failures do not interrupt guide generation or prevent a Gracenote-only export. Successful public-source downloads are cached for 24 hours, and an older cache is used when a refresh fails. Source URLs are server configuration; credentials and stream URLs are never part of the exported JSON.

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
| `POST /api/lineuparr/categories` | Atomically apply one category to a validated channel selection |
| `GET /api/lineuparr/channel-programs?channelId=<id>` | Return up to 24 current/upcoming programmes from the selected guide |
| `POST /api/lineuparr/remove-duplicates` | Exclude the reviewed `channelIds` subset, or all current suggestions for backward-compatible requests without that field |
| `POST /api/lineuparr/restore-all` | Restore every provider channel to the export |
| `GET /api/lineuparr/export` | Download the current Lineuparr-compatible JSON file |
| `POST /api/lineuparr/publish` | Save the current draft as the published snapshot; requires the draft's `sourceFingerprint`; returns its relative URL and filename |
| `GET, HEAD /lineuparr/exports/{filename}` | Read the last explicitly exported JSON by its descriptive download filename, without rebuilding; `?download=1` requests an attachment |
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
