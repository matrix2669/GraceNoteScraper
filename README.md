# GraceNote XMLTV Scraper

### Experimental category evidence and review

Category evidence uses ordinal priorities: manual choices (1), clear official
evidence (2), supported weekday schedule inference (3), and weak provider or
optional TMDB genre inference (4). Priority is not an accuracy percentage.
Provisional categories are marked for review and may be confirmed or changed;
the original proposal is retained separately from the manual choice.

The schedule experiment uses fourteen calendar days in the selected lineup's
known timezone, Monday-Friday only. It requires at least eight usable weekdays,
80% usable weekday coverage and twenty programmes. Movies require more than
50% movie airtime and mean programme length greater than sixty minutes. News,
Sports and Family use conservative provisional 80% content shares; news and
sports require a 15-point separation. Missing tags do not imply Entertainment,
Faith or Music. These provisional thresholds need continued reviewed-sample
validation, not claims of calibrated accuracy.

TMDB remains optional. Add `TMDB_TOKEN` to the container environment and restart
to enable enrichment. New lookups retain genre IDs separately from Gracenote
categories. Existing cache entries without genres remain valid metadata but
cannot supply genre evidence until refreshed. The Lineuparr TMDB panel can scan
the retained programme data without additional TMDB requests. Such results are
priority 4 and need review; title-search matches are not exact station evidence.

Generate XMLTV guide data from GraceNote/TMS listings for use with Jellyfin, Plex, Emby, TVHeadend, and other DVR/IPTV software.

## Features

Once an address is saved, its section shows only the provider, saved address and Forget address button. Forget reopens the full form only after successful removal; a failed removal preserves the saved address. Enrichment-source informational popups close when clicking outside their box.

After Confirm or Deny, a review group with a minimum displayed match score below 95% opens other qualifying targets, when available. This applies to similar as well as fuzzy matches. Confirming an alternative keeps the popup open for additional SD/HD or duplicate lineup targets; it never implicitly denies other targets. Close explicitly when finished; outside clicks do not dismiss this action popup.

Duplicate review presents whole connected groups of verified SD/HD pairs and exact repeated positions. Checked means Keep. Lower-quality removals are suggested, but all equivalent HD or exact duplicate positions start checked for a manual choice; a lower channel number does not prove better quality. At least one included position per group must remain. Exact repeats require the same nonempty Gracenote station ID and callsign; names alone never create these groups.

Channel programme popups close when clicking outside their box. Popups requiring choices, such as export and duplicate review, retain explicit controls. SD suggestions accept multiple HD positions only when those positions share one nonempty Gracenote station ID; the review link prefers an included position, then the lowest channel number. HD positions themselves are not removed by this rule.

