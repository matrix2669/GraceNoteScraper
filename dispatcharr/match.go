package dispatcharr

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const minimumCandidateScore = 78

type preparedIdentity struct {
	compact string
	tokens  map[string]bool
	ordered []string
	primary bool
}

type preparedChannel struct {
	channel    MatchChannel
	identities []preparedIdentity
	epgIDs     map[string]bool
	gramCount  int
}

type scoredCandidate struct {
	channel int
	score   int
	reason  string
}

// MatchStreams returns one current proposal per unreviewed stream. A denied
// proposal exposes the next-best candidate on the next pass; a confirmed
// stream is omitted until its decision is cleared. No score is auto-accepted.
func MatchStreams(sourceFingerprint string, channels []MatchChannel, streams []Stream, decisions map[string]Decision) []Candidate {
	prepared, exactIndex, tokenIndex, gramIndex, epgIndex := prepareChannels(channels)
	confirmedStreams := make(map[string]bool)
	denied := make(map[string]bool)
	confirmedAliases := make(map[string]bool)
	deniedAliases := make(map[string]bool)
	for _, decision := range decisions {
		alias := NormalizeAliasName(decision.StreamName)
		if decision.Decision == "confirmed" {
			confirmedStreams[decision.StreamHash] = true
			if alias != "" {
				confirmedAliases[alias] = true
			}
		} else if decision.Decision == "denied" {
			denied[decisionPairKey(decision.StreamHash, decision.ChannelID)] = true
			if alias != "" {
				deniedAliases[aliasDecisionKey(alias, decision.ChannelID)] = true
			}
		}
	}

	result := make([]Candidate, 0)
	for _, stream := range streams {
		streamHash := stream.Fingerprint()
		normalizedAlias := NormalizeAliasName(stream.Name)
		if confirmedStreams[streamHash] || confirmedAliases[normalizedAlias] {
			continue
		}
		if decorativeStreamName(stream.Name) {
			continue
		}
		streamIdentities := prepareIdentities([]string{stream.Name})
		if len(streamIdentities) == 0 {
			continue
		}
		indexes := make(map[int]bool)
		gramCandidates := make(map[int]bool)
		typoScores := make(map[int]int)
		if tvgID := strings.ToLower(strings.TrimSpace(stream.TVGID)); tvgID != "" {
			for _, index := range epgIndex[tvgID] {
				indexes[index] = true
			}
		}
		for _, identity := range streamIdentities {
			for _, index := range exactIndex[identity.compact] {
				indexes[index] = true
			}
			for token := range identity.tokens {
				if !distinctiveToken(token) {
					continue
				}
				for _, index := range tokenIndex[token] {
					indexes[index] = true
				}
			}
			grams := identityTrigrams(identity.compact)
			gramHits := make(map[int]int)
			for _, gram := range grams {
				for _, index := range gramIndex[gram] {
					gramHits[index]++
				}
			}
			for index, hits := range gramHits {
				shorterGramCount := min(len(grams), prepared[index].gramCount)
				if hits >= 2 && hits*5 >= shorterGramCount*2 {
					gramCandidates[index] = true
				}
			}
		}
		for index := range gramCandidates {
			if score := identitiesSingleTypoScore(streamIdentities, prepared[index].identities); !indexes[index] && score >= minimumCandidateScore {
				indexes[index] = true
				typoScores[index] = score
			}
		}

		scored := make([]scoredCandidate, 0, len(indexes))
		for index := range indexes {
			channel := prepared[index].channel
			if eventFeedName(stream.Name) && !channelAcceptsEventFeed(channel) {
				continue
			}
			score, reason := scoreStream(stream, streamIdentities, prepared[index])
			if typoScores[index] > score {
				score = typoScores[index]
				reason = fmt.Sprintf("Fuzzy name %d%%", score)
			}
			if score < minimumCandidateScore {
				continue
			}
			if denied[decisionPairKey(streamHash, channel.ID)] || deniedAliases[aliasDecisionKey(normalizedAlias, channel.ID)] {
				continue
			}
			scored = append(scored, scoredCandidate{channel: index, score: score, reason: reason})
		}
		if len(scored) == 0 {
			continue
		}
		sort.SliceStable(scored, func(i, j int) bool {
			if scored[i].score != scored[j].score {
				return scored[i].score > scored[j].score
			}
			left := prepared[scored[i].channel].channel
			right := prepared[scored[j].channel].channel
			if left.Number != right.Number {
				return channelNumberLess(left.Number, right.Number)
			}
			return left.ID < right.ID
		})
		best := scored[0]
		channel := prepared[best.channel].channel
		result = append(result, Candidate{
			Key:             candidateKey(sourceFingerprint, streamHash, channel.ID),
			ChannelID:       channel.ID,
			ChannelNumber:   channel.Number,
			ChannelName:     channel.Name,
			StreamID:        stream.ID,
			StreamKey:       stream.Key(),
			StreamName:      stream.Name,
			TVGID:           stream.TVGID,
			M3UAccountID:    stream.M3UAccountID,
			ChannelGroupID:  stream.ChannelGroupID,
			StreamChannelNo: stream.StreamChannelNo,
			StreamHash:      streamHash,
			Source:          sourceFingerprint,
			Score:           best.score,
			Reason:          best.reason,
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		return strings.ToLower(result[i].StreamName) < strings.ToLower(result[j].StreamName)
	})
	return result
}

func aliasDecisionKey(alias, channelID string) string {
	return alias + "\x00" + channelID
}

func NormalizeAliasName(value string) string {
	identities := prepareIdentities([]string{value})
	if len(identities) == 0 {
		return ""
	}
	best := identities[0].compact
	for _, identity := range identities[1:] {
		if len(identity.compact) < len(best) {
			best = identity.compact
		}
	}
	return best
}

func GroupCandidates(candidates []Candidate) []CandidateGroup {
	groups := make(map[string]*CandidateGroup)
	for _, candidate := range candidates {
		normalized := NormalizeAliasName(candidate.StreamName)
		if normalized == "" {
			continue
		}
		key := candidateGroupKey(candidate.Source, candidate.ChannelID, normalized)
		group := groups[key]
		if group == nil {
			group = &CandidateGroup{
				Key: key, ChannelID: candidate.ChannelID, ChannelNumber: candidate.ChannelNumber, ChannelName: candidate.ChannelName,
				Alias: strings.TrimSpace(candidate.StreamName), NormalizedAlias: normalized,
				MinimumScore: candidate.Score, MaximumScore: candidate.Score, Tier: candidateTier(candidate),
			}
			groups[key] = group
		}
		group.StreamCount++
		group.MinimumScore = min(group.MinimumScore, candidate.Score)
		group.MaximumScore = max(group.MaximumScore, candidate.Score)
		group.Tier = lowerTier(group.Tier, candidateTier(candidate))
		group.StreamNames = appendUniqueFold(group.StreamNames, strings.TrimSpace(candidate.StreamName))
		group.TVGIDs = appendUniqueFold(group.TVGIDs, strings.TrimSpace(candidate.TVGID))
		group.M3UAccountIDs = appendUniqueInt64(group.M3UAccountIDs, candidate.M3UAccountID)
		group.Reasons = appendUniqueFold(group.Reasons, candidate.Reason)
		group.CandidateKeys = append(group.CandidateKeys, candidate.Key)
		if candidateNameLess(candidate.StreamName, group.Alias) {
			group.Alias = strings.TrimSpace(candidate.StreamName)
		}
	}
	result := make([]CandidateGroup, 0, len(groups))
	for _, group := range groups {
		sort.Slice(group.M3UAccountIDs, func(i, j int) bool { return group.M3UAccountIDs[i] < group.M3UAccountIDs[j] })
		sort.Slice(group.StreamNames, func(i, j int) bool {
			return strings.ToLower(group.StreamNames[i]) < strings.ToLower(group.StreamNames[j])
		})
		sort.Slice(group.TVGIDs, func(i, j int) bool { return strings.ToLower(group.TVGIDs[i]) < strings.ToLower(group.TVGIDs[j]) })
		sort.Strings(group.CandidateKeys)
		result = append(result, *group)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if tierRank(result[i].Tier) != tierRank(result[j].Tier) {
			return tierRank(result[i].Tier) < tierRank(result[j].Tier)
		}
		if result[i].MaximumScore != result[j].MaximumScore {
			return result[i].MaximumScore > result[j].MaximumScore
		}
		if result[i].ChannelNumber != result[j].ChannelNumber {
			return channelNumberLess(result[i].ChannelNumber, result[j].ChannelNumber)
		}
		return strings.ToLower(result[i].Alias) < strings.ToLower(result[j].Alias)
	})
	return result
}

