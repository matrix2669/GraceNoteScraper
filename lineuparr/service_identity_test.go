package lineuparr

import (
	"context"
	"fmt"
	"testing"
)

func identityProof(value, station string) ServiceIdentityEvidence {
	return ServiceIdentityEvidence{Value: value, SourceID: "fixture-official-lineup", SourceRowID: "row-service", RootStationID: station, RootLineupKey: "fixture-lineup", Policy: "provider-identity-v1"}
}

// Public synthetic source rows preserve the reported numbers/callsigns but
// contain no subscriber address, retained index, programme or reviewed decision.
func TestReportedFifteenServiceGroups(t *testing.T) {
	cases := [][5]string{
		{"199", "699", "FREEFRM", "FREFMHD", "Freeform"}, {"76", "576", "YES", "YESHDNY", "YES Network"}, {"53", "553", "FX", "FXHD", "FX"}, {"75", "575", "SEC", "SECH", "SEC Network"}, {"85", "585", "BIG10V", "BIG10VH", "Big Ten Network"}, {"86", "586", "MLBN", "MLBHD", "MLB Network"}, {"123", "623", "ID", "IDHD", "Investigation Discovery"}, {"144", "644", "OXYGEN", "OXYGNHD", "Oxygen True Crime"}, {"149", "649", "WE", "WEHD", "WE TV"}, {"165", "665", "HGTV", "HGTVD", "HGTV"}, {"190", "690", "COMEDY", "CCHD", "Comedy Central"}, {"196", "696", "E", "EHD", "E!"}, {"238", "738", "UP", "UPHD", "UP"}, {"241", "741", "TVLAND", "TVLNDHD", "TV Land"}, {"335", "835", "NFLNRZ", "NFLNRZD", "NFL RedZone"},
	}
	for _, c := range cases {
		t.Run(c[0]+"-"+c[1], func(t *testing.T) {
			cs := []DraftChannel{{ID: "a", StationID: "station-a", Number: c[0], CallSign: c[2], OriginalName: c[2], Included: true, ServiceEvidence: []ServiceIdentityEvidence{identityProof(c[4], "station-a")}}, {ID: "b", StationID: "station-b", Number: c[1], CallSign: c[3], OriginalName: c[3], Included: true, ServiceEvidence: []ServiceIdentityEvidence{identityProof(c[4], "station-b")}}, {ID: "repeat", StationID: "station-a", Number: "1700", CallSign: c[2], OriginalName: c[2], Included: false}}
			got := duplicateReviewGroups(cs, findDuplicateSuggestions(cs))
			if len(got) != 1 || len(got[0].ChannelIDs) != 3 {
				t.Fatalf("equivalence lost: %+v", got)
			}
			// Removing every quality marker must leave identity unchanged.
			cs[0].CallSign = "SOURCEA"
			cs[0].OriginalName = "Source A"
			cs[2].CallSign = "SOURCEA"
			cs[2].OriginalName = "Source A"
			cs[1].CallSign = "SOURCEB"
			cs[1].OriginalName = "Source B"
			got = duplicateReviewGroups(cs, findDuplicateSuggestions(cs))
			if len(got) != 1 || len(got[0].ChannelIDs) != 3 {
				t.Fatalf("identity depended on quality: %+v", got)
			}
		})
	}
}
func TestServiceIdentityRejectsAmbiguityAndQualifiers(t *testing.T) {
	for _, tc := range []struct {
		name, a, b, alias string
		third             bool
	}{
		{"east-west", "Foo East HD", "Foo West SD", "Foo", false}, {"time shift", "Foo +1", "Foo", "Foo", false}, {"regional YES", "YESHDNY", "YESHDLA", "YES Network", false}, {"subchannels", "WABCDT2", "WABCDT3", "ABC", false}, {"placeholder", "FEEDA", "FEEDB", "[seasonal programming]", false}, {"three distinct stations", "FEEDA", "FEEDB", "Generic Service", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs := []DraftChannel{{ID: "a", StationID: "A", CallSign: tc.a, ServiceEvidence: []ServiceIdentityEvidence{identityProof(tc.alias, "A")}}, {ID: "b", StationID: "B", CallSign: tc.b, ServiceEvidence: []ServiceIdentityEvidence{identityProof(tc.alias, "B")}}}
			if tc.third {
				cs = append(cs, DraftChannel{ID: "c", StationID: "C", CallSign: "FEEDC", ServiceEvidence: []ServiceIdentityEvidence{identityProof(tc.alias, "C")}})
			}
			if g := findDuplicateSuggestions(cs); len(g) != 0 {
				t.Fatalf("unsafe equivalence: %+v", g)
			}
		})
	}
}
func TestUnknownPolicyAndSuppressedIdentityDoNotGroup(t *testing.T) {
	for _, suppress := range []bool{false, true} {
		a, b := identityProof("Station Name", "A"), identityProof("Station Name", "B")
		cs := []DraftChannel{{ID: "a", StationID: "A", CallSign: "SOURCEA", ServiceEvidence: []ServiceIdentityEvidence{a}}, {ID: "b", StationID: "B", CallSign: "SOURCEB", ServiceEvidence: []ServiceIdentityEvidence{b}}}
		if suppress {
			cs[0].SuppressedAliasEvidence = []AliasEvidence{{Value: a.Value}}
		} else {
			cs[0].ServiceEvidence[0].Policy = "invented"
		}
		if len(findDuplicateSuggestions(cs)) != 0 {
			t.Fatal("untrusted/suppressed evidence grouped")
		}
	}
}
func TestIdentityBuildPreservesReviewedChoices(t *testing.T) {
	svc := newTestService(t, "", "")
	yes, no := true, false
	if e := svc.store.SetIncluded("source", []string{"a"}, yes); e != nil {
		t.Fatal(e)
	}
	if e := svc.store.SetIncluded("source", []string{"repeat"}, no); e != nil {
		t.Fatal(e)
	}
	inputs := []InputChannel{}
	for i, station := range []string{"A", "B", "A"} {
		id := []string{"a", "b", "repeat"}[i]
		p := identityProof("Reviewed Service", station)
		inputs = append(inputs, InputChannel{Key: id, StationID: station, Number: fmt.Sprint(i + 1), CallSign: "SOURCE" + station, ExternalAliases: []AttributedAlias{{Value: p.Value, Source: p.SourceID, Method: "synthetic exact row", Identity: &p}}})
	}
	d, e := svc.Build(context.Background(), LineupContext{SourceFingerprint: "source"}, inputs)
	if e != nil {
		t.Fatal(e)
	}
	if len(d.DuplicateGroups) != 1 || d.DuplicateGroups[0].KeepID != "a" {
		t.Fatalf("review keeper lost: %+v", d.DuplicateGroups)
	}
	for _, c := range d.Channels {
		if c.ID == "repeat" && c.Included {
			t.Fatal("excluded repeat restored")
		}
	}
}

