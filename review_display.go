package main

import (
	"strings"

	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

// reviewDisplayName is presentation only. The draft's canonical name and matcher
// input are never replaced. Current eligible service families outrank historical
// aliases, even when an old alias is longer or has more provider provenance.
func reviewDisplayName(c builder.DraftChannel) string {
	family := ""
	for _, e := range c.ServiceEvidence {
		if e.RootStationID != c.StationID || e.ServiceFamily == "" || e.SourceID == "" || e.SourceRowID == "" || e.SourceRevision == "" {
			continue
		}
		active := false
		for _, a := range c.AliasEvidence {
			if a.Value == e.Value {
				for _, source := range a.Sources {
					active = active || source == e.SourceID
				}
			}
		}
		if active {
			if family != "" && !strings.EqualFold(family, e.ServiceFamily) {
				family = ""
				break
			}
			family = e.ServiceFamily
		}
	}
	if family != "" {
		return family
	}
	if c.Name != "" && !strings.EqualFold(c.Name, c.CallSign) && !strings.HasPrefix(c.NameSource, "dispatcharr") {
		return c.Name
	}
	best, score := c.Affiliate, 0
	for _, a := range c.AliasEvidence {
		methods := strings.ToLower(strings.Join(a.Methods, " "))
		if strings.Contains(methods, "historical") || strings.Contains(methods, "station id") || strings.Contains(methods, "position id") || strings.Contains(methods, "channel number plus") || strings.Contains(methods, "event callsign") {
			continue
		}
		external := false
		for _, source := range a.Sources {
			external = external || source != "gracenote" && !strings.HasPrefix(source, "dispatcharr")
		}
		if !external || !strings.Contains(a.Value, " ") || strings.EqualFold(a.Value, c.CallSign) {
			continue
		}
		weight := min(len(a.Value), 40)
		if weight > score {
			best, score = a.Value, weight
		}
	}
	if best != "" {
		return best
	}
	return c.Name
}
