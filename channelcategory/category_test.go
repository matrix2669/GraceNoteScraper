package channelcategory

import "testing"

func TestCategoriesAreTheMasterTaxonomy(t *testing.T) {
	want := []string{
		"Local & Public", "News & Weather", "Sports", "Movies", "Entertainment", "Kids & Family",
		"Music", "Faith", "International", "PPV & Events", "Other",
	}
	got := Categories()
	if len(got) != len(want) {
		t.Fatalf("Categories() = %v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("Categories()[%d] = %q, want %q", index, got[index], want[index])
		}
	}
	definitions := Definitions()
	if len(definitions) != len(want) {
		t.Fatalf("Definitions() = %+v", definitions)
	}
	definitions[0].Aliases[0] = "mutated"
	if Definitions()[0].Aliases[0] == "mutated" {
		t.Fatal("Definitions returned mutable package state")
	}
}

func TestResolveUsesCanonicalAliasesAndConservativeFuzzyMatching(t *testing.T) {
	tests := []struct {
		value   string
		want    string
		method  string
		matched bool
	}{
		{value: "Movies & Premium", want: Movies, method: MethodAlias, matched: true},
		{value: "Music & Radio", want: Music, method: MethodAlias, matched: true},
		{value: "Other & Services", want: Other, method: MethodAlias, matched: true},
		{value: "documentry", want: Entertainment, method: MethodFuzzy, matched: true},
		{value: "News & Information", want: NewsWeather, method: MethodAlias, matched: true},
		{value: "Family & Kids", want: KidsFamily, method: MethodAlias, matched: true},
		{value: "Information and education", want: Entertainment, method: MethodAlias, matched: true},
		{value: "Networks", matched: false},
		{value: "Premiums", want: Movies, method: MethodAlias, matched: true},
		{value: "PPV and subscription events", want: PPVEvents, method: MethodAlias, matched: true},
		{value: "International Sports", matched: false},
		{value: "unknown package", matched: false},
	}
	for _, test := range tests {
		match, ok := Resolve(test.value)
		if ok != test.matched || match.Category != test.want || (ok && match.Method != test.method) {
			t.Errorf("Resolve(%q) = %+v, %v", test.value, match, ok)
		}
	}
}

func TestBroadNetworkGroupRequiresExplicitLocalIdentity(t *testing.T) {
	local, ok := Resolve("Networks", "WABC", "AMERICAN BROADCASTING COMPANY")
	if !ok || local.Category != LocalPublic || local.Method == MethodAlias {
		t.Fatalf("local network group = %+v, %v", local, ok)
	}
	if match, ok := Resolve("Networks", "AETVHD"); ok {
		t.Fatalf("generic cable network was categorized = %+v", match)
	}
	if match, ok := Resolve("Networks", "WORD", "The Word"); ok {
		t.Fatalf("network name resembling a callsign was categorized = %+v", match)
	}
}

func TestIdentityCategoryRecognizesPEGAndBroadcastStationsConservatively(t *testing.T) {
	tests := []struct {
		callSign  string
		affiliate string
		want      string
	}{
		{callSign: "PEG024", want: LocalPublic},
		{callSign: "WNJN", affiliate: "PUBLIC BROADCASTING SERVICE", want: LocalPublic},
		{callSign: "WABC", affiliate: "AMERICAN BROADCASTING COMPANY", want: LocalPublic},
		{callSign: "WVVHCA", want: LocalPublic},
		{callSign: "WNBCDT2", want: LocalPublic},
		{callSign: "WORD", want: Faith},
		{callSign: "AETVHD", want: Entertainment},
	}
	for _, test := range tests {
		match, ok := ResolveIdentity(test.callSign, test.affiliate)
		if !ok || match.Category != test.want {
			t.Errorf("ResolveIdentity(%q, %q) = %+v, %v; want %q", test.callSign, test.affiliate, match, ok, test.want)
		}
	}
}