func candidateGroupKey(source, channelID, normalizedAlias string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{source, channelID, normalizedAlias}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func candidateTier(candidate Candidate) string {
	reason := strings.ToLower(candidate.Reason)
	if strings.Contains(reason, "fuzzy") || candidate.Score < 88 {
		return "fuzzy"
	}
	if strings.Contains(reason, "contain") || candidate.Score < 98 {
		return "contained"
	}
	return "exact"
}

func lowerTier(left, right string) string {
	if tierRank(right) > tierRank(left) {
		return right
	}
	return left
}

func tierRank(value string) int {
	switch value {
	case "exact":
		return 0
	case "contained":
		return 1
	default:
		return 2
	}
}

func appendUniqueFold(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if strings.EqualFold(existing, value) {
			return values
		}
	}
	return append(values, value)
}

func appendUniqueInt64(values []int64, value int64) []int64 {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func candidateNameLess(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if right == "" || len([]rune(left)) != len([]rune(right)) {
		return right == "" || len([]rune(left)) < len([]rune(right))
	}
	return strings.ToLower(left) < strings.ToLower(right)
}

func channelAcceptsEventFeed(channel MatchChannel) bool {
	return strings.EqualFold(strings.TrimSpace(channel.Category), "PPV & Events") || eventFeedName(channel.Name)
}

func eventFeedName(value string) bool {
	normalized := NormalizeAliasName(value)
	if normalized == "" {
		return false
	}
	for _, marker := range []string{
		"payperview", "ppv", "specialevent", "eventchannel", "overflow", "alternate", "altfeed",
		"leaguepass", "sundayticket", "extrainnings", "centerice",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	last := normalized[len(normalized)-1]
	if last < '0' || last > '9' {
		return false
	}
	for _, league := range []string{"wnba", "nba", "nfl", "nhl", "mlb", "mls", "ncaa", "ufc", "boxing", "wwe"} {
		for _, relation := range []string{"on", "at", "vs"} {
			if strings.Contains(normalized, league+relation) {
				return true
			}
		}
	}
	return false
}

func decisionPairKey(streamHash, channelID string) string {
	return streamHash + "\x00" + channelID
}

func prepareChannels(channels []MatchChannel) ([]preparedChannel, map[string][]int, map[string][]int, map[string][]int, map[string][]int) {
	prepared := make([]preparedChannel, 0, len(channels))
	exactIndex := make(map[string][]int)
	tokenIndex := make(map[string][]int)
	gramIndex := make(map[string][]int)
	epgIndex := make(map[string][]int)
	for _, channel := range channels {
		identities := prepareChannelIdentities(channel)
		if len(identities) == 0 {
			continue
		}
		epgIDs := make(map[string]bool)
		for _, epgID := range channel.EPGIDs {
			if key := strings.ToLower(strings.TrimSpace(epgID)); key != "" {
				epgIDs[key] = true
			}
		}
		index := len(prepared)
		gramCount := 0
		for _, identity := range identities {
			count := len(identityTrigrams(identity.compact))
			if count > 0 && (gramCount == 0 || count < gramCount) {
				gramCount = count
			}
		}
		prepared = append(prepared, preparedChannel{channel: channel, identities: identities, epgIDs: epgIDs, gramCount: gramCount})
		seenExact := make(map[string]bool)
		seenTokens := make(map[string]bool)
		seenGrams := make(map[string]bool)
		for _, identity := range identities {
			if !seenExact[identity.compact] {
				exactIndex[identity.compact] = append(exactIndex[identity.compact], index)
				seenExact[identity.compact] = true
			}
			for token := range identity.tokens {
				if distinctiveToken(token) && !seenTokens[token] {
					tokenIndex[token] = append(tokenIndex[token], index)
					seenTokens[token] = true
				}
			}
			for _, gram := range identityTrigrams(identity.compact) {
				if !seenGrams[gram] {
					gramIndex[gram] = append(gramIndex[gram], index)
					seenGrams[gram] = true
				}
			}
		}
		for epgID := range epgIDs {
			epgIndex[epgID] = append(epgIndex[epgID], index)
		}
	}
	return prepared, exactIndex, tokenIndex, gramIndex, epgIndex
}

func identityTrigrams(value string) []string {
	runes := []rune(value)
	if len(runes) < 5 {
		return nil
	}
	seen := make(map[string]bool, len(runes)-2)
	grams := make([]string, 0, len(runes)-2)
	for index := 0; index+3 <= len(runes); index++ {
		gram := string(runes[index : index+3])
		if seen[gram] {
			continue
		}
		seen[gram] = true
		grams = append(grams, gram)
	}
	return grams
}

func identitiesSingleTypoScore(left, right []preparedIdentity) int {
	best := 0
	for _, leftIdentity := range left {
		for _, rightIdentity := range right {
			if singleTypo(leftIdentity.compact, rightIdentity.compact) {
				longest := max(len([]rune(leftIdentity.compact)), len([]rune(rightIdentity.compact)))
				best = max(best, int(math.Round((1-1/float64(longest))*100)))
			}
		}
	}
	return best
}

func singleTypo(left, right string) bool {
	leftRunes, rightRunes := []rune(left), []rune(right)
	if min(len(leftRunes), len(rightRunes)) < 5 {
		return false
	}
	if levenshtein(leftRunes, rightRunes) == 1 {
		return true
	}
	if len(leftRunes) != len(rightRunes) {
		return false
	}
	firstMismatch := -1
	for index := range leftRunes {
		if leftRunes[index] == rightRunes[index] {
			continue
		}
		if firstMismatch < 0 {
			firstMismatch = index
			continue
		}
		return index == firstMismatch+1 && leftRunes[firstMismatch] == rightRunes[index] && leftRunes[index] == rightRunes[firstMismatch] && string(leftRunes[index+1:]) == string(rightRunes[index+1:])
	}
	return false
}

func scoreStream(stream Stream, streamIdentities []preparedIdentity, channel preparedChannel) (int, string) {
	if tvgID := strings.ToLower(strings.TrimSpace(stream.TVGID)); tvgID != "" && channel.epgIDs[tvgID] {
		return 100, "Exact EPG ID"
	}
	bestScore := 0
	bestReason := ""
	sameNumber := sameChannelNumber(stream.StreamChannelNo, channel.channel.Number)
	for _, streamIdentity := range streamIdentities {
		for _, channelIdentity := range channel.identities {
			score, reason := identityScore(streamIdentity, channelIdentity, sameNumber)
			if score > bestScore {
				bestScore, bestReason = score, reason
			}
		}
	}
	if sameNumber && bestScore >= 60 {
		bestScore = min(99, bestScore+4)
		bestReason += " + channel number"
	}
	return bestScore, bestReason
}

func identityScore(left, right preparedIdentity, allowBroadContainment bool) (int, string) {
	if left.compact == right.compact {
		if right.primary {
			return 99, "Exact normalized channel name"
		}
		return 98, "Exact normalized name or alias"
	}
	if qualifiedIdentityContained(left, right) || qualifiedIdentityContained(right, left) {
		return 88, "Qualified contained name"
	}
	if allowBroadContainment && len(left.compact) >= 5 && len(right.compact) >= 5 && (strings.Contains(left.compact, right.compact) || strings.Contains(right.compact, left.compact)) {
		shorter := min(len(left.compact), len(right.compact))
		longer := max(len(left.compact), len(right.compact))
		if float64(shorter)/float64(longer) >= 0.64 {
			return 88, "Contained name"
		}
	}
	jaccard := tokenJaccard(left.tokens, right.tokens)
	edit := editSimilarity(left.compact, right.compact)
	score := int(math.Round((0.58*edit + 0.42*jaccard) * 100))
	if jaccard == 1 && len(left.tokens) > 1 {
		score = max(score, 92)
	}
	return score, fmt.Sprintf("Fuzzy name %d%%", score)
}

func qualifiedIdentityContained(shorter, longer preparedIdentity) bool {
	if len(shorter.ordered) == 0 || len(shorter.ordered) >= len(longer.ordered) || len(longer.ordered) > 6 {
		return false
	}
	start := contiguousTokenIndex(longer.ordered, shorter.ordered)
	if start < 0 {
		return false
	}
	for index, token := range longer.ordered {
		if index >= start && index < start+len(shorter.ordered) {
			continue
		}
		if !channelQualifierToken(token) && !qualityToken(token) {
			return false
		}
	}
	for _, token := range shorter.ordered {
		if len([]rune(token)) >= 3 && distinctiveToken(token) {
			return true
		}
	}
	return false
}

func contiguousTokenIndex(haystack, needle []string) int {
	for start := 0; start+len(needle) <= len(haystack); start++ {
		match := true
		for offset := range needle {
			if haystack[start+offset] != needle[offset] {
				match = false
				break
			}
		}
		if match {
			return start
		}
	}
	return -1
}

func prepareChannelIdentities(channel MatchChannel) []preparedIdentity {
	identities := make([]preparedIdentity, 0, 2+len(channel.Aliases)*2)
	seen := make(map[string]bool)
	for _, identity := range prepareIdentities([]string{channel.Name}) {
		identity.primary = true
		identities = append(identities, identity)
		seen[identity.compact] = true
	}
	for _, alias := range channel.Aliases {
		for _, identity := range prepareIdentities([]string{alias}) {
			if seen[identity.compact] {
				continue
			}
			identities = append(identities, identity)
			seen[identity.compact] = true
		}
	}
	return identities
}

func prepareIdentities(values []string) []preparedIdentity {
	seen := make(map[string]bool)
	result := make([]preparedIdentity, 0, len(values)*2)
	for _, value := range values {
		for _, stripQuality := range []bool{false, true} {
			tokens := normalizedTokens(value, stripQuality)
			compact := strings.Join(tokens, "")
			if len(compact) < 3 || seen[compact] {
				continue
			}
			seen[compact] = true
			tokenSet := make(map[string]bool, len(tokens))
			for _, token := range tokens {
				tokenSet[token] = true
			}
			result = append(result, preparedIdentity{compact: compact, tokens: tokenSet, ordered: tokens})
		}
	}
	return result
}

func normalizedTokens(value string, stripQuality bool) []string {
	value = stripDelimitedCountryPrefix(strings.TrimSpace(value))
	var builder strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		} else {
			builder.WriteByte(' ')
		}
	}
	tokens := strings.Fields(builder.String())
	if !stripQuality {
		return tokens
	}
	result := tokens[:0]
	for _, token := range tokens {
		if qualityToken(token) {
			continue
		}
		result = append(result, token)
	}
	return result
}

