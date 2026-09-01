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
		matched   bool
	}{
		{callSign: "PEG024", matched: true},
		{callSign: "WNJN", affiliate: "PUBLIC BROADCASTING SERVICE", matched: true},
		{callSign: "WABC", affiliate: "AMERICAN BROADCASTING COMPANY", matched: true},
		{callSign: "WVVHCA", matched: true},
		{callSign: "WNBCDT2", matched: true},
		{callSign: "WORD", matched: false},
		{callSign: "AETVHD", matched: false},
	}
	for _, test := range tests {
		match, ok := ResolveIdentity(test.callSign, test.affiliate)
		if ok != test.matched || (ok && match.Category != LocalPublic) {
			t.Errorf("ResolveIdentity(%q, %q) = %+v, %v", test.callSign, test.affiliate, match, ok)
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
