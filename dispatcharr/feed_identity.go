package dispatcharr

import (
	"regexp"
	"strings"
)

// FeedIdentity is station-bound evidence, not a suffix guessed from a brand.
// Family retains every network/subbrand token; quality is not a feed region.
type FeedIdentity struct {
	FamilyAliases []string // Independently attributed aliases of the same exact station/source row.
	Family        string
	Region        string
	Evidence      string
}

var feedQuality = regexp.MustCompile(`(?i)(?:[\s_-]+(?:HD|SD|FHD|UHD|4K|8K|RAW))+$`)
var feedSuffix = regexp.MustCompile(`(?i)^(.*?)(?:\s*[-|:]\s*|\s*\(\s*|\s+)(EAST|WEST|PACIFIC)(?:\s+(?:COAST|FEED))?\s*\)?$`)
var feedPrefix = regexp.MustCompile(`(?i)^(?:US|USA|CA|CAN|UK|GB|AU|AUS|TV|PRIME|RK)\s*[:|]\s*`)
var familyPunctuation = regexp.MustCompile(`[^a-z0-9]+`)
var feedToken = regexp.MustCompile(`[A-Za-z0-9]+`)

// Decode only known presentation-only quality glyphs, never network/feed letters.
func streamQualityText(raw string) string {
	return strings.NewReplacer("ᴿᴬᵂ", " RAW", "ᴴᴰ", " HD", "ˢᴰ", " SD", "ᶠᴴᴰ", " FHD").Replace(raw)
}

func feedFamily(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = feedQuality.ReplaceAllString(raw, "")
	return strings.TrimSpace(familyPunctuation.ReplaceAllString(strings.ToLower(raw), " "))
}

// ParseStationFeedLabel requires an explicit qualifier, e.g. "Network - West"
// or "Network (Pacific)". A brand such as "Fox Sports West" is not sufficient.
func ParseStationFeedLabel(raw string) FeedIdentity {
	clean := feedQuality.ReplaceAllString(strings.TrimSpace(raw), "")
	m := feedSuffix.FindStringSubmatch(clean)
	if len(m) == 0 {
		return FeedIdentity{}
	}
	prefix := strings.TrimSpace(m[1])
	end := strings.TrimSpace(clean[len(m[1]):])
	qualified := strings.ContainsAny(end, "-|:(") || strings.Contains(strings.ToUpper(end), "FEED") || strings.Contains(strings.ToUpper(end), "COAST") || strings.EqualFold(m[2], "PACIFIC")
	if !qualified || prefix == "" {
		return FeedIdentity{}
	}
	region := strings.ToLower(m[2])
	if region == "pacific" {
		region = "west"
	}
	return FeedIdentity{Family: feedFamily(prefix), Region: region, Evidence: raw}
}

// ParseFeedHint recognizes an unqualified regional word only as uncertainty.
// It never establishes a time-shift identity (e.g. a regional brand may end West).
func ParseFeedHint(raw string) FeedIdentity {
	clean := feedQuality.ReplaceAllString(strings.TrimSpace(raw), "")
	m := feedSuffix.FindStringSubmatch(clean)
	if len(m) == 0 || strings.TrimSpace(m[1]) == "" {
		return FeedIdentity{}
	}
	return FeedIdentity{Family: feedFamily(m[1]), Region: "unknown", Evidence: "Regional label requires an explicit station-bound feed assertion"}
}

// StreamFeed combines explicit labels with exact station codes learned from
// independently attributed current-lineup feed facts. Codes are never generated
// by stripping arbitrary P/H/HD suffixes. Mixed evidence stays unresolved.
func StreamFeed(raw string, codes map[string]FeedIdentity) FeedIdentity {
	clean := feedPrefix.ReplaceAllString(strings.TrimSpace(raw), "")
	clean = streamQualityText(clean)
	clean = feedQuality.ReplaceAllString(clean, "")
	label := FeedIdentity{Family: feedFamily(clean)}
	body := clean
	if m := feedSuffix.FindStringSubmatch(clean); len(m) > 0 {
		body = m[1]
		label = FeedIdentity{Family: feedFamily(m[1]), Region: strings.ToLower(m[2]), Evidence: "explicit stream feed label"}
		if label.Region == "pacific" {
			label.Region = "west"
		}
	}
	// More than one region token is ambiguous even when the terminal label looks
	// clean ("Network East West"); do not silently discard the first marker.
	regionCount := 0
	for _, token := range feedToken.FindAllString(clean, -1) {
		switch strings.ToUpper(token) {
		case "EAST":
			regionCount++
		case "WEST", "PACIFIC":
			regionCount++
		}
	}
	if regionCount > 1 {
		return FeedIdentity{Family: label.Family, Evidence: "mixed stream feed labels"}
	}
	for _, token := range feedToken.FindAllString(clean, -1) {
		code, ok := codes[strings.ToUpper(token)]
		if !ok {
			continue
		}
		residual := regexp.MustCompile(`(?i)\b`+regexp.QuoteMeta(token)+`\b`).ReplaceAllString(body, "")
		family := feedFamily(residual)
		if family != "" && family != code.Family {
			return FeedIdentity{Family: family, Evidence: "station code conflicts with residual service-family label"}
		}
		if label.Region != "" && label.Region != code.Region {
			return FeedIdentity{Family: label.Family, Evidence: "conflicting stream label and station code"}
		}
		label = code
		label.Evidence = "exact attributed station code"
	}
	return label
}

// internalFeedLabel only proposes a parse. The caller must still prove the entire
// remaining family against this target's attributed identities. Nothing may drop
// ordinal/subbrand words, and a code conflict or repeated region is never rescued.
func internalFeedLabel(raw string, parsed FeedIdentity) FeedIdentity {
	if strings.Contains(parsed.Evidence, "conflict") || strings.Contains(parsed.Evidence, "mixed") {
		return FeedIdentity{}
	}
	clean := feedPrefix.ReplaceAllString(strings.TrimSpace(raw), "")
	clean = feedQuality.ReplaceAllString(streamQualityText(clean), "")
	positions := feedToken.FindAllStringIndex(clean, -1)
	region, start, end, count := "", 0, 0, 0
	for _, pos := range positions {
		switch strings.ToUpper(clean[pos[0]:pos[1]]) {
		case "EAST":
			region, start, end, count = "east", pos[0], pos[1], count+1
		case "WEST", "PACIFIC":
			region, start, end, count = "west", pos[0], pos[1], count+1
		}
	}
	if count != 1 || start == 0 || strings.TrimSpace(clean[end:]) == "" {
		return FeedIdentity{}
	}
	return FeedIdentity{Family: feedFamily(clean[:start] + " " + clean[end:]), Region: region, Evidence: "explicit internal feed label with complete attributed service family"}
}

// StreamFeedForTarget extends the conservative parser only with the complete
// independently attributed family of this exact target. Existing uncertainty
// and station-code conflicts take precedence; a bare family still has no region.
func StreamFeedForTarget(raw string, codes map[string]FeedIdentity, target FeedIdentity) FeedIdentity {
	parsed := StreamFeed(raw, codes)
	return targetFeed(parsed, internalFeedLabel(raw, parsed), target)
}

func targetFeed(parsed, internal, target FeedIdentity) FeedIdentity {
	if (target.Region == "east" || target.Region == "west") && internal.Region != "" && sameFeedFamily(internal.Family, target) {
		return internal
	}
	return parsed
}
