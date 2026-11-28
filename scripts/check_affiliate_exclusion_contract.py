"""Offline JSON exclusion contract against the unchanged vendored consumer matcher."""
import logging
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "lineuparr_matcher"))
from fuzzy_matcher import FuzzyMatcher

logging.disable(logging.CRITICAL)
foreign = ["KTVT", "KTXA", "KWTV", "KYW", "WBBM", "WBTV", "WBZ"]
raw_names = [f"US: CBS ({call})" for call in foreign]
raw_names += ["US: CBS (KTVT) HD", "US: CBS (WCBS)"]
# Same literal escaping as the generated JSON, not normalized network aliases.
def literal(name):
    return name.replace("\\", "\\\\").replace("*", "\\*")

for threshold in (70, 95):
    matcher = FuzzyMatcher(match_threshold=threshold)
    matcher.precompute_normalizations(raw_names)
    aliases = {"WCBSDT": ["CBS (WCBS)", "WCBS", "WCBSDT"]}
    exclusions = [literal(name) for name in raw_names[:-1]]
    hits = matcher.match_all_streams("WCBSDT", raw_names, aliases, 502,
                                   lineup_country="US", excluded_aliases=exclusions)
    assert hits == [("US: CBS (WCBS)", 100, "alias")], hits
    for call in foreign:
        name = f"US: CBS ({call})"
        assert matcher.match_all_streams(call, [name], {call: [call, f"CBS ({call})"]},
                                         1, lineup_country="US"), call
    for raw in ("M*A*S*H", r"US\M*A*S\*H", "Example ? [HD]!"):
        assert not matcher.match_all_streams("ONE", [raw], {"ONE": [raw]}, 1,
                                             lineup_country="US", excluded_aliases=[literal(raw)])
print("Unchanged matcher: selected raw variants excluded per channel; WCBS and seven proper targets preserved; escaping passed at 70/95")
