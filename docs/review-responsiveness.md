# Stream review responsiveness

The October 22 regression was a client lifecycle regression, not a change to
LineupARR scoring. Original commits `63de8a8` and `adf4fb7` removed expensive
per-click rematching and established optimistic, nonblocking review. Commit
`85126f2` replaced the per-key pending guard with a global pending guard and
waited for a full draft reload before releasing it. Thus the clicked item
vanished while later clicks were silently ignored.

The correction keeps the current server validation and atomic JSON state save.
Distinct review choices are accepted immediately; individual POSTs are ordered
so their state validation does not race another local decision. No batch API or
persistence schema is introduced. Only a duplicate pending key is suppressed.
Errors restore that item's original relative position without reverting newer
choices. Input changes cancel unsent writes, remove only their own optimistic
history, restore still-applicable rows and release current-view controls. Failed
rows retain a retry warning even if another save completes. Pending history is marked Saving, navigation warns, and generation,
Undo, reset and connection changes cannot race unsaved choices.

A single delayed draft reconciliation follows a settled burst. Source epoch,
request number and decision revision fence stale draft/workflow responses.
Reconciliation never replaces or reorders the current review page. Popup saves
retain their own pending/error state; the last successful item closes it. Queued
alternative dialogs preserve separate primary decisions and finish before refill
can advance the review epoch. Automatic refill waits
for persistence and runs only when the visible list is exhausted. No refresh,
matcher run or new export is triggered by these actions.

All-score visibility/Confirm/Deny, actual scores, ADR024 inclusion/exclusion
rules, automatic identity constraints and provisional feed holds are unchanged.
The UI tests exercise delayed acknowledgements and draft responses, rapid choices,
failure rollback, stale source cancellation, pending generation, popups, refill
and completion independently. An opt-in retained-copy cost test measures the
inventory projection without opening retained stores or running a matcher.
