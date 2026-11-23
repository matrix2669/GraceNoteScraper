package dispatcharr

import (
	"fmt"
	"reflect"
	"testing"
)

func affiliateCandidate(name string) Candidate {
	return Candidate{Key: name, ChannelID: "502", StreamName: name, NameScore: 100, Score: 100, Source: "source-A", StreamHash: name, NormalizedAlias: NormalizeAliasName(name), M3UAccountID: 3}
}

func TestAffiliateExclusionPlanReportedNamesAndVariants(t *testing.T) {
	var candidates []Candidate
	for _, call := range []string{"KTVT", "KTXA", "KWTV", "KYW", "WBBM", "WBTV", "WBZ"} {
		for _, name := range []string{"US: CBS (" + call + ")", "US: CBS [" + call + "-DT] HD"} {
			for _, account := range []int64{3, 4} {
				c := affiliateCandidate(name)
				c.M3UAccountID = account
				candidates = append(candidates, c)
			}
		}
	}
	before := append([]Candidate(nil), candidates...)
	plan := PlanAffiliateExclusions(candidates, map[string]AffiliateTarget{"502": {Country: "US", CallSign: "WCBSDT"}}, nil)
	if len(plan.Suggestions) != 28 || len(plan.Manual) != 0 {
		t.Fatalf("plan %+v", plan)
	}
	if !reflect.DeepEqual(candidates, before) {
		t.Fatal("mutated candidates")
	}
	for _, s := range plan.Suggestions {
		if s.TargetCallSign != "WCBS" || s.Candidate.StreamName == "" {
			t.Fatal(s)
		}
	}
}

func TestAffiliateExclusionConservativeBoundaries(t *testing.T) {
	tests := []struct {
		target, name string
		score        int
		want         bool
	}{
		{"WCBSDT", "US: CBS (KTVT)", 95, true},
		{"WCBSDT", "US: CBS (KTVT)", 94, false},
		{"WCBSDT", "US: CBS (WCBS)", 100, false},
		{"WCBS-TV", "WCBS-DT", 100, false},
		{"WCBSDT", "US: CBS", 100, false},
		{"WCBSDT", "CBS (WCBS) (KTVT)", 100, false},
		{"WCBSDT", "CBS (KTVT) [WCBS-DT]", 100, false},
		{"WCBSDT", "CBS (KTVT) WCBSDT", 100, false},
		{"WCBSDT", "CBS (KTVT) (KTVT)", 100, false},
		{"WCBSDT", "CBS (KTVT-DT2)", 100, false},
		{"WCBSDT", "CBS (KTVT.2)", 100, false},
		{"WCBSDT2", "CBS (KTVT)", 100, false},
		{"WCBSDT", "CBS (KTVT) WEST", 100, false},
		{"WCBSDT", "CBS KTVT", 100, false},
		{"WCBSDT", "Wild (WILD)", 100, false},
		{"WCBSDT", "CBS (KING)", 100, false},
		{"WCBSDT", "KYW", 100, true},
		{"WCBSDT", "US: CBS (KYWDT)", 100, true},
		{"WCBSDT", "US: CBS (WBZ-TV)", 100, true},
		{"WNBCDT", "US: NBC (KXAS)", 100, true},
		{"WABCDT", "US: ABC (KABC)", 100, true},
		{"WNYWDT", "US: FOX (KTTV)", 100, true},
		{"WCBSDT", "US: WRONG LABEL (KTVT)", 100, true},
		{"Discovery Channel", "US: Discovery Channel", 100, false},
		{"CBS", "US: CBS (KTVT)", 100, false},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			c := affiliateCandidate(tt.name)
			c.NameScore = tt.score
			p := PlanAffiliateExclusions([]Candidate{c}, map[string]AffiliateTarget{"502": {Country: "US", CallSign: tt.target}}, nil)
			if (len(p.Suggestions) == 1) != tt.want {
				t.Fatalf("%+v: %+v", tt, p)
			}
		})
	}
}

func TestAffiliateExclusionManualChoicesAndSourceIsolation(t *testing.T) {
	c := affiliateCandidate("US: CBS (KTVT)")
	targets := map[string]AffiliateTarget{"502": {Country: "US", CallSign: "WCBSDT"}}
	for _, kind := range []string{"confirmed", "denied"} {
		d := Decision{ChannelID: c.ChannelID, Source: c.Source, StreamHash: c.StreamHash, Decision: kind}
		if len(PlanAffiliateExclusions([]Candidate{c}, targets, []Decision{d}).Suggestions) != 0 {
			t.Fatal("overrode manual decision")
		}
		d.StreamHash = "old-variant"
		d.NormalizedAlias = c.NormalizedAlias
		if len(PlanAffiliateExclusions([]Candidate{c}, targets, []Decision{d}).Suggestions) != 0 {
			t.Fatal("overrode reviewed group")
		}
		d.NormalizedAlias = ""
		d.StreamName = c.StreamName
		if len(PlanAffiliateExclusions([]Candidate{c}, targets, []Decision{d}).Suggestions) != 0 {
			t.Fatal("overrode legacy reviewed group")
		}
		d.Source = "source-B"
		if len(PlanAffiliateExclusions([]Candidate{c}, targets, []Decision{d}).Suggestions) != 1 {
			t.Fatal("old source affected current")
		}
	}
	if len(PlanAffiliateExclusions([]Candidate{c}, targets, nil).Suggestions) != 1 {
		t.Fatal("undo did not restore suggestion")
	}
	targets["502"] = AffiliateTarget{Country: "CA", CallSign: "WCBSDT"}
	if len(PlanAffiliateExclusions([]Candidate{c}, targets, nil).Suggestions) != 0 {
		t.Fatal("country assumption leaked")
	}
	if len(PlanAffiliateExclusions([]Candidate{c}, nil, nil).Suggestions) != 0 {
		t.Fatal("missing current target accepted")
	}
	targets["502"] = AffiliateTarget{Country: "US", CallSign: "KTVTDT"}
	if len(PlanAffiliateExclusions([]Candidate{c}, targets, nil).Suggestions) != 0 {
		t.Fatal("source switch retained old target")
	}
}

func TestAffiliateExclusionMixedGroupStaysManual(t *testing.T) {
	a := affiliateCandidate("US: CBS (KTVT)")
	b := affiliateCandidate("US: CBS (KTVT) (WCBS)")
	a.NormalizedAlias = "same"
	b.NormalizedAlias = "same"
	plan := PlanAffiliateExclusions([]Candidate{a, b}, map[string]AffiliateTarget{"502": {Country: "US", CallSign: "WCBSDT"}}, nil)
	if len(plan.Suggestions) != 0 || len(plan.Manual) != 2 {
		t.Fatal(plan)
	}
}
