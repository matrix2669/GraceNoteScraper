package lineuparr

import "strings"

// Represent only already-admitted identity; never mine diagnostic method text
// or activate optional catalogs. Manual aliases remain untouched.
func applyIdentityRepresentations(c *channelWork) {
	original := make([]*aliasWork, 0, len(c.aliases))
	for _, a := range c.aliases {
		original = append(original, a)
	}
	for _, a := range original {
		if a.sources["dispatcharr-confirmed"] || a.sources["manual"] {
			continue
		}
		if strings.HasSuffix(a.value, "+") && len(a.value) > 1 {
			c.addIdentityRepresentation(strings.TrimSuffix(a.value, "+")+" PLUS", firstIdentitySource(a.sources), "representation of admitted terminal plus identity", a.value)
		}
		// Preserve the original evidence spelling. The pinned consumer splits
		// camel-case; an additional all-caps spelling is the same admitted identity.
		if !strings.ContainsAny(a.value, " -()") && strings.ToUpper(a.value) != a.value {
			upper := strings.ToUpper(a.value)
			key := strings.ToLower(a.value) + "\x00uppercase-representation"
			c.aliases[key] = &aliasWork{derivedFrom: []string{a.value}, value: upper, sources: a.sources, methods: map[string]bool{"uppercase representation of admitted identity": true}}
		}
	}

	// A station-bound compound service descriptor can recover its terminal
	// service token when the GN callsign is its exact abbreviation + quality.
	// This does not infer arbitrary suffix meanings or remove feed qualifiers.
	base := strings.ToUpper(strings.TrimSpace(c.input.CallSign))
	for _, quality := range []string{"HD", "SD"} {
		if strings.HasSuffix(base, quality) {
			base = strings.TrimSuffix(base, quality)
			break
		}
	}
	if len(base) < 4 {
		return
	}
	type compound struct {
		last  string
		proof ServiceIdentityEvidence
	}
	compounds := map[string]compound{}
	for _, proof := range c.draft.ServiceEvidence {
		if proof.BindingKind != "station-id-catalog-v1" || proof.RootStationID != c.input.StationID || proof.SourceRowID == "" || proof.SourceRevision == "" {
			continue
		}
		words := strings.Fields(proof.Value)
		if len(words) < 2 {
			continue
		}
		last := words[len(words)-1]
		upper := strings.ToUpper(last)
		if !strings.HasPrefix(upper, base) {
			continue
		}
		key := strings.ToUpper(last)
		previous, exists := compounds[key]
		if !exists || proof.Value+proof.SourceID+proof.SourceRowID < previous.proof.Value+previous.proof.SourceID+previous.proof.SourceRowID {
			compounds[key] = compound{last, proof}
		}
	}
	if len(compounds) != 1 {
		return
	}
	for _, chosen := range compounds {
		last, proof := chosen.last, chosen.proof
		if len(last)-len(base) < 3 || len(base)*2 < len(last) {
			return
		}
		c.addIdentityRepresentation(last, proof.SourceID, "terminal compound service identity from exact station-bound descriptor", proof.Value)
		// A bare abbreviation is unsafe when the verified compound identifies a
		// distinct service. Retain explicit decisions; do not broaden raw aliases.
		for key, alias := range c.aliases {
			if strings.EqualFold(alias.value, base) && !alias.sources["dispatcharr-confirmed"] && !alias.sources["manual"] {
				c.draft.InactiveAliasEvidence = append(c.draft.InactiveAliasEvidence, AliasEvidence{Value: alias.value, Sources: mapKeys(alias.sources), Methods: append(mapKeys(alias.methods), "Inactive abbreviation: exact station descriptor identifies the compound service "+last)})
				delete(c.aliases, key)
			}
		}

	}
}

func firstIdentitySource(s map[string]bool) string {
	keys := mapKeys(s)
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

func (c *channelWork) addIdentityRepresentation(value, source, method, parent string) {
	key := strings.ToLower(cleanText(value))
	if _, exists := c.aliases[key]; exists {
		return
	}
	c.addAlias(value, source, method)
	if a := c.aliases[key]; a != nil {
		a.derivedFrom = []string{parent}
	}
}
