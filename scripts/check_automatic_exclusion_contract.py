"""Exercise actual Go-exported JSON with the unchanged consumer matcher.

Synthetic review scores cover eligibility independently of matching confidence;
we also print the real one-pass/Exact behavior for the tested raw names.
"""
from pathlib import Path
import json
import logging
import os
import subprocess
import sys
import tempfile
root = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(root / 'lineuparr_matcher'))
from fuzzy_matcher import FuzzyMatcher
logging.disable(logging.CRITICAL)
names = ['US: HBO East HD', 'US: HBO HD', 'US: HBOHDP']
with tempfile.TemporaryDirectory() as directory:
    fixture = Path(directory) / 'exports.json'
    env = dict(os.environ, AUTOMATIC_EXPORT_FIXTURE=str(fixture))
    subprocess.run(['go', 'test', '.', '-run', '^TestAutomaticFeedJSONConsumerFixture$', '-count=1'], cwd=root, env=env, check=True)
    for record in json.loads(fixture.read_text()):
        channels = [channel for rows in record['lineup']['categories'].values() for channel in rows]
        if record.get('case') == 'actual-consumer-71':
            target = next(channel for channel in channels if channel['name'] == 'HBOCPHD')
            raw = ['TV| HBO COMEDY ᴿᴬᵂ', 'HBO COMEDY RAW EAST']
            aliases = {'HBOCPHD':['HBO Comedy - West','HBOCPHD','HBO Comedy']}
            for threshold in (70, 95):
                matcher = FuzzyMatcher(match_threshold=threshold)
                matcher.precompute_normalizations(raw)
                baseline = matcher.match_all_streams('HBOCPHD', raw, aliases, 901, lineup_country='US')
                if threshold == 70:
                    assert len(baseline)==2 and all(match[1]==71 for match in baseline), baseline
                else:
                    assert not baseline, baseline
                protected = matcher.match_all_streams('HBOCPHD',raw,aliases,901,lineup_country='US',excluded_aliases=target['excluded_aliases'])
                assert not protected, protected
            print('Actual score71 unresolved and opposing HBO Comedy Pacific candidates admitted at70 are excluded by generated JSON; absent already at95.')
            continue
        west = next(channel for channel in channels if channel['name'] == 'HBOHDP')
        for threshold in (70, 95):
            matcher = FuzzyMatcher(match_threshold=threshold)
            matcher.precompute_normalizations(names)
            aliases = {west['name']: ['HBO - West', 'HBOHDP', 'HBOP']}
            baseline = matcher.match_all_streams(west['name'], names, aliases, 901, lineup_country='US')
            protected = matcher.match_all_streams(west['name'], names, aliases, 901, lineup_country='US', excluded_aliases=west['excluded_aliases'])
            assert [match[0] for match in protected] == ['US: HBOHDP'], (record, threshold, protected)
            assert {match[0] for match in baseline} >= set(names), baseline
            # Per-channel negatives do not globally ban the same East stream.
            east = matcher.match_all_streams('HBOHD', ['US: HBO East HD'], {'HBOHD':['HBO - East','HBO']}, 899, lineup_country='US')
            assert east, (threshold, east)
    print('Actual consumer raw-name baseline: East, generic and Pacific code each score100; generated JSON retains only the Pacific code at thresholds70/95.')
    print('Automatic eligibility synthetic scores0/70/71/80/94/95/100 all protected; manual Deny threshold unchanged; proper East target unaffected.')
