package dispatcharr

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSupplementalRetrievalIdentityAndSeparation(t *testing.T) {
	cases := []struct{ label, good, bad string }{
		{"Rewind TV", "462 WPIX Rewind", "US| CW PIX11 (WPIX) NEW YORK"},
		{"Mega TV", "US: Mega TV HD", "Mega Sports"},
		{"Great American Living", "TV| Great American Living HD", "Great American Family"},
		{"Nickelodeon West", "US| Nickelodeon West HD", "Nickelodeon East"},
		{"Cinemáx", "US| Cinemáx HD", "US| Cinemax HD"},
		{"Fox Soccer Plus", "US: Fox Soccer Plus HD", "Fox Sports 1 HD"},
		{"Showtime 2", "869 Showtime 2 HD", "24/7 Showtime 2"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			target := SupplementalTarget{ID: "target", CallSign: "WPIXDT4", Labels: []SupplementalLabel{{tc.label, "attributed fixture"}}, Country: "US"}
			streams := []Stream{{ID: 1, M3UAccountID: 3, Name: tc.good}, {ID: 2, M3UAccountID: 3, Name: tc.bad}}
			rows, complete := RetrieveSupplemental([]SupplementalTarget{target}, ProjectIdentityInventory(streams), nil)
			if !complete || len(rows) != 1 || rows[0].Stream.ID != 1 {
				t.Fatalf("retrieval: %+v", rows)
			}
			rows, _ = RetrieveSupplemental([]SupplementalTarget{target}, ProjectIdentityInventory(streams), []Candidate{{ChannelID: "target", StreamHash: streams[0].Fingerprint()}})
			if len(rows) != 0 {
				t.Fatal("primary physical pair duplicated")
			}
		})
	}
	targets := []SupplementalTarget{{ID: "news", Labels: []SupplementalLabel{{"News 12", "attributed"}}}, {ID: "plus", Labels: []SupplementalLabel{{"News 12 Plus", "attributed"}}}}
	streams := []Stream{{ID: 1, Name: "News12 New Jersey"}, {ID: 2, Name: "News 12 Long Island"}, {ID: 3, Name: "News12 Plus"}}
	rows, _ := RetrieveSupplemental(targets, ProjectIdentityInventory(streams), nil)
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Target.ID]++
		if r.Target.ID == "news" && r.Stream.ID == 3 && !strings.Contains(r.Reason, "distinct") {
			t.Fatal("Plus merged")
		}
	}
	if counts["news"] != 3 || counts["plus"] != 1 {
		t.Fatalf("regional/Plus: %v", counts)
	}
}

func TestSupplementalPhysicalDuplicatesAndNumbersAreNotIdentity(t *testing.T) {
	target := SupplementalTarget{ID: "east", Country: "US", Feed: FeedIdentity{Region: "east"}, Labels: []SupplementalLabel{{"Showtime 2", "source-bound station"}}}
	streams := []Stream{{ID: 869, Name: "869 Showtime 2", M3UAccountID: 12}, {ID: 870, Name: "870 Showtime 2", M3UAccountID: 12}, {ID: 869, Name: "869 Cooking", M3UAccountID: 99}, {ID: 5, Name: "869 Showtime 2", M3UAccountID: 99}}
	rows, _ := RetrieveSupplemental([]SupplementalTarget{target}, ProjectIdentityInventory(streams), nil)
	if len(rows) != 3 {
		t.Fatal(rows)
	}
	for _, r := range rows {
		if !strings.Contains(r.Reason, "feed unresolved") {
			t.Fatal("number promoted to feed evidence")
		}
	}
	// Same-account duplicate IDs remain distinct physical review records.
	streams = []Stream{{ID: 100, Name: "Fox Soccer Plus", M3UAccountID: 3}, {ID: 101, Name: "Fox Soccer Plus", M3UAccountID: 3}}
	target.Labels = []SupplementalLabel{{"Fox Soccer Plus", "attributed"}}
	rows, _ = RetrieveSupplemental([]SupplementalTarget{target}, ProjectIdentityInventory(streams), nil)
	if len(rows) != 2 || rows[0].Stream.Fingerprint == rows[1].Stream.Fingerprint {
		t.Fatal("duplicate physical streams collapsed")
	}
	b, _ := json.Marshal(rows)
	if strings.Contains(string(b), "Score") {
		t.Fatal("retrieval acquired a matcher score")
	}
}
