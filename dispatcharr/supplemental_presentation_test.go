package dispatcharr

import (
	"strings"
	"testing"
)

func TestSupplementalPresentationUserExamples(t *testing.T) {
	cases := []struct {
		target, callsign string
		good, bad        []string
	}{
		{"BBC America - East", "BBCAHD", []string{"BBC AMERICA"}, []string{"BBC Comedy", "BBC Drama", "BBC Earth", "BBC Food"}},
		{"Bounce TV", "WPXNDT2", []string{"BOUNCE"}, []string{"Bounce XL"}},
		{"Cinemax - East", "MAXHD", []string{"CINEMAX"}, []string{"Cinemax Action", "Cinemax Classics", "Cinemax Hits", "HBO MAX"}},
		{"CMT - East", "CMTVHD", []string{"CMT"}, []string{"CMT Music"}},
		{"Comedy Central - East", "CCHD", []string{"COMEDY CENTRAL"}, []string{"Comedy Central Pluto TV"}},
		{"Disney Junior", "DJCH", []string{"DISNEY JR"}, []string{"Disney Channel", "Disney XD"}},
		{"Disney XD - East", "DXDHD", []string{"DISNEY XD"}, []string{"Disney Channel", "Disney Junior"}},
		{"E! - East", "EHD", []string{"E!"}, []string{"E", "E! News"}},
		{"FX", "FXHD", []string{"FX"}, []string{"FXM", "FXX"}},
		{"ION(WPXN)", "WPXNDT", []string{"ION"}, []string{"ION Plus", "ION Mystery"}},
		{"Law & Crime Network", "LCSTR", []string{"LAW & CRIME"}, []string{"Law & Crime Rewind"}},
		{"MeTV(WJLP)", "WJLPDT", []string{"METV"}, []string{"MeTV Plus", "MeTV Toons"}},
		{"START TV NETWORK", "WCBSDT2", []string{"START TV"}, []string{"Start TV Extra"}},
		{"Sundance TV", "SUNDHD", []string{"SUNDANCE"}, []string{"Sundance Movies"}},
		{"UP", "UPHD", []string{"UPTV"}, []string{"UP EXTRA", "UP 2"}},
	}
	for _, tc := range cases {
		t.Run(tc.target, func(t *testing.T) {
			target := SupplementalTarget{ID: "target", CallSign: tc.callsign, Country: "US", Labels: []SupplementalLabel{{tc.target, "independent fixture"}}}
			streams := []Stream{}
			for i, name := range append(append([]string{}, tc.good...), tc.bad...) {
				streams = append(streams, Stream{ID: int64(i + 1), M3UAccountID: 3, Name: name})
			}
			got, complete := RetrieveSupplemental([]SupplementalTarget{target}, ProjectIdentityInventory(streams), nil)
			if !complete || len(got) != len(tc.good) {
				t.Fatalf("unexpected candidates: %+v", got)
			}
			for _, c := range got {
				if c.Stream.ID > int64(len(tc.good)) || c.Stream.Name != tc.good[c.Stream.ID-1] {
					t.Fatalf("changed raw identity/noise: %+v", c)
				}
			}
		})
	}
}

func TestSupplementalPresentationBoundaries(t *testing.T) {
	same := [][2]string{{"Cinemax East", "Cinemax"}, {"Cinemax - East HD", "Cinemax"}, {"Disney Jr", "Disney Junior"}, {"MeTV", "ME TV NETWORK"}, {"UP", "UPTV"}}
	for _, p := range same {
		if supplementalPresentationKey(p[0], "") != supplementalPresentationKey(p[1], "") {
			t.Errorf("not equivalent: %q %q", p[0], p[1])
		}
	}
	different := [][2]string{{"Cinemax West", "Cinemax"}, {"Cinemax Pacific", "Cinemax"}, {"Cinemax East West", "Cinemax"}, {"E!", "E"}, {"FX", "FXX"}, {"HBO MAX", "MAX"}, {"Disney Channel", "Disney Junior"}, {"ION Plus", "ION"}}
	for _, p := range different {
		if supplementalPresentationKey(p[0], "") == supplementalPresentationKey(p[1], "") {
			t.Errorf("collapsed: %q %q", p[0], p[1])
		}
	}
	if supplementalPresentationKey("ION(WPXN)", "WJLPDT") == supplementalPresentationKey("ION", "WJLPDT") {
		t.Fatal("wrong affiliate wrapper stripped")
	}
	if supplementalPresentationKey("ION(WPXN)", "WPXNDT") != supplementalPresentationKey("ION", "WPXNDT") {
		t.Fatal("matching affiliate wrapper not stripped")
	}
	target := SupplementalTarget{ID: "x", Country: "US", Labels: []SupplementalLabel{{"FX", ""}}}
	got, _ := RetrieveSupplemental([]SupplementalTarget{target}, ProjectIdentityInventory([]Stream{{ID: 1, Name: "FX"}}), nil)
	if len(got) != 0 {
		t.Fatal("unattributed short label admitted")
	}
}

