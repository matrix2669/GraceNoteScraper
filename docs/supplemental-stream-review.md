# Supplemental stream review

Refresh streams still runs the pinned LineupARR matcher once at threshold 70. Its
returned scores, primary proposals, Confirm/Deny visibility and the established
95% export rules are unchanged. Backend retrieval adds additional plausible
streams to the same actionable queue from attributed service labels and the same successful inventory. Retrieval rank remains internal and is not a match score or probability.
Retrieval requires the complete attributed service identity after bounded provider,
quality and presentation cleanup. Extra brand, event, ordinal or geographic words,
foreign country contexts and playlist headers are rejected. Short labels such as
MAX, NFL, ESPN and Movies cannot retrieve every stream containing that word.
The specific News 12 local-service/Plus, attributed Showtime truncation and WPIX
Rewind review paths remain available. AccuWeather NY has its own complete,
station-bound review descriptor; geographic suffixes are never removed generally.

Current definite opposing-feed exclusions resolve those names automatically:
they remain in **Automatic identity exclusions** and are omitted from pending
supplemental counts, groups and decision offers. No saved Deny is created.
Unknown/mixed stream feeds and ambiguous target feeds remain reviewable. Actual
saved choices, confirmation conflicts, history and Undo remain intact. The same
filter applies to consumer-impact extras and uses current target/raw-name/account
constraints; missing or stale automatic evidence cannot suppress a proposal.

The review shows eligible current service names with the station callsign, such as
**902 · HBO Hits (HBOHTHD)**. Primary **Matched using** evidence identifies
a member of the pinned consumer's exact alias lookup, preserving its normalization
and insertion order. It does not claim to identify a fuzzy winner. Fuzzy results omit unproven winning-alias attribution. Retrieval descriptors
are labelled **Name evidence**. Target feed and raw stream feed are separate; unknown
feed never inherits the target region. Eligible exact-ID descriptions and single-word
brands remain readable. Existing exact review descriptors take presentation precedence. Full feed and source warnings expand separately
from the original matcher reason, so a long explanation cannot prevent saving.
One server-owned union applies a single visible limit and pending count;
confirmed/denied history counts normalized review groups, not clicks or records.

Review the complete raw names, target station, accounts and physical stream IDs.
Confirm stores a full raw-name positive alias. Deny stores a channel-specific
literal exclusion even when there is no primary score. Both choices keep a
distinct internal origin and support merged chronological history and Undo. Equal literal names
are grouped with every captured physical member, including same-account duplicate
IDs. Primary decisions remain authoritative. New or changed physical members are
not silently approved. Suppressed aliases stay suppressed.
Saved history expands to every raw spelling, account and physical stream ID,
including pending local choices. Positive alias suppression can be restored and
never removes a reviewed negative exclusion for that name.

Saved name effects are validated automatically in the background after successful
durable decisions, Undo, relevant draft changes and successful explicit snapshots.
A750ms quiet period coalesces a burst. One server-owned child serializes the actual
prospective lineup and protected baseline through unchanged Exact95 on the same
immutable inventory. Primary refresh has priority, obsolete work is cancelled and
awaited, and one transient retry is allowed before stable actionable failure.
Reload review recovers failure once; polling only observes cached state. Shutdown
cancels process-owned work. No Python or full draft construction runs inside a
save lock or durable acknowledgement. Reads and generation never launch checks.

Newly exposed extras enter the same Confirm/Deny queue; none are automatically
denied. Completed still-eligible offers survive a replacement-running check.
Identity/snapshot/evidence/inventory invalidation retires stale offers. Findings
include same-name sibling collateral matches, missed selected names, protected
conflicts and all-account limits. Entry-target correspondence preserves duplicate
names and numbers that serialize alike. Step6 stays incomplete during necessary
queued/running/stale/failed validation or unresolved findings. Generation remains
available, and saved choices remain durable independently of validation.

