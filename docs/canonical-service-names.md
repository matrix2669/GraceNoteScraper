# Canonical service names

The draft may use a descriptive name from the audited exact-station registry.
This initial scope is deliberately restricted to the registry row's own name,
not its other aliases, a provider-number join, a review-only identity, or a
stream confirmation. The exact station binding, row/revision, active alias owner
census and user suppression must all survive. Conflicting registry row names
are not resolved by input order. Explicit provider rename rules retain their
existing precedence; aliases from other providers are not interpreted as typed
source-row descriptions or as contradictions merely because they differ.

The raw Gracenote identifier, callsign, aliases, inclusion/category choices and
saved review decisions remain unchanged. The selected name's existing source and
method fields identify its registry provenance. Included name/category collisions
are checked case-insensitively with collapsed whitespace; affected proposed names
fall back to their prior names until stable, and existing collisions are reported.
A terminal “ - East” or “ (East)” feed annotation from the exact source row
is omitted for the primary feed; East within a brand and West/Pacific labels are
not stripped. No feed, quality, language, or affiliate qualifier is invented to
resolve a collision. This is a downstream identity safeguard, not a claim that all matching
consequences were validated. Renames can change consumer matching and can create a
new downstream channel identity. Existing published exports are not regenerated
or withdrawn by this resolver; operators must explicitly generate a new export.
