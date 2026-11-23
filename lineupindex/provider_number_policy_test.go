package lineupindex

import "testing"

func TestProviderNumberProvenancePreservesQuarantine(t *testing.T) {
	for _, tc := range []struct {
		marker string
		want   bool
	}{{"", false}, {"; number-policy-local-v1", true}, {"; number-policy-provider-v2", true}} {
		fact := StationFact{SourceID: "verizon-fios-official-lineup", Kind: FactCategory, Method: "exact provider channel number plus exact identity across same-number variants; identity-policy-v2" + tc.marker}
		if got := usableFact(fact); got != tc.want {
			t.Fatalf("marker %q: usable=%v, want %v", tc.marker, got, tc.want)
		}
	}
	if usableFact(StationFact{Method: "exact provider channel number; number-policy-provider-v2"}) {
		t.Fatal("new provenance must not permit number-only matches")
	}
	if usableFact(StationFact{Kind: FactAlias, Method: "unique provider-local channel number; number-policy-provider-alias-v3"}) {
		t.Fatal("pre-alignment provider aliases must remain quarantined")
	}
	if !usableFact(StationFact{Kind: FactAlias, Method: "unique provider-local channel number; number-policy-provider-alias-v3; " + ProviderSourceAlignmentV1}) {
		t.Fatal("provider-local unique-number aliases must remain usable")
	}
	if usableFact(StationFact{Kind: FactCategory, Method: "unique provider-local channel number; number-policy-provider-alias-v3; " + ProviderSourceAlignmentV1}) {
		t.Fatal("provider-local unique-number evidence must not assign categories")
	}
}
