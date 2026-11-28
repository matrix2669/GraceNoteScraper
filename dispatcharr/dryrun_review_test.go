package dispatcharr

import "testing"

func TestShortRawReviewIsLossless(t *testing.T) {
	names := []string{"US| FX HD", "US: FX HD", "US: A&E HD", "US| A&E HD", "US| ID HD"}
	candidates := []Candidate{}
	for i, n := range names {
		if NormalizeAliasName(n) != "" {
			t.Fatalf("fixture no longer short: %s", n)
		}
		candidates = append(candidates, Candidate{Key: n, ChannelID: "target", Source: "source", StreamName: n, M3UAccountID: int64(i), Score: 100})
	}
	groups := GroupCandidates(candidates)
	if len(groups) != 5 {
		t.Fatalf("lost/merged short reviews: %+v", groups)
	}
	// Opaque review identity never enters a candidate's positive alias.
	for _, c := range candidates {
		if c.NormalizedAlias != "" {
			t.Fatal("upgraded alias")
		}
	}
	candidates = append(candidates, candidates[0])
	if GroupCandidates(candidates)[0].StreamCount < 1 {
		t.Fatal("lost account")
	}
	if ReviewIdentity(names[0], "") == ReviewIdentity(names[2], "") {
		t.Fatal("unrelated short services merged")
	}
}
func TestRealFeedPresentationLabels(t *testing.T) {
	for _, family := range []string{"HBO", "CINEMAX", "FREEFORM", "DISCOVERY", "PARAMOUNT+ WITH SHOWTIME"} {
		for _, prefix := range []string{"TV: ", "TV| ", "US: ", "PRIME: ", "RK: "} {
			for _, quality := range []string{"HD", "ᴿᴬᵂ", "ᴴᴰ", "SD"} {
				got := StreamFeed(prefix+family+" EAST "+quality, nil)
				if got.Region != "east" || got.Family != feedFamily(family) {
					t.Fatalf("%s %s %s: %+v", prefix, family, quality, got)
				}
			}
		}
	}
	for _, raw := range []string{"TV: HBO EAST WEST ᴿᴬᵂ", "TV: HBO ᴿᴬᵂ", "Fox Sports West News"} {
		if got := StreamFeed(raw, nil); got.Region != "" {
			t.Fatalf("guessed feed: %s %+v", raw, got)
		}
	}
}

func TestLegacyReviewIdentityUsesExistingGrouping(t *testing.T) {
	raw := "US: Example Network HD"
	if ReviewIdentity(raw, "") != NormalizeAliasName(raw) {
		t.Fatal("legacy Undo grouping changed")
	}
}
