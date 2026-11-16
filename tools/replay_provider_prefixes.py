"""Offline protected-outcome comparison; never captures data or changes decisions.

Inputs are sanitized private copies plus prospective_go_export.json produced by
TestProviderPrefixPrivateProspectiveExport. Baseline modules must be materialized
separately from the intended pinned revision, not inferred from a live install.
Reports contain raw names and belong only in the explicit private directory.
"""
import argparse
import json
import subprocess
import sys
from pathlib import Path


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--directory', type=Path, required=True)
    parser.add_argument('--baseline-modules', type=Path, required=True)
    parser.add_argument('--candidate-modules', type=Path, required=True)
    args = parser.parse_args()
    private = args.directory.resolve()
    read = lambda name: json.loads((private / name).read_text())
    inventory = read('inventory.json')
    streams = [dict(key=f"{s['m3u_account_id']}:{s['id']}", name=s['name'],
                    accountId=s['m3u_account_id'], groupId=s['channel_group_id'],
                    groupKnown=s['channel_group_id'] is not None, groupName=s['group_name']) for s in inventory]
    prospective = read('prospective_go_export.json')
    baseline = read('baseline_go_export.json')
    request = dict(country=read('active_targets.json')['country'], streams=streams,
                   baseline=json.dumps(baseline), proposed=json.dumps(prospective))
    results = {}
    for label, modules in [('old', args.baseline_modules), ('new', args.candidate_modules)]:
        output = private / f'{label}_impact.json'
        with output.open('wb') as out, (private / f'{label}_impact.log').open('wb') as err:
            process = subprocess.run([sys.executable, '-I', '-B', str(modules / 'impact_runner.py')],
                                     input=json.dumps(request).encode(), stdout=out, stderr=err, timeout=300)
        output.chmod(0o600)
        if process.returncode:
            raise RuntimeError(f'{label} impact failed; private log has details')
        results[label] = json.loads(output.read_text())
    targets = {(r['category'], r['entryIndex']): r['channelId'] for r in read('prospective_target_index.json')}
    names = {}
    for stream in streams:
        names.setdefault(stream['name'], set()).add(stream['key'])
    def physical(rows):
        pairs = set()
        for row in rows:
            target = targets[(row['category'], row['entryIndex'])]
            # Only the historical API expands by name. New API keys are final.
            keys = row.get('keys')
            if keys is None:
                keys = {k for name in row['names'] for k in names.get(name, ())}
            pairs.update((target, key) for key in keys)
        return pairs
    old = physical(results['old']['baseline'])
    new = physical(results['new']['proposed'])
    saved = read('protected_decisions.json')
    target_ids = set(targets.values())
    report = {'oldBaselinePairs': len(old), 'newProspectivePairs': len(new),
              'addedPairs': len(new-old), 'lostPairs': len(old-new),
              'newlyMatchingDenials': [], 'previouslyMatchingDenials': [],
              'lostConfirmations': [], 'alreadyMissingConfirmations': [],
              'outOfActiveTargets': 0, 'newExcludedMatches': [],
              'note': 'Old consumer/current captured Go export versus new consumer/new projected Go export. Read-only offline replay; no decision or publication mutation. Existing conflicts remain conflicts.'}
    for kind, records in [('primary', saved['matchDecisions']), ('supplemental', saved['supplementalDecisions'])]:
        for key, decision in records.items():
            channel = decision['channelId']
            if channel not in target_ids:
                report['outOfActiveTargets'] += 1
                continue
            stream = f"{decision['m3uAccountId']}:{decision['streamId']}"
            pair = (channel, stream)
            detail = dict(kind=kind, key=key, channelId=channel, streamKey=stream,
                          rawName=decision['streamName'], historicalScore=decision.get('nameScore'))
            if decision['decision'] == 'confirmed' and pair not in new:
                report['lostConfirmations' if pair in old else 'alreadyMissingConfirmations'].append(detail)
            if decision['decision'] == 'denied':
                denied = {(channel, k) for raw, keys in names.items()
                          if ' '.join(raw.casefold().split()) == ' '.join(decision['streamName'].casefold().split()) for k in keys}
                if new & denied:
                    report['previouslyMatchingDenials' if old & denied else 'newlyMatchingDenials'].append(detail)
    sys.path.insert(0, str(args.candidate_modules.resolve()))
    from fuzzy_matcher import FuzzyMatcher, _exclusion_parts, parse_excluded_aliases
    matcher = FuzzyMatcher(match_threshold=95)
    stream_by_key = {s['key']: s for s in streams}
    for category, rows in prospective['categories'].items():
        for i, entry in enumerate(rows):
            target = targets[(category, i)]
            parts = tuple(_exclusion_parts(x) for x in parse_excluded_aliases(entry.get('excluded_aliases')))
            for channel, key in new:
                if target == channel and matcher._candidate_is_excluded(stream_by_key[key]['name'], parts):
                    report['newExcludedMatches'].append(dict(channelId=channel, streamKey=key))
    output = private / 'protected_replay_report.json'
    output.write_text(json.dumps(report, indent=2)); output.chmod(0o600)
    print(json.dumps({key: len(value) if isinstance(value, list) else value for key, value in report.items()}))


if __name__ == '__main__':
    main()
