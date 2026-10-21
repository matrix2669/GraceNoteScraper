package dispatcharr

import (
	"fmt"
	"testing"
)

func TestAutomaticAffiliateExclusionsAndExplicitChoices(t *testing.T) {
	targets := map[string]AffiliateTarget{"502": {Country: "US", CallSign: "WCBSDT"}}
	var candidates []Candidate
	for _, call := range []string{"KTVT", "KTXA", "KWTV", "KYW", "WBBM", "WBTV", "WBZ"} {
		candidates = append(candidates, affiliateCandidate("US: CBS ("+call+")"))
	}
	for _, name := range []string{"US: CBS (WCBS)", "US: CBS", "US: CBS (WCBS) (KTVT)", "US: CBS (KTVT-DT2)", "US: CBS (KTVT) West"} {
		candidates = append(candidates, affiliateCandidate(name))
	}
	result := DeriveAutomaticExclusions(candidates, targets, nil, nil, nil)
	if len(result.Rows) != 7 || len(result.Hidden) != 7 {
		t.Fatalf("result %+v", result)
	}
	c := candidates[0]
	decision := Decision{Source: c.Source, ChannelID: c.ChannelID, StreamHash: c.StreamHash, StreamName: c.StreamName, Decision: "confirmed"}
	result = DeriveAutomaticExclusions(candidates, targets, nil, nil, []Decision{decision})
	conflict := 0
	for _, row := range result.Rows {
		if row.Conflict {
			conflict++
		}
	}
	if conflict != 1 || len(result.Hidden) != 6 {
		t.Fatal(result)
	}
	decision.Decision = "denied"
	if got := DeriveAutomaticExclusions(candidates, targets, nil, nil, []Decision{decision}); len(got.Rows) != 7 {
		t.Fatal(got)
	}
	decision.Source = "old-connection"
	decision.Decision = "confirmed"
	if got := DeriveAutomaticExclusions(candidates, targets, nil, nil, []Decision{decision}); len(got.Hidden) != 6 || !got.Rows[0].Conflict {
		t.Fatal("prior-source explicit confirmation silently overridden", got)
	}
	decision.Decision = "denied"
	if got := DeriveAutomaticExclusions(candidates, targets, nil, nil, []Decision{decision}); len(got.Hidden) != 7 {
		t.Fatal("old source affected current result")
	}
}
func TestAutomaticFeedPolicyAllFamiliesAndQuality(t *testing.T) {
	cases := []struct {
		name            string
		pending, hidden bool
	}{
		{"US: HBO East HD", false, true}, {"US: HBO West HD", false, false},
		{"US: HBO Pacific SD", false, false}, {"US: HBO (West) UHD", false, false},
		{"US: HBOHD", false, true}, {"US: HBOHDP", false, false},
		{"US: HBO", true, false}, {"US: HBO HD", true, false},
		{"US: HBO P HD", true, false}, {"US: HBO EAST WEST", true, false},
		{"US: HBOHDP (East)", true, false}, {"US: HBO Comedy HBOHDP", true, false}, {"US: HBOHDP (West)", false, false}, {"US: HBO Comedy Pacific", true, false},
	}
	for i, tt := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			c := affiliateCandidate(tt.name)
			feeds := map[string]FeedIdentity{"502": {Family: "hbo", Region: "west", Evidence: "HBO - West"}}
			codes := map[string]FeedIdentity{"HBOHD": {Family: "hbo", Region: "east"}, "HBOHDP": {Family: "hbo", Region: "west"}}
			result := DeriveAutomaticExclusions([]Candidate{c}, nil, feeds, codes, nil)
			if result.Hidden[c.Key] != tt.hidden {
				t.Fatal(result)
			}
			if tt.pending && (len(result.Rows) != 1 || !result.Rows[0].PendingReview) {
				t.Fatal(result)
			}
			if !tt.pending && !tt.hidden && len(result.Rows) != 0 {
				t.Fatal(result)
			}
		})
	}
	for _, family := range []string{"news24", "discovery kids", "sports extra", "music live", "international news"} {
		c := affiliateCandidate("US: " + family + " East HD")
		r := DeriveAutomaticExclusions([]Candidate{c}, nil, map[string]FeedIdentity{"502": {Family: family, Region: "west", Evidence: family + " - Pacific"}}, nil, nil)
		if !r.Hidden[c.Key] {
			t.Fatalf("category-specific policy for %s: %+v", family, r)
		}
	}
}
func TestFeedLabelEvidenceBoundaries(t *testing.T) {
	for _, name := range []string{"HBO - West", "Discovery Kids (Pacific)", "News24 - East HD", "Sports Extra West Feed", "Music Pacific"} {
		if ParseStationFeedLabel(name).Region == "" {
			t.Fatal(name)
		}
	}
	for _, name := range []string{"Fox Sports West", "STARZ ENCORE WESTERNS", "SHOP", "HBOHDP", "Western Channel", "HBO P HD"} {
		if ParseStationFeedLabel(name).Region != "" {
			t.Fatal("guessed station feed", name)
		}
	}
}
func TestAutomaticFeedManualThresholdAndConstituentScoping(t *testing.T) {
	c := affiliateCandidate("US: HBO HD")
	feeds := map[string]FeedIdentity{"502": {Family: "hbo", Region: "west", Evidence: "HBO - West"}}
	c.NameScore = 71
	r := DeriveAutomaticExclusions([]Candidate{c}, nil, feeds, nil, nil)
	if len(r.Rows) != 1 || !r.Rows[0].PendingReview || len(r.Hidden) != 0 {
		t.Fatal(r)
	}
	c.NameScore = 100
	d := Decision{Source: c.Source, ChannelID: c.ChannelID, StreamName: c.StreamName, Decision: "confirmed"}
	r = DeriveAutomaticExclusions([]Candidate{c}, nil, feeds, nil, []Decision{d})
	if len(r.Rows) != 1 || !r.Rows[0].Conflict {
		t.Fatal(r)
	}
	d.StreamName = "US: HBO"
	d.NormalizedAlias = c.NormalizedAlias
	r = DeriveAutomaticExclusions([]Candidate{c}, nil, feeds, nil, []Decision{d})
	if r.Rows[0].Conflict {
		t.Fatal("lossy alias expanded explicit confirmation")
	}
}

