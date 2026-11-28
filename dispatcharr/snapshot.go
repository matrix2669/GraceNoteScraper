package dispatcharr

// FilterSnapshotCandidates applies decisions without rescoring or changing the
// saved pairs. Undo therefore exposes the original scores and alternatives.
func FilterSnapshotCandidates(candidates []Candidate, included map[string]bool, decisions map[string]Decision) []Candidate {
	pairs, aliases := make(map[string]bool), make(map[string]bool)
	for _, d := range decisions {
		if d.Decision != "confirmed" && d.Decision != "denied" {
			continue
		}
		pairs[decisionPairKey(d.StreamHash, d.ChannelID)] = true
		alias := d.NormalizedAlias
		if alias == "" {
			alias = NormalizeAliasName(d.StreamName)
		}
		if alias != "" {
			aliases[aliasDecisionKey(alias, d.ChannelID)] = true
		}
	}
	result := make([]Candidate, 0)
	for _, c := range candidates {
		if included[c.ChannelID] && !pairs[decisionPairKey(c.StreamHash, c.ChannelID)] && !aliases[aliasDecisionKey(c.NormalizedAlias, c.ChannelID)] {
			result = append(result, c)
		}
	}
	sortCandidates(result)
	return result
}