func TestBBCNewsIsOnlyAnExactStationScopedUncertainReviewLead(t *testing.T) {
	mk := func(id, callsign string) SupplementalTarget {
		return SupplementalTarget{ID: "target", StationID: id, CallSign: callsign, Country: "US", Labels: []SupplementalLabel{{"BBC World News", "current station attribution"}}}
	}
	inventory := ProjectIdentityInventory([]Stream{{ID: 1, Name: "BBC NEWS"}, {ID: 2, Name: "BBC COMEDY"}, {ID: 3, Name: "BBC NEWS EXTRA"}, {ID: 4, Name: "FR: BBC NEWS"}, {ID: 5, Name: "BBC NEWS TV"}})
	got, _ := RetrieveSupplemental([]SupplementalTarget{mk("89690", "BBCNAHD")}, inventory, nil)
	if len(got) != 1 || got[0].Stream.ID != 1 || !strings.Contains(got[0].Reason, "identity unverified") || !strings.Contains(got[0].Evidence, "overlap have not been independently verified") {
		t.Fatalf("review evidence: %+v", got)
	}
	for _, target := range []SupplementalTarget{mk("89542", "BBCNAHD"), mk("89690", "OTHER")} {
		if rows, _ := RetrieveSupplemental([]SupplementalTarget{target}, inventory, nil); len(rows) != 0 {
			t.Fatalf("scope leaked: %+v", rows)
		}
	}
}

func TestRecognizedProviderPrefixRoutesExamplesToOrdinaryReview(t *testing.T) {
	pairs := []struct{ id, station, call, label, raw string }{
		{"bbca", "64492", "BBCAHD", "BBC America - East", "BBC AMERICA"},
		{"bbc", "89690", "BBCNAHD", "BBC World News", "BBC NEWS"},
		{"bounce", "54618", "WPXNDT2", "BOUNCE TV", "BOUNCE"},
		{"max", "34933", "MAXHD", "Cinemax - East", "CINEMAX"},
		{"cmt", "59440", "CMTVHD", "CMT - East", "CMT"},
		{"comedy", "62420", "CCHD", "Comedy Central - East", "COMEDY CENTRAL"},
		{"jr", "74796", "DJCH", "Disney Junior", "DISNEY JR"},
		{"xd", "60006", "DXDHD", "Disney XD - East", "DISNEY XD"},
		{"e", "61812", "EHD", "E! - East", "E!"},
		{"fx", "58574", "FXHD", "FX", "FX"},
		{"ion", "43818", "WPXNDT", "ION(WPXN)", "ION"},
		{"law", "109553", "LCSTR", "Law & Crime Network", "LAW & CRIME"},
		{"metv", "75523", "WJLPDT", "MeTV(WJLP)", "METV"},
		{"start", "74916", "WCBSDT2", "START TV NETWORK", "START TV"},
		{"sundance", "71280", "SUNDHD", "Sundance TV", "SUNDANCE"},
		{"up", "66143", "UPHD", "UP", "UPTV"},
		{"a", "a", "A", "Alpha Network", "ALPHA NETWORK"},
		{"b", "b", "B", "Bravo Network", "BRAVO NETWORK"},
		{"c", "c", "C", "Charlie Network", "CHARLIE NETWORK"},
	}
	group := int64(1715)
	targets := []SupplementalTarget{}
	streams := []Stream{}
	for i, p := range pairs {
		targets = append(targets, SupplementalTarget{ID: p.id, StationID: p.station, CallSign: p.call, Country: "US", Labels: []SupplementalLabel{{p.label, "attributed fixture"}}})
		streams = append(streams, Stream{ID: int64(i + 1), M3UAccountID: 3, ChannelGroupID: &group, Name: "AT&T: " + p.raw + " ᴿᴬᵂ"})
	}
	rows, complete := RetrieveSupplemental(targets, ProjectIdentityInventory(streams), nil)
	if !complete {
		t.Fatal("incomplete")
	}
	found := map[string]bool{}
	for _, row := range rows {
		if strings.Contains(row.Reason, "repeated provider prefix") {
			found[row.Target.ID+"/"+row.Stream.Name] = true
		}
	}
	for _, p := range pairs {
		if !found[p.id+"/AT&T: "+p.raw+" ᴿᴬᵂ"] {
			t.Errorf("missing %s / %s", p.id, p.raw)
		}
	}
}

