package dispatcharr

import "strings"

// FilterSnapshotCandidates applies decisions without rescoring or changing the
// saved pairs. Undo therefore exposes the original scores and alternatives.
func FilterSnapshotCandidates(candidates []Candidate, included map[string]bool, decisions map[string]Decision) []Candidate {
	pairs, rawNames := make(map[string]bool), make(map[string]bool)
	for _, d := range decisions {
		if d.Decision != "confirmed" && d.Decision != "denied" {
			continue
		}
		if d.StreamHash != "" {
			pairs[decisionPairKey(d.StreamHash, d.ChannelID)] = true
		}
		if d.StreamName != "" {
			// Match the consumer's literal-name equality, never its lossy
			// positive-alias normalizer. New raw variants need their own review.
			rawNames[decisionPairKey(literalReviewName(d.StreamName), d.ChannelID)] = true
		}
	}
	result := make([]Candidate, 0)
	for _, c := range candidates {
		if included[c.ChannelID] && !pairs[decisionPairKey(c.StreamHash, c.ChannelID)] && !rawNames[decisionPairKey(literalReviewName(c.StreamName), c.ChannelID)] {
			result = append(result, c)
		}
	}
	sortCandidates(result)
	return result
}

func literalReviewName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}
