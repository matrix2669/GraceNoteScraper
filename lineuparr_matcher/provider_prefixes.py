"""Captured-inventory provider wrappers. Never a persistent/global prefix list.

Only one delimiter-bounded prefix is removed. Discovery is deliberately lossy in
recall, not identity: conservative complete titles corroborate a repeated wrapper;
normalization, fuzzy similarity and the current match's outcome are not evidence.
"""
import re
import time
import unicodedata
from dataclasses import dataclass

PROFILE = "scoped-prefix-v1"
_PREFIX = re.compile(r"^\s*([A-Za-z][A-Za-z0-9&._ -]{1,23}?)\s*[:|]\s*(\S.*?)\s*$")
_QUALITY = re.compile(r"(?:\s+(?:HD|SD|FHD|HEVC|RAW))+$", re.IGNORECASE)
_SEMANTIC = re.compile(r"\b(?:east|west|eastern|western|pacific|coast|feed|plus|hd|sd|fhd|uhd|hdr|4k|8k|hevc|raw)\b", re.IGNORECASE)
_HEADER = re.compile(r"[#=*]{2,}|\|{3,}")


def identity(text):
    """Full identity only: case/Unicode/spacing and terminal presentation quality.

    Keep country, network, subbrand, punctuation, ordinal, feed, callsign and +1.
    Never use the matcher's deliberately lossy normalizer for corroboration.
    """
    return " ".join(_QUALITY.sub("", unicodedata.normalize("NFKC", text).strip()).casefold().split())


@dataclass(frozen=True)
class CandidateView:
    raw: str
    comparison: str
    origin: str
    keys: tuple
    country: str | None


@dataclass(frozen=True)
class OccurrenceMatch:
    key: str
    raw_name: str
    score: int
    match_type: str
    origin: str
    comparison_name: str
    exact_alias: str


@dataclass(frozen=True)
class StreamContext:
    views: tuple
    countries: tuple
    profile: str = PROFILE


def occurrence(stream):
    """Canonical adapter shape; plugin rows and Go bridge share this projection."""
    account = stream.get("accountId", stream.get("m3u_account"))
    group = stream.get("groupId", stream.get("channel_group_id"))
    known = stream.get("groupKnown", group is not None)
    key = stream.get("key")
    if key is None and stream.get("id") is not None:
        key = f"{account}:{stream['id']}"
    if key is None or not str(key):
        raise ValueError("stream occurrence requires a physical key")
    scope = (account, group) if (isinstance(account, int) and not isinstance(account, bool)
        and account > 0 and known is True and isinstance(group, int)
        and not isinstance(group, bool) and group > 0) else None
    return str(key), stream.get("name") or "", scope, stream.get("groupName", stream.get("_group_name")) or ""


def prepare_context(streams, reference_names, detect_country, detect_group_country, callsign):
    if len(streams) > 250000:
        raise ValueError("discovery inventory exceeds 250000 physical streams")
    references = {identity(n) for n in reference_names if isinstance(n, str) and n.strip()}
    semantic_prefixes = set()
    for ref in references:
        words = ref.split()
        semantic_prefixes.update(" ".join(words[:n]) for n in range(1, len(words) + 1) if len(" ".join(words[:n])) <= 24)
    records = {}
    for index, stream in enumerate(streams):
        if index % 500 == 0:
            time.sleep(0)
        key, raw, scope, group_name = occurrence(stream)
        value = (raw, scope, group_name)
        if key in records and records[key] != value:
            raise ValueError("conflicting records for one stream occurrence")
        records[key] = value
    by_name, groups, country_sets = {}, {}, {}
    for key, (raw, scope, group_name) in sorted(records.items()):
        if not raw:
            continue
        by_name.setdefault(raw, []).append(key)
        country = detect_group_country(group_name)
        if country:
            country_sets.setdefault(raw, set()).add(country)
        if scope is not None:
            groups.setdefault(scope, {}).setdefault(raw, []).append(key)
    views = [CandidateView(raw, raw, "original", tuple(keys), None) for raw, keys in sorted(by_name.items())]
    for scope, names in sorted(groups.items()):
        time.sleep(0)
        candidates = {}
        distinct_names = {identity(raw) for raw in names}
        for index, raw in enumerate(names):
            if index % 500 == 0:
                time.sleep(0)
            match = _PREFIX.fullmatch(raw)
            if not match or _HEADER.search(raw):
                continue
            prefix, tail = match.groups()
            prefix = " ".join(prefix.split())
            if len(prefix) < 2:
                continue
            folded = identity(prefix)
            # A known country, station, quality/feed marker or catalogue identity
            # is semantic. Never reinterpret it as presentation-only wrapping.
            if (detect_country(prefix + ": Probe") or callsign(prefix) or _SEMANTIC.search(prefix)
                or folded in semantic_prefixes
                or ":" in tail or "|" in tail or _HEADER.search(tail)):
                continue
            candidates.setdefault(folded, []).append((raw, tail))
        if len(candidates) > 64:
            continue  # bounded ambiguous scope: retain originals, learn nothing
        for rows in candidates.values():
            tails = {identity(tail) for _, tail in rows}
            # At least 5 distinct complete identities; at least 3 independently
            # corroborated titles/aliases. Wrapper dominates this captured group.
            # Duplicate physical streams and quality copies cannot inflate either.
            if len(tails) < 5 or len(tails & references) < 3 or len(tails) * 5 < len(distinct_names) * 4:
                continue
            for raw, tail in sorted(rows):
                keys = tuple(names[raw])
                countries = {detect_group_country(records[k][2]) for k in keys}
                if len(countries) != 1:
                    continue
                views.append(CandidateView(raw, tail, "discovered-prefix", keys, next(iter(countries))))
    countries = tuple(sorted((raw, next(iter(cc))) for raw, cc in country_sets.items() if len(cc) == 1 and None not in cc))
    return StreamContext(tuple(views), countries)
