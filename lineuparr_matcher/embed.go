// Package lineuparrmatcher embeds the pinned Python matcher for both native and
// container builds. The subprocess is created only for explicit stream refresh.
package lineuparrmatcher

import "embed"

//go:embed runner.py impact_runner.py fuzzy_matcher.py provider_prefixes.py matching_core.py aliases.py LICENSE
var Files embed.FS

const Revision = "17fa3f69e1706e902af45a82c57584e884e3e0ab"
