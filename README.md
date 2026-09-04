# GraceNote XMLTV Scraper

Generate XMLTV guide data from GraceNote/TMS listings for use with Jellyfin, Plex, Emby, TVHeadend, and other DVR/IPTV software.

## Features

- Scrapes 14 days of GraceNote/TMS program listings and outputs standard XMLTV format
- Enriches programs with TMDB poster images, ratings, descriptions, and release dates
- Enriches channel icons via the [tv-logo/tv-logos](https://github.com/tv-logo/tv-logos) project
- Runs as a long-lived server with automatic 24-hour refresh, or as a one-shot scrape for cron jobs
- First-run ZIP/postal-code setup with cable, satellite, and over-the-air lineup selection
- On-demand alias and category discovery across every provider and Gracenote device variant in the configured ZIP, plus resumable ranked-market coverage
- Lineuparr JSON builder for the active provider, with attributable aliases, category review, per-channel inclusion, and optional duplicate-SD cleanup
- Optional Dispatcharr M3U matching with explicit confirm/deny review and reversible alias cleanup
- No-key OpenStreetMap/Nominatim service-address search for official provider sources that cannot localize from ZIP alone
- Guide data cached on disk — fast restarts without re-scraping, with stale-guide fallback during background refresh
- Automatic XMLTV file rotation with 7-day retention
- Optional Jellyfin Live TV integration with in-browser streaming
- Optional channel filter to limit guide output to Jellyfin-available channels
- Bonus: built-in retro TV guide web UI ("The Grid")

## Jellyfin / Plex Setup

1. Run the scraper in server mode (see below).
2. Open `http://<your-host>:8080/setup` and choose your provider lineup.
3. When the first guide finishes building, click the XMLTV guide URL on the setup page to copy it, or add this equivalent URL to your DVR software:
   ```
   http://<your-host>:8080/xmlguide.xmltv
   ```
4. Guide data refreshes automatically every 24 hours.

Alternatively, use `--guide-only` mode with a cron job and point your DVR software at the local `xmlguide.xmltv` file.

## Quick Start (Docker Compose)

1. Clone the repo:
   ```sh
   git clone --branch dev-test https://github.com/matrix2669/GraceNoteScraper.git
   cd GraceNoteScraper
   ```

2. Copy the environment file for optional integrations:
   ```sh
   cp .env.example .env
   # Optionally add a TMDB token or Jellyfin settings
   ```

3. Start the container:
   ```sh
   docker compose up -d
   ```

4. Open `http://<your-host>:8080/setup`, enter your ZIP or postal code, and choose a provider lineup.

5. After the first guide finishes building, click the XMLTV guide URL shown on the setup page to copy it into your DVR software.

6. Open `http://<your-host>:8080/lineuparr` to review and export a Lineuparr-compatible JSON file for the same active lineup.

Setup, guide data, caches, and images are persisted in a Docker volume. The container restarts automatically and refreshes guide data every 24 hours. A restart loads a source-matching cached guide immediately. If it is at least 24 hours old, the scraper keeps serving it while refreshing in the background; changing the configured lineup still invalidates the old lineup's guide.

To view logs:
```sh
docker compose logs -f
```

Routine scrape and enrichment progress is written to stdout. Warnings and failures are written to stderr so container log viewers can distinguish severity correctly.

To pull and run the newest image for the selected channel:
```sh
docker compose pull
docker compose up -d
```

## Requirements

- Docker and Docker Compose, **or** Go 1.25+ for building from source
- (Optional) A [TMDB API read access token](https://www.themoviedb.org/settings/api) for poster images and metadata
- Internet access to the public Nominatim search service, or an optional hosted/self-managed Nominatim endpoint, for provider address lookup

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
| `GRACENOTESCRAPER_IMAGE_TAG` | GHCR image channel used by Docker Compose; keep `dev-test` until promotion | `dev-test` |
| `CONFIG_PATH` | Saved non-secret setup configuration | `config.json` |
| `MARKET_INDEX_PATH` | Saved station/alias observations from on-demand market scans | `market_index.json` |
| `MARKET_ZIPS_PATH` | Optional replacement for the embedded representative-market catalog | embedded catalog |
| `LINEUP_SNAPSHOT_DIR` | Runtime-generated identity/category JSON for every successfully scanned lineup; blank stores it beside `MARKET_INDEX_PATH` | `lineup_snapshots` beside market index |
| `LINEUPARR_STATE_PATH` | Saved channel, alias, category, and Dispatcharr match-review choices for the current lineup | `lineuparr_state.json` |
| `LINEUPARR_CACHE_DIR` | Cache for public Lineuparr and iptv-org enrichment sources | `lineuparr_source_cache` |
| `LINEUPARR_EXPORT_DIR` | Persistent last-exported JSON snapshots, one per lineup | `lineuparr_exports` beside `LINEUPARR_STATE_PATH` |
| `LINEUPARR_CATALOG_URLS` | Optional comma-separated Lineuparr JSON URLs, or `default` to enable the legacy mapped catalogs | disabled |
| `LINEUPARR_IPTV_ORG_URL` | Optional public channel database URL | disabled |
| `LINEUPARR_REFERENCE_CATALOGS` | Set to `on` to enable bundled provider, PrismCast, and PBS snapshots as supplemental evidence | disabled |
| `DISPATCHARR_CONFIG_PATH` | Separate owner-only Dispatcharr connection file saved from the builder | `dispatcharr_config.json` |
| `NOMINATIM_URL` | OpenStreetMap/Nominatim search service. Set to a hosted/self-managed endpoint, or `off` to disable provider-address search. | `https://nominatim.openstreetmap.org` |
| `GN_HEADEND` | Legacy/bootstrap GraceNote headend ID; use with `GN_LINEUP` and `GN_ZIPCODE` | — |
| `GN_LINEUP` | Legacy/bootstrap full lineup string | — |
| `GN_COUNTRY` | Country code | `USA` |
| `GN_ZIPCODE` | Legacy/bootstrap ZIP or postal code | — |
| `GN_LANGUAGE` | Language code | `en-us` |
| `GN_DEVICE` | Device identifier | `-` |
| `TMDB_TOKEN` | Optional TMDB read-access token; adds programme posters, star ratings, release years, original languages, TMDB IDs, and missing-description fallback | — |
| `TMDB_WORKERS` | Concurrent TMDB title workers; all requests retain the shared rate limiter (1-16) | `4` |
| `BASE_URL` | Server base URL — rewrites XMLTV image URLs to use the built-in proxy cache (e.g. `http://192.168.1.50:8080`) | — |
| `PORT` | HTTP server port | `8080` |
| `JELLYFIN_URL` | Jellyfin server URL (optional — enables live TV integration) | — |
| `JELLYFIN_API_KEY` | Jellyfin API key | — |
| `JELLYFIN_CHANNEL_FILTER` | Set to any non-empty value to filter guide to only Jellyfin-available channels. Requires `JELLYFIN_URL` and `JELLYFIN_API_KEY`. | — |

A saved `CONFIG_PATH` selection takes precedence over legacy `GN_*` settings. Delete or move that file if you intentionally want to bootstrap from environment settings again.

## Station Alias Discovery

The Alias discovery section on `/lineuparr` builds a local station-name index only when you ask it to. It does not run at startup or on the guide-refresh schedule.

- **Scan providers in this ZIP** discovers every unique Gracenote lineup returned for the active setup ZIP and joins supported live official provider sources to their own provider grids. Exact station IDs are reused directly. Different station IDs become alias/category bridges only after independent pair-level identity evidence and matching weekday schedules satisfy the rules below.
- Every successfully downloaded lineup produces its own schema-versioned JSON file under `LINEUP_SNAPSHOT_DIR`. It contains provider positions, Gracenote station IDs, identity aliases, normalized category evidence, source URLs, match methods, and fuzzy confidence, but never programme events, credentials, service addresses, or stream URLs.
- Runtime adapters now cover Verizon FiOS, Optimum, DIRECTV, DISH, AFN, Glorystar, AT&T U-verse, Xfinity, Spectrum, and BroadStar. Every adapter parses the provider's public source at scan time; reviewed compatibility snapshots remain disabled unless `LINEUPARR_REFERENCE_CATALOGS=on` is explicitly set.
- The embedded catalog contains one representative central-city ZIP for each of the top 100 publicly reported 2025-26 US television markets. These ZIPs are discovery seeds, not official market boundaries or ZIP-to-DMA assignments.
- **Scan first/next 25 markets** creates a checkpoint and then pauses so marginal yield can be reviewed before continuing.
- Provider results are deduplicated by lineup ID before grid retrieval. Gracenote's postal-specific OTA placeholder is keyed by ZIP so different local broadcast lineups remain distinct.
- Every lineup first uses a Tuesday prime-time grid in its local Gracenote timezone. Only plausible cross-ID pairs fetch a Wednesday afternoon block. A Thursday prime-time or Friday afternoon fallback is fetched only when the corresponding primary block has less than 80 percent meaningful coverage. Grid starts remain spaced by five seconds.
- A cross-ID pair requires a shared normalized callsign, non-generic affiliate/network, exact position within the same provider family, or attributable official provider name. Schedule-only collisions never create aliases. Both selected six-hour blocks must have at least 80 percent meaningful coverage and at least 80 percent matching coverage. Confirmation requires six matched programme occurrences across the blocks, or the long-form exception of two titles and ten matched hours.
- Paid programming, sign-off, local origination, public/educational/government access, Optimum event placeholders, and equivalent filler do not count as evidence. Exact programme IDs match, as do equal normalized titles with identical start and end times. Plain `HD`, `SD`, and `DT` suffixes may be normalized for identity; numbered digital subchannels such as `DT2` and `DT3` remain distinct.
- Only provider, lineup, station ID, channel number, observed-name provenance, attributable category evidence, and the resulting alias/category facts are retained; programme events and titles are discarded after comparison.
- Failed or stopped batches resume from incomplete lineups. **Refresh** deliberately rescans one market. **Rebuild index** first preserves the prior file at `<MARKET_INDEX_PATH>.bak`.
- Meaningful aliases include punctuation/case-normalized callsigns observed on the same Gracenote station ID and names from separately identified station IDs that pass the pair-level weekday EPG confirmation. Affiliate/network evidence alone is never exported as an alias unless the schedule confirmation succeeds.
- Official provider aliases and categories retain their source URL and exact join method. Conflicting categories and aliases shared by multiple station IDs are not applied automatically.

Provider coverage is intentionally explicit about source limitations:

| Provider | Runtime public source | Current limitation |
| --- | --- | --- |
| Verizon FiOS | Official national PDF | National channels and provider-published channel-range categories; local positions still come from Gracenote |
| Optimum | NY/NJ/CT/PA/selected-NC market PDFs or the public address-qualified Suddenlink/Optimum services | Eastern PDFs contribute their explicit section categories by exact channel number, including column continuations and compact ranges; western service areas require a user-selected address for an exact local lineup |
| DIRECTV | Channel data embedded in the official lineup page | National names/categories are available without login; local/RSN selection remains Gracenote-owned |
| DISH | Public channel-lineup JSON service | Provider category labels are normalized conservatively |
| AFN | Official guide PDF format | AFN's CDN may reject automated downloads; that source reports an isolated error when unavailable |
| Glorystar | Public channel table | The provider is faith-focused, so its published channel rows map to `Faith` |
| AT&T U-verse | Official public PDF | AT&T's current download URL serves a document marked effective February 2023 and is reported as limited |
| Xfinity | Public address-qualified channel API | Requires a user-selected address for the active Xfinity lineup |
| Spectrum | Public lineup page | No stable no-login residential payload is currently exposed; account/login automation is intentionally disabled |
| BroadStar | Official public PDF | Categories are used only where the provider document has explicit Sports, Premium, Music, or service sections |

The default catalog and its provenance are in `marketindex/market_zips.json`. Set `MARKET_ZIPS_PATH` to a compatible catalog if you want to maintain a different list without rebuilding the binary.

## Lineuparr JSON Builder

Click **Export JSON** and choose **Download JSON** or **Copy URL**. Either option publishes a snapshot of your current included channels, categories, and aliases. Cancelling the dialog does not publish anything. The download and URL serve the same saved JSON. Copy URL leaves you on the builder page and shows a selectable URL if automatic clipboard access is unavailable.

The URL serves the **last explicitly exported version**. Editing channels, refreshing enrichment, or fetching the URL does not change it; reopen Export and choose either option to publish an updated version at the same URL. Each configured lineup has its own URL, and previously exported lineups remain available after switching providers. Snapshots survive container restarts and remain readable while a guide rebuild is in progress. Keep `LINEUPARR_EXPORT_DIR` on persistent storage; old lineup snapshots are retained until removed from that directory.

Use a scraper hostname and port that the Lineuparr host can reach. A compatible Lineuparr URL-import action can fetch the JSON using its normal lineup filename from the `Content-Disposition` header. The URL grants read access to the exported lineup to anyone who can reach the scraper. It contains no provider credentials or stream URLs. The existing `/api/lineuparr/export` endpoint remains a direct download of the live draft for older clients; it does not update the published snapshot.

The builder at `/lineuparr` is an extension of the active scraper lineup rather than a second provider configuration. Gracenote remains authoritative for provider membership and channel numbers. Every raw provider position starts included, even when two positions point to the same station, so SD removal is an explicit and reversible choice.

Generated files are designed for Lineuparr's **Exact** match sensitivity (95%). Set the Lineuparr plugin to **Exact** before previewing or applying stream matches. The generated file intentionally relies on that threshold to keep reviewed M3U evidence compact.

Aliases derived directly from Gracenote include callsigns, station IDs, lineup-position IDs, number-plus-callsign names, safe affiliate names, and event callsigns. The corresponding Gracenote station ID is exported as `epg_ids`. Runtime evidence is primary; configured optional sources may add attributable aliases and EPG identifiers. The builder applies only unique identity matches from:

Both detailed and compact channel rows lead with the callsign and append the best distinct name from attributable affiliate, official-provider, or catalog evidence; punctuation-only, identifier, and channel-number duplicates are suppressed. The channel list opens with only included positions visible and can be sorted by channel number or name; excluded positions remain available through the visibility filter. Clicking the name opens the next 24 current or upcoming programmes from the selected guide to help identify an unfamiliar channel. **Batch categorize** can select every currently visible filtered row and apply one category atomically.

The same GN brand mark shown in the page header is served as the SVG favicon for the guide, setup, and Lineuparr pages.

- Supported public official sources for provider lineups returned for the configured ZIP. Device variants that share one Gracenote lineup ID remain distinct because their channel membership and station IDs can differ. Each source is first joined to its own Gracenote grid by exact provider channel number or unique exact identity. Aliases and categories cross into the selected lineup through an identical Gracenote station ID or a separately confirmed pair-level weekday EPG match. Providers without a runtime adapter still receive a Gracenote identity snapshot and remain visibly unresolved rather than borrowing another provider's channel numbers.
- Optional matching provider/country catalogs from [Dispatcharr Lineuparr Plugin](https://github.com/matrix2669/Dispatcharr-Lineuparr-Plugin), enabled with `LINEUPARR_CATALOG_URLS`.
- The optional public-domain [iptv-org channel database](https://github.com/iptv-org/database), restricted to the active lineup country and active channel records, enabled with `LINEUPARR_IPTV_ORG_URL`.
- Optional reviewed exact-ID network catalogs generated from [PrismCast](https://github.com/hjdhjd/prismcast) and [Stream Link Manager for Channels](https://github.com/babsonnexus/stream-link-manager-for-channels), enabled with `LINEUPARR_REFERENCE_CATALOGS=on`.

The master taxonomy is `Local & Public`, `News & Weather`, `Sports`, `Movies`, `Entertainment`, `Kids & Family`, `Music`, `Faith`, `International`, `PPV & Events`, and `Other`. Adult channels map to `Other`; explicit pay-per-view and event feeds map to `PPV & Events`. Provider labels are resolved by canonical name, maintained aliases, and then conservative fuzzy alias matching. Fuzzy matches must clear both a confidence threshold and a winning margin, retain the original provider label and score, and are not applied when ambiguous. Broad provider group headings such as Optimum's `Networks` are not category evidence by themselves; explicit PEG/public-access identities and broadcast callsigns with affiliate evidence resolve to `Local & Public`, while ordinary network rows wait for a more specific source. One unambiguous category from the selected provider's exact official source takes precedence over broader classifications copied from competing lineups; if the selected source has no category, competing official sources must agree. Conflicts within the selected source or with an enabled exact-ID network catalog remain `Uncategorized` rather than being forced into `Other`.

User categories take precedence. For channels that remain unresolved, a conservative Gracenote schedule profile may assign a master category when one useful program filter covers at least 70% of scheduled minutes, at least eight programs and six guide-hours are present, and family programming belongs to a clearly child-oriented network.

The optional SD-duplicate action is conservative: it appears when two provider positions map to the same exact sourced identity and one has a stronger HD, UHD, 4K, or digital marker, or when normalized callsigns differ only by a terminal `HD`, `SD`, or unnumbered broadcast `DT` suffix and have one unique strongest variant. It can also bridge different callsign spellings when exactly two positions share the same normalized, nonnumeric alias and both positions have attributable evidence. An explicitly marked SD position still requires the other position to share that alias through the same source. An unmarked lower-quality position may instead pair with an explicitly marked HD/digital position only when both share a non-schedule source or confirmed schedule evidence on one position references the opposite position's actual channel number. A pair such as `WCBS`/`WCBSDT` keeps the `DT` digital/HD position and proposes the otherwise identical base position for removal; `NWSNTSD`/`NEWSNTN` uses the shared exact `NewsNation` identity to propose the explicitly marked SD position; and `I24NWEN`/`I24NEHD` uses the exact normalized `i24 News` identity plus its cross-position evidence to keep the explicitly marked HD position. Quality-suffix and attributable-alias matching require a base of at least three alphanumeric characters where a suffix is interpreted, never strip numbered digital-subchannel suffixes such as `DT2` or `DT3`, reject schedule-only bridges to unrelated or self-referential positions, and suppress ambiguous groups or competing keep candidates. Clicking the action opens every proposed remove/keep pair for review; all safe proposals start selected, individual pairs can be unchecked, and only the confirmed subset is excluded. The affected channels remain individually reversible, and **Restore all** puts every provider position back into the export. The `Categorized` and `Needs category` header totals count only channels currently included in the draft.

Provider-source failures do not interrupt guide generation, invalidate successfully downloaded Gracenote lineups, or prevent a Gracenote-only export. Optional Lineuparr catalog downloads have their own 24-hour cache; official provider adapters run only during an on-demand ZIP scan. Source URLs are server configuration; credentials and stream URLs are never part of the exported JSON.

The enrichment-source panel consolidates registration, capture, and derived-category reports for the same provider into one row. Direct PDF sources open the captured lineup document; other source names and every available matched count open a searchable evidence view of the exact selected-lineup channels, identities, categories, aliases, IDs, and methods contributed by that source. Alias discovery also shows the local date and time of the last configured-ZIP provider refresh.

### Dispatcharr match review

The optional Dispatcharr panel compares the active lineup with every non-stale stream from active M3U accounts. Choose either a normal Dispatcharr username/password or an API key; only the fields for the selected method are shown and enabled. Password authentication uses Dispatcharr's JWT API and keeps access and refresh tokens in memory only. The saved connection settings live in the separate `DISPATCHARR_CONFIG_PATH` file, created with owner-only (`0600`) permissions on POSIX systems. The default file is excluded from Git and Docker build context. Use HTTPS unless both applications communicate only over a trusted private network.

Matching prioritizes exact EPG IDs, direct channel names, and attributable aliases before offering bounded fuzzy-name candidates. Delimited `US`, `GO`, `Prime`, `Tubi`, and `ROKU` provider prefixes, common HD/UHD markers, punctuation, and spacing are normalized. A leading HDHomeRun-style number is removed only when it exactly equals Dispatcharr's channel-number metadata, so event years and unrelated numeric names remain intact. A score is never accepted automatically:

- **Confirm** adds one representative reviewed stream-name alias only when the independent name score is below 95%. Names at or above 95% are already eligible under Lineuparr's required **Exact** sensitivity and are not duplicated in the JSON. Provider-reported `tvg_id` values remain internal matching evidence and are not added by the browser.
- **Deny** records that stream/channel pairing as rejected. When the independent name score is 95% or higher, one representative name is also exported in that channel's `excluded_aliases` list so a compatible Lineuparr plugin rejects the reviewed false positive before positive alias or fuzzy matching. Lower-scoring denials are not exported because Exact mode would not accept them. When a fuzzy proposal had other qualifying targets, the already-scored alternatives open immediately for separate confirmation or denial.
- **Undo** reverses either decision. Confirmed aliases can also be removed from or restored to the export with the same alias controls used for other sources.

Confirm and deny actions remove their row immediately without locking or re-sorting the remaining review page. The initial page contains 100 groups; **Load more** explicitly requests the next 100. Decisions retain the safe normalized stream identity as well as the active lineup, stream fingerprint, and target channel, so equivalent account variants, authentication changes, and container restarts do not restore reviewed rows. The confirmed counter opens reviewed matches.

The threshold decision uses an independent name score rather than the overall proposal score. An exact provider TVG/EPG ID can make the overall proposal 100% even when the names are unrelated; because Lineuparr does not consume lineup JSON `epg_ids`, that confirmation still exports a name alias when its name score is below 95%. This prevents non-name evidence from being mistaken for a match that Lineuparr can reproduce.

Only the metadata needed for review—stream ID, name, `tvg_id`, M3U account/group IDs, and provider channel number—is retained. Dispatcharr stream URLs, logos, tokens, and statistics are discarded as the API response is decoded and are never returned to the browser, saved in Lineuparr state, or exported. Stream lists are cached in memory for five minutes; if a refresh fails, a visible warning identifies the older list being used.

Official provider sources use the active lineup ZIP and Gracenote location automatically. Optimum lineups in NY, NJ, CT, PA, Hendersonville, NC, and West Jefferson, NC use Optimum's regional market list; its other service areas use the address-qualified public lineup services. When the resolved provider source requires a precise service address, `/lineuparr` offers an explicit OpenStreetMap/Nominatim search restricted to the active lineup ZIP. The shared public service is limited to one request per second and is not used for live autocomplete; repeated searches are cached in browser memory. The selected structured address is passed in memory only to the active provider's adapter and is not persisted in scraper configuration, Lineuparr state, source caches, logs, snapshots, or exports. Public-service searches may be logged, so use a hosted/self-managed `NOMINATIM_URL` for private addresses. GraceNoteScraper does not invent a generic address, collect provider-account logins, or use Dispatcharr group names as category evidence. See `THIRD_PARTY_NOTICES.md` for optional embedded catalog attribution and licenses.

## HTTP Endpoints

| Endpoint | Description |
|---|---|
| `GET /setup` | Choose or change the active provider lineup |
| `GET /api/setup/config` | Read the current non-secret lineup selection |
| `GET /api/setup/providers?postalCode=...` | Find Gracenote lineups for an area |
| `POST /api/setup/provider` | Save the selected provider and queue a fresh guide |
| `GET /lineuparr` | Review the current lineup and export Lineuparr JSON |
| `GET /api/lineuparr/provider-address/config` | Read Nominatim availability and active-lineup constraints for an address-gated provider source |
| `POST /api/lineuparr/provider-address/search` | Search for complete provider addresses in the active lineup postal code |
| `GET /api/lineuparr/draft` | Current builder draft with aliases, provenance, and duplicate suggestions |
| `GET /api/lineuparr/alias-index` | Read market-scan progress and marginal alias yield |
| `POST /api/lineuparr/alias-index/run` | Scan all providers in the configured ZIP, continue ranked markets, selectively refresh, or rebuild the on-demand index |
| `POST /api/lineuparr/alias-index/stop` | Stop or cancel a running alias-index batch safely |
| `POST /api/lineuparr/channel` | Include/exclude one channel or update its category |
| `POST /api/lineuparr/alias` | Remove or restore one attributable alias for the active lineup |
| `POST /api/lineuparr/categories` | Atomically apply one category to a validated channel selection |
| `GET /api/lineuparr/channel-programs?channelId=<id>` | Return up to 24 current/upcoming programmes from the selected guide |
| `POST /api/lineuparr/remove-duplicates` | Exclude the reviewed `channelIds` subset, or all current suggestions for backward-compatible requests without that field; counts include only suggestions that remain included |
| `POST /api/lineuparr/restore-all` | Restore every provider channel to the export |
| `GET /api/lineuparr/export` | Download the current Lineuparr-compatible JSON file |
| `GET, POST, DELETE /api/lineuparr/dispatcharr/config` | Read, test/save, or remove the Dispatcharr connection; saved credentials are never returned |
| `GET /api/lineuparr/dispatcharr/review` | Fetch the current safe M3U match-review queue; `limit` controls the visible page and `refresh=true` refreshes streams |
| `POST, DELETE /api/lineuparr/dispatcharr/decision` | Confirm/deny a current candidate, or undo one reviewed decision |
| `POST /api/lineuparr/publish` | Save the current draft as the published snapshot; requires the draft's `sourceFingerprint`; returns its relative URL and filename |
| `GET, HEAD /lineuparr/exports/{source}/lineup.json` | Read the last explicitly exported JSON, without rebuilding; `?download=1` requests an attachment |
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
providersource/  Focused official provider evidence adapters and generated snapshots
tools/           Deterministic source-catalog maintenance generators
lineuparr.html   Lineuparr review/export UI (embedded at build time)
marketindex/     Ranked ZIP catalog, resumable scanner, alias index, and yield reporting
guide.tmpl       XMLTV output template (embedded at build time)
```

---

<sub>Portions of this project were developed with the assistance of generative AI ([Claude](https://claude.ai)).</sub>