func TestMaintainedIdentityCatalogUsesExactPriorityOneMatches(t *testing.T) {
	tests := []struct {
		identity string
		want     string
	}{
		{identity: "USAHD", want: Entertainment},
		{identity: "FREEFRM", want: Entertainment},
		{identity: "FREFMHD", want: Entertainment},
		{identity: "SYFYHD", want: Entertainment},
		{identity: "BBC America", want: Entertainment},
		{identity: "BBCAHD", want: Entertainment},
		{identity: "COZI", want: Entertainment},
		{identity: "IONHD", want: Entertainment},
		{identity: "Antenna TV", want: Entertainment},
		{identity: "REWINDTV", want: Entertainment},
		{identity: "HBOHTS", want: Movies},
		{identity: "HBODRMA", want: Movies},
		{identity: "HBOLAHD", want: Movies},
		{identity: "MTVCLAS", want: Music},
		{identity: "DYST", want: Faith},
		{identity: "LOC7", want: LocalPublic},
		{identity: "SOCINS", want: International},
		{identity: "VMEKIDS", want: International},
		{identity: "BABY1AS", want: International},
		{identity: "CENTROA", want: International},
		{identity: "CMEX", want: International},
		{identity: "CINLUS", want: International},
		{identity: "VMOVHD", want: International},
		{identity: "CDINAHD", want: International},
		{identity: "GALAHD", want: International},
		{identity: "ANT3I", want: International},
		{identity: "ECUAVI", want: International},
		{identity: "UNIMPHD", want: International},
		{identity: "UNIPHD", want: International},
		{identity: "ZEETVH", want: International},
		{identity: "SETHHD", want: International},
		{identity: "JADESF", want: International},
		{identity: "FILPEHD", want: International},
		{identity: "GMAPNY", want: International},
		{identity: "SIC", want: International},
		{identity: "TV5MOHD", want: International},
		{identity: "RAIIHD", want: International},
		{identity: "WLTV", want: International},
		{identity: "WAMIDT", want: International},
		{identity: "Hustler HD (Comcast)", want: Other},
		{identity: "VIVIDHC", want: Other},
		{identity: "MATURE", want: Other},
		{identity: "Penthouse HD (Comcast)", want: Other},
		{identity: "VIXENHD", want: Other},
		{identity: "AROUSE", want: Other},
		{identity: "CXTXX5H", want: Other},
	}
	for _, test := range tests {
		match, ok := ResolveIdentity(test.identity, "")
		if !ok || match.Category != test.want || match.Priority != MaintainedIdentityPriority || match.Method != MaintainedIdentityMethod {
			t.Errorf("ResolveIdentity(%q) = %+v, %v; want %q priority %d", test.identity, match, ok, test.want, MaintainedIdentityPriority)
		}
	}
}

func TestMaintainedIdentityCatalogCorrectsReviewedPriorityThreeOutliers(t *testing.T) {
	tests := []struct {
		channel  string
		identity string
		want     string
	}{
		{channel: "53", identity: "FREEFRM", want: Entertainment},
		{channel: "383", identity: "FREFMHD", want: Entertainment},
		{channel: "1742", identity: "FREFMHD", want: Entertainment},
		{channel: "68", identity: "SYFY", want: Entertainment},
		{channel: "427", identity: "SYFYHD", want: Entertainment},
		{channel: "1411", identity: "SYFYHD", want: Entertainment},
		{channel: "114", identity: "BBCA", want: Entertainment},
		{channel: "377", identity: "BBCAHD", want: Entertainment},
		{channel: "1418", identity: "BBCAHD", want: Entertainment},
		{channel: "91", identity: "LOC7", want: LocalPublic},
		{channel: "611", identity: "SOCINS", want: International},
		{channel: "632", identity: "VMEKIDS", want: International},
		{channel: "645", identity: "BABY1AS", want: International},
		{channel: "661", identity: "CENTROA", want: International},
		{channel: "680", identity: "CMEX", want: International},
		{channel: "681", identity: "CINLUS", want: International},
		{channel: "682", identity: "VMOV", want: International},
		{channel: "683", identity: "CDINA", want: International},
		{channel: "3443", identity: "SOCINS", want: International},
		{channel: "3447", identity: "VMOVHD", want: International},
		{channel: "1885", identity: "CHSTLRH", want: Other},
		{channel: "1886", identity: "VIVIDHC", want: Other},
		{channel: "1887", identity: "MATURE", want: Other},
		{channel: "1888", identity: "CPENTHH", want: Other},
		{channel: "1889", identity: "VIXENHD", want: Other},
		{channel: "1890", identity: "AROUSE", want: Other},
		{channel: "1891", identity: "CXTXX5H", want: Other},
	}
	for _, test := range tests {
		match, ok := ResolveIdentity(test.identity, "")
		if !ok || match.Category != test.want || match.Priority != MaintainedIdentityPriority {
			t.Errorf("channel %s identity %q = %+v, %v; want %q priority %d", test.channel, test.identity, match, ok, test.want, MaintainedIdentityPriority)
		}
	}
}

