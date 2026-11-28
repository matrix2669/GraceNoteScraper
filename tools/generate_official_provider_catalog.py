#!/usr/bin/env python3
"""Build the optional embedded official-provider compatibility snapshot.

Runtime provider adapters are authoritative for configured-ZIP scans. This
maintainer tool converts reviewed source captures into a small deterministic,
default-off compatibility snapshot with attribution.
"""

import argparse
import json
import re
from html.parser import HTMLParser
from pathlib import Path

import pdfplumber


OPTIMUM_URL = "https://static.tvlistings.optimum.net/ool/static/prod/documents/channel-listings/channel-lineup-IslipWoodbury.pdf"
GLORYSTAR_URL = "https://www.glorystar.tv/channels/"
AFN_URL = "https://media.myafn.dodmedia.osd.mil/bulletins/AFNProgramGuide.pdf"
DIRECTV_URL = "https://www.directv.com/channel-lineup/?tvType=dtv"
VERIZON_URL = "https://www.verizon.com/home/fios-tv/channel-lineup/"
UVERSE_URL = "https://www.att.com/idpassets/pdfs/channel_lineups/Uverse_Channel_Lineup.pdf"
XFINITY_URL = "https://www.xfinity.com/learn/channel-lineup"
SPECTRUM_URL = "https://www.spectrum.com/cable-tv/channel-lineup"
BROADSTAR_URL = "https://www.broadstar.com/wp-content/uploads/2026/04/BDYWN_AmFav.pdf"


class TableParser(HTMLParser):
    def __init__(self):
        super().__init__()
        self.rows = []
        self.row = None
        self.cell = None

    def handle_starttag(self, tag, attrs):
        if tag == "tr":
            self.row = []
        elif tag in {"td", "th"} and self.row is not None:
            self.cell = []

    def handle_data(self, data):
        if self.cell is not None:
            self.cell.append(data)

    def handle_endtag(self, tag):
        if tag in {"td", "th"} and self.cell is not None:
            self.row.append(" ".join("".join(self.cell).split()))
            self.cell = None
        elif tag == "tr" and self.row is not None:
            if self.row:
                self.rows.append(self.row)
            self.row = None


def split_numbers(value):
    values = []
    for part in value.split("/"):
        part = part.strip()
        if part and "-" not in part:
            values.append(part)
    return values


def clean_optimum_name(value):
    value = " ".join(value.split())
    if "beIN Sports 230/1070" in value:
        return "beIN Sports"
    if "Paramount+ 327" in value and "Showtime 2 West" in value:
        return "Showtime 2 West"
    value = re.sub(r"(?:1,\s*8|1,\s*7|1,\s*6,\s*8|1,\s*6,\s*7|2,\s*7|3,\s*7|3,\s*6,\s*7|3,\s*4,\s*7|3,\s*4|6,\s*8|6,\s*7)$", "", value)
    if value != "ESPN2" and re.search(r"[A-Za-z)](?:2|3|6|7|8)$", value):
        value = value[:-1]
    if value.endswith("1") and value not in {"Sports Overflow 1", "FOX Sports 1"}:
        value = value[:-1]
    return value.strip()


def parse_optimum(path):
    entries = []
    with pdfplumber.open(path) as document:
        page = document.pages[0]
        regions = [
            ("Kids", (295, 430, 555, 552)),
            ("Sports", (555, 480, 800, 760)),
            ("Movies", (800, 118, 1060, 680)),
            ("Music", (1060, 118, 1320, 141)),
            ("International", (1060, 148, 1580, 760)),
        ]
        for category, bounds in regions:
            text = page.crop(bounds).extract_text(x_tolerance=1, y_tolerance=3) or ""
            pending = ""
            for raw_line in text.splitlines():
                line = " ".join(raw_line.split()).replace("••", "").strip()
                if not line or "Ch. Packages" in line or line.upper().startswith(("KIDS ", "INTERNATIONAL ")):
                    continue
                if "DEMAND & PPV" in line:
                    break
                matches = list(re.finditer(r"\s(\d+(?:/\d+)*)(?=\s|$)", line))
                if not matches:
                    pending = (pending + " " + line).strip()
                    continue
                match = matches[-1]
                name = clean_optimum_name((pending + " " + line[: match.start()]).strip())
                pending = ""
                numbers = split_numbers(match.group(1))
                if name and numbers:
                    for number in numbers:
                        entries.append({"numbers": [number], "name": name, "category": category})
        for number in range(850, 900):
            entries.append({"numbers": [str(number)], "name": "Stingray Music", "category": "Music"})
        for number in range(520, 534):
            entries.append({"numbers": [str(number)], "name": "Adult Programming", "category": "Adult & PPV"})
    unique = {}
    for entry in entries:
        key = (tuple(entry["numbers"]), entry["name"], entry["category"])
        unique[key] = entry
    return sorted(unique.values(), key=lambda item: (int(item["numbers"][0]), item["name"]))