func stripDelimitedCountryPrefix(value string) string {
	lower := strings.ToLower(value)
	for _, prefix := range []string{"usa", "us", "ca", "can", "uk", "gb"} {
		if !strings.HasPrefix(lower, prefix) || len(value) <= len(prefix) {
			continue
		}
		rest := value[len(prefix):]
		trimmed := strings.TrimLeftFunc(rest, unicode.IsSpace)
		if trimmed != rest {
			rest = trimmed
		}
		if len(rest) > 0 && strings.ContainsRune("|:-/", rune(rest[0])) {
			return strings.TrimSpace(rest[1:])
		}
	}
	return value
}

func qualityToken(token string) bool {
	switch token {
	case "sd", "hd", "fhd", "uhd", "4k", "8k", "720p", "1080p", "2160p", "h264", "h265", "hevc", "raw", "backup",
		"ᴴᴰ", "ᴿᴬᵂ", "ᶠᴴᴰ", "ᵁᴴᴰ", "⁴ᴷ", "⁸ᴷ", "ᶠᵖˢ", "⁶⁰ᶠᵖˢ", "³⁸⁴⁰ᴾ", "²¹⁶⁰ᴾ", "¹⁰⁸⁰ᴾ", "⁷²⁰ᴾ":
		return true
	default:
		return false
	}
}