func TestMaintainedIdentityCatalogDoesNotFuzzyMatchNames(t *testing.T) {
	for _, identity := range []string{"Hustler News", "BBC America-ish", "Local News 7", "Mature Audiences"} {
		if match, ok := ResolveIdentity(identity, ""); ok {
			t.Errorf("ResolveIdentity(%q) unexpectedly matched %+v", identity, match)
		}
	}
}

func TestMaintainedIdentityCatalogRejectsConflictingChannelEvidence(t *testing.T) {
	if match, ok := ResolveIdentity("UNKNOWN", "", "HBO", "CNN"); ok {
		t.Fatalf("conflicting exact identities unexpectedly matched %+v", match)
	}
	if match, ok := ResolveIdentity("UNKNOWN", "", "HBO", "HBOHD"); !ok || match.Category != Movies {
		t.Fatalf("equivalent exact identities = %+v, %v", match, ok)
	}
}

func TestMaintainedInternationalIdentityOutranksBroadcastHeuristic(t *testing.T) {
	for _, callSign := range []string{"WLTV", "WLTVDT", "WAMI", "WAMIDT"} {
		match, ok := ResolveIdentity(callSign, "UNIVISION")
		if !ok || match.Category != International || match.Priority != MaintainedIdentityPriority {
			t.Errorf("ResolveIdentity(%q) = %+v, %v; want priority-1 International", callSign, match, ok)
		}
	}
}

func TestMaintainedMulticastNetworkOutranksLocalStationCallSign(t *testing.T) {
	for _, affiliate := range []string{"COZI", "ION Independent Television", "Antenna TV", "Rewind TV"} {
		match, ok := ResolveIdentity("WXYZDT3", affiliate)
		if !ok || match.Category != Entertainment || match.Priority != MaintainedIdentityPriority {
			t.Errorf("ResolveIdentity(WXYZDT3, %q) = %+v, %v; want priority-1 Entertainment", affiliate, match, ok)
		}
	}
}

func TestMaintainedIdentityCatalogHasNoConflictingAliases(t *testing.T) {
	seen := map[string]string{}
	for _, definition := range maintainedIdentities {
		for _, alias := range definition.aliases {
			key := channelIdentityKey(alias)
			if previous, ok := seen[key]; ok && previous != definition.category {
				t.Fatalf("identity %q maps to both %q and %q", alias, previous, definition.category)
			}
			seen[key] = definition.category
		}
	}
}

