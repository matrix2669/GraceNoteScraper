// Package channelcategory owns the small, stable category taxonomy used by
// Lineuparr exports and provider-evidence scans.
package channelcategory

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

const CurrentVersion = 1

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
		"business", "business news", "legislative", "noticias",
	}},
	{name: Sports, aliases: []string{
		"sport", "sports channels", "regional sports", "deportes", "racing", "outdoor sports",
		"sports overflow", "sports multidiffusion", "sports multi diffusion",
	}},
	{name: Movies, aliases: []string{
		"movie", "cinema", "cine", "cine y series", "premium", "premium movies", "movies premium",
		"movies and premium", "movie channels",
	}},
	{name: Entertainment, aliases: []string{
		"general entertainment", "general", "generalistas", "classic", "series", "comedy", "crime",
		"discovery", "documentary", "documentaries", "documentales", "science", "culture", "education",
		"reality", "reality lifestyle", "reality and lifestyle", "reality game shows", "reality and game shows",
		"food", "travel", "food travel", "food and travel", "cooking", "shopping", "shop",
		"lifestyle", "divertissement", "entretenimiento", "decouverte", "relax",
	}},
	{name: KidsFamily, aliases: []string{
		"kids", "children", "childrens", "family", "kids family", "kids and family", "animation",
		"jeunesse", "infantil",
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
	}},
	{name: Other, aliases: []string{
		"adult", "adult channels", "other services", "other and services", "services", "service",
		"provider services", "interactive", "interactive services", "on demand", "vod", "dvr",
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
// optional identities are used only to disambiguate a mixed "Adult & PPV"
// label: an explicit PPV/event marker selects PPV & Events, an explicit adult
// identity selects Other, and an unknown mixed identity remains unresolved.
// Channel names are never generally fuzzy-classified.
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
	if isMixedAdultPPV(normalized) {
		if hasEventIdentity(identities) {
			return Match{Category: PPVEvents, MatchedAlias: strings.TrimSpace(value), Method: MethodAlias + "; mixed Adult/PPV label disambiguated by explicit event identity", Confidence: 1}, true
		}
		if hasAdultIdentity(identities) {
			return Match{Category: Other, MatchedAlias: strings.TrimSpace(value), Method: MethodAlias + "; mixed Adult/PPV label disambiguated by explicit adult identity", Confidence: 1}, true
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