func distinctiveToken(token string) bool {
	if len([]rune(token)) < 3 || qualityToken(token) {
		return false
	}
	switch token {
	case "the", "and", "network", "channel", "television", "live":
		return false
	default:
		return true
	}
}

func channelQualifierToken(token string) bool {
	switch token {
	case "us", "usa", "east", "west", "central", "pacific", "atlantic", "national", "network", "channel", "television", "tv", "feed", "backup", "alternate", "alt", "new", "york", "ny":
		return true
	default:
		return false
	}
}

func decorativeStreamName(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 6 {
		return false
	}
	leading := 0
	for leading < len(value) && value[leading] == '#' {
		leading++
	}
	trailing := 0
	for index := len(value) - 1; index >= 0 && value[index] == '#'; index-- {
		trailing++
	}
	return leading >= 2 && trailing >= 2
}

func tokenJaccard(left, right map[string]bool) float64 {
	intersection := 0
	union := make(map[string]bool, len(left)+len(right))
	for token := range left {
		union[token] = true
		if right[token] {
			intersection++
		}
	}
	for token := range right {
		union[token] = true
	}
	if len(union) == 0 {
		return 0
	}
	return float64(intersection) / float64(len(union))
}

func editSimilarity(left, right string) float64 {
	leftRunes, rightRunes := []rune(left), []rune(right)
	longest := max(len(leftRunes), len(rightRunes))
	if longest == 0 {
		return 1
	}
	distance := levenshtein(leftRunes, rightRunes)
	return 1 - float64(distance)/float64(longest)
}

