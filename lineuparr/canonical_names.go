package lineuparr

import (
	"sort"
	"strings"
)

// resolveCanonicalNames admits only catalog row descriptors that survived the
// exact-station owner census and the user's alias suppression. Existing explicit
// rename rules take precedence. A name change never changes a channel's identity.
func resolveCanonicalNames(result []DraftChannel, work []*channelWork) {
	byID := make(map[string]*channelWork, len(work))
	for _, w := range work {
		byID[w.draft.ID] = w
	}
	old := make([]string, len(result))
	changed := make([]bool, len(result))
	for i := range result {
		c := &result[i]
		old[i] = c.Name
		w := byID[c.ID]
		if w == nil || w.input.PreferredName != nil {
			continue
		}
		options := map[string][]AttributedAlias{}
		for _, a := range w.input.CanonicalNameCandidates {
			p := a.Identity
			if p == nil || p.RootStationID == "" || p.RootStationID != c.StationID || p.SourceID != a.Source || p.SourceRowID == "" || p.SourceRevision == "" || !canonicalProofAllowed(p) {
				continue
			}
			if !containsFold(c.Aliases, a.Value) || containsSuppressedName(c.SuppressedAliasEvidence, a.Value) {
				continue
			}
			key := strings.ToLower(strings.Join(strings.Fields(a.Value), " "))
			if key != "" {
				options[key] = append(options[key], a)
			}
		}
		if len(options) > 1 {
			c.NameDiagnostic = "Conflicting exact-station descriptors; retained existing name"
			continue
		}
		for _, candidates := range options {
			sort.Slice(candidates, func(i, j int) bool {
				a, b := candidates[i], candidates[j]
				return a.Source+"\x00"+a.Identity.SourceRevision+"\x00"+a.Identity.SourceRowID+"\x00"+a.Value < b.Source+"\x00"+b.Identity.SourceRevision+"\x00"+b.Identity.SourceRowID+"\x00"+b.Value
			})
			a := candidates[0]
			// A terminal feed annotation in the exact row denotes the default
			// East feed; it is not part of the primary service display name.
			name := defaultEastName(a.Value)
			if name == c.Name {
				continue
			}
			c.Name = name
			c.NameSource = a.Source
			c.NameMethod = a.Method
			if name != a.Value {
				c.NameMethod += "; default East feed annotation omitted"
			}
			changed[i] = true
		}
	}
	// Downstream identifies a channel by literal name and group. Reject new
	// collisions case-insensitively and with collapsed whitespace too. Re-run
	// after fallback because restoring an old name can expose another collision.
	for {
		owners := map[string][]int{}
		for i, c := range result {
			if c.Included {
				key := canonicalCollisionKey(c.Category, c.Name)
				owners[key] = append(owners[key], i)
			}
		}
		rejected := false
		for _, ids := range owners {
			if len(ids) < 2 {
				continue
			}
			for _, i := range ids {
				if changed[i] {
					result[i].Name = old[i]
					result[i].NameSource = byID[result[i].ID].draft.NameSource
					result[i].NameMethod = byID[result[i].ID].draft.NameMethod
					result[i].NameDiagnostic = "Canonical name would collide with another included name in this category; retained existing name"
					changed[i] = false
					rejected = true
				}
			}
		}
		if !rejected {
			for _, ids := range owners {
				if len(ids) > 1 {
					for _, i := range ids {
						if result[i].NameDiagnostic == "" {
							result[i].NameDiagnostic = "Included name and category collide with another channel"
						}
					}
				}
			}
			break
		}
	}
}
func canonicalCollisionKey(category, name string) string {
	category = cleanCategory(category)
	if category == "" {
		category = uncategorized
	}
	return strings.ToLower(strings.Join(strings.Fields(category), " ")) + "\x00" + strings.ToLower(strings.Join(strings.Fields(name), " "))
}
func containsSuppressedName(values []AliasEvidence, name string) bool {
	for _, a := range values {
		if strings.EqualFold(strings.TrimSpace(a.Value), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

func canonicalProofAllowed(p *ServiceIdentityEvidence) bool {
	return (p.BindingKind == "station-id-catalog-v1" && p.Policy == "verified-portable-station-alias-v1") || (p.BindingKind == "user-attested-station-v1" && p.Policy == "user-attested-rename-v1")
}

// Strip only an explicit terminal source-row feed annotation, never a word
// inside a service brand or a West/Pacific, language or quality qualifier.
func defaultEastName(name string) string {
	value := strings.TrimSpace(name)
	for _, suffix := range []string{" - East", " (East)"} {
		if len(value) > len(suffix) && strings.EqualFold(value[len(value)-len(suffix):], suffix) {
			return strings.TrimSpace(value[:len(value)-len(suffix)])
		}
	}
	return value
}
