package dispatcharr

import (
	"strings"
	"testing"
)

func TestSupplementalRequiresCompleteAttributedIdentity(t *testing.T) {
	cases := []struct {
		label     string
		good, bad []string
	}{
		{"NFL", []string{"US: NFL HD"}, []string{"NFL REPLAY 1", "NFL TEAMS CBS PANTHERS", "NFL NETWORK (N556) RICHMOND"}},
		{"MAX", []string{"US: MAX HD"}, []string{"FR: BEIN SPORTS MAX 4", "FR BEIN SPORTS MAX 4", "CNN MAX", "MAX SPORTS EVENTS 5", "MAX 2", "#### MAX ####"}},
		{"ESPN", []string{"US: ESPN HD"}, []string{"ESPN 2", "ESPN2", "ESPN PLAY EVENTS 58", "NL: ESPN 1 ZIGGO", "NL ESPN 4"}},
		{"Movies", []string{"Movies HD"}, []string{"FilmRise Free Movies", "At The Movies", "Miramax Movies", "=== Movies ===", "||| Movies |||"}},
		{"News 12", []string{"News 12 New York", "News12 Plus"}, []string{"News 12 Replay", "News 12 Movies", "News 12 4"}},
		{"AccuWeather", []string{"ACCUWEATHER"}, []string{"AccuWeather NY", "AccuWeather Florida"}},
		{"Showtime Extreme", []string{"873 Showtime Extrem"}, []string{"Showtime Extr", "Showtime Extreme 2", "Showtime Extreme Event 1"}},
		{"Great American Living", []string{"US| GREAT AMERICAN LIVING HD"}, []string{"Great American Family", "Great American Living Extra"}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			target := SupplementalTarget{ID: "t", Country: "US", Labels: []SupplementalLabel{{tc.label, "attributed"}}}
			streams := []Stream{}
			for i, name := range append(append([]string{}, tc.good...), tc.bad...) {
				streams = append(streams, Stream{ID: int64(i + 1), Name: name})
			}
			got, complete := RetrieveSupplemental([]SupplementalTarget{target}, ProjectIdentityInventory(streams), nil)
			if !complete || len(got) != len(tc.good) {
				t.Fatalf("retrieval %+v", got)
			}
			for _, row := range got {
				if row.Stream.ID > int64(len(tc.good)) || row.Descriptor != tc.label {
					t.Fatalf("noise/provenance: %+v", row)
				}
			}
		})
	}
	target := SupplementalTarget{ID: "ny", Country: "US", Labels: []SupplementalLabel{{"AccuWeather NY", "exact station descriptor"}}}
	got, _ := RetrieveSupplemental([]SupplementalTarget{target}, ProjectIdentityInventory([]Stream{{ID: 1, Name: "619 AccuWeather NY"}, {ID: 2, Name: "AccuWeather NY Extra"}, {ID: 3, Name: "AccuWeather NY", GroupName: "FR | Weather"}, {ID: 4, Name: "NL: AccuWeather NY"}}), nil)
	if len(got) != 1 || got[0].Stream.ID != 1 {
		t.Fatalf("full descriptor/country: %+v", got)
	}
}

func TestTargetAwareInternalFeedKeepsFullFamilyAndAmbiguity(t *testing.T) {
	east := FeedIdentity{Family: "hbo hits", FamilyAliases: []string{"hbo2", "hbo 2"}, Region: "east", Evidence: "USER-ATTESTED fixture"}
	cases := []struct{ raw, region string }{
		{"RK: HBO EAST 2", "east"}, {"RK: HBO WEST 2 HD", "west"}, {"HBO Pacific 2", "west"},
		{"WEST HBO 2", ""}, {"US: HBO 2 HD", ""}, {"HBO EAST 3", ""}, {"HBO EAST 2 PLUS", ""}, {"HBO EAST WEST 2", ""}, {"HBO EAST EAST 2", ""},
	}
	for _, tc := range cases {
		parsed := StreamFeedForTarget(tc.raw, nil, east)
		if parsed.Region != tc.region {
			t.Errorf("%s: %+v", tc.raw, parsed)
		}
	}
	for _, raw := range []string{"Fox Sports West", "HBO EAST 2"} {
		other := StreamFeedForTarget(raw, nil, FeedIdentity{Family: "fox sports west", Region: "east"})
		if sameFeedFamily(other.Family, FeedIdentity{Family: "fox sports west"}) && other.Region != "" {
			t.Fatalf("geographic brand became feed: %+v", other)
		}
	}
	codes := map[string]FeedIdentity{"HBO2": {Family: "hbo2", Region: "west"}}
	conflict := StreamFeedForTarget("HBO2 EAST", codes, east)
	if conflict.Region != "" || !strings.Contains(conflict.Evidence, "conflict") {
		t.Fatalf("code conflict lost: %+v", conflict)
	}
	streams := []Stream{{ID: 1, Name: "RK: HBO WEST 2"}, {ID: 2, Name: "RK: HBO EAST 2"}, {ID: 3, Name: "US: HBO 2 HD"}, {ID: 4, Name: "HBO EAST WEST 2"}, {ID: 5, Name: "HBO WEST 2 PLUS"}}
	candidates := []Candidate{}
	for _, s := range streams {
		candidates = append(candidates, Candidate{Key: s.Name, ChannelID: "t", Source: "s", StreamName: s.Name, StreamHash: s.Fingerprint(), Score: 100})
	}
	auto := DeriveAutomaticExclusions(candidates, nil, map[string]FeedIdentity{"t": east}, nil, nil)
	if len(auto.Rows) != 4 || auto.Rows[0].PendingReview || !auto.Hidden[streams[0].Name] {
		t.Fatalf("automatic: %+v", auto)
	}
	inventory := DeriveInventoryFeedExclusions("s", ProjectIdentityInventory(streams), []FeedTarget{{ID: "t", Country: "US", Feed: east}}, nil, nil, nil)
	if len(inventory) != 1 || inventory[0].StreamName != streams[0].Name {
		t.Fatalf("inventory: %+v", inventory)
	}
}

func TestSupplementalKeepsTargetFeedUncertaintyVisible(t *testing.T) {
	target := SupplementalTarget{ID: "discovery", Country: "US", Labels: []SupplementalLabel{{"Discovery - East", "attributed source"}}, Feed: FeedIdentity{Family: "discovery", Region: "unknown", Evidence: "Conflicting station-bound feed assertions require manual review"}}
	rows, _ := RetrieveSupplemental([]SupplementalTarget{target}, ProjectIdentityInventory([]Stream{{ID: 1, Name: "US: DISCOVERY WEST HD"}}), nil)
	if len(rows) != 1 || !strings.Contains(rows[0].Reason, "target feed unresolved") || strings.Contains(rows[0].Reason, "opposing feed") {
		t.Fatalf("target uncertainty hidden or guessed: %+v", rows)
	}
}