func TestFeedFamiliesRetainBrandPrefix(t *testing.T) {
	a := ParseStationFeedLabel("News24: Extra - West")
	b := ParseStationFeedLabel("Sports: Extra - West")
	if a.Family == b.Family || a.Family != "news24 extra" || b.Family != "sports extra" {
		t.Fatal(a, b)
	}
	stream := StreamFeed("Sports: Extra Pacific", nil)
	if stream.Family != "sports extra" {
		t.Fatal(stream)
	}
}

func TestAutomaticNegativesIndependentFromPositiveScoreAndManualDeny(t *testing.T) {
	for _, score := range []int{0, 70, 71, 80, 94, 95, 100} {
		c := affiliateCandidate("US: HBO East HD")
		c.NameScore = score
		feeds := map[string]FeedIdentity{"502": {Family: "hbo", Region: "west", Evidence: "HBO - West"}}
		denied := Decision{Source: c.Source, ChannelID: c.ChannelID, StreamName: c.StreamName, Decision: "denied"}
		r := DeriveAutomaticExclusions([]Candidate{c}, nil, feeds, nil, []Decision{denied})
		if len(r.Rows) != 1 || r.Rows[0].Conflict || !r.Hidden[c.Key] {
			t.Fatalf("score%d: %+v", score, r)
		}
		c = affiliateCandidate("US: HBO HD")
		c.NameScore = score
		r = DeriveAutomaticExclusions([]Candidate{c}, nil, feeds, nil, nil)
		if len(r.Rows) != 1 || !r.Rows[0].PendingReview || r.Hidden[c.Key] {
			t.Fatalf("unknown%d: %+v", score, r)
		}
		c = affiliateCandidate("US: CBS (KTVT)")
		c.NameScore = score
		r = DeriveAutomaticExclusions([]Candidate{c}, map[string]AffiliateTarget{"502": {Country: "US", CallSign: "WCBSDT"}}, nil, nil, nil)
		if len(r.Rows) != 1 || !r.Hidden[c.Key] {
			t.Fatalf("affiliate%d: %+v", score, r)
		}
	}
}
