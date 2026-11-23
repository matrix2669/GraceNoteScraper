# Automatic channel-scoped stream exclusions

The builder derives exclusions after an explicit successful **Refresh streams**. LineupARR's matcher, immutable pin, single threshold-70 pass and JSON contract are unchanged. No bulk selection or confirmation is needed for a clear mismatch. Expand **Automatic identity exclusions** in Step 6 to inspect full names, account IDs, reasons, unresolved feed names and conflicts with saved confirmations.

## Identity and uncertainty

An unambiguous raw target US broadcast callsign and a different unambiguous raw stream callsign establish an affiliate mismatch. Network affiliation alone does not. Missing/multiple callsigns, numbered subchannels, regional uncertainty and other ambiguous names remain manual.

Feed identity is independent of quality and applies across all categories/networks. The builder admits a target's qualified East/West/Pacific label only from active, attributed station evidence: an exact station-ID catalog binding or applicable current selected-provider/lineup row, with source, revision, row and root station. It retains the complete service family. Exact raw station codes are learned from that evidence; arbitrary P/H/HD suffix stripping is prohibited. Bare regional labels such as “Network West” remain uncertain, since West can be part of a regional brand. They cannot establish an opposing feed or register a station code.

A proven same-service-family opposing stream feed is excluded automatically across every concrete name in the same successful stream inventory, even when the matcher returned no candidate for that name. These inventory-only constraints have no score and do not create review proposals. Explicit country conflicts, subbrands, language tokens, regional ambiguity and mixed labels cannot be discarded to prove equivalence. A matching attributed feed remains eligible for ordinary review. A generic, mixed, family-conflicting or unresolved feed name remains in Confirm/Deny and receives a provisional JSON exclusion until explicitly confirmed. This is a review hold, not a saved Deny or an assertion that the stream is the wrong feed. Regional brands may therefore require manual review when evidence cannot distinguish a brand from a time-shift. Same quality never establishes same feed.

For example, attributed HBOHD/East and HBOHDP/West remain distinct. “US: HBO East HD” conflicts with West; “US: HBO Pacific HD” agrees with West. “US: HBO HD” supplies no feed identity and remains manual for either target. Exact HBOHD/HBOHDP codes are recognized only when those codes are independently attributed in the current lineup. No HBO-specific whitelist implements this rule.

## Manual choices and score rules

Each exclusion is one escaped full raw stream name on one target channel. Prefixes, punctuation, word boundaries and quality variants remain significant; only case and repeated whitespace deduplicate equivalent names. All current accounts/variants are examined; a foreign affiliate remains eligible for its proper channel.

Explicit saved confirmations for the actual raw constituent are preserved and flagged as conflicts rather than silently reversed, including prior connection fingerprints retained by the established review history. Use existing history **Undo** to reconsider one. A lossy normalized-group confirmation does not cover a new raw constituent; unresolved new names remain actionable. Manual Deny/history/Undo persistence is unchanged. Automatic rules never create or migrate manual decision records.

Manual ADR024 export eligibility stays unchanged: confirmed below 95 adds an alias; manual denied names at or above 95 add exclusions. Automatic identity constraints and unresolved-feed holds are different: scored rules apply regardless of returned positive score, including 70–94. The approved inventory extension additionally derives definite opposite-feed constraints without a score; uncertain holds and affiliate rules remain limited to actual scored candidates. A positive fuzzy score cannot override a proven mismatch or establish an unknown feed. Saved low-score Deny records cannot create a hole in an independent automatic constraint. There is no second matcher pass, score boost or consumer change.

## Lifecycle and publication

Derived state is ephemeral, source/connection/guide/input/snapshot scoped and recomputed on decision changes. Removed streams/targets disappear; added targets or changed inputs require refresh. Failed refreshes preserve an inspectable old review but do not provide current automatic evidence. Restart requires Refresh streams. No automatic writes occur to the retained manual state or enrichment records.

Draft provenance distinguishes automatic names from manual exclusions. Export and explicit publication bind the complete saved builder state, connection, source, immutable guide and successful snapshot; concurrent changes fail with a conflict before publication. Existing atomic publication preserves the previous record on failure.

Published JSON remains an explicit historical snapshot, not a live rule service. Refresh/choice changes mark export signatures stale; explicitly regenerate/re-publish the JSON to update what the consumer reads. Advisory navigation and export availability remain unchanged. An absent/stale snapshot displays Refresh required and supplies no current automatic exclusions. Do not claim protection until a current refresh and explicit publication.

Definite opposite-feed coverage uses an immutable safe name/account/fingerprint projection retained with the successful matcher snapshot. It covers known same-family opposite feeds even if the one-pass matcher suppressed them before returning pairs. A later cache fetch cannot replace this evidence; stale or failed snapshots supply no new constraints. Positive candidate coverage, uncertain holds and affiliate rules still use returned pairs only. Unseen or newly added names, unresolved identities and unknown families are not guaranteed. A saved explicit confirmation can intentionally override an automatic hold/conflict. Nothing here guarantees Pacific-only matching outside those limits.

## Why inventory coverage is necessary

The unchanged consumer removes literal exclusions before applying its region preference. For a regionless code, it may fall back to West alternatives when every non-West name has been removed. Deriving negatives only from returned pairs therefore misses those suppressed alternatives. The builder now records their independent identity constraints without filtering matcher inputs, rerunning scoring, fabricating positive pairs or changing aliases. Proper opposite-region targets remain eligible for the same stream. Existing explicit confirmations win and stay visible as conflicts.

## Offline verification

`go test ./...` and `go test -race ./...` cover automatic classification, manual choices, source/snapshot changes, grouped raw constituents, stale leases and publication. `scripts/check_automatic_exclusion_contract.py` validates actual Go-generated JSON using the unchanged pinned consumer at 70/95; synthetic candidate scores 0/70/71/80/94/95/100 separately test automatic eligibility. UI scripts retain their prior affiliate filenames but now test the automatic inspection panel. All fixtures are public synthetic data; retained private provider evidence is excluded.

`scripts/check_inventory_exclusion_contract.py` exercises actual generated JSON with the unchanged consumer at 70/95: Travel/FX fallback, proper West targets, preserved Cinemax confirmation and literal escaping. Server tests assert one unchanged matcher invocation, immutable inventory, no unscored candidates, deterministic exports and failure/reset/concurrent-export fencing.
