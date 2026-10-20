# Service identity and provider evidence policy

Duplicate identity is independent of HD/SD quality. Exact station repeats and verified station pairs form complete groups, including excluded positions. Quality ranks a default keeper after identity is established. Explicit inclusion, keeper, category and alias choices remain unchanged. Regional, time-shift and numbered-subchannel conflicts block cross-station grouping; ambiguous alias graphs do not select an arbitrary winner. Generic seasonal placeholders are not identity evidence. Corroborated official aliases can establish seasonal service identity without matching weekday schedules.

Provider catalog alignment is necessary but insufficient for number recovery. A contradictory row cannot override an identity elsewhere in the catalog or grid. Xfinity address responses lack a verified Gracenote headend binding and therefore contribute exact identity matches only. Frontier Fios does not route through the Verizon adapter. Other adequately aligned, noncontradictory provider-number recovery remains available.

`provider-identity-v1` carries row, root station/source and typed station-pair provenance through schedule propagation. A verified schedule does not upgrade an unsafe provider alias. `provider-evidence-quarantine-v1` excludes unsafe historical Xfinity number joins, Verizon-on-Frontier origins and their attributable derived evidence from the active view. Legacy reciprocal confirmed schedule transfers can be re-evaluated without new scans. Unproven legacy evidence remains unavailable rather than becoming authoritative.

## Dry run and deployment proposal

Run `go run ./cmd/evidence-audit /path/to/private/index-copy.json` against a private copied index. The command validates the schema and prints source/row/station reasons without fetching, writing, migrating data or reading builder decisions. Keep its output private. Quarantine annotations are unexported in-memory fields; tests assert byte-identical serialized data and unchanged input files. Reviewed choices are never erased.

Before a separately authorized deployment: archive the current binary/image and private retained-data snapshot, run the dry run on a copy, review affected evidence and compare an offline draft with saved decisions. Deploy only after approval, then inspect the draft and all reported pairs without initiating provider scans or live matching. No destructive cleanup is required. A binary rollback restores the previous evidence view and its previous unsafe-alias behavior; preserve the snapshot for recovery. Additive provenance fields require no schema migration. New scans, live Preview/Apply and any permanent reconciliation require separate authorization.

## Verification

Public synthetic tests exercise all 15 reported pairs, excluded repeats, unknown/equal quality, short names, regional/time-shift/subchannel conflicts, ambiguous alias graphs, saved choices, all five wrong assignment paths with passing alignment, and quarantine provenance. A separate private retained-data replay exercises index loading, alias projection and draft grouping without network or retained-data writes. Private provider records are not committed. The accepted one-pass Dispatcharr matcher policy is unchanged.
