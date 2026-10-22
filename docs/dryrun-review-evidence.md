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
even with the same count, makes review stale and removes current automatic evidence. A newly requested export
remains available with a refresh warning; a changed inventory observation after
draft construction rejects only that stale in-flight export.
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
labels retain the same established review holds as Pacific/West labels. Existing confirmations win
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
PBS fallback IDs, generic local-subchannel placeholders and aliases shared across
distinct active station IDs are withheld. When `LINEUPARR_REFERENCE_CATALOGS=on`,
reviewed categories from those same exact-ID rows are also eligible as attributed
hints, preserving conflicts and manual choices. Portable aliases do not depend
on that optional category setting. Legacy
provider snapshots stay disabled. Diagnostic method text is not identity evidence.
No Dispatcharr account is restored or used as a reference catalog.

## Provider admission

ADR-036's provider-local descriptive alias recovery remains supported. A unique
applicable official provider position may recover an abbreviated station name
after the existing 10 percent same-number exact-identity alignment gate passes.
Ambiguous positions, contradictory recognized identities, event rows and
cross-provider/lineup joins remain rejected. Required address, headend and source
applicability checks still apply; bundled catalogs never join by channel number.
A recovered alias may support a reviewable category relationship, not independent
category confirmation. Same-position multi-station rows remain transient schedule
candidates; schedule agreement does not promote every provider label.

The shared source-bound evidence view continues to admit qualified current-scope
`number-policy-provider-alias-v3` facts with their alignment marker. Old,
provenance-poor or inapplicable records stay audit-only. The active policy remains
`station-evidence-eligibility-v1`: this repair does not retire ADR-036, migrate
retained evidence, or force an unrelated old-policy export invalidation.

## Builder reset and recovery

Start builder fresh clears builder-owned choices only, with a private backup
before atomic replacement. Reset and restore each offer Keep last valid published
JSON (default) or Withdraw until regeneration. Both invalidate stale clients and
review snapshots. A separate monotonic publication generation prevents restoring
choices or choosing Keep from resurrecting an already withdrawn export. Retained
JSON bytes and copies already imported by Dispatcharr are unchanged. Guide,
TMDB data, current-source category completion, reusable evidence, provider/source
settings and connection credentials survive both options. See README for the
confirmation and recovery workflow.

## Verification

Public regression fixtures contain synthetic safe metadata. The optional
`TestRetainedDryRunReplay` accepts private captured input/output paths through
`DRYRUN_REPLAY_INPUT` / `DRYRUN_REPLAY_OUTPUT`, constructs temporary state, reapplies
captured omissions/suppressions and saved decisions, and invokes the unchanged
single-pass runner. It replays the captured active admission set plus this policy
delta; it does not claim a new live evidence scan. Full consumer comparison uses
isolated saved JSON/inventory at its configured Exact threshold. A live refresh or
regeneration is never part of tests.
