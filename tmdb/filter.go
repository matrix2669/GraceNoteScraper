package tmdb

import "strings"

// tmdbSkipReason classifies normalized cache keys that are clearly not
// movie/series catalog titles. Returning a reason causes the lookup to be
// treated as a negative cache hit without making an HTTP request to TMDB.
//
// Movies are deliberately never filtered: a film may legitimately contain
// words such as news, church, football, or jewelry in its title.
func tmdbSkipReason(key string) string {
	if !strings.HasPrefix(key, "tv:") {
		return ""
	}

	title := strings.TrimSpace(strings.TrimPrefix(key, "tv:"))
	if title == "" {
		return "empty"
	}

	// Known scripted/catalog titles that would otherwise look like broadcasts.
	if _, ok := tmdbFilterExceptions[title]; ok {
		return ""
	}

	if hasAnyTitlePhrase(title, shoppingTitlePhrases) {
		return "shopping"
	}
	if hasAnyTitlePhrase(title, religiousTitlePhrases) {
		return "religious"
	}
	if hasAnyTitlePhrase(title, fillerTitlePhrases) {
		return "filler"
	}
	if isObviousNewsTitle(title) {
		return "news"
	}
	if isObviousSportsTitle(title) {
		return "sports"
	}

	return ""
}

func hasAnyTitlePhrase(title string, phrases []string) bool {
	padded := " " + title + " "
	for _, phrase := range phrases {
		if strings.Contains(padded, " "+phrase+" ") {
			return true
		}
	}
	return false
}

func isObviousNewsTitle(title string) bool {
	if hasAnyTitlePhrase(title, newsTitlePhrases) {
		return true
	}

	fields := strings.Fields(title)
	if len(fields) == 0 {
		return false
	}

	// Network-branded newscasts are commonly named "SBS News", "CTV News",
	// or "News Q". Exceptions above protect known catalog series.
	return fields[0] == "news" || fields[len(fields)-1] == "news"
}

func isObviousSportsTitle(title string) bool {
	if hasAnyTitlePhrase(title, sportsBroadcastPhrases) {
		return true
	}
	if !hasAnyTitlePhrase(title, sportsTitleTerms) {
		return false
	}
	if hasAnyTitlePhrase(title, sportsBroadcastCues) {
		return true
	}

	// Short generic listings such as "IPL Cricket", "Liga ACB Basketball",
	// and "PGA Tour Golf" are almost always event programming. Longer titles
	// without broadcast cues remain eligible so catalog series such as
	// "Formula 1: Drive to Survive" are not discarded.
	return len(strings.Fields(title)) <= 4
}

var tmdbFilterExceptions = map[string]struct{}{
	"good news":                  {},
	"great news":                 {},
	"newsradio":                  {},
	"the newsroom":               {},
	"the newsreader":             {},
	"sports night":               {},
	"friday night lights":        {},
	"formula 1 drive to survive": {},
	"racing wives":               {},
}

var newsTitlePhrases = []string{
	"breaking news",
	"local news",
	"national news",
	"world news",
	"morning news",
	"noon news",
	"evening news",
	"nightly news",
	"news at",
	"news live",
	"news update",
	"news tonight",
	"newscast",
	"noticiero",
	"noticias",
}

var shoppingTitlePhrases = []string{
	"home shopping",
	"homeshopping",
	"shop hq",
	"shophq",
	"qvc",
	"hsn",
	"jtv",
	"jewelry",
	"jewels",
	"skincare",
	"mattresses",
	"free shipping",
	"fashion clearance",
	"collection celebration",
	"designer showcase",
	"clearance sale",
	"rise shine savings",
	"today special",
	"under 50",
	"under 100",
}

var religiousTitlePhrases = []string{
	"religious programming",
	"church service",
	"holy mass",
	"sunday mass",
	"daily mass",
	"with pastor",
	"with bishop",
	"bible study",
	"biblical truth",
	"gospel service",
	"catholic mass",
	"active catholics",
	"gurudwara",
	"rosary",
	"coronilla",
	"ministry",
	"worship service",
	"sadhguru",
}

var fillerTitlePhrases = []string{
	"paid programming",
	"to be announced",
	"program guide",
	"homeshopping live broadcast",
	"mysterious tv surprise",
	"network home",
	"sign off",
	"filler",
	"tbd",
}

var sportsBroadcastPhrases = []string{
	"pregame",
	"pre game",
	"postgame",
	"post match",
	"game break",
	"match day",
	"matchday",
	"qualifier",
	"qualifiers",
	"training camp",
	"classic games",
	"college golf",
	"college basketball",
	"college football",
	"college track and field",
}

var sportsTitleTerms = []string{
	"golf",
	"basketball",
	"football",
	"baseball",
	"hockey",
	"soccer",
	"cricket",
	"tennis",
	"wrestling",
	"boxing",
	"mixed martial arts",
	"mma",
	"ufc",
	"wnba",
	"nba",
	"nfl",
	"mlb",
	"nhl",
	"pga",
	"fifa",
	"concacaf",
	"champions league",
	"formula 1",
	"formula one",
	"motogp",
	"nascar",
	"indycar",
	"supercars",
	"motorsport",
	"racing",
	"lacrosse",
	"swimming",
	"karate combat",
	"strongest man",
}

var sportsBroadcastCues = []string{
	"live",
	"today",
	"tonight",
	"weekly",
	"recap",
	"highlights",
	"preview",
	"championship",
	"tournament",
	"classic",
	"all access",
	"full impact",
	"magazine show",
	"analysis",
}
