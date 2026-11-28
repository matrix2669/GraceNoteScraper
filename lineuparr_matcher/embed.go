// Package lineuparrmatcher embeds the pinned Python matcher for both native and
// container builds. The subprocess is created only for explicit stream refresh.
package lineuparrmatcher

import "embed"

//go:embed runner.py fuzzy_matcher.py matching_core.py LICENSE
var Files embed.FS

const Revision = "c766a6bb582ca8ef28ae9548d83af1e61f6cde5d"
