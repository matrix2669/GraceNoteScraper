# Stream review card presentation

Normal cards show the raw stream group, readable target and callsign, the primary score when available, one concise match reason, and Confirm/Deny. Raw variants remain collapsed; actionable save failures remain visible. A single raw stream name and returned alias are shown separately, for example `Matched alias: “TV:HBODRAMA” → “HBO Drama West”.` Returned aliases reflect pinned normalized lookup, not byte equality or feed proof. Aggregated alias/raw-name lists do not establish pairwise mappings. Retrieved descriptors are labelled Name evidence without repeating the target. Unknown feed and provenance paragraphs are omitted.

The primary feed uses the ordinary service name. A verified West target adds West before the callsign, for example `HBO Drama West (HBODHDP)`; the primary is `HBO Drama (HBODRHD)`. The operator explained that the primary service was programmed for the East feed and the West feed was added for the West coast. This is a display convention only: no schedule offset is assumed or used for matching. Existing distinct names stay intact. A target feed never establishes the feed of a raw stream.

Routine Validation details and Reload review are hidden. Successful primary Confirm/Deny choices do not open the automatic alternatives dialog; eligible alternate target pairs remain as ordinary ordered cards with real Confirm/Deny controls. Inline impact results do not repeat Extra rows as a second action surface or emit generic unresolved prose based only on hidden Extras. Missing matches, saved-confirmation conflicts, denial conflicts and same-name conflicts remain visible; actual load/validation failure exposes existing recovery. Background validation and completion gates continue. After the last acknowledged choice, an accessible native indeterminate bar shows Waiting, Preparing or Checking saved choices for the current queued, preparing or running validation. It never derives a percentage from per-pass counters. Ordinary visible cards keep their Confirm/Deny controls without a per-click bar. Pending or uncertain saves retain their separate recovery presentation. The bar continues through the owned final review read and any prefix refill until actual current cards render or a current terminal read reconciles an explicitly empty queue. Failed or skipped loading stops animation and exposes Reload review or Load more retry. Source, reset incarnation, request, accepted read order, epoch, decision and local edit revisions fence that handoff; a stale callback cannot clear or resurrect another view’s bar. Missing metadata is unavailable rather than empty, and genuine terminal conflicts stop animation while Step6 remains incomplete. A persistent status beside Generate/Re-generate explains that generation publishes current saved choices while checks continue. Successful generation is qualified while validation remains active; later current completed extras show `Additional matches need review. The published file has not changed.` This notice survives export-summary refreshes. Pending and uncertain writes retain distinct status and the existing generation guard.

Decision acknowledgement waits at most 15 seconds for headers and complete body, then checks the original saved-state receipt once with a 5-second deadline. An unverified save remains Uncertain; accepted later choices wait in order. Summary totals exclude unsettled Confirm/Deny intentions until verified; they remain in the remaining-review total. Check saved state is reachable inside a popup; retry during uncertainty checks the receipt only. A definitive not-saved receipt enables a fresh explicitly chosen retry after admission refresh. Closing the popup remains available. This bounds browser waiting, not backend completion.

Matcher inputs, scores, pins, feed evidence, durable choice semantics and generated JSON policy remain unchanged. A deadline never proves a write failed, and no automatic POST retry occurs.

A post-review draft-read failure reports only that the draft could not be updated. It does not announce a save or change an already verified saved, not-saved, cancelled or retired choice.

Category review and lineup customization use the same provenance-filtered friendly channel name as stream review, followed by the callsign when distinct. East is omitted only for a verified primary feed; West and Pacific qualifiers require source-bound service evidence. When no friendly name is available, the callsign or channel number remains the fallback. The display uses text nodes and does not change channel keys, saved categories, or customization choices.

A readable decision HTTP 5xx response can replace a durable acknowledgement. It follows the same one-shot receipt check and ordered uncertainty barrier as a lost response; a definite application rejection such as 409 retains its existing failure behavior. Other API requests are unaffected.

Changing included channels retires the review view separately from an already transmitted save. Unsent old-view intentions are cancelled; transmitted jobs retain unload protection and original-operation recovery until their outcome is accounted for. A valid acknowledgement preserves still-applicable history/counts, while source/scope fences prevent publication into a replacement view or reopening an obsolete popup. Retirement itself never proves that a transmitted write was unsaved. No automatic POST retry or global channel-edit disable is introduced.

An uncertain proposal remains one logical intention when channel removal clones its visible row. Original source/job ownership and a stable local proposal id govern terminal cleanup: a saved receipt removes that owned proposal; a not-saved receipt updates one retry row with exact group/physical counts. Newer choices sharing a display key keep their own state. This reconciliation is local UI state and does not change decision payloads or persistence policy.


Publication UI ownership is scoped by the source fingerprint, reset generation,
and a local incarnation counter. Source replacement and a successful reset retire
that view; a failed reset does not. A generation request captures its view,
completed validation identities and edit revision before sending. Its successful
acknowledgment retires only those captured identities. Findings accepted while
that request is in flight remain actionable. A retired view’s completion cannot
change the replacement view’s signatures, dirty warnings or messages.

Published snapshot metadata is immutable and separate from draft signatures.
Local edits during generation retain their signatures and dirty warnings after
acknowledgment. Export-summary reads carry request and publication-transition
fences; an earlier read cannot replace a newer accepted summary or publication.
A summary-only publication change supplies context but does not prove unresolved
findings obsolete. Authoritative absence is distinct from a failed summary and
never claims a nonexistent publication is unchanged.

Validation results register once at accepted-response ingress, keyed by opaque
job and input identities within the view. Rendering only derives presentation;
it neither registers nor retires findings. Read ownership fences older replies;
timestamps and ID spelling never establish chronology. Definitive publication
failure and unconfirmed outcomes preserve findings and dirty state without an
automatic retry or a new generation barrier. Existing ordered saves, receipt
recovery, queued/running validation, all-score Confirm/Deny and advisory generation
retain their previous contracts.


Unacknowledged generation (including a lost response, unreadable response body or
ambiguous publication-server error) leaves the publication outcome unconfirmed.
The UI retains and labels the last confirmed snapshot, findings, draft signatures
and dirty warnings. It does not claim the published file is unchanged. A failed
summary keeps that uncertainty; only a current authoritative summary or a later
acknowledged generation resolves it. No automatic generation retry or new barrier
is added. Current completed no-extra validation results resolve the extra-match
notice independently of remaining missed, protected-confirmation or same-name
conflicts; those conflicts stay visible and continue to prevent review completion.


Accepted terminal workflow status uses the same owned queue handoff as terminal polling. When the visible page is empty, the authoritative union count/tokens advance and the existing prefix refill supplies ordinary cards. Loading remains visible through that refill. Older status callbacks are retired by the existing request fence; repeated workflow reads reuse an owned load or explicit retry rather than starting another loop. A later authoritative terminal zero can resolve an earlier failed refill. Nonempty pages retain their cards and order. Missing metadata, failed/skipped loads and superseded reads expose recovery rather than hiding discovered work.

Friendly-name filtering rejects actual opaque channel-number/callsign combinations, station/placement IDs, numeric-only values and callsign-equivalent spellings. An attributable provider description is not rejected merely because its merged method sentence contains `channel number plus`; the captured My9 (WWOR) description is one regression example, not a channel-specific mapping. JavaScript and Go use the same value controls and preserve historical/inactive/unattributed exclusions, current exact-station descriptor/family precedence, stable source ranking, primary/West/Pacific presentation and safe text rendering. This resolution changes no canonical identity, aliases, evidence, saved choices or matching/export inputs.
