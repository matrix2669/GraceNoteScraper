package main

import (
	"strings"

	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"

	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

// reviewDisplayName is presentation only. The draft's canonical name and matcher
// input are never replaced. Current eligible service families outrank historical
// aliases, even when an old alias is longer or has more provider provenance.
func reviewDisplayName(c builder.DraftChannel) string {
	// These descriptors have already passed exact-station/current-call and
	// suppression gates in the immutable builder projection. They are review
	// labels only, never aliases or downstream identity proof.
	for _, e := range c.SupplementalEvidence {
		if e.RootStationID == c.StationID && e.Value != "" {
			return e.Value
		}
	}
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
		if strings.Contains(methods, "historical") || strings.Contains(methods, "position id") || strings.Contains(methods, "channel number plus") || strings.Contains(methods, "event callsign") {
			continue
		}
		external := false
		for _, source := range a.Sources {
			external = external || source != "gracenote" && !strings.HasPrefix(source, "dispatcharr")
		}
		if !external || strings.TrimSpace(a.Value) == "" || strings.EqualFold(a.Value, c.CallSign) {
			continue
		}
		weight := 1
		// Typed exact-ID descriptions outrank untyped provider names; retain stable
		// source order among peers rather than choosing an arbitrary longest alias.
		if strings.Contains(methods, "exact") && strings.Contains(methods, "station id") {
			weight = 3
		}
		for _, e := range c.ServiceEvidence {
			if e.RootStationID == c.StationID && e.Value == a.Value && e.BindingKind == "station-id-catalog-v1" {
				weight = 3
			}
		}
		if weight > score {
			best, score = a.Value, weight
		}
	}
	if best != "" {
		return best
	}
	return c.Name
}

// reviewIdentity is shared by both backend origins, alternatives and history.
// It carries presentation facts only; saved/exported identities remain unchanged.
type reviewIdentity struct {
	TargetFeed        reviewFeed         `json:"targetFeed"`
	StreamFeeds       []reviewStreamFeed `json:"streamFeeds"`
	IdentityConflicts []string           `json:"identityConflicts,omitempty"`
}
type reviewFeed struct {
	Region         string `json:"region"`
	ProvenanceKind string `json:"provenanceKind,omitempty"`
	Reason         string `json:"reason,omitempty"`
}
type reviewStreamFeed struct {
	Name string `json:"name"`
	reviewFeed
}

func presentReviewIdentity(c builder.DraftChannel, names []string, feeds, codes map[string]dispatcharr.FeedIdentity) reviewIdentity {
	convert := func(f dispatcharr.FeedIdentity) reviewFeed {
		region := f.Region
		if region != "east" && region != "west" {
			region = "unknown"
		}
		if strings.Contains(strings.ToLower(f.Evidence), "conflict") || strings.Contains(f.Evidence, "mixed") {
			region = "conflicting"
		}
		kind := "verified"
		if region == "unknown" || region == "conflicting" {
			kind = "unresolved"
		}
		if strings.Contains(f.Evidence, "USER-ATTESTED") {
			kind = "user-attested"
		}
		return reviewFeed{region, kind, f.Evidence}
	}
	result := reviewIdentity{TargetFeed: convert(feeds[c.ID]), StreamFeeds: []reviewStreamFeed{}}
	for _, name := range names {
		result.StreamFeeds = append(result.StreamFeeds, reviewStreamFeed{name, convert(dispatcharr.StreamFeedForTarget(name, codes, feeds[c.ID]))})
	}
	// A descriptor intentionally does not suppress contradictory active evidence.
	for _, e := range c.SupplementalEvidence {
		if e.RootStationID != c.StationID {
			continue
		}
		for _, a := range c.AliasEvidence {
			for _, method := range a.Methods {
				if (strings.Contains(method, "provider-local number") || strings.Contains(method, "provider-local channel number") || strings.Contains(method, "number-policy-provider-alias-v3")) && !strings.EqualFold(a.Value, e.Value) {
					result.IdentityConflicts = appendUniqueReviewString(result.IdentityConflicts, "Provider name conflicts: "+a.Value)
				}
			}
		}
	}
	return result
}
func enrichReviewGroup(g dispatcharr.CandidateGroup, current map[string]builder.DraftChannel, feeds, codes map[string]dispatcharr.FeedIdentity) unifiedReviewItem {
	alternatives := []unifiedReviewItem{}
	for _, a := range g.Alternatives {
		alternatives = append(alternatives, enrichReviewGroup(a, current, feeds, codes))
	}
	g.Alternatives = nil
	return unifiedReviewItem{CandidateGroup: g, reviewIdentity: presentReviewIdentity(current[g.ChannelID], g.StreamNames, feeds, codes), Alternatives: alternatives}
}

type unifiedReviewItem struct {
	dispatcharr.CandidateGroup
	reviewIdentity
	Alternatives []unifiedReviewItem `json:"alternatives,omitempty"`
}

func reviewIdentityCatalog(draft *builder.Draft, config appconfig.Config) (map[string]builder.DraftChannel, map[string]dispatcharr.FeedIdentity, map[string]dispatcharr.FeedIdentity) {
	current := map[string]builder.DraftChannel{}
	for _, c := range draft.Channels {
		current[c.ID] = c
	}
	feeds, codes := stationFeedCatalog(draft, config)
	return current, feeds, codes
}

// Raw variants are presentation evidence. Keep bytes/spelling intact even when
// grouping and consumer decisions deliberately use a normalized literal key.
func appendExactReviewName(values []string, raw string) []string {
	for _, value := range values {
		if value == raw {
			return values
		}
	}
	return append(values, raw)
}
func exactReviewGroupNames(g dispatcharr.CandidateGroup, candidates map[string]dispatcharr.Candidate) dispatcharr.CandidateGroup {
	names := []string{}
	for _, key := range g.CandidateKeys {
		if c, ok := candidates[key]; ok {
			names = appendExactReviewName(names, c.StreamName)
		}
	}
	if len(names) > 0 {
		g.Alias = names[0]
		g.StreamNames = names
	}
	for i := range g.Alternatives {
		g.Alternatives[i] = exactReviewGroupNames(g.Alternatives[i], candidates)
	}
	return g
}
