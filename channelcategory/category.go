// Package channelcategory owns the small, stable category taxonomy used by
// Lineuparr exports and provider-evidence scans.
package channelcategory

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

const CurrentVersion = 2

const (
	LocalPublic   = "Local & Public"
	NewsWeather   = "News & Weather"
	Sports        = "Sports"
	Movies        = "Movies"
	Entertainment = "Entertainment"
	KidsFamily    = "Kids & Family"
	Music         = "Music"
	Faith         = "Faith"
	International = "International"
	PPVEvents     = "PPV & Events"
	Other         = "Other"
)

const (
	MethodCanonical = "exact master category"
	MethodAlias     = "exact category alias"
	MethodFuzzy     = "fuzzy category alias"
)

// Match describes how a provider category was translated into the master
// taxonomy. Confidence is 1 for exact matches and a 0..1 similarity for fuzzy
// matches.
type Match struct {
	Category     string
	MatchedAlias string
	Method       string
	Confidence   float64
	Priority     int
}

type definition struct {
	name    string
	aliases []string
}

type Definition struct {
	Name    string
	Aliases []string
}

var definitions = []definition{
	{name: LocalPublic, aliases: []string{
		"local", "local channels", "broadcast", "broadcast channels", "ota", "over the air",
		"affiliate", "network affiliates", "public", "public access", "peg", "government",
		"government access", "educational access", "community access", "community television",
	}},
	{name: NewsWeather, aliases: []string{
		"news", "weather", "news and weather", "news info", "news and info", "information",
		"news information", "news and information", "business", "business news", "legislative", "noticias",
	}},
	{name: Sports, aliases: []string{
		"sport", "sports channels", "regional sports", "deportes", "racing", "outdoor sports",
		"sports overflow", "sports multidiffusion", "sports multi diffusion",
	}},
	{name: Movies, aliases: []string{
		"movie", "cinema", "cine", "cine y series", "premium", "premiums", "premium movies", "movies premium",
		"movies and premium", "movie channels",
	}},
	{name: Entertainment, aliases: []string{
		"general entertainment", "general", "generalistas", "classic", "series", "comedy", "crime",
		"discovery", "documentary", "documentaries", "documentales", "science", "culture", "education",
		"reality", "reality lifestyle", "reality and lifestyle", "reality game shows", "reality and game shows",
		"food", "travel", "food travel", "food and travel", "cooking",
		"lifestyle", "women", "home and leisure", "pop culture", "people and culture",
		"information and education", "divertissement", "entretenimiento", "decouverte", "relax",
	}},
	{name: KidsFamily, aliases: []string{
		"kids", "children", "childrens", "family", "kids family", "kids and family", "animation",
		"family kids", "family and kids", "jeunesse", "infantil",
	}},
	{name: Music, aliases: []string{
		"music radio", "music and radio", "radio", "music choice", "musique", "musica",
	}},
	{name: Faith, aliases: []string{
		"religious", "religion", "inspirational", "spiritual", "worship",
	}},
	{name: International, aliases: []string{
		"foreign", "world", "international channels", "spanish", "latino", "internacional",
		"multicultural", "ethnic",
	}},
	{name: PPVEvents, aliases: []string{
		"ppv", "pay per view", "payperview", "event", "events", "special event", "special events",
		"event channels", "eventos", "seasonal", "sports ppv", "movie ppv", "sports events",
		"ppv and subscription events", "ppv and subscription sports",
	}},
	{name: Other, aliases: []string{
		"shopping", "shop", "marketplace",
		"adult", "adult channels", "other services", "other and services", "services", "service",
		"provider services", "help and services", "interactive", "interactive services", "on demand", "vod", "dvr",
		"caller id", "information services", "secondary information", "secondary and information",
		"uncategorized other", "miscellaneous", "misc",
	}},
}

var canonicalByKey map[string]string
var candidates []aliasCandidate

type aliasCandidate struct {
	category   string
	label      string
	normalized string
	canonical  bool
}

func init() {
	canonicalByKey = make(map[string]string, len(definitions))
	for _, item := range definitions {
		key := normalize(item.name)
		canonicalByKey[key] = item.name
		candidates = append(candidates, aliasCandidate{category: item.name, label: item.name, normalized: key, canonical: true})
		seen := map[string]bool{key: true}
		for _, alias := range item.aliases {
			normalized := normalize(alias)
			if normalized == "" || seen[normalized] {
				continue
			}
			seen[normalized] = true
			candidates = append(candidates, aliasCandidate{category: item.name, label: alias, normalized: normalized})
		}
	}
}

// Categories returns the canonical categories in their intended UI order.
func Categories() []string {
	result := make([]string, 0, len(definitions))
	for _, item := range definitions {
		result = append(result, item.name)
	}
	return result
}

