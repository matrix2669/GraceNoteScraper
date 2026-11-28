# Third-party notices

## Google Maps instructional screenshot

`assets/google-maps-address-help.png` is the operator-supplied annotated screenshot
of Google Maps dated 2026-09-05, cropped to remove the recent-search sidebar.
It illustrates where a person can copy a place's address; it is not a dataset or
an API integration. Google Maps and the displayed imagery/map-data attributions
remain owned by their respective providers; the original visible attribution
is retained. The project's MIT license does not relicense those third-party
materials. Google Maps is not affiliated with this project.

## ledongthuc/pdf

Runtime parsing of public provider PDF guides uses
[`github.com/ledongthuc/pdf`](https://github.com/ledongthuc/pdf), pinned in
`go.mod`. The package is distributed under the BSD 3-Clause License. Its full
license text is included with the module source and remains authoritative.

## razvandimescu/gopdf

Optimum PDFs can place their categorized channel table inside a Form XObject,
which the primary PDF parser does not traverse. The focused Optimum adapter
uses [`github.com/razvandimescu/gopdf`](https://github.com/razvandimescu/gopdf),
pinned in `go.mod`, to recover positioned text recursively from that table.
The package is distributed under the MIT License. Its full license text is
included with the module source and remains authoritative.

## PrismCast channel registry

This compatibility snapshot is disabled by default and is used only when
`LINEUPARR_REFERENCE_CATALOGS=on`.

`lineuparr/network_catalog.json` is a normalized snapshot of station identity,
tag, direct-site, and service-name records from PrismCast commit
`0aed952e80ec6a1bd997a7cb16a3abe256bf253c`:

https://github.com/hjdhjd/prismcast

PrismCast is licensed under the ISC License:

Copyright (c) 2024-2026 HJD (https://github.com/hjdhjd)

Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH
REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY AND
FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT,
INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM
LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR
OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR
PERFORMANCE OF THIS SOFTWARE.

## Stream Link Manager PBS station map

This compatibility snapshot is disabled by default and is used only when
`LINEUPARR_REFERENCE_CATALOGS=on`.

`lineuparr/pbs_catalog.json` is a normalized snapshot of PBS station-to-
Gracenote records from Stream Link Manager for Channels commit
`b651b3e14fb0f8d9b01610cd221b60285b4c2812`:

https://github.com/babsonnexus/stream-link-manager-for-channels

Stream Link Manager for Channels is licensed under the MIT License:

Copyright (c) 2024 Basil Junction Publishing

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

## Official provider source snapshots

These compatibility snapshots are disabled by default and are used only when
`LINEUPARR_REFERENCE_CATALOGS=on`.

`providersource/official_catalog.json` contains normalized factual channel
names, numbers, and provider-defined sections from the public official sources
identified in that file. It does not include the source documents themselves.
Use `tools/generate_official_provider_catalog.py` to rebuild the reviewed
snapshot after downloading fresh copies from those recorded URLs.
