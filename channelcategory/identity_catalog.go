package channelcategory

import "strings"

const (
	MaintainedIdentityPriority = 1
	MaintainedIdentityMethod   = "exact maintained channel category identity"
)

type identityDefinition struct {
	category string
	aliases  []string
}

// maintainedIdentities contains exact channel identities whose category is
// stable enough to outrank programme-format inference. It is deliberately not
// a fuzzy matcher: additions require a reviewed network name, brand, or
// provider callsign and focused regression coverage.
var maintainedIdentities = []identityDefinition{
	{category: LocalPublic, aliases: []string{
		"ABC", "American Broadcasting Company", "CBS", "CBS Television Network",
		"NBC", "National Broadcasting Company", "FOX", "Fox Entertainment",
		"CW", "The CW Television Network", "MyNetworkTV", "PBS", "LOC7", "Local 7",
	}},
	{category: NewsWeather, aliases: []string{
		"CNN", "CNN International", "CNBC", "MSNBC", "Fox News", "FNC", "HLN",
		"NewsNation", "NWSNTN", "NWSNTSD", "C-SPAN", "CSPAN", "CSPAN2", "CSPAN3",
		"The Weather Channel", "TWC", "WeatherNation", "Newsmax",
	}},
	{category: Sports, aliases: []string{
		"ESPN", "ESPN2", "ESPNU", "ESPNEWS", "FS1", "FS2", "Fox Sports 1", "Fox Sports 2",
		"NFL Network", "NBA TV", "MLB Network", "NHL Network", "Golf Channel", "Tennis Channel",
		"CBS Sports Network", "CBSSN", "SEC Network", "ACC Network", "Big Ten Network", "BTN",
	}},
	{category: Movies, aliases: []string{
		"HBO", "HBO2", "HBO Signature", "HBO Family", "HBO Comedy", "HBO Zone", "HBO Latino",
		"HBO Hits", "HBOHTS", "HBO Drama", "HBODRMA", "HBOLA",
		"Cinemax", "MoreMax", "ActionMax", "ThrillerMax", "MovieMax", "5StarMax", "OuterMax",
		"Showtime", "SHO", "Showtime 2", "SHO2", "Showcase", "Showtime Extreme", "SHO Extreme",
		"SHO x BET", "Showtime Women", "Showtime Family Zone", "Showtime Next",
		"The Movie Channel", "TMC", "TMC Xtra", "TMCX", "Flix",
		"Starz", "Starz Edge", "Starz Comedy", "Starz Cinema", "Starz Black",
		"Starz Kids & Family", "Starz Encore", "MGM+", "MGM Plus", "EPIX",
		"Turner Classic Movies", "TCM", "FX Movie Channel", "FXM",
	}},
	{category: Entertainment, aliases: []string{
		"USA", "USA Network", "TNT", "TBS", "Syfy", "Sci Fi Channel",
		"E!", "E Entertainment Television", "Freeform", "FREEFRM", "FREFM",
		"BBC America", "BBCA", "FX", "FXX", "AMC", "A&E", "AETV", "Bravo",
		"Comedy Central", "Paramount Network", "TV Land", "History", "HSTRY",
		"Discovery Channel", "Discovery", "TLC", "HGTV", "Food Network", "Travel Channel",
		"OWN", "Lifetime", "WE tv", "Vice", "Reelz", "UPtv", "Hallmark Channel", "Nat Geo Wild",
		"MTV", "VH1", "BET", "CMT",
	}},
	{category: KidsFamily, aliases: []string{
		"Disney Channel", "DISN", "Disney XD", "DXD", "Nickelodeon", "NICK",
		"Nicktoons", "NIKTON", "Nick Too", "NICKTOO", "TeenNick",
		"Cartoon Network", "TOON", "Boomerang", "Universal Kids", "PBS Kids",
		"Discovery Family",
	}},
	{category: Music, aliases: []string{
		"MTV Classic", "MTVCLAS", "MTV Live", "BET Soul", "BETSOUL",
		"CMT Music", "CMTMUS", "Music Choice",
	}},
	{category: Faith, aliases: []string{
		"Daystar", "DYST", "The Word Network", "WORD", "TBN", "EWTN", "BYUtv", "CTN",
	}},
	{category: International, aliases: []string{
		"Sony Cine", "SOCINS", "V-me Kids", "VMEKIDS", "BabyFirst Americas", "BABY1AS",
		"CentroAmerica TV", "CentroAmericaTV", "CENTROA", "Cine Mexicano", "Cine Mexicano US Feed", "CMEX",
		"Cine Latino", "Cine Latino US", "CINLUS", "ViendoMovies", "VMOV",
		"Cinema Dinamita", "CDINA",
	}},
	{category: Other, aliases: []string{
		"Adult Programming", "Hustler", "Hustler TV", "Hustler HD (Comcast)", "CHSTLRH",
		"Vivid TV", "VividTV", "VividTV HD (Comcast)", "VIVIDHC", "Mature Lust", "MATURE",
		"Penthouse", "Penthouse TV", "Penthouse HD (Comcast)", "CPENTHH",
		"Vixen", "Vixen HD", "VIXENHD", "Arouse", "AROUSE",
		"XTSY", "XTSY XX5", "XTSY XX5 HD (Comcast)", "CXTXX5H",
	}},
}

var maintainedIdentityCategories = buildMaintainedIdentityCategories()

func buildMaintainedIdentityCategories() map[string]string {
	result := make(map[string]string)
	for _, definition := range maintainedIdentities {
		for _, alias := range definition.aliases {
			key := channelIdentityKey(alias)
			if key == "" {
				continue
			}
			if existing, ok := result[key]; ok && existing != definition.category {
				panic("channel category identity appears in multiple categories: " + alias)
			}
			result[key] = definition.category
		}
	}
	return result
}

func resolveMaintainedIdentity(identities ...string) (Match, bool) {
	var result Match
	for _, identity := range identities {
		key := channelIdentityKey(identity)
		category := maintainedIdentityCategories[key]
		if category == "" {
			continue
		}
		if result.Category != "" && result.Category != category {
			return Match{}, false
		}
		if result.Category != "" {
			continue
		}
		result = Match{
			Category:     category,
			MatchedAlias: strings.TrimSpace(identity),
			Method:       MaintainedIdentityMethod,
			Confidence:   1,
			Priority:     MaintainedIdentityPriority,
		}
	}
	return result, result.Category != ""
}

func channelIdentityKey(value string) string {
	key := strings.ToUpper(compact(value))
	for _, suffix := range []string{"HD", "SD"} {
		if strings.HasSuffix(key, suffix) {
			return strings.TrimSuffix(key, suffix)
		}
	}
	return key
}