Full raw positive aliases still participate in fuzzy/normalized consumer matching.
They are not exact physical-stream associations. Current JSON cannot split
outcomes for identical names on different accounts or duplicate IDs. A literal
Deny affects that name for this channel across accounts. A successful check
protects only the captured inventory; a future name can match an existing alias.
The displayed profile uses pinned built-in/country/embedded aliases, specific
numbering and quality filtering off. Missing group-country metadata and differing
custom consumer settings are disclosed limits. Unchecked, stale, incomplete or
unresolved impact is advisory; explicit generation remains available.

Candidate admission requires the current source, connection, guide, evidence,
base inputs, inventory and reset generation. History survives restart; candidate
and impact memory does not. Changing source/connection or target station/feed
identity makes earlier supplemental selectors inactive. Source exclusion and
Undo do not reuse old operation receipts to resurrect a decision.
Durable persistence and the saved operation receipt publish under the same
builder/review locks, so a concurrent review read cannot discard an already-saved
choice's retry admission. Failed writes publish no saved receipt. Obsolete
uncommitted offers still retire, and Undo/source/snapshot/reset fences still apply.

## Existing alias storage and user-attested renames

`lineupindex` persists provider-scoped station facts and original provenance.
Its EvidenceView eligibility rules remain unchanged. Portable exact-ID evidence
already lives in `lineuparr/verified_station_catalog.json`, passes through
`ApplyEmbeddedCatalogs`, and contributes base aliases and service evidence.
Historical HBO/Cinemax names were missing there. The implementation extends that
registry, with USER-ATTESTED provenance and exact station plus current callsign
checks. It does not activate provenance-poor historical provider facts.

| Current service | East station / callsign | West station / callsign | Historical family |
| --- | --- | --- | --- |
| HBO Hits | 59368 / HBOHTHD | 59355 / HBOHHDP | HBO2, HBO 2 |
| HBO Drama | 59363 / HBODRHD | 59366 / HBODHDP | HBO Signature |
| HBO Movies | 59845 / HBOMVHD | 59847 / HBOMHDP | HBO Zone |
| Cinemax Hits | 59373 / CINEHHD | 59375 / CINHHDP | MoreMAX, More Max |

Feed-qualified aliases are separate. The registry also contains explicitly
attributed review-only descriptors for abbreviated services; these never enter
primary aliases. Its generator preserves user-attested entries separately from
regenerating the pinned public rows. These assertions are user evidence, not an
independently verified network announcement.

A single explicit internal East/West/Pacific label, such as `RK: HBO EAST 2`,
identifies a feed only when its entire residual service family belongs to that
exact target's current or approved historical evidence. The shared rule applies
to scored and inventory constraints. It preserves ordinals/subbrands, geographic
brands, code conflicts and repeated/mixed-region uncertainty. `US: HBO 2 HD`
remains unresolved. Canonical channel names, primary scoring and manual export
thresholds are unchanged.

Numeric decoration is never a catalog or feed join. For example, a source-bound
HDHomeRun physical stream can be reviewed as Showtime 2 East or West; the number
869 or 870 alone does not establish that identity on another account. News 12
regional services are offered for human selection, with Plus labelled distinctly.
Language, affiliate, subchannel, subbrand and regional differences remain visible.

## Validation and private replay

Public regression fixtures are synthetic. Optional retained replay accepts only
explicit private file paths, uses a temporary state store, and executes the pure
consumer modules without application/Django initialization. It verifies the
40-target feedback manifest: 43 accepted names / 44 physical pairs, all 17 News 12
review variants, and preservation of the captured 15 Denys and other supplied
prior decisions. Rejected or unendorsed feedback and experimental extra masks
are never seeded into production. The separate 40/240 consumer reports retain
remaining misses, extras and conflicts; retrieval success is not consumer success.

## News12 evidence limitation

The existing exact-station review descriptor displays News12 with callsignN12HD
and its conflicting provider name Estrella National TV. This does not repair its
active alias or matching/export relationship. The original Verizon PDF/positioned
row, region context and applicability are unavailable. No channel blacklist,
number-recovery quarantine or alias deletion is justified. ADR036 provider-local
recovery and ADR039 eligibility remain unchanged pending original-row evidence.