- Scrapes 14 days of GraceNote/TMS program listings and outputs standard XMLTV format
- Enriches programs with TMDB poster images, ratings, descriptions, and release dates
- Enriches channel icons via the [tv-logo/tv-logos](https://github.com/tv-logo/tv-logos) project
- Runs as a long-lived server with automatic 24-hour refresh, or as a one-shot scrape for cron jobs
- First-run ZIP/postal-code setup with cable, satellite, and over-the-air lineup selection
- On-demand alias and category discovery across every provider and Gracenote device variant in the configured ZIP
- Lineuparr JSON builder for the active provider, with attributable aliases, category review, per-channel inclusion, and optional duplicate-SD cleanup
- No-key Google Maps lookup link, illustrated copy/paste instructions, and provider address testing for sources that cannot localize from ZIP alone
- Optional Dispatcharr M3U matching with explicit confirm/deny review and reversible alias cleanup
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

### Faster first guide with optional TMDB enrichment

On initial setup, or when no usable guide exists, server mode publishes the Gracenote guide after downloading its listings and resolving channel logos. You can use the guide and, when installed, the Lineuparr builder immediately. Optional TMDB artwork and programme metadata are added in the background; setup displays **Guide available now** alongside enrichment progress. The completed enriched guide replaces the base guide without changing channel membership or reviewed Lineuparr exports.

The base guide stays usable if enrichment is interrupted. A source-matching pending guide less than 24 hours old resumes enrichment after restart using its saved programmes and TMDB cache, without fetching the grids again. Keep the data directory persistent. Failures retain the base guide and use the normal 15-minute retry. A changed lineup cannot receive results from the old lineup.

This publish-first behavior applies only to the initial/missing guide. Regular scheduled refreshes with a usable guide and `--guide-only` still finish enrichment before publishing. Without `TMDB_TOKEN`, there is no TMDB background work. The configured worker limit and shared request rate are unchanged. With the background-aware provider-scan coordinator, local scans and enabled enrichment-source downloads can run during background TMDB after the base guide is published. Ordinary guide download/enrichment and final saving remain gated; the separate coordinator retains one scan at a time and existing source rate limits.

## Quick Start (Docker Compose)

On the Lineuparr page, Enrichment sources starts collapsed on every visit. Expand it to review source evidence. Required provider addresses are entered in Alias discovery above the scan controls. Other sections retain their saved expansion preferences.

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
- Browser access to Google Maps for manual address lookup; no Google API key or billing account is required

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
| `MARKET_INDEX_PATH` | Persistent lineup evidence (legacy setting name retained) | `market_index.json` |
| `LINEUP_SNAPSHOT_DIR` | Runtime-generated identity/category JSON for every successfully scanned lineup; blank stores it beside `MARKET_INDEX_PATH` | `lineup_snapshots` beside market index |
| `LINEUPARR_STATE_PATH` | Saved channel inclusion and category choices for the current lineup | `lineuparr_state.json` |
| `LINEUPARR_CACHE_DIR` | Cache for public Lineuparr and iptv-org enrichment sources | `lineuparr_source_cache` |
| `LINEUPARR_EXPORT_DIR` | Persistent last-exported JSON snapshots, keyed by export filename | `lineuparr_exports` beside `LINEUPARR_STATE_PATH` |
| `LINEUPARR_CATALOG_URLS` | Optional comma-separated Lineuparr JSON URLs, or `default` to enable the legacy mapped catalogs | disabled |
| `LINEUPARR_IPTV_ORG_URL` | Optional public channel database URL | disabled |
| `LINEUPARR_REFERENCE_CATALOGS` | Set to `on` to enable bundled provider, PrismCast, and PBS snapshots as supplemental evidence | disabled |
| `NOMINATIM_URL` | Legacy API-only Nominatim search endpoint. Not used by the Google Maps copy/paste UI. Set to `off` to disable the legacy endpoint. | `https://nominatim.openstreetmap.org` |
| `DISPATCHARR_CONFIG_PATH` | Separate owner-only Dispatcharr connection file saved from the builder | `dispatcharr_config.json` |
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

The experimental **Major-market enrichment** section scans exactly one market per click, starting at the first unfinished market in the retained 2025-26 ranked ZIP list. No automatic or 25-market runs occur. Each market uses the local provider-source/weekday EPG pipeline and a transient grid of the selected lineup for comparison; it never changes the selected provider or publishes another full guide. Address-required sources are skipped, never given the subscriber's local address. Reference addresses are not bundled in this initial version; the skip report will guide any later curated additions. Existing AFN/Glorystar exclusions remain.

Per-lineup/device reports distinguish successful enrichment, no matches, unsupported sources, address requirements, and source/grid failures. All-provider and new-family-only candidate yields are compared using the same downloaded data, with the selected-lineup comparison reference retained in both. These count new identity/category facts and current station IDs reached, not guaranteed category changes after conflict resolution. Repeat-provider skipping remains disabled. Market-specific lineup keys prevent repeated national lineup IDs from overwriting another market's records. Stop uses the shared scan stop control; interrupted markets are retried on the next click. Keep `MARKET_INDEX_PATH` on persistent storage. Legacy ranked actions remain rejected; the new endpoint runs one curated market only.

Channel-number evidence is permitted only for the selected local provider's own official lookup, with identity corroboration. Other local providers and all market providers require unique channel identities. EPG comparisons never use channel numbers, including within the same provider family. Older number-assisted facts without the new local-scope provenance are retained on disk but withheld until refreshed.

The Alias discovery section on `/lineuparr` builds a local station-name index only when you ask it to. It does not run at startup or on the guide-refresh schedule.

- **Scan providers in this ZIP** discovers every unique Gracenote lineup returned for the active setup ZIP and joins supported live official provider sources to their own provider grids. Exact station IDs are reused directly. Different station IDs become alias/category bridges only after independent pair-level identity evidence and matching weekday schedules satisfy the rules below.
- Every successfully downloaded lineup produces its own schema-versioned JSON file under `LINEUP_SNAPSHOT_DIR`. It contains provider positions, Gracenote station IDs, identity aliases, normalized category evidence, source URLs, match methods, and fuzzy confidence, but never programme events, credentials, service addresses, or stream URLs.
- Runtime adapters cover Verizon FiOS, Optimum, DIRECTV, DISH, AT&T U-verse, Xfinity, Spectrum, and BroadStar. Every adapter parses the provider's public source at scan time; reviewed compatibility snapshots remain disabled unless `LINEUPARR_REFERENCE_CATALOGS=on` is explicitly set. AFN and Glorystar are excluded from enrichment scans and source listings, but remain selectable as the main guide provider. Existing index files are not deleted; their direct provider facts and provider-only names are ignored.
When initial-guide background TMDB enrichment is enabled, local lineup scans may start as soon as the usable base guide reaches the background-enrichment stage. This includes enabled provider/source downloads and alias/category processing; they do not wait for TMDB. Already queued scans start automatically on the next queue check (within approximately one second). Guide downloading, logo processing, ordinary scheduled enrichment and final guide saving remain gated. Only one local scan runs at a time, and source rate limits and cancellation are unchanged.
- Provider results are deduplicated by lineup ID before grid retrieval. Gracenote's postal-specific OTA placeholder is keyed by ZIP so different local broadcast lineups remain distinct.
- Every lineup first uses a Tuesday prime-time grid in its local Gracenote timezone. Only plausible cross-ID pairs fetch a Wednesday afternoon block. A Thursday prime-time or Friday afternoon fallback is fetched only when the corresponding primary block has less than 80 percent meaningful coverage. Grid starts remain spaced by five seconds.
- A cross-ID pair requires a shared normalized callsign, non-generic affiliate/network, or attributable official provider name. Schedule-only collisions never create aliases. Both selected six-hour blocks must have at least 80 percent meaningful coverage and at least 80 percent matching coverage. Confirmation requires six matched programme occurrences across the blocks, or the long-form exception of two titles and ten matched hours.
- Paid programming, sign-off, local origination, public/educational/government access, Optimum event placeholders, and equivalent filler do not count as evidence. Exact programme IDs match, as do equal normalized titles with identical start and end times. Plain `HD`, `SD`, and `DT` suffixes may be normalized for identity; numbered digital subchannels such as `DT2` and `DT3` remain distinct.
- Only provider, lineup, station ID, channel number, observed-name provenance, attributable category evidence, and the resulting alias/category facts are retained; programme events and titles are discarded after comparison.
- Meaningful aliases include punctuation/case-normalized callsigns observed on the same Gracenote station ID and names from separately identified station IDs that pass the pair-level weekday EPG confirmation. Affiliate/network evidence alone is never exported as an alias unless the schedule confirmation succeeds.
- Official provider aliases and categories retain their source URL and exact join method. Conflicting categories and aliases shared by multiple station IDs are not applied automatically.

Provider coverage is intentionally explicit about source limitations:

| Provider | Runtime public source | Current limitation |
| --- | --- | --- |
| Verizon FiOS | Official national PDF | National channels and provider-published channel-range categories; local positions still come from Gracenote |
| Optimum | NY/NJ/CT/PA/selected-NC market PDFs or the public address-qualified Suddenlink/Optimum services | Eastern PDFs contribute explicit section categories via verified identity; the selected local provider may also use corroborated channel numbers; western service areas require a user-selected address for an exact local lineup |
| DIRECTV | Channel data embedded in the official lineup page | National names/categories are available without login; local/RSN selection remains Gracenote-owned |
| DISH | Public channel-lineup JSON service | Provider category labels are normalized conservatively |
| AT&T U-verse | Official public PDF | AT&T's current download URL serves a document marked effective February 2023 and is reported as limited |
| Xfinity | Public address-qualified channel API | Uses `genreName`, feed-local names/callsign and explicit PPV flags. Unknown genres stay unresolved; linked SD/HD names are not mixed. Xfinity station IDs are not Gracenote IDs. Requires a saved service address. |

Source matched counts represent selected-lineup channels with attributable alias/category evidence, including corroboration of an already-known alias. They are not counts of newly added alias strings; ZIP-wide joins remain separately described. After upgrading the parser, run the ZIP scan again to obtain new categories. Xfinity itself labels many records `Unknown`, so this source cannot categorize every channel.
| Spectrum | Public lineup page | No stable no-login residential payload is currently exposed; account/login automation is intentionally disabled |
| BroadStar | Official public PDF | Categories are used only where the provider document has explicit Sports, Premium, Music, or service sections |


## Lineuparr JSON Builder

Category review also supports row checkboxes, **Select all pending**, and **Approve selected**. A batch accepts each selected channel's own proposed category in one save; it does not assign a shared category. Save individual corrections first. Excluded or already-reviewed channels are not eligible, and stale selections are rejected without partially approving the batch.

Use the separate collapsible **Category review** section for included channels with provisional assignments. Each row displays its proposed category and expandable source evidence. Choose **Confirm** to accept it, or select another category and choose **Save correction**. Successful saves remove the row and reduce the remaining count; manual choices and review history persist. The Channels section retains ordinary channel editing without inline confirmation buttons.

TMDB category evidence is a separate collapsible section before **Category review**. **Scan TMDB enrichment data** reuses retained programme genre IDs or older cached genre names when the programme already has the same TMDB movie/series identity. This is a local metadata scan, not a guide rebuild or a TMDB download. You do not need to clear the cache or reset your lineup; manual category choices remain unchanged. Programme matches without usable genre evidence await normal enrichment. Proposals remain priority 4 and require review.

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

Generated files are designed for Lineuparr's **Exact** match sensitivity (95%). Set the Lineuparr plugin to **Exact** before previewing or applying stream matches. The generated file intentionally relies on that threshold to keep reviewed M3U evidence compact.

Aliases derived directly from Gracenote include callsigns, station IDs, lineup-position IDs, number-plus-callsign names, safe affiliate names, and event callsigns. The corresponding Gracenote station ID is exported as `epg_ids`. Runtime evidence is primary; configured optional sources may add attributable aliases and EPG identifiers. The builder applies only unique identity matches from:

Both detailed and compact channel rows lead with the callsign and append the best distinct name from attributable affiliate, official-provider, or catalog evidence; punctuation-only, identifier, and channel-number duplicates are suppressed. The channel list opens with only included positions visible and can be sorted by channel number or name; excluded positions remain available through the visibility filter. Clicking the name opens the next 24 current or upcoming programmes from the selected guide to help identify an unfamiliar channel. **Batch categorize** can select every currently visible filtered row and apply one category atomically.

The same GN brand mark shown in the page header is served as the SVG favicon for the guide, setup, and Lineuparr pages.

- Supported public official sources for provider lineups returned for the configured ZIP. Device variants that share one Gracenote lineup ID remain distinct because their channel membership and station IDs can differ. Each source is first joined to its own Gracenote grid by unique exact identity; only the selected local provider may additionally use corroborated channel numbers; aliases and categories cross into the selected lineup through an identical Gracenote station ID or the separately confirmed weekday-EPG bridge. Providers without a runtime adapter still receive a Gracenote identity snapshot and remain visibly unresolved rather than borrowing another provider's channel numbers.
- Optional matching provider/country catalogs from [Dispatcharr Lineuparr Plugin](https://github.com/matrix2669/Dispatcharr-Lineuparr-Plugin), enabled with `LINEUPARR_CATALOG_URLS`.
- The optional public-domain [iptv-org channel database](https://github.com/iptv-org/database), restricted to the active lineup country and active channel records, enabled with `LINEUPARR_IPTV_ORG_URL`.
- Optional reviewed exact-ID network catalogs generated from [PrismCast](https://github.com/hjdhjd/prismcast) and [Stream Link Manager for Channels](https://github.com/babsonnexus/stream-link-manager-for-channels), enabled with `LINEUPARR_REFERENCE_CATALOGS=on`.

The master taxonomy is `Local & Public`, `News & Weather`, `Sports`, `Movies`, `Entertainment`, `Kids & Family`, `Music`, `Faith`, `International`, `PPV & Events`, and `Other`. Adult channels map to `Other`; explicit pay-per-view and event feeds map to `PPV & Events`. Provider labels are resolved by canonical name, maintained aliases, and then conservative fuzzy alias matching. Fuzzy matches must clear both a confidence threshold and a winning margin, retain the original provider label and score, and are not applied when ambiguous. Broad provider group headings such as Optimum's `Networks` are not category evidence by themselves; explicit PEG/public-access identities and broadcast callsigns with affiliate evidence resolve to `Local & Public`, while ordinary network rows wait for a more specific source. One unambiguous category from the selected provider's exact official source takes precedence over broader classifications copied from competing lineups; if the selected source has no category, competing official sources must agree. Conflicts within the selected source or with an enabled exact-ID network catalog remain `Uncategorized` rather than being forced into `Other`.

User categories take precedence. For channels that remain unresolved, a conservative Gracenote schedule profile may assign a master category when one useful program filter covers at least 70% of scheduled minutes, at least eight programs and six guide-hours are present, and family programming belongs to a clearly child-oriented network.

The optional SD-duplicate action is conservative: it appears when two provider positions map to the same exact sourced identity and one has a stronger HD, UHD, 4K, or digital marker, or when normalized callsigns differ only by a terminal `HD`, `SD`, or unnumbered broadcast `DT` suffix and have one unique strongest variant. It can also bridge different callsign spellings when exactly two positions share the same normalized, nonnumeric alias and both positions have attributable evidence. An explicitly marked SD position still requires the other position to share that alias through the same source. An unmarked lower-quality position may instead pair with an explicitly marked HD/digital position only when both share a non-schedule source or confirmed schedule evidence on one position references the opposite position's actual channel number. A pair such as `WCBS`/`WCBSDT` keeps the `DT` digital/HD position and proposes the otherwise identical base position for removal; `NWSNTSD`/`NEWSNTN` uses the shared exact `NewsNation` identity to propose the explicitly marked SD position; and `I24NWEN`/`I24NEHD` uses the exact normalized `i24 News` identity plus its cross-position evidence to keep the explicitly marked HD position. Quality-suffix and attributable-alias matching require a base of at least three alphanumeric characters where a suffix is interpreted, never strip numbered digital-subchannel suffixes such as `DT2` or `DT3`, reject schedule-only bridges to unrelated or self-referential positions, and suppress ambiguous groups or competing keep candidates. Clicking the action opens every proposed remove/keep pair for review; all safe proposals start selected, individual pairs can be unchecked, and only the confirmed subset is excluded. The affected channels remain individually reversible, and **Restore all** puts every provider position back into the export. The `Categorized` and `Needs category` header totals count only channels currently included in the draft.

Provider-source failures do not interrupt guide generation, invalidate successfully downloaded Gracenote lineups, or prevent a Gracenote-only export. Optional Lineuparr catalog downloads have their own 24-hour cache; official provider adapters run during an on-demand ZIP scan or an explicit Save & test address lookup. Source URLs are server configuration; credentials and stream URLs are never part of the exported JSON.

The enrichment-source panel consolidates registration, capture, and derived-category reports for the same provider into one row. Direct PDF sources open the captured lineup document; other source names and every available matched count open a searchable evidence view of the exact selected-lineup channels, identities, categories, aliases, IDs, and methods contributed by that source. Alias discovery also shows the local date and time of the last configured-ZIP provider refresh.

Official provider sources use the active lineup ZIP and Gracenote location automatically. Optimum lineups in NY, NJ, CT, PA, Hendersonville, NC, and West Jefferson, NC use Optimum's regional market list; its other service areas use the address-qualified public lineup services. Alias discovery checks every provider in the ZIP. If any source requires an address, use the address section above the scan controls:

1. Enter your street and click **Find in Google Maps**. The lineup ZIP is included in a new browser tab; Google receives this query only when you click.
2. Select the correct result and copy the address beside the map-pin icon in its details—not the search-box text, place name, Plus Code, or Share link. **Show instructions and example** displays an annotated screenshot.
3. Paste the full street, city, state and ZIP, retaining unit details and the displayed street spelling. Do not universally replace directional words such as Northeast with NE. US state names or abbreviations are accepted and state names are converted to their two-letter form. The pasted ZIP must match the lineup (matching ZIP+4 is accepted).
4. Click **Save & test**. Each address-required provider family is tested once, without downloading Gracenote grids or changing guide/draft/export data. Only a response containing usable channel records is marked provider-verified; this is not USPS verification or proof that every returned channel matches your lineup. Tests are limited to one per minute per running scraper. Failures remain visible and the address stays editable; scanning with a saved address is still allowed, although failed sources may contribute nothing.
5. Click **Scan providers in this ZIP** separately to collect enrichment. Saving does not start a scan.

The structured address, last test time and per-provider results are saved in the owner-only `CONFIG_PATH.address.json` file, surviving page refreshes and restarts. Changing the configured provider or choosing **Forget address** removes it. Older saved addresses remain available but show as not tested until Save & test is used. The address is sent only to the listed address-required provider families, including competitors in the same ZIP. It stays out of public setup configuration, Lineuparr state, source caches, logs, snapshots and exports. Treat the scraper UI as private: anyone with UI access can view the saved address or request a lookup. No Google key, hosted intermediary, account login or automatic Google-page scraping is used. The old Nominatim endpoint remains for compatibility but the page no longer calls it. See `THIRD_PARTY_NOTICES.md` for screenshot attribution and optional catalog licenses.

### Dispatcharr match review

The optional Dispatcharr panel compares the active lineup with every non-stale stream from active M3U accounts. Choose either a normal Dispatcharr username/password or an API key; only the fields for the selected method are shown and enabled. Password authentication uses Dispatcharr's JWT API and keeps access and refresh tokens in memory only. The saved connection settings live in the separate `DISPATCHARR_CONFIG_PATH` file, created with owner-only (`0600`) permissions on POSIX systems. The default file is excluded from Git and Docker build context. Use HTTPS unless both applications communicate only over a trusted private network.

Matching prioritizes exact EPG IDs, direct channel names, and attributable aliases before offering bounded fuzzy-name candidates. Delimited `US`, `GO`, `Prime`, `Tubi`, and `ROKU` provider prefixes, common HD/UHD markers, punctuation, and spacing are normalized. A leading HDHomeRun-style number is removed only when it exactly equals Dispatcharr's channel-number metadata, so event years and unrelated numeric names remain intact. A score is never accepted automatically:

- **Confirm** adds one representative reviewed stream-name alias only when the independent name score is below 95%. Names at or above 95% are already eligible under Lineuparr's required **Exact** sensitivity and are not duplicated in the JSON. Provider-reported `tvg_id` values remain internal matching evidence and are not added by the browser.
- **Deny** records that stream/channel pairing as rejected. When the independent name score is 95% or higher, one representative name is also exported in that channel's `excluded_aliases` list so a compatible Lineuparr plugin rejects the reviewed false positive before positive alias or fuzzy matching. Lower-scoring denials are not exported because Exact mode would not accept them. When a fuzzy proposal had other qualifying targets, the already-scored alternatives open immediately for separate confirmation or denial.
- **Undo** reverses either decision. Confirmed aliases can also be removed from or restored to the export with the same alias controls used for other sources.

Confirm and deny actions remove their row immediately without locking or re-sorting the remaining review page. The initial page contains 100 groups; **Load more** explicitly requests the next 100. Decisions retain the safe normalized stream identity as well as the active lineup, stream fingerprint, and target channel, so equivalent account variants, authentication changes, and container restarts do not restore reviewed rows. The confirmed counter opens reviewed matches.

The threshold decision uses an independent name score rather than the overall proposal score. An exact provider TVG/EPG ID can make the overall proposal 100% even when the names are unrelated; because Lineuparr does not consume lineup JSON `epg_ids`, that confirmation still exports a name alias when its name score is below 95%. This prevents non-name evidence from being mistaken for a match that Lineuparr can reproduce.

Only the metadata needed for review—stream ID, name, `tvg_id`, M3U account/group IDs, and provider channel number—is retained. Dispatcharr stream URLs, logos, tokens, and statistics are discarded as the API response is decoded and are never returned to the browser, saved in Lineuparr state, or exported. Stream lists are cached in memory for five minutes; if a refresh fails, a visible warning identifies the older list being used.

## HTTP Endpoints

| Endpoint | Description |
|---|---|
| `GET /setup` | Choose or change the active provider lineup |
| `GET /api/setup/config` | Read the current non-secret lineup selection |
| `GET /api/setup/providers?postalCode=...` | Find Gracenote lineups for an area |
| `POST /api/setup/provider` | Save the selected provider and queue a fresh guide |
| `GET /lineuparr` | Review the current lineup and export Lineuparr JSON |
| `GET /api/lineuparr/provider-address/config` | Read ZIP-wide address requirements, saved address and previous provider-test results |
| `POST /api/lineuparr/provider-address/config` | Save & test `{fingerprint,addressText}`; legacy structured `{fingerprint,address}` saves without a verification claim |
| `DELETE /api/lineuparr/provider-address/config` | Forget the address and test results for `{fingerprint}` |
| `GET /lineuparr/address-help.png` | Embedded cropped Google Maps instructional screenshot |
| `POST /api/lineuparr/provider-address/search` | Legacy Nominatim search; not used by the current UI |
| `GET /api/lineuparr/draft` | Current builder draft with aliases, provenance, and duplicate suggestions |
| `GET /api/lineuparr/alias-index` | Read configured-ZIP scan progress and attributed alias evidence |
| `POST /api/lineuparr/alias-index/run` | Scan or refresh all providers in the configured ZIP; ranked scan actions are rejected |
| `POST /api/lineuparr/alias-index/stop` | Stop a running batch safely |
| `POST /api/lineuparr/channel` | Include/exclude one channel or update its category |
| `POST /api/lineuparr/categories` | Atomically apply one category to a validated channel selection |
| `GET /api/lineuparr/channel-programs?channelId=<id>` | Return up to 24 current/upcoming programmes from the selected guide |
| `POST /api/lineuparr/remove-duplicates` | Exclude the reviewed `channelIds` subset, or all current suggestions for backward-compatible requests without that field |
| `POST /api/lineuparr/restore-all` | Restore every provider channel to the export |
| `GET /api/lineuparr/export` | Download the current Lineuparr-compatible JSON file |
| `POST /api/lineuparr/publish` | Save the current draft as the published snapshot; requires the draft's `sourceFingerprint`; returns its relative URL and filename |
| `GET, HEAD /lineuparr/exports/{filename}` | Read the last explicitly exported JSON by its descriptive download filename, without rebuilding; `?download=1` requests an attachment |
| `POST /api/lineuparr/alias` | Remove or restore one attributable alias for the active lineup |
| `GET, POST, DELETE /api/lineuparr/dispatcharr/config` | Read, test/save, or remove the Dispatcharr connection; saved credentials are never returned |
| `GET /api/lineuparr/dispatcharr/review` | Fetch the current safe M3U match-review queue; `limit` controls the visible page and `refresh=true` refreshes streams |
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

### Optional Docker-network links

In Setup, save an **Internal base URL**, for example `http://gracenote-dev:8080`, to show an additional copyable XMLTV URL for other containers. Use the scraper's listening port, not the host's published port. The importer and scraper must share a Docker network with that name or alias; being on the same host alone is insufficient. The application does not discover Docker names or test connectivity. Leave the setting blank to hide these links.

The setting is saved separately at `CONFIG_PATH.links.json`. Persist the configuration directory to retain it across container replacements. Changing providers does not clear this installation-level setting. It does not change the normal browser URL, image URLs, guide generation or export contents.

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
lineupindex/     Scanner-independent lineup evidence, local provider scans and storage
providersource/  Focused official provider evidence adapters and generated snapshots
tools/           Deterministic source-catalog maintenance generators
lineuparr.html   Lineuparr review/export UI (embedded at build time)
guide.tmpl       XMLTV output template (embedded at build time)
```

---

### Provider scan safety and refreshes

Provider numbers are never cross-provider join keys. Even within a provider source, a number requires corroborating normalized identity; a generic PDF for another headend cannot rename a station just because its number matches. Terminal HD and broadcast DT can normalize for identity matching, but DT2/DT3 remain distinct.

After updating, run **Scan providers in this ZIP** once. Unsafe legacy number-only joins and older derived EPG evidence are excluded from drafts until refreshed. Evidence files remain available for audit; published JSON snapshots change only when you export again.

Meaningful aliases counts distinct safe names beyond each indexed station's baseline, including provider and weekday EPG evidence. Aliases for current lineup excludes names already in the selected Gracenote lineup. These are unique station/name counts, not raw fact totals. A failed scan start stays visible rather than being replaced by the previous scan's status.

<sub>Portions of this project were developed with the assistance of generative AI ([Claude](https://claude.ai)).</sub>