def parse_glorystar(path):
    parser = TableParser()
    parser.feed(Path(path).read_text(errors="ignore"))
    entries = []
    for row in parser.rows:
        if len(row) < 3 or not re.fullmatch(r"\d+", row[0]):
            continue
        name = row[2].strip()
        if not name or name.lower() in {"available!", "channel name / link"}:
            continue
        entries.append({"numbers": [row[0]], "name": name, "category": "Faith"})
    unique = {(entry["numbers"][0], entry["name"]): entry for entry in entries}
    return sorted(unique.values(), key=lambda item: (int(item["numbers"][0]), item["name"]))


def afn_entries():
    categories = {
        "AFN prime Atlantic": "Entertainment",
        "AFN prime Pacific": "Entertainment",
        "AFN news": "News",
        "AFN sports": "Sports",
        "AFN sports 2": "Sports",
        "AFN spectrum": "Entertainment",
        "AFN movie": "Movies",
        "AFN family": "Kids",
    }
    return [
        {"name": name, "aliases": [name.replace(" ", "|", 1)], "category": category}
        for name, category in categories.items()
    ]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--optimum-pdf", required=True)
    parser.add_argument("--glorystar-html", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()

    payload = {
        "asOf": "2026-08-28",
        "sources": [
            {
                "id": "optimum-islip-woodbury-official",
                "label": "Optimum Islip/Woodbury official lineup",
                "url": OPTIMUM_URL,
                "providers": ["optimum", "cablevision"],
                "postalCodes": ["11743"],
                "method": "exact market-specific Optimum PDF channel number",
                "entries": parse_optimum(args.optimum_pdf),
            },
            {
                "id": "glorystar-official-lineup",
                "label": "Glorystar official lineup",
                "url": GLORYSTAR_URL,
                "providers": ["glorystar"],
                "method": "exact public Glorystar channel number or unique official channel name",
                "entries": parse_glorystar(args.glorystar_html),
            },
            {
                "id": "afn-official-channel-guide",
                "label": "AFN official channel guide",
                "url": AFN_URL,
                "providers": ["afn", "armed forces network"],
                "method": "unique exact AFN network name from the official channel guide",
                "entries": afn_entries(),
            },
            {
                "id": "directv-official-lineup",
                "label": "DIRECTV official lineup",
                "url": DIRECTV_URL,
                "providers": ["directv"],
                "method": "official DIRECTV ZIP/county lineup; normalized adapter pending",
                "status": "registered",
                "message": "The public ZIP/county lineup is registered; a stable structured adapter is still needed.",
                "entries": [],
            },
            {
                "id": "verizon-fios-official-lineup",
                "label": "Verizon FiOS official lineup",
                "url": VERIZON_URL,
                "providers": ["verizon", "fios"],
                "method": "official Verizon location lineup; normalized adapter pending",
                "status": "registered",
                "message": "The recovered FiOS enrichment remains available through exact catalogs; a reusable public-site adapter is still needed.",
                "entries": [],
            },
            {
                "id": "att-uverse-official-lineup",
                "label": "AT&T U-verse official lineup",
                "url": UVERSE_URL,
                "providers": ["u-verse", "uverse", "at&t"],
                "method": "official AT&T PDF; normalized adapter pending",
                "status": "registered",
                "message": "The official PDF is registered; a reviewed normalized snapshot is still needed.",
                "entries": [],
            },
            {
                "id": "xfinity-official-lineup",
                "label": "Xfinity official lineup",
                "url": XFINITY_URL,
                "providers": ["xfinity", "comcast"],
                "method": "official address-based Xfinity lineup; adapter requires ephemeral selected address",
                "status": "address-required",
                "message": "The public source needs a selected service address; no subscriber login will be automated.",
                "entries": [],
            },
            {
                "id": "spectrum-official-lineup",
                "label": "Spectrum official lineup",
                "url": SPECTRUM_URL,
                "providers": ["spectrum", "charter", "time warner cable"],
                "method": "official address-based Spectrum lineup; adapter requires ephemeral selected address",
                "status": "address-required",
                "message": "The public source needs a selected service address; no subscriber login will be automated.",
                "entries": [],
            },
            {
                "id": "broadstar-official-lineup",
                "label": "BroadStar official lineup",
                "url": BROADSTAR_URL,
                "providers": ["broadstar", "broadstream"],
                "method": "official BroadStar PDF; normalized adapter pending",
                "status": "registered",
                "message": "The official PDF and its explicit rename evidence are registered; a complete normalized snapshot is still needed.",
                "entries": [],
            },
        ],
    }
    Path(args.output).write_text(json.dumps(payload, indent=2, ensure_ascii=False) + "\n")


if __name__ == "__main__":
    main()
