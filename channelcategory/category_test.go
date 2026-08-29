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