// Definitions returns a defensive copy of the maintained provider-label alias
// list for auditing, tests, and future UI descriptions.
func Definitions() []Definition {
	result := make([]Definition, 0, len(definitions))
	for _, item := range definitions {
		result = append(result, Definition{Name: item.name, Aliases: append([]string(nil), item.aliases...)})
	}
	return result
}

// IsCanonical reports whether value is already one of the master categories.
func IsCanonical(value string) bool {
	_, ok := canonicalByKey[normalize(value)]
	return ok
}

// Resolve maps a provider-supplied category label to the master taxonomy. The
// optional identities disambiguate mixed Adult/PPV and On Demand/PPV labels:
// explicit event markers select PPV & Events, while explicit adult or service
// markers select Other. Unknown mixed identities remain unresolved. Channel
// names are never generally fuzzy-classified.
func Resolve(value string, identities ...string) (Match, bool) {
	parts := categoryParts(value)
	if len(parts) > 1 {
		var matches []Match
		seen := make(map[string]bool)
		for _, part := range parts {
			match, ok := resolveOne(part, identities...)
			if !ok || seen[match.Category] {
				continue
			}
			seen[match.Category] = true
			matches = append(matches, match)
		}
		if len(matches) != 1 {
			return Match{}, false
		}
		matches[0].MatchedAlias = strings.TrimSpace(value)
		return matches[0], true
	}
	return resolveOne(value, identities...)
}

func resolveOne(value string, identities ...string) (Match, bool) {
	normalized := normalize(value)
	if normalized == "" {
		return Match{}, false
	}
	if isGenericNetworkGroup(normalized) {
		if match, ok := inferLocalPublic(identities); ok {
			match.MatchedAlias = strings.TrimSpace(value)
			match.Method = MethodAlias + "; broad provider network group disambiguated by explicit local/public identity"
			return match, true
		}
		// Provider headings such as Optimum's "Networks" combine local
		// stations with unrelated cable networks. They are useful grouping
		// labels, but are not category evidence on their own.
		return Match{}, false
	}
	if isMixedAdultPPV(normalized) {
		if hasEventIdentity(identities) {
			return Match{Category: PPVEvents, MatchedAlias: strings.TrimSpace(value), Method: MethodAlias + "; mixed Adult/PPV label disambiguated by explicit event identity", Confidence: 1}, true
		}
		if hasAdultIdentity(identities) {
			return Match{Category: Other, MatchedAlias: strings.TrimSpace(value), Method: MethodAlias + "; mixed Adult/PPV label disambiguated by explicit adult identity", Confidence: 1}, true
		}
		return Match{}, false
	}
	if isMixedOnDemandPPV(normalized) {
		if hasEventIdentity(identities) {
			return Match{Category: PPVEvents, MatchedAlias: strings.TrimSpace(value), Method: MethodAlias + "; mixed On Demand/PPV label disambiguated by explicit event identity", Confidence: 1}, true
		}
		if hasOnDemandIdentity(identities) || hasAdultIdentity(identities) {
			return Match{Category: Other, MatchedAlias: strings.TrimSpace(value), Method: MethodAlias + "; mixed On Demand/PPV label disambiguated by explicit service identity", Confidence: 1}, true
		}
		return Match{}, false
	}
	for _, candidate := range candidates {
		if normalized != candidate.normalized {
			continue
		}
		method := MethodAlias
		if candidate.canonical {
			method = MethodCanonical
		}
		return Match{Category: candidate.category, MatchedAlias: candidate.label, Method: method, Confidence: 1}, true
	}

	if len([]rune(normalized)) < 5 {
		return Match{}, false
	}
	byCategory := make(map[string]Match)
	for _, candidate := range candidates {
		score := similarity(normalized, candidate.normalized)
		if current, ok := byCategory[candidate.category]; !ok || score > current.Confidence {
			byCategory[candidate.category] = Match{
				Category: candidate.category, MatchedAlias: candidate.label, Method: MethodFuzzy, Confidence: score,
			}
		}
	}
	ranked := make([]Match, 0, len(byCategory))
	for _, match := range byCategory {
		ranked = append(ranked, match)
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].Confidence > ranked[j].Confidence })
	if len(ranked) == 0 || ranked[0].Confidence < 0.82 {
		return Match{}, false
	}
	if len(ranked) > 1 && ranked[0].Confidence-ranked[1].Confidence < 0.10 {
		return Match{}, false
	}
	return ranked[0], true
}

