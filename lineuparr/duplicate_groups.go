package lineuparr

import "sort"

// Groups are connected components of verified duplicate edges, never new
// fuzzy/name matches. All provider positions remain available for review.
func duplicateReviewGroups(channels []DraftChannel, suggestions []DuplicateSuggestion) []DuplicateGroup {
	byID := make(map[string]DraftChannel)
	for _, channel := range channels {
		byID[channel.ID] = channel
	}
	edges := make(map[string][]string)
	for _, s := range suggestions {
		edges[s.RemoveID] = append(edges[s.RemoveID], s.KeepID)
		edges[s.KeepID] = append(edges[s.KeepID], s.RemoveID)
	}
	keys := make([]string, 0, len(edges))
	for id := range edges {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	seen := make(map[string]bool)
	groups := []DuplicateGroup{}
	for _, start := range keys {
		if seen[start] {
			continue
		}
		queue := []string{start}
		ids := []string{}
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			if seen[id] {
				continue
			}
			seen[id] = true
			if _, ok := byID[id]; ok {
				ids = append(ids, id)
			}
			queue = append(queue, edges[id]...)
		}
		if len(ids) < 2 {
			continue
		}
		sort.Slice(ids, func(i, j int) bool {
			a, b := byID[ids[i]], byID[ids[j]]
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
		groups = append(groups, DuplicateGroup{ChannelIDs: ids, KeepID: ids[0]})
	}
	return groups
}
