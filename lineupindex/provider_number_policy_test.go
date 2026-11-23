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
}
