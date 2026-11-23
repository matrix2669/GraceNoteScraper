#!/usr/bin/env python3
"""Explicit offline Exact95 impact check. Never imported by the primary runner."""
import json, logging, sys
from pathlib import Path
sys.path.insert(0, str(Path(__file__).resolve().parent))
from fuzzy_matcher import FuzzyMatcher, detect_category_country
from aliases import CHANNEL_ALIASES, COUNTRY_ALIASES
logging.disable(logging.CRITICAL)

def main():
    request = json.load(sys.stdin)
    names = sorted({s['name'] for s in request['streams'] if s.get('name')})
    observed = {}
    for stream in request['streams']:
        cc = detect_category_country(stream.get('groupName') or '')
        if cc:
            name = stream['name']
            observed[name] = cc if name not in observed or observed[name] == cc else None
    countries = {k: v for k, v in observed.items() if v}
    result = {}
    for phase in ('baseline', 'proposed'):
        # Decode actual exported JSON, including its boundary-escaped literal masks.
        lineup = json.loads(request[phase])
        matcher = FuzzyMatcher(match_threshold=95)
        matcher.precompute_normalizations(names)
        maps = {}
        entries = [(cat, i, e) for cat, rows in lineup['categories'].items() for i, e in enumerate(rows)]
        for country in {request['country'], *(detect_category_country(c) or request['country'] for c, _, _ in entries)}:
            aliases = {k: list(v) for k, v in CHANNEL_ALIASES.items()}
            for k, v in COUNTRY_ALIASES.get(country, {}).items():
                aliases[k] = list(dict.fromkeys(aliases.get(k, []) + list(v)))
            for _, _, entry in entries:
                k = entry['name']
                aliases[k] = list(dict.fromkeys(aliases.get(k, []) + (entry.get('aliases') or [])))
            maps[country] = aliases
        rows = []
        for index, (category, entry_index, entry) in enumerate(entries):
            cc = detect_category_country(category) or request['country']
            matches = matcher.match_all_streams(entry['name'], names, maps[cc],
                channel_number=None, lineup_country=cc, quality_aware=False,
                candidate_countries=countries, excluded_aliases=entry.get('excluded_aliases'))
            # Correlate each exported occurrence, even when names/numbers collide.
            rows.append({'category': category, 'entryIndex': entry_index, 'name': entry['name'], 'number': str(entry['number']), 'names': [m[0] for m in matches]})
            if sum(len(r['names']) for r in rows) > 250000:
                raise ValueError('Impact exceeds 250000 name pairs')
            print('GNS_PROGRESS %d %d' % (index + 1, len(entries)), file=sys.stderr, flush=True)
        result[phase] = rows
    result['revision'] = 'c766a6bb582ca8ef28ae9548d83af1e61f6cde5d'
    json.dump(result, sys.stdout, separators=(',', ':'))

if __name__ == '__main__':
    main()
