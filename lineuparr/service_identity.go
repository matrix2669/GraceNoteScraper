package lineuparr

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

func reviewedKeep(c DraftChannel) bool { return c.InclusionChoice != nil && *c.InclusionChoice }
func serviceKey(v string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(cleanText(v)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	k := b.String()
	if k == "" || isDigits(k) {
		return ""
	}
	switch k {
	case "seasonalprogramming", "programming", "tba", "tbd", "offair", "channel", "network":
		return ""
	}
	return k
}

var timeShiftQualifier = regexp.MustCompile(`\+\s*([0-9]+)\b`)

func serviceQualifier(c DraftChannel) string {
	value := strings.ToLower(timeShiftQualifier.ReplaceAllString(c.CallSign+" "+c.OriginalName, ""))
	for _, q := range []string{"east", "west", "pacific", "mountain", "central", "timeshift", "+1", "+2"} {
		for _, word := range strings.FieldsFunc(value, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '+' }) {
			if word == q {
				return q
			}
		}
	}
	for _, suffix := range []string{"hdny", "hdla", "hdne", "hdnw", "hdse", "hdsw"} {
		if strings.HasSuffix(strings.ToLower(timeShiftQualifier.ReplaceAllString(c.CallSign, "")), suffix) {
			return suffix[2:]
		}
	}
	return ""
}
func serviceCompatible(a, b DraftChannel) bool {
	if looksLikeNumberedDigitalSubchannel(a.CallSign) || looksLikeNumberedDigitalSubchannel(b.CallSign) {
		return a.StationID != "" && a.StationID == b.StationID && serviceKey(a.CallSign) == serviceKey(b.CallSign)
	}
	shift := func(c DraftChannel) string {
		m := timeShiftQualifier.FindStringSubmatch(c.CallSign + " " + c.OriginalName)
		if len(m) > 1 {
			return m[1]
		}
		return ""
	}
	if shift(a) != shift(b) {
		return false
	}
	x, y := serviceQualifier(a), serviceQualifier(b)
	return x == "" || y == "" || x == y
}
func safeIdentityProof(c DraftChannel, p ServiceIdentityEvidence) bool {
	if serviceKey(p.Value) == "" || p.SourceID == "" || (p.Policy != "provider-identity-v1" && p.Policy != "provider-evidence-quarantine-v1-reevaluated") {
		return false
	}
	// Explicit suppression remains authoritative for grouping as well as export.
	for _, s := range c.SuppressedAliasEvidence {
		if serviceKey(s.Value) == serviceKey(p.Value) {
			return false
		}
	}
	return p.PeerStationID != "" || strings.HasSuffix(p.SourceID, "-official-lineup") && p.SourceRowID != "" && p.RootStationID == c.StationID && p.RootLineupKey != ""
}

