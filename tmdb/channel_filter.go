package tmdb

import (
	"log"
	"os"
	"strings"
	"sync"
)

type programEligibility struct {
	eligibleSeen bool
	excludedSeen bool
	reason       string
}

var channelEligibilityRegistry = struct {
	sync.RWMutex
	channels map[string]string
	programs map[string]programEligibility
}{
	channels: make(map[string]string),
	programs: make(map[string]programEligibility),
}

// RegisterChannelEligibility records whether a channel should remain in the
// guide but be excluded from TMDB enrichment.
func RegisterChannelEligibility(channelID, callSign, affiliate, channelNo string) {
	reason := channelExclusionReason(callSign, affiliate, channelNo)

	channelEligibilityRegistry.Lock()
	previous := channelEligibilityRegistry.channels[channelID]
	channelEligibilityRegistry.channels[channelID] = reason
	channelEligibilityRegistry.Unlock()

	if reason != "" && previous == "" {
		name := strings.TrimSpace(firstNonEmpty(affiliate, callSign, channelID))
		log.Printf("TMDB: channel %s %s excluded from enrichment (%s)", channelNo, name, reason)
	}
}

// RegisterProgramEligibility records whether a title occurs on an enrichment-
// eligible channel. A title is skipped only when every observed occurrence is
// on an excluded channel; an occurrence on any normal channel keeps it eligible.
func RegisterProgramEligibility(title string, isMovie bool, channelID string) {
	key := cacheKey(title, isMovie)

	channelEligibilityRegistry.Lock()
	state := channelEligibilityRegistry.programs[key]
	if reason := channelEligibilityRegistry.channels[channelID]; reason != "" {
		state.excludedSeen = true
		if state.reason == "" {
			state.reason = reason
		}
	} else {
		state.eligibleSeen = true
	}
	channelEligibilityRegistry.programs[key] = state
	channelEligibilityRegistry.Unlock()
}

func programChannelSkipReason(key string) string {
	channelEligibilityRegistry.RLock()
	state, ok := channelEligibilityRegistry.programs[key]
	channelEligibilityRegistry.RUnlock()
	if !ok || state.eligibleSeen || !state.excludedSeen {
		return ""
	}
	return "channel-" + state.reason
}

func channelExclusionReason(callSign, affiliate, channelNo string) string {
	value := normalizeChannelText(strings.Join([]string{callSign, affiliate, channelNo}, " "))
	groups := configuredChannelGroups()

	if groups["local-access"] && isLocalAccessChannel(value) {
		return "local-access"
	}
	if groups["shopping"] && isShoppingChannel(value) {
		return "shopping"
	}
	if groups["religious"] && isReligiousChannel(value) {
		return "religious"
	}

	for _, pattern := range configuredCustomChannelPatterns() {
		if strings.Contains(value, pattern) {
			return "custom"
		}
	}
	return ""
}

func configuredChannelGroups() map[string]bool {
	value, exists := os.LookupEnv("TMDB_EXCLUDE_CHANNEL_GROUPS")
	if !exists {
		value = "local-access,shopping,religious"
	}
	groups := make(map[string]bool)
	for _, group := range strings.Split(value, ",") {
		group = strings.ToLower(strings.TrimSpace(group))
		switch group {
		case "local", "local-access", "access", "peg":
			groups["local-access"] = true
		case "shopping", "shop":
			groups["shopping"] = true
		case "religious", "faith":
			groups["religious"] = true
		}
	}
	return groups
}

func configuredCustomChannelPatterns() []string {
	var patterns []string
	for _, value := range strings.Split(os.Getenv("TMDB_EXCLUDE_CHANNEL_PATTERNS"), ",") {
		value = normalizeChannelText(value)
		if value != "" {
			patterns = append(patterns, value)
		}
	}
	return patterns
}

func normalizeChannelText(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func isLocalAccessChannel(value string) bool {
	fields := strings.Fields(value)
	for _, field := range fields {
		if strings.HasPrefix(field, "peg") && len(field) > 3 {
			return true
		}
	}
	return containsChannelPhrase(value,
		"public educational government access",
		"public access",
		"community access",
		"government access",
		"municipal access",
		"education access",
	)
}

func isShoppingChannel(value string) bool {
	return containsChannelToken(value,
		"qvc", "qvc2", "qvc3", "hsn", "hsn2", "jtv", "shophq", "shoplc", "gemshd",
	) || containsChannelPhrase(value,
		"shop lc",
		"jewelry television",
		"jewelry tv",
		"home shopping network",
		"home shopping",
		"shop headquarters",
	)
}

func isReligiousChannel(value string) bool {
	return containsChannelToken(value,
		"ewtn", "tbn", "daystar", "sonlife", "sbn", "cbn", "ctn", "godtv",
	) || containsChannelPhrase(value,
		"daystar television network",
		"sonlife broadcasting network",
		"trinity broadcasting network",
		"christian broadcasting network",
		"the word network",
		"word network",
		"church channel",
		"god tv",
	)
}

func containsChannelToken(value string, tokens ...string) bool {
	fields := strings.Fields(value)
	for _, field := range fields {
		for _, token := range tokens {
			if field == token {
				return true
			}
		}
	}
	return false
}

func containsChannelPhrase(value string, phrases ...string) bool {
	padded := " " + value + " "
	for _, phrase := range phrases {
		if strings.Contains(padded, " "+phrase+" ") {
			return true
		}
	}
	return false
}

func resetChannelEligibilityRegistryForTest() {
	channelEligibilityRegistry.Lock()
	channelEligibilityRegistry.channels = make(map[string]string)
	channelEligibilityRegistry.programs = make(map[string]programEligibility)
	channelEligibilityRegistry.Unlock()
}
