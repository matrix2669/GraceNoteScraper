package dispatcharr

import (
	"regexp"
	"strings"
)

// AffiliateTarget is current-lineup identity, never an edited display name or
// an inferred network affiliation. CallSign must come from the raw station row.
type AffiliateTarget struct {
	Country  string // Canonical ISO alpha-2, e.g. US; never raw appconfig USA.
	CallSign string
}

// AffiliateExclusionSuggestion is a review proposal, not an applied decision.
// Keeping the candidate intact preserves account, raw-name and source identity.
type AffiliateExclusionSuggestion struct {
	Candidate      Candidate
	TargetCallSign string
	StreamCallSign string
}

type AffiliateExclusionPlan struct {
	Suggestions []AffiliateExclusionSuggestion
	Manual      []Candidate
}

var affiliateCall = regexp.MustCompile(`^([KW][A-Z]{2,3})(?:-?(?:DT|TV|HD|SD))?$`)
var affiliateToken = regexp.MustCompile(`[A-Za-z0-9]+(?:[.-][A-Za-z0-9]+)*`)
var explicitAffiliate = regexp.MustCompile(`\(([A-Za-z0-9.-]+)\)|\[([A-Za-z0-9.-]+)\]`)
var affiliateBoundary = regexp.MustCompile(`(?i)\b(EAST|WEST|PACIFIC|MOUNTAIN|CENTRAL|ATLANTIC|NORTH|SOUTH|REGIONAL|ALTERNATE|ALT|PLUS|DELAY)\b`)

// Common words cannot establish station identity just because they have the
// lexical shape of a callsign. This intentionally withholds real stations with
// colliding names until a stronger station catalogue is available.
const affiliateWords = " WWE WWF WCW WAR WARS WARM WASH WATCH WAVE WAVES WAY WAYS WEB WEEK WELL WENT WERE WEST WHAT WHEN WHERE WHICH WHILE WHITE WHO WHY WIDE WIFE WILD WILL WIND WINE WING WINGS WINS WIRE WISE WISH WITH WOLF WOMAN WOMEN WOOD WORD WORDS WORK WORKS WORLD WORM WORN WRAP KEEN KEEP KEPT KEY KEYS KICK KID KIDS KILL KIND KING KINGS KISS KITE KNEE KNEW KNOW KNOWN "

func affiliateIdentity(raw string) string {
	m := affiliateCall.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(raw)))
	if len(m) == 0 || strings.Contains(affiliateWords, " "+m[1]+" ") {
		return ""
	}
	return m[1]
}

// explicitStreamAffiliate requires an attributable bracketed callsign or a
// whole-name callsign. Loose words never prove that a stream is an affiliate.
// Multiple mentions (even equivalent mentions) are deliberately manual review.
func explicitStreamAffiliate(raw string) string {
	if affiliateBoundary.MatchString(raw) {
		return ""
	}
	if identity := affiliateIdentity(raw); identity != "" {
		return identity
	}
	mentions := []string{}
	for _, token := range affiliateToken.FindAllString(raw, -1) {
		if affiliateIdentity(token) != "" {
			mentions = append(mentions, token)
		}
		// Numbered suffixes / subchannels are not interchangeable primary feeds.
		upper := strings.ToUpper(token)
		if strings.HasPrefix(upper, "K") || strings.HasPrefix(upper, "W") {
			if strings.ContainsAny(upper, "0123456789") {
				return ""
			}
		}
	}
	if len(mentions) != 1 {
		return ""
	}
	matches := explicitAffiliate.FindAllStringSubmatch(raw, -1)
	identity := ""
	for _, m := range matches {
		token := m[1]
		if token == "" {
			token = m[2]
		}
		if id := affiliateIdentity(token); id != "" {
			if identity != "" {
				return ""
			}
			identity = id
		}
	}
	return identity
}

// PlanAffiliateExclusions is pure and runs only over existing current review
// candidates. It neither calls the matcher nor changes aliases or decisions.
// Callers must revalidate snapshot/source generation before applying selected
// suggestions through the existing Deny path. Scores below 95 stay manual per
// ADR024; explicit confirmed/denied group decisions always take precedence.
func PlanAffiliateExclusions(candidates []Candidate, targets map[string]AffiliateTarget, decisions []Decision) AffiliateExclusionPlan {
	plan := AffiliateExclusionPlan{}
	for _, candidate := range candidates {
		target, ok := targets[candidate.ChannelID]
		targetCall := affiliateIdentity(target.CallSign)
		streamCall := explicitStreamAffiliate(candidate.StreamName)
		reviewed := false
		for _, decision := range decisions {
			alias := decision.NormalizedAlias
			if alias == "" {
				alias = NormalizeAliasName(decision.StreamName)
			}
			if decision.ChannelID == candidate.ChannelID && decision.Source == candidate.Source &&
				((decision.StreamHash != "" && decision.StreamHash == candidate.StreamHash) || (alias != "" && alias == candidate.NormalizedAlias)) &&
				(decision.Decision == "confirmed" || decision.Decision == "denied") {
				reviewed = true
				break
			}
		}
		if candidate.Key == "" || candidate.Source == "" || candidate.StreamHash == "" || !ok || target.Country != "US" || targetCall == "" || streamCall == "" || streamCall == targetCall || candidate.NameScore < 95 || reviewed {
			plan.Manual = append(plan.Manual, candidate)
			continue
		}
		plan.Suggestions = append(plan.Suggestions, AffiliateExclusionSuggestion{Candidate: candidate, TargetCallSign: targetCall, StreamCallSign: streamCall})
	}
	// Existing review and Undo operate on alias groups. A mixed group must
	// stay manual as a whole rather than silently deciding its uncertain siblings.
	manualGroups := map[string]bool{}
	for _, c := range plan.Manual {
		manualGroups[c.ChannelID+"\x00"+c.NormalizedAlias] = true
	}
	safe := plan.Suggestions[:0]
	for _, suggestion := range plan.Suggestions {
		c := suggestion.Candidate
		if manualGroups[c.ChannelID+"\x00"+c.NormalizedAlias] {
			plan.Manual = append(plan.Manual, c)
		} else {
			safe = append(safe, suggestion)
		}
	}
	plan.Suggestions = safe
	return plan
}
