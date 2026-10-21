# Dry-run review and identity repairs

This candidate changes the builder only. The pinned LineupARR matcher, plugin,
thresholds and literal `excluded_aliases` contract are unchanged.

## Review and coverage

Short raw names that are ineligible as positive aliases still form lossless review
rows. Their opaque review keys never become matching aliases. Confirm/Deny,
account constituents, history and Undo continue to use saved decisions.

Coverage reports channels with no scored candidates before decisions/exclusions.
A current successful review with zero unresolved proposals completes Step 6. All
scores remain visible together with Confirm/Deny controls, including additional
matches. Confirm at >=95 records review without adding an inclusion alias; below95
confirmation adds an inclusion alias. Deny retains each raw name's existing exclusion
eligibility. Scores do not change review visibility or completion requirements. A complete review
does not mean that every channel has a stream.
Coverage describes the last explicit Refresh streams only. New or renamed streams
require another refresh and explicit JSON regeneration. There is no background
inventory request or guarantee for unseen names. A later observed changed inventory,
even with the same count, makes review stale and fences automatic/export leases.
Navigation remains advisory; loading, failed refresh and restart still use the
existing authoritative completion rules.

Conflicting saved confirmations appear first in automatic inspection with the
existing explicit Undo action. The list can expand beyond its initial page.
Confirmations absent from the latest complete candidate snapshot get a warning;
they may be unsupported or the stream may have disappeared. No choice is reversed.

## Identity and JSON

Stream feed parsing recognizes the captured TV/account wrappers and presentation
quality glyphs, including superscript RAW/HD, without changing raw exclusions.
Service family, feed region and quality remain separate. Opposing proved feeds
produce per-name negatives at every admitted score, including 70–94. Unknown Pacific
identities remain visible for manual review/held until confirmed. Unknown East
labels retain ordinary score-based review; they do not acquire a Pacific hold. Existing confirmations win
and are flagged. There is no new matcher pass or global wildcard.

Positive representations use admitted identity only: terminal `+` gains ` PLUS`,
and compact mixed-case identity gains an uppercase spelling for the unchanged
consumer's case-sensitive tokenization. Original attribution remains. Suppressing
a parent alias suppresses its derived representations. Independent explicit
confirmed aliases are preserved.

A typed exact-station catalog descriptor can expose its terminal compound service
name when the raw callsign is its exact prefix plus a recognized quality suffix.
This resolves a branded compound such as `Family Brand RetroPlex` / `RETROHD`
without retaining the misleading bare `RETRO` abbreviation. The inactive evidence
remains inspectable. No arbitrary brand/feed letters are stripped, and missing,
wrong-station or ambiguous descriptors do not authorize a representation.

The portable pinned PrismCast/PBS registry supplies exact-station-ID aliases
independently of the legacy reference switch. Inputs must match reviewed Git-blob
hashes. Provider positions, conflicting bindings, local identities on national
PBS fallback IDs and generic local-subchannel placeholders are withheld. Legacy
provider snapshots stay disabled. Diagnostic method text is not identity evidence.
No Dispatcharr account is restored or used as a reference catalog.

## Provider admission

Catalog-wide number alignment never proves an individual row. Uncorroborated
single-number rows no longer add aliases/categories. Exact row identities still
recover their attributable descriptors. Same-position multi-station rows remain
transient schedule candidates, not authoritative aliases; schedule agreement does
not promote all provider labels. Historical `number-policy-provider-alias-v3`
assertions are inactive through the shared evidence view, including category
relations, without rewriting retained files or choices. Applicable exact-ID and
independently justified station facts remain reusable.

The active evidence policy is `station-evidence-eligibility-v2`. On a future
approved deployment, older published snapshots require explicit regeneration;
they remain retained for audit/rollback. No provider requests, data cleanup or
migration are performed by this change.

## Verification

Public regression fixtures contain synthetic safe metadata. The optional
`TestRetainedDryRunReplay` accepts private captured input/output paths through
`DRYRUN_REPLAY_INPUT` / `DRYRUN_REPLAY_OUTPUT`, constructs temporary state, reapplies
captured omissions/suppressions and saved decisions, and invokes the unchanged
single-pass runner. It replays the captured active admission set plus this policy
delta; it does not claim a new live evidence scan. Full consumer comparison uses
isolated saved JSON/inventory at its configured Exact threshold. A live refresh or
regeneration is never part of tests.