func TestExplicitSportsEventIdentityOutranksScheduleClassification(t *testing.T) {
	tests := []struct {
		callSign string
		identity string
	}{
		{callSign: "MLBARI", identity: "Arizona Diamondbacks: MLB Extra Innings"},
		{callSign: "NBAMIA", identity: "Miami Heat: NBA League Pass"},
		{callSign: "NHLFLA", identity: "Florida Panthers: NHL Center Ice"},
		{callSign: "NFLNRZD"},
	}
	for _, test := range tests {
		match, ok := ResolveIdentity(test.callSign, "", test.identity)
		wantAlias := test.identity
		if wantAlias == "" {
			wantAlias = test.callSign
		}
		if !ok || match.Category != PPVEvents || match.Priority != MaintainedIdentityPriority || match.Method != ExplicitEventIdentityMethod || match.MatchedAlias != wantAlias {
			t.Errorf("ResolveIdentity(%q, %q) = %+v, %v; want priority-1 PPV & Events", test.callSign, test.identity, match, ok)
		}
	}
}

func TestExplicitSportsEventIdentityDoesNotOverridePermanentNetwork(t *testing.T) {
	for _, test := range []struct {
		callSign string
		alias    string
	}{
		{callSign: "NFL Network", alias: "NFL RedZone event feed"},
		{callSign: "MLB Network", alias: "MLB Extra Innings"},
		{callSign: "NBA TV", alias: "NBA League Pass"},
		{callSign: "NHL Network", alias: "NHL Center Ice"},
	} {
		match, ok := ResolveIdentity(test.callSign, "", test.alias)
		if !ok || match.Category != Sports || match.Priority != MaintainedIdentityPriority || match.Method != MaintainedIdentityMethod {
			t.Errorf("ResolveIdentity(%q, %q) = %+v, %v; want permanent priority-1 Sports identity", test.callSign, test.alias, match, ok)
		}
	}
	for _, identity := range []string{"MLBARI", "Miami Heat", "NBA Basketball", "NHL Tonight", "League Passing Report", "Extra Inningsish"} {
		if match, ok := ResolveIdentity(identity, ""); ok {
			t.Errorf("ResolveIdentity(%q) unexpectedly inferred an event identity: %+v", identity, match)
		}
	}
}

func TestOnDemandAndPPVDisambiguation(t *testing.T) {
	onDemand, ok := Resolve("On Demand & PPV", "HBO On Demand")
	if !ok || onDemand.Category != Other {
		t.Fatalf("on-demand match = %+v, %v", onDemand, ok)
	}
	adult, ok := Resolve("On Demand & PPV", "Adult Programming")
	if !ok || adult.Category != Other {
		t.Fatalf("adult service match = %+v, %v", adult, ok)
	}
	event, ok := Resolve("On Demand & PPV", "Sports PPV Event Feed 1")
	if !ok || event.Category != PPVEvents {
		t.Fatalf("event match = %+v, %v", event, ok)
	}
	if match, ok := Resolve("On Demand & PPV", "Unknown channel"); ok {
		t.Fatalf("ambiguous mixed category resolved = %+v", match)
	}
}

func TestAdultAndPPVDisambiguation(t *testing.T) {
	adult, ok := Resolve("Adult & PPV", "Playboy TV")
	if !ok || adult.Category != Other {
		t.Fatalf("adult match = %+v, %v", adult, ok)
	}
	event, ok := Resolve("Adult & PPV", "Sports PPV Event Feed 1")
	if !ok || event.Category != PPVEvents {
		t.Fatalf("event match = %+v, %v", event, ok)
	}
	ppv, ok := Resolve("Pay Per View")
	if !ok || ppv.Category != PPVEvents {
		t.Fatalf("PPV match = %+v, %v", ppv, ok)
	}
	if match, ok := Resolve("Adult & PPV", "Unknown channel"); ok {
		t.Fatalf("ambiguous mixed category resolved = %+v", match)
	}
}

func TestPipeCategoriesMustResolveToOneMasterCategory(t *testing.T) {
	match, ok := Resolve("News & Info|Weather")
	if !ok || match.Category != NewsWeather {
		t.Fatalf("same-category pipe = %+v, %v", match, ok)
	}
	if match, ok := Resolve("Sports|International"); ok {
		t.Fatalf("conflicting pipe category resolved = %+v", match)
	}
}
