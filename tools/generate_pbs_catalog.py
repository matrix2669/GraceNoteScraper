#!/usr/bin/env python3
"""Normalize Stream Link Manager's PBS-to-Gracenote map."""

import argparse
import csv
import json
import subprocess
from pathlib import Path


SOURCE_URL = "https://github.com/babsonnexus/stream-link-manager-for-channels"
PBS_API_URL = "https://station.services.pbs.org/api/public/v1/stations/"


def read_csv(path):
    with Path(path).open(encoding="utf-8-sig", newline="") as source:
        return list(csv.DictReader(source))


def service_identity(channel_id, station):
    call_sign = station.get("call_sign", "").strip()
    original = station.get("original_name", "").strip()
    clean = station.get("clean_name", "").strip()
    if channel_id.endswith("_kids_main"):
        return "PBS Kids", [f"{call_sign} PBS Kids"], "Kids"
    if channel_id.endswith("_ga_create"):
        return "Create TV", [f"{call_sign} Create"], "Food & Travel"
    if channel_id.endswith("_ga_nhk"):
        return "NHK World-Japan", [f"{call_sign} NHK World"], "International"
    if channel_id.endswith("_ga_world"):
        return "WORLD Channel", [f"{call_sign} WORLD"], "Discovery"
    if channel_id.endswith("_ga_fnx"):
        return "FNX", ["First Nations Experience", f"{call_sign} FNX"], "Discovery"
    if "_ga_local_subchannel_" in channel_id:
        return f"{call_sign} PBS subchannel", [], "Local"
    name = original or clean or f"PBS {call_sign}"
    return name, [call_sign, clean, f"PBS {call_sign}"], "Local"


def build(repo):
    stations = {row["channel_id"]: row for row in read_csv(repo / "executables/pbs_stations.csv")}
    entries = {}
    for row in read_csv(repo / "executables/pbs_gracenote_map.csv"):
        channel_id = row["channel_id"].strip()
        base_id = channel_id.split("_ga_", 1)[0].split("_kids_", 1)[0]
        station = stations.get(base_id, {})
        name, aliases, category = service_identity(channel_id, station)
        aliases = sorted({value.strip() for value in aliases if value.strip() and value.strip() != name}, key=str.casefold)
        for field in ("gracenote_id", "gracenote_id_natural"):
            station_id = row.get(field, "").strip()
            if not station_id.isdigit():
                continue
            entries[station_id] = {
                "stationId": station_id,
                "name": name,
                "aliases": aliases,
                "category": category,
                "channelKey": channel_id,
            }
    commit = subprocess.check_output(["git", "-C", str(repo), "rev-parse", "HEAD"], text=True).strip()
    return {
        "schemaVersion": 1,
        "source": {
            "id": "pbs-gracenote-station-map",
            "label": "PBS station map",
            "url": SOURCE_URL,
            "officialApiUrl": PBS_API_URL,
            "license": "MIT",
            "commit": commit,
            "method": "exact Gracenote station ID from the maintained PBS station map",
        },
        "channels": [entries[key] for key in sorted(entries, key=int)],
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    payload = build(Path(args.repo))
    Path(args.output).write_text(json.dumps(payload, indent=2, ensure_ascii=False) + "\n")


if __name__ == "__main__":
    main()
