#!/usr/bin/env python3
"""One-shot, stdin/stdout wrapper around Lineuparr's canonical matcher."""

import json
import logging
import re
import sys
from pathlib import Path

# Isolated Python (-I) excludes the script directory from the import path.
sys.path.insert(0, str(Path(__file__).resolve().parent))

from fuzzy_matcher import FuzzyMatcher


def channel_number(raw):
    # Same parsing as Lineuparr Plugin._parse_channel_number.
    if raw is None:
        return None
    value = str(raw).strip()
    if not value:
        return None
    if "-" in value and value.split("-", 1)[0].strip().isdigit():
        return int(value.split("-", 1)[0].strip())
    return int(value) if value.isdigit() else raw


def exact_alias_lookup(matcher, aliases):
    """Mirror only alias_match's exact lookup insertion/overwrite order.

    This is normalization membership evidence, never fuzzy-winner attribution.
    The consumer's scoring and final filtering remain the sole match authority.
    """
    lookup = {}
    for alias in aliases:
        norm = matcher.normalize_name(alias, [])
        if norm:
            lower = norm.lower()
            lookup[lower] = alias
            nospace = re.sub(r'[\s&\-]+', '', lower)
            if nospace != lower:
                lookup[nospace] = alias
    return lookup


def exact_alias_evidence(matcher, lookup, name, match_type):
    if match_type != "alias":
        return ""
    lower, nospace = matcher._get_cached_norm(name, [])
    if not lower:
        return ""
    # The consumer tests the spaced key first, then its no-space form.
    return lookup.get(lower, lookup.get(nospace, ""))


def main():
    request = json.load(sys.stdin)
    matcher = FuzzyMatcher(match_threshold=70, logger=logging.getLogger("gracenote.lineuparr"))
    matcher.logger.setLevel(logging.WARNING)
    streams = request.get("streams") or []
    names = list(dict.fromkeys(stream["name"] for stream in streams if stream.get("name")))
    matcher.precompute_normalizations(names)
    context = matcher.prepare_stream_context(streams,
        [title for channel in (request.get("channels") or [])
         for title in [channel.get("name", ""), *(channel.get("aliases") or [])]])
    matches = []
    channels = request.get("channels") or []
    for index, channel in enumerate(channels):
        name = channel.get("name", "")
        aliases = {name: channel.get("aliases") or []}
        for match in matcher.match_stream_occurrences(
            name, context, aliases, channel_number(channel.get("number")),
            lineup_country=request.get("country"), candidate_countries={}
        ):
            matches.append({
                "channelId": channel["id"], "streamKey": match.key,
                "score": match.score, "reason": "Lineuparr " + match.match_type,
                "exactAlias": match.exact_alias,
                "comparisonName": match.comparison_name if match.origin != "original" else "",
            })
            if len(matches) > 250000:
                raise ValueError("Match result exceeds 250000 pairs")
        print("GNS_PROGRESS %d %d" % (index + 1, len(channels)), file=sys.stderr, flush=True)
    json.dump({"matches": matches, "comparisonNames": {key: v.comparison for v in context.views if v.origin != "original" for key in v.keys}, "matcherVersion": "17fa3f69e1706e902af45a82c57584e884e3e0ab"}, sys.stdout, separators=(",", ":"))


if __name__ == "__main__":
    main()
