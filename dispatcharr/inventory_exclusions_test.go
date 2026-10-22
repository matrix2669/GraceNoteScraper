package dispatcharr

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func inventoryTarget(region string) FeedTarget {
	return FeedTarget{ID: region, Name: "CODE" + region, Country: "US", Feed: FeedIdentity{Family: "example network", Region: region, Evidence: "Example Network - " + region}}
}

func TestInventoryFeedRulesBothDirectionsAndAmbiguity(t *testing.T) {
	cases := []struct {
		raw, target string
		want        bool
	}{
		{"US: Example Network WEST HD", "east", true}, {"TV| Example Network PACIFIC ᴿᴬᵂ", "east", true},
		{"US| Example Network EAST SD", "west", true}, {"US: Example Network WEST HD", "west", false},
		{"US: Example Network HD", "west", false}, {"US: Example Network EAST WEST HD", "east", false},
		{"US: Example Network EXTRA WEST HD", "east", false}, {"US: Example Network Spanish WEST HD", "east", false},
		{"CA: Example Network WEST HD", "east", false}, {"UK| Example Network WEST HD", "east", false},
		{"US: Example Network WESTERNS HD", "east", false}, {"US: Example Network DT2 WEST HD", "east", false},
		{"US: Example Network WEST HD", "unknown", false}, {"US: Example Network P HD", "east", false},
	}
	for _, tc := range cases {
		t.Run(tc.raw+tc.target, func(t *testing.T) {
			streams := ProjectIdentityInventory([]Stream{{ID: 1, Name: tc.raw, M3UAccountID: 3}})
			rows := DeriveInventoryFeedExclusions("source", streams, []FeedTarget{inventoryTarget(tc.target)}, nil, nil, nil)
			if (len(rows) == 1) != tc.want {
				t.Fatalf("unexpected result: %+v", rows)
			}
		})
	}
	// Bare regional station labels still do not establish a time-shift target.
	target := inventoryTarget("east")
	target.Feed = ParseFeedHint("Example Network West")
	if got := DeriveInventoryFeedExclusions("source", ProjectIdentityInventory([]Stream{{Name: "US: Example Network EAST HD"}}), []FeedTarget{target}, nil, nil, nil); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestInventoryUnreturnedNamesHaveNoScoresOrReviewCandidates(t *testing.T) {
	streams := ProjectIdentityInventory([]Stream{{ID: 1, Name: "US: Example Network WEST HD", M3UAccountID: 3}, {ID: 2, Name: "US: Example Network HD", M3UAccountID: 3}})
	for _, score := range []int{70, 94, 95, 100} {
		c := Candidate{Key: "scored", ChannelID: "east", StreamName: streams[0].Name, StreamHash: streams[0].Fingerprint, Source: "source", M3UAccountID: 3, Score: score, NameScore: score}
		before := c
		scored := DeriveAutomaticExclusions([]Candidate{c}, nil, map[string]FeedIdentity{"east": inventoryTarget("east").Feed}, nil, nil)
		if len(scored.Rows) != 1 || len(DeriveInventoryFeedExclusions("source", streams, []FeedTarget{inventoryTarget("east")}, nil, nil, scored.Rows)) != 0 || c != before {
			t.Fatal("changed/duplicated scored pair", score)
		}
	}
	rows := DeriveInventoryFeedExclusions("source", streams, []FeedTarget{inventoryTarget("east")}, nil, nil, nil)
	if len(rows) != 1 || rows[0].StreamName != streams[0].Name {
		t.Fatal(rows)
	}
	b, _ := json.Marshal(rows)
	if strings.Contains(string(b), `"score"`) || strings.Contains(string(b), `"Score"`) || strings.Contains(string(b), "PendingReview") {
		t.Fatal("invented scored review row", string(b))
	}
}

func TestInventoryLiteralConfirmationsAccountsAndUndo(t *testing.T) {
	streams := ProjectIdentityInventory([]Stream{{ID: 1, Name: "US: Example Network WEST HD", M3UAccountID: 3}, {ID: 2, Name: "US: Example Network WEST HD", M3UAccountID: 7}, {ID: 3, Name: "US| Example Network WEST HD", M3UAccountID: 7}})
	d := Decision{ChannelID: "east", Source: "older-source", StreamHash: streams[0].Fingerprint, Decision: "confirmed"}
	rows := DeriveInventoryFeedExclusions("source", streams, []FeedTarget{inventoryTarget("east")}, nil, []Decision{d}, nil)
	conflicts := 0
	for _, r := range rows {
		if r.Conflict {
			conflicts++
		}
	}
	if conflicts != 2 || len(rows) != 3 {
		t.Fatal(rows)
	}
	// Undo removes only the saved override, recomputing the derived constraint.
	for _, r := range DeriveInventoryFeedExclusions("source", streams, []FeedTarget{inventoryTarget("east")}, nil, nil, nil) {
		if r.Conflict {
			t.Fatal("undo did not recompute", r)
		}
	}
	d.Decision = "denied"
	d.Source = "source"
	for _, r := range DeriveInventoryFeedExclusions("source", streams, []FeedTarget{inventoryTarget("east")}, nil, []Decision{d}, nil) {
		if r.Conflict {
			t.Fatal("Deny disabled constraint", r)
		}
	}
}

func TestInventoryProjectionAndOrderAreStable(t *testing.T) {
	n := 7.0
	streams := []Stream{{ID: 1, Name: "US: Example Network WEST HD", M3UAccountID: 3, TVGID: "private-unneeded-id", StreamChannelNo: &n}, {ID: 2, Name: "TV| Example Network EAST ᴿᴬᵂ", M3UAccountID: 7}}
	projected := ProjectIdentityInventory(streams)
	original := append([]IdentityStream(nil), projected...)
	streams[0].Name = "changed"
	n = 99
	if !reflect.DeepEqual(projected, original) {
		t.Fatal("projection retained mutable source")
	}
	if b, _ := json.Marshal(projected); strings.Contains(string(b), "private-unneeded-id") {
		t.Fatal("unneeded metadata retained")
	}
	targets := []FeedTarget{inventoryTarget("east"), inventoryTarget("west")}
	want := DeriveInventoryFeedExclusions("source", projected, targets, nil, nil, nil)
	projected[0], projected[1] = projected[1], projected[0]
	targets[0], targets[1] = targets[1], targets[0]
	if got := DeriveInventoryFeedExclusions("source", projected, targets, nil, nil, nil); !reflect.DeepEqual(got, want) {
		t.Fatal("nondeterministic constraints")
	}
}
