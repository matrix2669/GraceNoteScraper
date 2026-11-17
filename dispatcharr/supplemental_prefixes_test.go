package dispatcharr

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func prefixFixture() ([]SupplementalTarget, []IdentityStream) {
	names := []string{"Alpha Network", "Bravo Network", "Charlie Network", "Delta Network", "Echo Network"}
	var targets []SupplementalTarget
	var streams []IdentityStream
	for i, n := range names {
		targets = append(targets, SupplementalTarget{ID: fmt.Sprint(i), Country: "US", Labels: []SupplementalLabel{{Value: n, Evidence: "attributed"}}})
		streams = append(streams, IdentityStream{ID: int64(i + 1), AccountID: 1, GroupKnown: true, GroupID: 10, Name: "Mystery: " + n + " HD", Fingerprint: fmt.Sprint(i)})
	}
	return targets, streams
}
func TestSupplementalLearnedPrefixIsScopedAndRaw(t *testing.T) {
	targets, streams := prefixFixture()
	streams = append(streams, IdentityStream{ID: 99, AccountID: 2, GroupID: 10, GroupKnown: true, Name: streams[0].Name, Fingerprint: "other"}, IdentityStream{ID: 98, AccountID: 1, Name: streams[0].Name, Fingerprint: "missing"})
	rows, complete := RetrieveSupplemental(targets, streams, nil)
	if !complete || len(rows) != 5 {
		t.Fatalf("complete=%v rows=%+v", complete, rows)
	}
	for _, r := range rows {
		if !strings.HasPrefix(r.Stream.Name, "Mystery:") || r.Stream.AccountID != 1 || !strings.Contains(r.Reason, "repeated provider prefix") {
			t.Fatalf("wrong evidence: %+v", r)
		}
	}
	reversed := append([]IdentityStream(nil), streams...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	again, _ := RetrieveSupplemental(targets, reversed, nil)
	if !reflect.DeepEqual(rows, again) {
		t.Fatalf("nondeterministic")
	}
}
func TestPrefixEvidenceConservative(t *testing.T) {
	targets, streams := prefixFixture()
	tests := []struct {
		name   string
		mutate func()
	}{
		{"fewer identities", func() { streams = streams[:4] }},
		{"missing group", func() {
			for i := range streams {
				streams[i].GroupKnown = false
			}
		}},
		{"insufficient coverage", func() {
			streams = append(streams, IdentityStream{AccountID: 1, GroupID: 10, GroupKnown: true, Fingerprint: "u", Name: "unrelated"}, IdentityStream{AccountID: 1, GroupID: 10, GroupKnown: true, Fingerprint: "v", Name: "unrelated two"})
		}},
		{"insufficient target corroboration", func() { targets = targets[:2] }},
		{"quality duplicates do not inflate", func() { streams = append(streams[:4], streams[0], streams[1]) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			targets, streams = prefixFixture()
			tc.mutate()
			p, _ := discoverSupplementalPrefixes(targets, streams)
			if len(p) != 0 {
				t.Fatalf("unexpected %+v", p)
			}
		})
	}
	for _, name := range []string{"US: Alpha", "EAST: Alpha", "KABC: Alpha", "HD: Alpha", "Mystery: US: Alpha", "Mystery: Alpha vs Bravo", "###: Alpha", "Mystery 2: Alpha"} {
		if _, _, ok := splitProviderPrefix(name); ok {
			t.Errorf("accepted %q", name)
		}
	}
}
func TestPrefixConflictingFingerprintAndBounds(t *testing.T) {
	targets, streams := prefixFixture()
	copy := streams[0]
	copy.GroupID = 11
	streams = append(streams, copy)
	p, _ := discoverSupplementalPrefixes(targets, streams)
	if _, ok := p[copy.Fingerprint]; ok {
		t.Fatal("conflicting scope accepted")
	}
	_, complete := discoverSupplementalPrefixes(targets, make([]IdentityStream, maxPrefixInventory+1))
	if complete {
		t.Fatal("truncation reported complete")
	}
}