// ResolveIdentity maps only exact channel identities that carry direct,
// auditable category meaning. Maintained network names and callsigns are
// accepted; arbitrary or fuzzy channel-name classification is not.
func ResolveIdentity(callSign, affiliate string, identities ...string) (Match, bool) {
	values := append([]string{callSign, affiliate}, identities...)
	if match, ok := resolveMaintainedIdentity(values...); ok {
		return match, true
	}
	for _, identity := range values {
		key := strings.ToUpper(compact(identity))
		key = strings.TrimSuffix(key, "HD")
		category := ""
		switch key {
		case "ANTENNATV", "GRIT", "GRITTV", "BOUNCE", "BOUNCETV":
			category = Entertainment
		case "QVC", "QVC2", "QVC3", "HSN", "HSN2", "JEWELRYTELEVISION", "JTV":
			category = Other
		}
		if category != "" {
			return Match{Category: category, MatchedAlias: identity, Method: "maintained explicit network category identity", Confidence: 1}, true
		}
	}
	for _, identity := range values {
		key := strings.ToUpper(compact(identity))
		if isPEGIdentity(key) || isPublicIdentity(key) {
			return Match{Category: LocalPublic, MatchedAlias: strings.TrimSpace(identity), Method: "explicit public/PEG channel identity", Confidence: 1}, true
		}
	}
	key := strings.ToUpper(compact(callSign))
	if isQualifiedBroadcastCallSign(key) || (strings.TrimSpace(affiliate) != "" && isBroadcastCallSign(key)) {
		return Match{Category: LocalPublic, MatchedAlias: strings.TrimSpace(callSign), Method: "explicit broadcast callsign and affiliate identity", Confidence: 1}, true
	}
	return Match{}, false
}

func inferLocalPublic(identities []string) (Match, bool) {
	for _, identity := range identities {
		key := strings.ToUpper(compact(identity))
		if isPEGIdentity(key) || isPublicIdentity(key) {
			return Match{Category: LocalPublic, MatchedAlias: strings.TrimSpace(identity), Method: "explicit public/PEG channel identity", Confidence: 1}, true
		}
	}

	hasAffiliate := false
	for _, identity := range identities {
		key := strings.ToUpper(compact(identity))
		if isBroadcastAffiliateIdentity(key) {
			hasAffiliate = true
		}
	}
	for _, identity := range identities {
		key := strings.ToUpper(compact(identity))
		if isQualifiedBroadcastCallSign(key) || (hasAffiliate && isBroadcastCallSign(key)) {
			return Match{Category: LocalPublic, MatchedAlias: strings.TrimSpace(identity), Method: "explicit broadcast callsign and affiliate identity", Confidence: 1}, true
		}
	}
	return Match{}, false
}

func isBroadcastAffiliateIdentity(value string) bool {
	switch value {
	case "ABC", "AMERICANBROADCASTINGCOMPANY",
		"CBS", "CBSTELEVISIONNETWORK",
		"NBC", "NATIONALBROADCASTINGCOMPANY",
		"FOX", "FOXENTERTAINMENT",
		"CW", "THECWTELEVISIONNETWORK",
		"MYNETWORKTV", "IONINDEPENDENTTELEVISION",
		"TELEMUNDO", "UNIVISION", "UNIMAS", "ESTRELLATV", "INDEPENDENT":
		return true
	default:
		return false
	}
}

func isGenericNetworkGroup(value string) bool {
	switch value {
	case "network", "networks", "network channel", "network channels":
		return true
	default:
		return false
	}
}

