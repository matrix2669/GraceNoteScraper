# Vendored Lineuparr matcher

The unchanged canonical modules below are pinned to local consumer source revision
`17fa3f69e1706e902af45a82c57584e884e3e0ab` on the isolated provider-prefix work branch. Its prerequisite
composition is current upstream `b38a4e5d6134db8a752003f10ed491ab9c583c72` plus
PR26 `c766a6bb582ca8ef28ae9548d83af1e61f6cde5d`, assembled at local commit
`270f44856d48be7e1552f41b46a913ac74de0986`. The MIT license is included.
This is source provenance, not a claim of publication or installed runtime parity.

SHA-256:

- fuzzy_matcher.py: `0692ca7234762229712546dcd7203ac4392af3df6403de5ad6b637991f783b3e`
- provider_prefixes.py: `078644e564413ce4bf85615e5833debe24bc3130bb77e49d211426b140a5f8b4`
- matching_core.py: `33ca1c61dad02d16a9362dab4e4f3b1edb3347db0a377eae592b44812449231d`
- aliases.py: `756d3bafc79a93ab2439a5a0b8d56f02528eb97f3cc7bedb2deddc7af45e52f8`

## Primary prediction

`runner.py` creates one matcher at70 and one operation-local discovery context.
Original and scoped comparison views enter one scoring pipeline per channel;
actual winning scores/type/exact-alias provenance return by physical stream key.
There is no second Exact95 pass, Go score boost or EPG-based admission. Base aliases
only are supplied; generated review aliases and exclusions apply after scoring.
The profile uses active lineup country and channel-number boost; optional quality
filtering, ignored tags and external consumer alias catalogues remain absent.
Original candidates retain the prior no-group-country profile; new variants must
also satisfy their captured group country. Equivalent scores require equivalent
profiles, not merely the same threshold or revision.

The generic `scoped-prefix-v1` discovery policy is documented in the consumer README.
It requires known account/group scope, five distinct tails, three complete
catalogue-title/base-alias corroborations and 80% group coverage. It refuses
semantic prefixes and requires complete target/explicit-alias identity agreement
for new stripped paths. Sparse or ambiguous inputs retain original-only matching.
Raw names, keys and fingerprints never change. Separate immutable comparison
evidence feeds the existing Go feed/inventory/supplemental identity checks without
changing raw literal exclusions or persisted choices.

## Explicit impact and physical identity

`impact_runner.py` decodes actual baseline/prospective exported JSON and uses the
same occurrence API at Exact95 with built-in/country/embedded aliases, specific
numbering (no boost), quality filtering off and captured group-country metadata.
It mirrors consumer channel-country-prefix rewriting. Each exported target is
identified by category plus entry position, while each accepted stream is returned
by physical key. Names remain display-only. The Go comparison cannot spread a
supported scope's match to another occurrence with an equal raw name.

Raw exported aliases/exclusions can still affect equal names in multiple accounts;
the JSON schema has not acquired account-specific selectors. Saved scores, Deny
eligibility, confirmations, suppression and Undo are not migrated or reinterpreted.
Protected-decision replay must report conflicts rather than inventing new choices.

Go extracts embedded scripts into a private temporary directory for an explicit
refresh or impact action. Python runs isolated (`-I -B`), with a minimal environment,
a five-minute deadline and bounded output (64MiB/250000 pairs). Normal reads and
choices spawn no matcher; failed or stale refreshes cannot replace a complete
snapshot. Discovery is captured, immutable and operation-local, never global state.

Python3 with RapidFuzz is recommended; native fallback remains supported. Docker
uses its pinned runtime. A changed consumer pin requires unchanged-module hashes,
primary/impact/consumer physical parity and protected replay. Local source and
synthetic tests do not authorize publication, live refresh or deployment.