func levenshtein(left, right []rune) int {
	if len(left) > len(right) {
		left, right = right, left
	}
	previous := make([]int, len(left)+1)
	for i := range previous {
		previous[i] = i
	}
	for row, rightRune := range right {
		current := make([]int, len(left)+1)
		current[0] = row + 1
		for column, leftRune := range left {
			cost := 0
			if leftRune != rightRune {
				cost = 1
			}
			current[column+1] = min(current[column]+1, previous[column+1]+1, previous[column]+cost)
		}
		previous = current
	}
	return previous[len(left)]
}

func sameChannelNumber(streamNumber *float64, channelNumber string) bool {
	if streamNumber == nil {
		return false
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(channelNumber), 64)
	return err == nil && math.Abs(parsed-*streamNumber) < 0.0001
}

func channelNumberLess(left, right string) bool {
	leftNumber, leftErr := strconv.ParseFloat(strings.TrimSpace(left), 64)
	rightNumber, rightErr := strconv.ParseFloat(strings.TrimSpace(right), 64)
	if leftErr == nil && rightErr == nil && leftNumber != rightNumber {
		return leftNumber < rightNumber
	}
	return strings.ToLower(left) < strings.ToLower(right)
}

func candidateKey(sourceFingerprint, streamFingerprint, channelID string) string {
	sum := sha256.Sum256([]byte(sourceFingerprint + "\x00" + streamFingerprint + "\x00" + channelID))
	return hex.EncodeToString(sum[:])
}
