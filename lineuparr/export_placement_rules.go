package lineuparr

import "strings"

// shareCollapsedPlacementRules uses the same effective stream rules on each
// same-station row that Dispatcharr will address as one channel. Do not share
// evidence across different stations merely because their display name agrees.
func shareCollapsedPlacementRules(channels []DraftChannel) []DraftChannel {
	out := append([]DraftChannel(nil), channels...)
	type key struct{ category, name string }
	groups := make(map[key][]int)
	for i, c := range channels {
		if !c.Included {
			continue
		}
		category := cleanCategory(c.Category)
		if category == "" {
			category = uncategorized
		}
		groups[key{category, c.Name}] = append(groups[key{category, c.Name}], i)
	}
	for _, idx := range groups {
		if len(idx) < 2 {
			continue
		}
		station := strings.TrimSpace(channels[idx[0]].StationID)
		if station == "" {
			continue
		}
		same := true
		for _, i := range idx {
			if strings.TrimSpace(channels[i].StationID) != station {
				same = false
				break
			}
		}
		if !same {
			continue
		}
		canon := func(v string) string { return strings.ToLower(strings.TrimSpace(v)) }
		var allow, deny []string
		denied, allowed := map[string]bool{}, map[string]bool{}
		for _, i := range idx {
			for _, s := range channels[i].ExcludedAliases {
				k := canon(s)
				if k != "" && !denied[k] {
					denied[k] = true
					deny = append(deny, s)
				}
			}
		}
		for _, i := range idx {
			for _, s := range channels[i].Aliases {
				k := canon(s)
				if k != "" && !denied[k] && !allowed[k] {
					allowed[k] = true
					allow = append(allow, s)
				}
			}
		}
		for _, i := range idx {
			out[i].Aliases = allow
			out[i].ExcludedAliases = deny
		}
	}
	return out
}

// orderCollapsedPlacementRows keeps the lowest retained provider number as the
// deterministic default only for positions split from the same provider row.
// An explicitly excluded position is never chosen. Leave other rows' normal
// order alone. LineupARR sync uses the last row's number for the shared target.
func orderCollapsedPlacementRows(categories map[string][]ExportChannel, channels []DraftChannel) {
	type identity struct {
		raw, station string
		count        int
		mixed        bool
	}
	groups := make(map[string]map[string]identity)
	for _, c := range channels {
		if !c.Included {
			continue
		}
		cat := cleanCategory(c.Category)
		if cat == "" {
			cat = uncategorized
		}
		if groups[cat] == nil {
			groups[cat] = make(map[string]identity)
		}
		v := groups[cat][c.Name]
		raw, station := strings.TrimSpace(c.PlacementID), strings.TrimSpace(c.StationID)
		if v.count == 0 {
			v.raw = raw
			v.station = station
		}
		if raw == "" || station == "" || raw != v.raw || station != v.station {
			v.mixed = true
		}
		v.count++
		groups[cat][c.Name] = v
	}
	for cat, rows := range categories {
		type bounds struct{ first, last int }
		pos := make(map[string]bounds)
		for i, row := range rows {
			v := groups[cat][row.Name]
			if v.count < 2 || v.mixed {
				continue
			}
			p, ok := pos[row.Name]
			if !ok {
				p.first = i
			}
			p.last = i
			pos[row.Name] = p
		}
		for _, p := range pos {
			if p.first != p.last {
				rows[p.first], rows[p.last] = rows[p.last], rows[p.first]
			}
		}
	}
}