func isPEGIdentity(value string) bool {
	if !strings.HasPrefix(value, "PEG") || len(value) == 3 {
		return false
	}
	for _, character := range value[3:] {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func isPublicIdentity(value string) bool {
	for _, marker := range []string{
		"PUBLICBROADCASTINGSERVICE", "PUBLICACCESS", "COMMUNITYACCESS", "GOVERNMENTACCESS",
		"EDUCATIONALACCESS", "PUBLICEDUCATIONALGOVERNMENTACCESS",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func isQualifiedBroadcastCallSign(value string) bool {
	base, qualified := broadcastCallSignBase(value)
	return qualified && validBroadcastCallSignBase(base)
}

func isBroadcastCallSign(value string) bool {
	base, _ := broadcastCallSignBase(value)
	return validBroadcastCallSignBase(base)
}

func broadcastCallSignBase(value string) (string, bool) {
	value = strings.ToUpper(strings.TrimSpace(value))
	for _, suffix := range []string{"HD", "SD"} {
		if strings.HasSuffix(value, suffix) {
			value = strings.TrimSuffix(value, suffix)
			break
		}
	}
	qualified := false
	if len(value) > 0 && value[len(value)-1] >= '2' && value[len(value)-1] <= '9' {
		value = value[:len(value)-1]
		qualified = true
	}
	for _, suffix := range []string{"DT", "TV", "CA", "LP", "LD", "CD"} {
		if strings.HasSuffix(value, suffix) {
			value = strings.TrimSuffix(value, suffix)
			qualified = true
			break
		}
	}
	return value, qualified
}

func validBroadcastCallSignBase(value string) bool {
	if len(value) != 4 || (value[0] != 'K' && value[0] != 'W') {
		return false
	}
	for _, character := range value[1:] {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}

func categoryParts(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	// Pipes are used by provider payloads for multiple classifications. Do not
	// split ampersands because they are part of canonical labels.
	parts := strings.Split(value, "|")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			result = append(result, part)
		}
	}
	return result
}

func isMixedAdultPPV(value string) bool {
	tokens := tokenSet(value)
	return tokens["adult"] && (tokens["ppv"] || (tokens["pay"] && tokens["view"]))
}

func isMixedOnDemandPPV(value string) bool {
	tokens := tokenSet(value)
	return tokens["on"] && tokens["demand"] && (tokens["ppv"] || (tokens["pay"] && tokens["view"]))
}

func hasEventIdentity(identities []string) bool {
	for _, identity := range identities {
		key := compact(identity)
		for _, marker := range []string{
			"ppv", "payperview", "specialevent", "eventchannel", "eventfeed", "seasonpass", "sportspackage",
			"leaguepass", "sundayticket", "extrainnings", "centerice", "directkick", "fullcourt", "gameplan",
		} {
			if strings.Contains(key, marker) {
				return true
			}
		}
	}
	return false
}

func hasAdultIdentity(identities []string) bool {
	for _, identity := range identities {
		key := compact(identity)
		for _, marker := range []string{
			"adult", "playboy", "penthouse", "hustler", "redlight", "hardcore", "xtsy", "fresh", "vivid",
		} {
			if strings.Contains(key, marker) {
				return true
			}
		}
	}
	return false
}

func hasOnDemandIdentity(identities []string) bool {
	for _, identity := range identities {
		key := compact(identity)
		for _, marker := range []string{"ondemand", "vod", "playback", "interactive"} {
			if strings.Contains(key, marker) {
				return true
			}
		}
	}
	return false
}

func normalize(value string) string {
	var builder strings.Builder
	space := false
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		character = foldAccent(character)
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			if space && builder.Len() > 0 {
				builder.WriteByte(' ')
			}
			builder.WriteRune(character)
			space = false
		} else {
			space = true
		}
	}
	return strings.TrimSpace(builder.String())
}

func compact(value string) string {
	return strings.ReplaceAll(normalize(value), " ", "")
}

func foldAccent(character rune) rune {
	switch character {
	case 'á', 'à', 'â', 'ä', 'ã', 'å':
		return 'a'
	case 'ç':
		return 'c'
	case 'é', 'è', 'ê', 'ë':
		return 'e'
	case 'í', 'ì', 'î', 'ï':
		return 'i'
	case 'ñ':
		return 'n'
	case 'ó', 'ò', 'ô', 'ö', 'õ':
		return 'o'
	case 'ú', 'ù', 'û', 'ü':
		return 'u'
	default:
		return character
	}
}

func similarity(left, right string) float64 {
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	maximum := len(leftRunes)
	if len(rightRunes) > maximum {
		maximum = len(rightRunes)
	}
	if maximum == 0 {
		return 0
	}
	edit := 1 - float64(levenshtein(leftRunes, rightRunes))/float64(maximum)
	leftTokens := tokenSet(left)
	rightTokens := tokenSet(right)
	intersection := 0
	for token := range leftTokens {
		if rightTokens[token] {
			intersection++
		}
	}
	dice := 0.0
	if len(leftTokens)+len(rightTokens) > 0 {
		dice = 2 * float64(intersection) / float64(len(leftTokens)+len(rightTokens))
	}
	return math.Max(edit, 0.72*edit+0.28*dice)
}

func tokenSet(value string) map[string]bool {
	result := make(map[string]bool)
	for _, token := range strings.Fields(normalize(value)) {
		switch token {
		case "and", "the", "channel", "channels", "network", "networks", "tv", "television":
			continue
		}
		if strings.HasSuffix(token, "s") && len(token) > 4 {
			token = strings.TrimSuffix(token, "s")
		}
		result[token] = true
	}
	return result
}

func levenshtein(left, right []rune) int {
	previous := make([]int, len(right)+1)
	for index := range previous {
		previous[index] = index
	}
	for leftIndex, leftRune := range left {
		current := make([]int, len(right)+1)
		current[0] = leftIndex + 1
		for rightIndex, rightRune := range right {
			cost := 0
			if leftRune != rightRune {
				cost = 1
			}
			current[rightIndex+1] = min3(current[rightIndex]+1, previous[rightIndex+1]+1, previous[rightIndex]+cost)
		}
		previous = current
	}
	return previous[len(right)]
}

func min3(first, second, third int) int {
	if second < first {
		first = second
	}
	if third < first {
		first = third
	}
	return first
}