func TestTwoAliasesCannotBridgeCompetingServices(t *testing.T) {
	cs := []DraftChannel{{ID: "a", StationID: "A", CallSign: "SOURCEA", ServiceEvidence: []ServiceIdentityEvidence{identityProof("Alpha Service", "A")}}, {ID: "b", StationID: "B", CallSign: "SOURCEB", ServiceEvidence: []ServiceIdentityEvidence{identityProof("Alpha Service", "B"), identityProof("Beta Service", "B")}}, {ID: "c", StationID: "C", CallSign: "SOURCEC", ServiceEvidence: []ServiceIdentityEvidence{identityProof("Beta Service", "C")}}}
	if got := findDuplicateSuggestions(cs); len(got) != 0 {
		t.Fatal("contradicted middle station bridged services", got)
	}
}
func TestCompactTimeShiftsDoNotJoinUnshiftedService(t *testing.T) {
	for _, v := range []string{"Foo+1", "Foo +24", "FOO+2"} {
		cs := []DraftChannel{{ID: "a", StationID: "A", CallSign: v, ServiceEvidence: []ServiceIdentityEvidence{identityProof("Foo", "A")}}, {ID: "b", StationID: "B", CallSign: "FOO", ServiceEvidence: []ServiceIdentityEvidence{identityProof("Foo", "B")}}}
		if len(findDuplicateSuggestions(cs)) != 0 {
			t.Fatal("time shift collapsed", v)
		}
	}
}

