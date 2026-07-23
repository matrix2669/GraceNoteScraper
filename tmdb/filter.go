package tmdb

import "strings"

// tmdbSkipReason classifies normalized cache keys that are clearly not
// movie/series catalog titles. Returning a reason causes the lookup to be
// treated as a negative cache hit without making an HTTP request to TMDB.
//
// Movies are deliberately never filtered by title: a film may legitimately
// contain words such as news, church, football, or jewelry in its title.
// Channel-level exclusions apply to both movies and TV programmes.
func tmdbSkipReason(key string) string {
	if reason := programChannelSkipReason(key); reason != "" {
		return reason
	}

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

	// A sport name by itself is not sufficient. This keeps catalog series such
	// as "Basketball Wives" and "Racing Wives" eligible. Generic events must
	// also contain a strong broadcast cue such as live, highlights, qualifier,
	// championship, tournament, preview, or analysis.
	return hasAnyTitlePhrase(title, sportsBroadcastCues)
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
	"basketball wives":           {},
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
	"gem collectors",
	"gemstone",
	"opal hunter",
	"fine art auction",
	"auction live",
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
	"church of our lord",
	"holy mass",
	"sunday mass",
	"daily mass",
	"santa messa",
	"with pastor",
	"with bishop",
	"with prophet",
	"bible study",
	"biblical truth",
	"sabbath school",
	"gospel service",
	"gospel outreach",
	"catholic mass",
	"active catholics",
	"christians and jews",
	"3abn",
	"jesus 4 asia",
	"gurudwara",
	"rosary",
	"coronilla",
	"ministries",
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
	"pga tour golf",
	"pga korn ferry tour golf",
	"korn ferry tour golf",
	"major league table tennis",
	"nrl women's premiership",
	"wnba on ion",
	"nhra drag racing",
	"nhra racing",
	"ufl football",
	"college golf",
	"college basketball",
	"college football",
	"college baseball",
	"college softball",
	"college hockey",
	"college soccer",
	"college volleyball",
	"college lacrosse",
	"college wrestling",
	"college swimming",
	"college swimming and diving",
	"college track and field",
	"high school football",
	"high school basketball",
	"kickboxing",
}

var sportsTitleTerms = []string{
	"golf",
	"basketball",
	"football",
	"baseball",
	"softball",
	"hockey",
	"soccer",
	"cricket",
	"tennis",
	"table tennis",
	"volleyball",
	"rugby",
	"wrestling",
	"boxing",
	"kickboxing",
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
	"nhra",
	"supercars",
	"motorsport",
	"racing",
	"drag racing",
	"lacrosse",
	"swimming",
	"track and field",
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
	"premiership",
	"classic",
	"all access",
	"full impact",
	"magazine show",
	"analysis",
}
