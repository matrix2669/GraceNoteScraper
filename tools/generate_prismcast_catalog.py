#!/usr/bin/env python3
"""Normalize PrismCast's maintained station-ID catalog for exact-ID matching."""

import argparse
import json
import re
import subprocess
from pathlib import Path


SOURCE_URL = "https://github.com/hjdhjd/prismcast"


def matching_brace(text, start):
    depth = 0
    quote = None
    escaped = False
    line_comment = False
    block_comment = False
    index = start
    while index < len(text):
        char = text[index]
        next_char = text[index + 1] if index + 1 < len(text) else ""
        if line_comment:
            if char == "\n":
                line_comment = False
        elif block_comment:
            if char == "*" and next_char == "/":
                block_comment = False
                index += 1
        elif quote:
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == quote:
                quote = None
        elif char == "/" and next_char == "/":
            line_comment = True
            index += 1
        elif char == "/" and next_char == "*":
            block_comment = True
            index += 1
        elif char in {'"', "'", "`"}:
            quote = char
        elif char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0:
                return index
        index += 1
    raise ValueError("unbalanced source object")


def top_level_definitions(text):
    marker = "const BASE_CHANNEL_DEFINITIONS"
    marker_index = text.index(marker)
    root_start = text.index("{", marker_index)
    root_end = matching_brace(text, root_start)
    body = text[root_start + 1 : root_end]
    definitions = []
    cursor = 0
    pattern = re.compile(r"(?m)^\s*([A-Za-z0-9_]+):\s*\{")
    while True:
        match = pattern.search(body, cursor)
        if not match:
            break
        object_start = body.index("{", match.start())
        object_end = matching_brace(body, object_start)
        definitions.append((match.group(1), body[object_start : object_end + 1]))
        cursor = object_end + 1
    return definitions


def quoted_value(block, field):
    match = re.search(rf"\b{re.escape(field)}:\s*\"([^\"]+)\"", block)
    return match.group(1) if match else ""


def quoted_list(block, field):
    match = re.search(rf"\b{re.escape(field)}:\s*\[([^\]]*)\]", block)
    return re.findall(r'"([^"]+)"', match.group(1)) if match else []


def category_for(tags):
    priority = [
        ("Sports", "Sports"),
        ("News", "News"),
        ("Kids", "Kids"),
        ("Local", "Local"),
        ("Movies", "Movies"),
        ("HBO", "Movies"),
        ("Showtime", "Movies"),
        ("Starz", "Movies"),
        ("Documentary", "Discovery"),
        ("Lifestyle", "Reality & Lifestyle"),
        ("Entertainment", "Entertainment"),
    ]
    for tag, category in priority:
        if tag in tags:
            return category
    return ""


def clean_aliases(name, block):
    aliases = []
    for value in re.findall(r'\bchannelSelector:\s*"([^"]+)"', block):
        value = " ".join(value.split())
        if not value or value == name or len(value) > 80:
            continue
        if value.lower().startswith(("image-", "poster-")):
            continue
        if any(token in value for token in ("_", "#", "[", "]", "{", "}", "//", "xpath", ">")):
            continue
        aliases.append(value)
    return sorted(set(aliases), key=lambda value: value.casefold())


def build(source_path, commit):
    text = Path(source_path).read_text()
    entries = []
    for key, block in top_level_definitions(text):
        name = quoted_value(block, "name")
        station_id = quoted_value(block, "stationId")
        pacific_id = quoted_value(block, "pacificStationId")
        tags = quoted_list(block, "tags")
        if not name:
            continue
        aliases = clean_aliases(name, block)
        category = category_for(tags)
        site_match = re.search(r'\bsite:\s*\{[^{}]*\burl:\s*"([^"]+)"', block)
        url = site_match.group(1) if site_match else ""
        for identifier in (station_id, pacific_id):
            if not identifier:
                continue
            entries.append(
                {
                    "stationId": identifier,
                    "name": name,
                    "aliases": aliases,
                    "category": category,
                    "tags": tags,
                    "url": url,
                    "key": key,
                }
            )
    unique = {entry["stationId"]: entry for entry in entries}
    return {
        "schemaVersion": 1,
        "source": {
            "id": "prismcast-network-catalog",
            "label": "PrismCast network catalog",
            "url": SOURCE_URL,
            "license": "ISC",
            "commit": commit,
            "method": "exact Gracenote station ID from the maintained PrismCast channel registry",
        },
        "channels": [unique[key] for key in sorted(unique, key=lambda value: int(value))],
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    repo = Path(args.repo)
    commit = subprocess.check_output(["git", "-C", str(repo), "rev-parse", "HEAD"], text=True).strip()
    payload = build(repo / "src/channels/index.ts", commit)
    Path(args.output).write_text(json.dumps(payload, indent=2, ensure_ascii=False) + "\n")


if __name__ == "__main__":
    main()