func TestRegionAndTimeShiftAreIndependent(t *testing.T) {
	cs := []DraftChannel{{ID: "a", StationID: "A", CallSign: "Foo East+1", ServiceEvidence: []ServiceIdentityEvidence{identityProof("Foo", "A")}}, {ID: "b", StationID: "B", CallSign: "Foo West+1", ServiceEvidence: []ServiceIdentityEvidence{identityProof("Foo", "B")}}}
	if len(findDuplicateSuggestions(cs)) != 0 {
		t.Fatal("time shift hid region conflict")
	}
}
func TestContradictedTypedPairGraphHasNoPartialWinner(t *testing.T) {
	pair := func(peer string) ServiceIdentityEvidence {
		return ServiceIdentityEvidence{Value: "Foo", SourceID: "gracenote-weekday-epg-fixture", PeerStationID: peer, Policy: "provider-identity-v1"}
	}
	cs := []DraftChannel{{ID: "east", StationID: "E", CallSign: "Foo East", ServiceEvidence: []ServiceIdentityEvidence{pair("M")}}, {ID: "middle", StationID: "M", CallSign: "Foo", ServiceEvidence: []ServiceIdentityEvidence{pair("W")}}, {ID: "west", StationID: "W", CallSign: "Foo West"}}
	for i := 0; i < 2; i++ {
		if g := findDuplicateSuggestions(cs); len(g) != 0 {
			t.Fatal("conflict selected partial winner", g)
		}
		cs[0], cs[2] = cs[2], cs[0]
	}
}

func TestRegionalSuffixWithTimeShiftRemainsDistinct(t *testing.T) {
	cs := []DraftChannel{{ID: "a", StationID: "A", CallSign: "YESHDNY+1", ServiceEvidence: []ServiceIdentityEvidence{identityProof("YES", "A")}}, {ID: "b", StationID: "B", CallSign: "YESHDLA+1", ServiceEvidence: []ServiceIdentityEvidence{identityProof("YES", "B")}}}
	if got := verifiedServiceSuggestions(cs); len(got) != 0 {
		t.Fatalf("regional suffix plus shift grouped: %+v", got)
	}
}

func TestExactStationCatalogProofGroupsWithoutLineupNumberAssertion(t *testing.T) {
	proof := func() ServiceIdentityEvidence {
		return ServiceIdentityEvidence{Value: "Verified Service", SourceID: "spectrum-official-lineup", SourceRowID: "catalog-row", SourceRevision: "catalog-revision", BindingKind: "station-id-catalog-v1", Policy: "provider-identity-v1"}
	}
	cs := []DraftChannel{{ID: "a", StationID: "A", CallSign: "SOURCEA", ServiceEvidence: []ServiceIdentityEvidence{proof()}}, {ID: "b", StationID: "B", CallSign: "SOURCEB", ServiceEvidence: []ServiceIdentityEvidence{proof()}}}
	if len(verifiedServiceSuggestions(cs)) != 1 {
		t.Fatal("valid exact-ID catalog identity ignored")
	}
	cs[0].ServiceEvidence[0].SourceRevision = ""
	if len(verifiedServiceSuggestions(cs)) != 0 {
		t.Fatal("incomplete catalog proof admitted")
	}
}
