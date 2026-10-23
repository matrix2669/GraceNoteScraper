# Service identity and provider evidence policy

Duplicate identity is independent of HD/SD quality. Exact station repeats and verified station pairs form complete groups, including excluded positions. Quality ranks a default keeper after identity is established. Explicit inclusion, keeper, category and alias choices remain unchanged. Regional, time-shift and numbered-subchannel conflicts block cross-station grouping; ambiguous alias graphs do not select an arbitrary winner. Generic seasonal placeholders are not identity evidence. Corroborated official aliases can establish seasonal service identity without matching weekday schedules.

Provider catalog alignment is necessary but insufficient for number recovery. A contradictory row cannot override an identity elsewhere in the catalog or grid. Xfinity address responses lack a verified Gracenote headend binding and therefore contribute exact identity matches only. Frontier Fios does not route through the Verizon adapter. Other adequately aligned, noncontradictory provider-number recovery remains available.

`provider-identity-v1` carries row, root station/source and typed station-pair provenance through schedule propagation. A verified schedule does not upgrade an unsafe provider alias. `provider-evidence-quarantine-v2` excludes unsafe historical Xfinity number joins, Verizon-on-Frontier origins and their attributable derived evidence from the active view. Legacy reciprocal confirmed schedule transfers can be re-evaluated without new scans. Unproven legacy evidence remains unavailable rather than becoming authoritative.

## Dry run and deployment proposal

Run `go run ./cmd/evidence-audit /path/to/private/index-copy.json` against a private copied index. The command validates the schema and prints source/row/station reasons without fetching, writing, migrating data or reading builder decisions. Keep its output private. Quarantine annotations are unexported in-memory fields; tests assert byte-identical serialized data and unchanged input files. Reviewed choices are never erased.

Before a separately authorized deployment: archive the current binary/image and private retained-data snapshot, run the dry run on a copy, review affected evidence and compare an offline draft with saved decisions. Deploy only after approval, then inspect the draft and all reported pairs without initiating provider scans or live matching. No destructive cleanup is required. A binary rollback restores the previous evidence view and its previous unsafe-alias behavior; preserve the snapshot for recovery. Additive provenance fields require no schema migration. New scans, live Preview/Apply and any permanent reconciliation require separate authorization.

## Verification

Public synthetic tests exercise all 15 reported pairs, excluded repeats, unknown/equal quality, short names, regional/time-shift/subchannel conflicts, ambiguous alias graphs, saved choices, all five wrong assignment paths with passing alignment, and quarantine provenance. A separate private retained-data replay exercises index loading, alias projection and draft grouping without network or retained-data writes. Private provider records are not committed. The accepted one-pass Dispatcharr matcher policy is unchanged.

## Active evidence eligibility

One immutable EvidenceView binds country, ZIP, headend, lineup, device, language, fingerprint and current raw callsigns. It projects aliases, categories, binding proofs and active counts together under the index lock. Provider positions and number-only rows are local assertions; historical row associations are not global identity just because they are stored under a station ID. Reusable exact-ID catalog facts carry station-bound source/revision/row evidence. Other official row reuse must be independently corroborated by the current raw identity. Missing-provenance legacy schedule copies are inactive. Multiple unsafe root scopes are retained for quarantine analysis, without deleting records.

New confirmed schedule pairs preserve only raw callsign identity, target/peer identity, programme revision, timezone and non-concurrent windows. Provider aliases/categories never inherit a valid schedule score. Programme and affiliate names cannot seed maintained rename identities. Regional/time-shift/subchannel and seasonal protections remain conservative.

Previously published records are preserved but serve only with the current evidence policy and source fingerprint. Old-policy or old-source GET/HEAD returns 409 and the summary requires explicit regeneration. Eligible snapshots remain stable across draft edits and restart without a guide. No automatic export or live scan occurs. This supersedes previous unrestricted serving across provider changes.

The private retained-lineup replay supports 6/15 original pairs under this stricter policy; the other 9 are withheld rather than supported by unproven history. All 15 have synthetic positive coverage with adequate exact-ID evidence. All original records and reviewed choices remain unchanged. ACC's old MLB alias is inactive in draft/export/matching. Recovery of withheld pairings requires adequate fresh or independently verified reusable evidence, not a blacklist or weaker threshold.

### Short raw identities

A short raw callsign and its terminal HD/SD/DT counterpart can establish a
quality-neutral service relationship when each station independently has an
eligible official alias corroborating that exact base. Unknown policy, missing
row provenance and suppressed evidence do not qualify. Exact-station repeated
positions share that station's proof without requiring identical per-position
alias choices. Before any union, count all competing attributed short labels,
including labels with terminal quality markers on unrelated raw callsigns; three
competing station identities cannot choose a partial winner. Regional,
time-shift, numbered-subchannel and ambiguity checks still apply.

Historical number-only provider rows remain source assertions. A shared row ID
across device variants is a candidate for validation, not automatically a reusable
station-pair proof. Neither seasonal schedule absence nor unknown quality rules
out identity, but the actual row applicability and station joins must be justified
independently before promoting that relationship. This repair performs no scans,
retained-record migration or implicit alias/category promotion.


The versioned adjudicated-assignment ledger also excludes one user-rejected,
unsupported provider-row binding using its complete original source/row/root
lineup/root station tuple. Audit payload preserves original assertion values and
revision independently of mutable facts; a context SHA256 commitment avoids
committing private postal configuration. Runtime audits verify observed context.
Revision/method changes, missing context or disappearing direct records cannot
reactivate that tuple. Direct and derived alias/category support and transient
EPG seeds use the same predicate; unrelated rows, roots and independently supported
same-text evidence retain ordinary eligibility. This is not a name blacklist.

`station-evidence-eligibility-v2` invalidates old-policy saved exports for active
serving while retaining their exact bytes. Explicit regeneration is required;
Keep does not bypass evidence eligibility. No automatic live refresh, export,
publication or deployment is part of this local correction. Matcher/profile,
thresholds, reviewed choices and valid provider-identity-v1 proofs remain intact.