// Group all verified positions before choosing a review reference. An alias
// alone is not a transitive union rule: at most two distinct station identities
// may share an unpaired alias, and every proposed union checks all qualifiers.
func verifiedServiceSuggestions(channels []DraftChannel) []DuplicateSuggestion {
	parent := make([]int, len(channels))
	for i := range parent {
		parent[i] = i
	}
	var root func(int) int
	root = func(i int) int {
		for parent[i] != i {
			i = parent[i]
		}
		return i
	}
	join := func(a, b int) bool {
		x, y := root(a), root(b)
		if x == y {
			return true
		}
		for i := range channels {
			if root(i) != x {
				continue
			}
			for j := range channels {
				if root(j) == y && !serviceCompatible(channels[i], channels[j]) {
					return false
				}
			}
		}
		parent[y] = x
		return true
	}
	exact := map[string]int{}
	for i, c := range channels {
		if c.StationID == "" || serviceKey(c.CallSign) == "" {
			continue
		}
		k := c.StationID + "\x00" + serviceKey(c.CallSign)
		if j, ok := exact[k]; ok {
			join(i, j)
		} else {
			exact[k] = i
		}
	}
	// Raw callsigns differing only by a terminal quality marker establish service
	// identity directly, including short brands. Preserve regional/subchannel guards.
	suffixes := map[string][]int{}
	hasSuffix := map[string]bool{}
	for i, c := range channels {
		key := serviceKey(c.CallSign)
		if key == "" {
			continue
		}
		base := key
		marked := false
		for _, suffix := range []string{"hd", "sd", "dt"} {
			if strings.HasSuffix(key, suffix) && len(key) > len(suffix) {
				base = strings.TrimSuffix(key, suffix)
				marked = true
				break
			}
		}
		if len(base) < 3 {
			continue
		}
		suffixes[base] = append(suffixes[base], i)
		hasSuffix[base] = hasSuffix[base] || marked
	}
	for base, ids := range suffixes {
		if !hasSuffix[base] {
			continue
		}
		stations := map[string]bool{}
		compatible := true
		for _, i := range ids {
			station := channels[i].StationID
			if station == "" {
				station = channels[i].ID
			}
			stations[station] = true
			for _, j := range ids {
				if !serviceCompatible(channels[i], channels[j]) {
					compatible = false
				}
			}
		}
		if !compatible || len(stations) > 2 || rawSuffixAmbiguous(channels, ids) {
			continue
		}
		for _, i := range ids {
			join(ids[0], i)
		}
	}
	aliases := map[string]map[int]bool{}
	for i, c := range channels {
		for _, p := range c.ServiceEvidence {
			if !safeIdentityProof(c, p) || p.PeerStationID != "" || !strings.HasSuffix(p.SourceID, "-official-lineup") {
				continue
			}
			k := serviceKey(p.Value)
			if aliases[k] == nil {
				aliases[k] = map[int]bool{}
			}
			aliases[k][i] = true
		}
	}
	keys := make([]string, 0, len(aliases))
	for k := range aliases {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// A position cannot use two different aliases to bridge competing services.
	peers := map[string]map[string]bool{}
	for _, members := range aliases {
		stations := map[string]bool{}
		for i := range members {
			if channels[i].StationID != "" {
				stations[channels[i].StationID] = true
			}
		}
		if len(stations) != 2 {
			continue
		}
		for a := range stations {
			for b := range stations {
				if a == b {
					continue
				}
				if peers[a] == nil {
					peers[a] = map[string]bool{}
				}
				peers[a][b] = true
			}
		}
	}
	for _, k := range keys {
		members := aliases[k]
		ids := []int{}
		stations := map[string]bool{}
		for i := range members {
			if channels[i].StationID != "" {
				ids = append(ids, i)
				stations[channels[i].StationID] = true
			}
		}
		sort.Ints(ids)
		if len(stations) != 2 {
			continue
		}
		ambiguous := false
		for station := range stations {
			if len(peers[station]) > 1 {
				ambiguous = true
			}
		}
		if ambiguous {
			continue
		}
		combined := map[string]bool{}
		for j, c := range channels {
			for _, i := range ids {
				if root(j) == root(i) && c.StationID != "" {
					combined[c.StationID] = true
				}
			}
		}
		if len(combined) > 2 {
			continue
		}
		// Reject the entire proposed alias relationship if any region conflicts;
		// never connect an unqualified middle position to two different regions.
		compatible := true
		for _, i := range ids {
			for _, j := range ids {
				if !serviceCompatible(channels[i], channels[j]) {
					compatible = false
				}
			}
		}
		if !compatible {
			continue
		}
		for _, i := range ids {
			join(ids[0], i)
		}
	}
	// Validate the whole provisional typed-pair graph before accepting any
	// connection. Conflicting regions cannot choose a partial winner by sort order.
	provisional := append([]int(nil), parent...)
	var pr func(int) int
	pr = func(i int) int {
		for provisional[i] != i {
			i = provisional[i]
		}
		return i
	}
	pairEdges := [][2]int{}
	for i, c := range channels {
		for _, p := range c.ServiceEvidence {
			if !safeIdentityProof(c, p) || p.PeerStationID == "" {
				continue
			}
			for j, peer := range channels {
				if peer.StationID == p.PeerStationID {
					pairEdges = append(pairEdges, [2]int{i, j})
					provisional[pr(j)] = pr(i)
				}
			}
		}
	}
	blocked := map[int]bool{}
	for i := range channels {
		for j := i + 1; j < len(channels); j++ {
			if pr(i) == pr(j) && !serviceCompatible(channels[i], channels[j]) {
				blocked[pr(i)] = true
			}
		}
	}
	for _, e := range pairEdges {
		if !blocked[pr(e[0])] {
			join(e[0], e[1])
		}
	}
	// Preserve exact-station repeats inside a disputed component, but withhold
	// every cross-station relationship rather than retaining an arbitrary side.
	for i := range channels {
		if blocked[pr(i)] {
			parent[i] = i
		}
	}
	for i, c := range channels {
		if !blocked[pr(i)] || c.StationID == "" {
			continue
		}
		for j := 0; j < i; j++ {
			peer := channels[j]
			if c.StationID == peer.StationID && serviceKey(c.CallSign) == serviceKey(peer.CallSign) {
				join(i, j)
			}
		}
	}

	groups := map[int][]int{}
	for i := range channels {
		groups[root(i)] = append(groups[root(i)], i)
	}
	result := []DuplicateSuggestion{}
	for _, ids := range groups {
		if len(ids) < 2 {
			continue
		}
		sort.Slice(ids, func(i, j int) bool {
			a, b := channels[ids[i]], channels[ids[j]]
			if reviewedKeep(a) != reviewedKeep(b) {
				return reviewedKeep(a)
			}
			if a.Included != b.Included {
				return a.Included
			}
			if qualityRank(a) != qualityRank(b) {
				return qualityRank(a) > qualityRank(b)
			}
			if a.Number != b.Number {
				return numberLess(a.Number, b.Number)
			}
			return a.ID < b.ID
		})
		keep := channels[ids[0]]
		for _, i := range ids[1:] {
			c := channels[i]
			result = append(result, DuplicateSuggestion{Exact: c.StationID != "" && c.StationID == keep.StationID || qualityRank(c) == qualityRank(keep), RemoveID: c.ID, RemoveNumber: c.Number, RemoveName: c.Name, KeepID: keep.ID, KeepNumber: keep.Number, KeepName: keep.Name, Reason: "Verified same service (HD/SD/DT quality suffix or corroborated identity); quality affects only the default review keeper"})
		}
	}
	return result
}

// Unattested aliases never create an edge; competing attributable aliases can
// still make a raw suffix proposal ambiguous and keep it out of cleanup.
func rawSuffixAmbiguous(channels []DraftChannel, ids []int) bool {
	members := map[int]bool{}
	for _, i := range ids {
		members[i] = true
	}
	aliases := map[string]bool{}
	for _, i := range ids {
		for _, e := range channels[i].AliasEvidence {
			for _, source := range e.Sources {
				if source != "gracenote" {
					aliases[serviceKey(e.Value)] = true
				}
			}
		}
	}
	for j, c := range channels {
		if members[j] {
			continue
		}
		for _, e := range c.AliasEvidence {
			if key := serviceKey(e.Value); key != "" && aliases[key] {
				for _, source := range e.Sources {
					if source != "gracenote" {
						return true
					}
				}
			}
		}
	}
	return false
}
