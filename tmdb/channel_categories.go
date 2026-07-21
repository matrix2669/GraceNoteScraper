package tmdb

import (
	"log"
	"strings"
	"sync"
)

var channelProgramCategoryRegistry = struct {
	sync.RWMutex
	categories map[string][]string
}{categories: make(map[string][]string)}

// RegisterChannelProgramCategories records categories inherited by every
// programme on a dedicated news or sports network. This classification is
// separate from TMDB enrichment eligibility: these channels remain eligible
// for enrichment, and no channel is removed from the guide.
func RegisterChannelProgramCategories(channelID, callSign, affiliate, channelNo string) {
	categories := classifyChannelProgramCategories(callSign, affiliate)

	channelProgramCategoryRegistry.Lock()
	channelProgramCategoryRegistry.categories[channelID] = append([]string(nil), categories...)
	channelProgramCategoryRegistry.Unlock()

	if len(categories) > 0 {
		name := strings.TrimSpace(firstNonEmpty(affiliate, callSign, channelID))
		log.Printf("Guide: channel %s %s adds programme category %s", channelNo, name, strings.Join(categories, ", "))
	}
}

// ChannelProgramCategories returns a copy of the categories inherited by
// programmes on the specified channel.
func ChannelProgramCategories(channelID string) []string {
	channelProgramCategoryRegistry.RLock()
	categories := append([]string(nil), channelProgramCategoryRegistry.categories[channelID]...)
	channelProgramCategoryRegistry.RUnlock()
	return categories
}

func classifyChannelProgramCategories(callSign, affiliate string) []string {
	callSign = normalizeChannelText(callSign)
	affiliate = normalizeChannelText(affiliate)

	// Check sports first because names such as ESPNEWS contain "news" but are
	// dedicated sports networks.
	if isDedicatedSportsChannel(callSign, affiliate) {
		return []string{"Sports"}
	}
	if isDedicatedNewsChannel(callSign, affiliate) {
		return []string{"News"}
	}
	return nil
}

func isDedicatedSportsChannel(callSign, affiliate string) bool {
	if hasChannelCallSignPrefix(callSign,
		"espn", "msg", "nfl", "nba", "mlb", "nhl", "golf", "tennis",
		"big10", "cbssn", "bein", "tudn", "sny", "yes",
	) {
		return true
	}

	if hasExactChannelCallSign(callSign,
		"fs1", "fs1hd", "fs2", "fs2hd",
		"acc", "accsd", "sech", "sec",
	) {
		return true
	}

	return containsChannelPhrase(affiliate,
		"espn",
		"cbs sports network",
		"fox sports 1",
		"fox sports 2",
		"nfl network",
		"nfl redzone",
		"nba tv",
		"mlb network",
		"nhl network",
		"golf channel",
		"tennis channel",
		"big ten network",
		"acc network",
		"sec network",
		"yes network",
		"sportsnet new york",
		"bein sports",
		"tudn",
	)
}

func isDedicatedNewsChannel(callSign, affiliate string) bool {
	if hasChannelCallSignPrefix(callSign,
		"cnn", "cnbc", "newsmx", "newsntn", "cspan", "bbcwdn", "bbcnews",
	) {
		return true
	}
	if hasExactChannelCallSign(callSign, "fnc", "fnchd", "hln") {
		return true
	}

	return containsChannelPhrase(affiliate,
		"cnn",
		"cnn international",
		"cnbc",
		"cnbc world",
		"fox news channel",
		"newsmax",
		"newsnation",
		"c span",
		"bbc world news",
		"bbc news",
	)
}

func hasChannelCallSignPrefix(callSign string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(callSign, prefix) {
			return true
		}
	}
	return false
}

func hasExactChannelCallSign(callSign string, values ...string) bool {
	for _, value := range values {
		if callSign == value {
			return true
		}
	}
	return false
}

func resetChannelProgramCategoryRegistryForTest() {
	channelProgramCategoryRegistry.Lock()
	channelProgramCategoryRegistry.categories = make(map[string][]string)
	channelProgramCategoryRegistry.Unlock()
}
