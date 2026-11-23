# Vendored Lineuparr matcher

The unmodified `fuzzy_matcher.py` and `matching_core.py` come from
PiratesIRC/Dispatcharr-Lineuparr-Plugin revision
`c766a6bb582ca8ef28ae9548d83af1e61f6cde5d` (PR #26). They were compared
byte-for-byte with the installed Lineuparr beta.5 on 2026-09-07.
The upstream MIT license is included in LICENSE.

SHA-256:

- fuzzy_matcher.py: `04c66ac8156f5cbdd7a8c43a9ea0b8ae83ef1a89888d15da5e6a5ae3fa39ee91`
- matching_core.py: `33ca1c61dad02d16a9362dab4e4f3b1edb3347db0a377eae592b44812449231d`

`runner.py` is GraceNoteScraper's adapter. It creates one matcher at 70,
precomputes unique stream names once, and returns every accepted pair with the
consumer's returned score. There is no second Exact/95 pass and no Go score
adjustment or EPG-based candidate admission.

The input profile uses the active lineup's country, channel numbers, and base
embedded aliases. Optional quality filtering, custom ignored tags, external
consumer alias catalogs, and provider-group country overrides are not supplied.
Score equivalence requires the same source revision and inputs; changing
Lineuparr's optional settings can change results. Generated review aliases and
exclusions are applied after scoring, so saved decisions do not alter the scan.

The consumer can boost a name accepted at threshold 70 from 91 to 96, even when
an independent threshold-95 run would reject it before boosting. This adapter
deliberately retains that returned 96 under the operator-selected single-pass
contract. Equal returned scores are not proof of identical threshold admission.

Go embeds the scripts and extracts them into a private temporary directory for
each refresh. Python runs with isolated imports, a minimal environment, a
five-minute deadline, and bounded output (64 MiB / 250000 pairs). The temporary
files are removed after exit. Normal reads and decisions do not spawn Python. Channel removals reuse the remaining cached pairs; additions or re-inclusions hide results until explicit refresh.

Docker supplies Python 3.13 and RapidFuzz 3.14.5. Native installations need
Python 3 (set LINEUPARR_MATCHER_PYTHON to its executable if necessary).
RapidFuzz is optional for native use; the canonical fallback returns the same
similarity calculation but can be slower.

When upgrading: replace both canonical files unchanged, retain their license,
update the revision in embed.go and runner.py, update hashes here and in the
provenance test, and run Go/Python integration tests against the intended
consumer revision. Existing saved reviews retain their historical revision;
use Undo and refresh to review them with the new matcher.
