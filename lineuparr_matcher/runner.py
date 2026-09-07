#!/usr/bin/env python3
"""One-shot, stdin/stdout wrapper around Lineuparr's canonical matcher."""

import json
import logging
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


def main():
    request = json.load(sys.stdin)
    matcher = FuzzyMatcher(match_threshold=70, logger=logging.getLogger("gracenote.lineuparr"))
    matcher.logger.setLevel(logging.WARNING)
    streams = request.get("streams") or []
    names = list(dict.fromkeys(stream["name"] for stream in streams if stream.get("name")))
    matcher.precompute_normalizations(names)
    by_name = {}
    for stream in streams:
        by_name.setdefault(stream.get("name"), []).append(stream)
    matches = []
    channels = request.get("channels") or []
    for index, channel in enumerate(channels):
        name = channel.get("name", "")
        aliases = {name: channel.get("aliases") or []}
        for matched_name, score, match_type in matcher.match_all_streams(
            name, names, aliases, channel_number(channel.get("number")), lineup_country=request.get("country")
        ):
            for stream in by_name.get(matched_name, []):
                matches.append({
                    "channelId": channel["id"], "streamKey": stream["key"],
                    "score": score, "reason": "Lineuparr " + match_type,
                })
                if len(matches) > 250000:
                    raise ValueError("Match result exceeds 250000 pairs")
        print("GNS_PROGRESS %d %d" % (index + 1, len(channels)), file=sys.stderr, flush=True)
    json.dump({"matches": matches, "matcherVersion": "c766a6bb582ca8ef28ae9548d83af1e61f6cde5d"}, sys.stdout, separators=(",", ":"))


if __name__ == "__main__":
    main()
