#!/usr/bin/env python3
"""Explicit offline Exact95 impact check. Never imported by the primary runner."""
import json, logging, sys
from pathlib import Path
sys.path.insert(0, str(Path(__file__).resolve().parent))
from fuzzy_matcher import FuzzyMatcher, detect_category_country, detect_name_country_prefix
from aliases import CHANNEL_ALIASES, COUNTRY_ALIASES
logging.disable(logging.CRITICAL)

def main():
    request = json.load(sys.stdin)
    names = sorted({s['name'] for s in request['streams'] if s.get('name')})
    result = {}
    for phase in ('baseline', 'proposed'):
        # Decode actual exported JSON, including its boundary-escaped literal masks.
        lineup = json.loads(request[phase])
        for rows in lineup['categories'].values():
            for entry in rows:
                cc, stripped = detect_name_country_prefix(entry.get('name'))
                if cc:
                    entry['_country'] = cc
                    entry['name'] = stripped
        matcher = FuzzyMatcher(match_threshold=95)
        matcher.precompute_normalizations(names)
        maps = {}
        entries = [(cat, i, e) for cat, rows in lineup['categories'].items() for i, e in enumerate(rows)]
        for country in {request['country'], *(e.get('_country') or detect_category_country(c) or request['country'] for c, _, e in entries)}:
            aliases = {k: list(v) for k, v in CHANNEL_ALIASES.items()}
            for k, v in COUNTRY_ALIASES.get(country, {}).items():
                aliases[k] = list(dict.fromkeys(aliases.get(k, []) + list(v)))
            for _, _, entry in entries:
                k = entry['name']
                aliases[k] = list(dict.fromkeys(aliases.get(k, []) + (entry.get('aliases') or [])))
            maps[country] = aliases
        context = matcher.prepare_stream_context(request['streams'],
            [title for cat, _, entry in entries
             for title in [entry['name'], *maps[entry.get('_country') or detect_category_country(cat) or request['country']].get(entry['name'], [])]])
        rows = []
        for index, (category, entry_index, entry) in enumerate(entries):
            cc = entry.get('_country') or detect_category_country(category) or request['country']
            matches = matcher.match_stream_occurrences(entry['name'], context, maps[cc],
                channel_number=None, lineup_country=cc, quality_aware=False,
                excluded_aliases=entry.get('excluded_aliases'))
            # Correlate each exported occurrence, even when names/numbers collide.
            rows.append({'category': category, 'entryIndex': entry_index, 'name': entry['name'], 'number': str(entry['number']), 'names': list(dict.fromkeys(m.raw_name for m in matches)), 'keys': [m.key for m in matches]})
            if sum(len(r['keys']) for r in rows) > 250000:
                raise ValueError('Impact exceeds 250000 name pairs')
            print('GNS_PROGRESS %d %d' % (index + 1, len(entries)), file=sys.stderr, flush=True)
        result[phase] = rows
    result['revision'] = '17fa3f69e1706e902af45a82c57584e884e3e0ab'
    json.dump(result, sys.stdout, separators=(',', ':'))

if __name__ == '__main__':
    main()
