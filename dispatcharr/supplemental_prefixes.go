package dispatcharr

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Discovery is local to the supplemental retrieval pass; it never rewrites
// stream identity or contributes to the pinned consumer's inputs.
type prefixScope struct{ account, group int64 }
type discoveredPrefix struct{ label, residual string }

var providerLabel = regexp.MustCompile(`^([^:|]{2,32})\s*[:|]\s*(.+)$`)
var unsafeLabel = regexp.MustCompile(`(?i)^(?:us|usa|ca|can|uk|gb|au|aus|fr|nl|de|es|it|pt|br|mx|east|west|eastern|western|feed|hd|sd|fhd|uhd|4k|8k|raw|tv|live|sports?|news|movies?|channels?|entertainment|premium|local|national|international|english|spanish|latino|ppv|events?|plus|\+\d+|[kw][a-z]{2,4}(?:-?dt\d*)?)$`)

func splitProviderPrefix(raw string) (string, string, bool) {
	m := providerLabel.FindStringSubmatch(strings.TrimSpace(raw))
	if len(m) != 3 {
		return "", "", false
	}
	label, rest := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
	if unsafeLabel.MatchString(label) || strings.ContainsAny(rest, ":|") || supplementalHeader.MatchString(raw) || supplementalEvent.MatchString(raw) {
		return "", "", false
	}
	// Reject catalogue/header-like labels and labels bearing digits, punctuation,
	// or feed modifiers. These can encode service identities rather than providers.
	for _, r := range label {
		if !unicode.IsLetter(r) && r != ' ' && r != '-' && r != '\'' {
			return "", "", false
		}
	}
	words := strings.Fields(strings.ToLower(label))
	if len(words) == 0 || len(words) > 3 {
		return "", "", false
	}
	for _, w := range words {
		if unsafeLabel.MatchString(w) {
			return "", "", false
		}
	}
	if supplementalText(rest) == "" {
		return "", "", false
	}
	return strings.ToLower(strings.Join(words, " ")), rest, true
}

// Refuse partial evidence rather than infer a prefix from truncated inventory.
const maxPrefixInventory = 200000
const maxPrefixTargets = 50000
const maxPrefixLabels = 200000

func discoverSupplementalPrefixes(targets []SupplementalTarget, streams []IdentityStream) (map[string]discoveredPrefix, bool) {
	result := map[string]discoveredPrefix{}
	if len(streams) > maxPrefixInventory || len(targets) > maxPrefixTargets {
		return result, false
	}
	// A physical fingerprint observed with conflicting scopes is ambiguous.
	scopes := map[string]prefixScope{}
	conflict := map[string]bool{}
	type group struct {
		names  map[string]bool
		labels map[string]map[string]bool
	}
	groups := map[prefixScope]*group{}
	for _, s := range streams {
		if s.Fingerprint == "" {
			continue
		}
		if s.AccountID <= 0 || !s.GroupKnown || s.GroupID <= 0 {
			conflict[s.Fingerprint] = true
			continue
		}
		scope := prefixScope{s.AccountID, s.GroupID}
		if old, ok := scopes[s.Fingerprint]; ok && old != scope {
			conflict[s.Fingerprint] = true
		}
		scopes[s.Fingerprint] = scope
	}
	for _, s := range streams {
		if s.Fingerprint == "" || conflict[s.Fingerprint] || s.AccountID <= 0 || !s.GroupKnown || s.GroupID <= 0 {
			continue
		}
		scope := prefixScope{s.AccountID, s.GroupID}
		g := groups[scope]
		if g == nil {
			g = &group{map[string]bool{}, map[string]map[string]bool{}}
			groups[scope] = g
		}
		name := supplementalText(s.Name)
		if name != "" {
			g.names[name] = true
		}
		label, rest, ok := splitProviderPrefix(s.Name)
		if !ok {
			continue
		}
		if g.labels[label] == nil {
			g.labels[label] = map[string]bool{}
		}
		g.labels[label][supplementalText(rest)] = true
	}
	// Independent target IDs corroborate complete residual identities. Empty target
	// IDs and labels are not evidence. No fuzzy overlap is used for discovery.
	attributed := map[string]map[string]bool{}
	labelCount := 0
	for _, t := range targets {
		labelCount += len(t.Labels)
		if labelCount > maxPrefixLabels {
			return result, false
		}
		if t.ID == "" {
			continue
		}
		for _, l := range t.Labels {
			if strings.TrimSpace(l.Evidence) == "" {
				continue
			}
			term := supplementalText(l.Value)
			if term != "" {
				if attributed[term] == nil {
					attributed[term] = map[string]bool{}
				}
				attributed[term][t.ID] = true
			}
		}
	}
	accepted := map[prefixScope]map[string]bool{}
	for scope, g := range groups {
		for label, services := range g.labels {
			if len(services) < 5 || len(services)*5 < len(g.names)*4 {
				continue
			}
			ids := map[string]bool{}
			corroborated := 0
			for service := range services {
				if len(attributed[service]) > 0 {
					corroborated++
				}
				for id := range attributed[service] {
					ids[id] = true
				}
			}
			if len(ids) < 3 || corroborated < 3 {
				continue
			}
			if accepted[scope] == nil {
				accepted[scope] = map[string]bool{}
			}
			accepted[scope][label] = true
		}
	}
	for _, s := range streams {
		if conflict[s.Fingerprint] || !s.GroupKnown {
			continue
		}
		label, rest, ok := splitProviderPrefix(s.Name)
		if ok && accepted[prefixScope{s.AccountID, s.GroupID}][label] {
			result[s.Fingerprint] = discoveredPrefix{label, rest}
		}
	}
	return result, true
}
func prefixReason(label string) string {
	return fmt.Sprintf("repeated provider prefix %q in this account/group; verify complete raw identity", label)
}