func TestSupplementalNewViewsPreservePrimaryAndSafetyFilters(t *testing.T) {
	target := SupplementalTarget{ID: "fx", Country: "US", Labels: []SupplementalLabel{{"FX - East", "fixture attribution"}}}
	streams := []Stream{{ID: 1, M3UAccountID: 3, Name: "US: FX"}, {ID: 2, M3UAccountID: 3, Name: "FR: FX"}, {ID: 3, M3UAccountID: 3, Name: "FX EVENTS 1"}, {ID: 4, M3UAccountID: 3, Name: "### FX ###"}, {ID: 5, M3UAccountID: 3, Name: "FX EAST WEST"}}
	inventory := ProjectIdentityInventory(streams)
	primary := []Candidate{{ChannelID: "fx", StreamHash: streams[0].Fingerprint()}}
	rows, complete := RetrieveSupplemental([]SupplementalTarget{target}, inventory, primary)
	if !complete || len(rows) != 0 {
		t.Fatalf("primary pair or unsafe row returned: %+v", rows)
	}
	if rows, _ := RetrieveSupplemental([]SupplementalTarget{target}, inventory, nil); len(rows) != 1 || rows[0].Stream.ID != 1 {
		t.Fatalf("valid/default pair lost: %+v", rows)
	}
}

func TestEastDefaultDoesNotEraseUnattributedRegionalBrands(t *testing.T) {
	target := SupplementalTarget{ID: "sports", Country: "US", Labels: []SupplementalLabel{{"Fox Sports", "attributed service"}}}
	rows, _ := RetrieveSupplemental([]SupplementalTarget{target}, ProjectIdentityInventory([]Stream{{ID: 1, Name: "Fox Sports East"}}), nil)
	if len(rows) != 0 {
		t.Fatalf("regional brand silently treated as feed: %+v", rows)
	}
	target.Feed = FeedIdentity{Family: "fox sports", Region: "east", Evidence: "station-bound fixture"}
	rows, _ = RetrieveSupplemental([]SupplementalTarget{target}, ProjectIdentityInventory([]Stream{{ID: 1, Name: "Fox Sports East"}}), nil)
	if len(rows) != 1 {
		t.Fatalf("station-bound east/default not retrieved: %+v", rows)
	}
}

func TestPresentationRetainsPlusAndSubchannelIdentity(t *testing.T) {
	for _, pair := range [][2]string{{"ESPN", "ESPN+"}, {"MGM", "MGM+"}, {"ION", "ION+"}} {
		if supplementalPresentationKey(pair[0], "") == supplementalPresentationKey(pair[1], "") {
			t.Fatalf("plus lost: %q / %q", pair[0], pair[1])
		}
		target := SupplementalTarget{ID: "t", Country: "US", Labels: []SupplementalLabel{{pair[0], "attributed"}}}
		rows, _ := RetrieveSupplemental([]SupplementalTarget{target}, ProjectIdentityInventory([]Stream{{ID: 1, Name: pair[1]}}), nil)
		if len(rows) != 0 {
			t.Fatalf("plus service proposed as base: %+v", rows)
		}
	}
	for _, tc := range []struct {
		wrapper, target string
		want            bool
	}{
		{"WPXN", "WPXNDT2", true}, {"WPXN-DT2", "WPXNDT2", true}, {"WPXNDT3", "WPXNDT2", false}, {"WPXNDT2", "WPXN", false}, {"WJLP", "WPXNDT2", false},
	} {
		if got := supplementalWrapperMatches(tc.wrapper, tc.target); got != tc.want {
			t.Errorf("%s / %s: %t", tc.wrapper, tc.target, got)
		}
		key := supplementalPresentationKey("ION ("+tc.wrapper+")", tc.target)
		if (key == supplementalPresentationKey("ION", tc.target)) != tc.want {
			t.Errorf("wrapper unexpectedly %q for %s / %s", key, tc.wrapper, tc.target)
		}
	}
}
