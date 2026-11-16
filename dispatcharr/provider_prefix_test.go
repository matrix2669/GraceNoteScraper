package dispatcharr

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func TestProviderPrefixPhysicalPrimaryAndImpact(t *testing.T) {
	group := int64(10)
	titles := []string{"Alpha Network", "Bravo Service", "Charlie Television", "Delta Movies", "Echo News"}
	channels := []MatchChannel{}
	streams := []Stream{}
	entries := []map[string]any{}
	for i, title := range titles {
		channels = append(channels, MatchChannel{ID: fmt.Sprint(i), Name: title})
		streams = append(streams, Stream{ID: int64(i + 1), M3UAccountID: 1, ChannelGroupID: &group, GroupName: "US| Test", Name: "ZetaNet: " + title + " HD"})
		entries = append(entries, map[string]any{"name": title, "number": i + 1})
	}
	streams = append(streams, Stream{ID: 99, M3UAccountID: 2, ChannelGroupID: &group, Name: streams[0].Name})
	got, _, err := MatchStreamCandidatesWithLineuparr(context.Background(), "source", "US", channels, streams, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.All) != 5 {
		t.Fatalf("scope leaked or expected pairs lost: %+v", got.All)
	}
	for _, c := range got.All {
		if c.M3UAccountID != 1 || c.Score != 100 || c.ComparisonName == "" {
			t.Fatalf("wrong provenance: %+v", c)
		}
		if c.StreamHash != streams[c.StreamID-1].Fingerprint() {
			t.Fatal("identity mutated")
		}
	}
	exported, _ := json.Marshal(map[string]any{"categories": map[string]any{"Test": entries}})
	req := ImpactRequest{Country: "US", Baseline: string(exported), Proposed: string(exported)}
	for _, s := range streams {
		req.Streams = append(req.Streams, ImpactStream{Key: s.Key(), Name: s.Name, AccountID: s.M3UAccountID, GroupID: group, GroupKnown: true, GroupName: s.GroupName})
	}
	result, err := CheckSupplementalImpact(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Proposed) != 5 {
		t.Fatal(result)
	}
	for _, row := range result.Proposed {
		if len(row.Keys) != 1 || row.Keys[0] != fmt.Sprintf("1:%d", row.EntryIndex+1) {
			t.Fatalf("impact physical parity: %+v", row)
		}
	}
}

func TestScopedComparisonEvidenceProtectsFeedAndSupplementalWithoutIdentityRewrite(t *testing.T) {
	group := int64(10)
	physical := []Stream{{ID: 1, M3UAccountID: 1, ChannelGroupID: &group, Name: "ZetaNet: Example Network West HD"}, {ID: 2, M3UAccountID: 2, ChannelGroupID: &group, Name: "ZetaNet: Example Network West HD"}}
	raw := ProjectIdentityInventory(physical)
	scoped := WithComparisonEvidence(raw, map[string]string{"1:1": "Example Network West HD"})
	if raw[0].ComparisonName != "" || scoped[0].Name != raw[0].Name || scoped[0].Fingerprint != raw[0].Fingerprint || scoped[1].ComparisonName != "" {
		t.Fatal("mutable or unscoped evidence")
	}
	east := FeedIdentity{Family: "example network", Region: "east", Evidence: "station"}
	exclusions := DeriveInventoryFeedExclusions("source", scoped, []FeedTarget{{ID: "one", Name: "Example Network", Country: "US", Feed: east}}, nil, nil, nil)
	if len(exclusions) != 1 || exclusions[0].AccountID != 1 || exclusions[0].StreamName != raw[0].Name {
		t.Fatalf("feed evidence leaked: %+v", exclusions)
	}
	targets := []SupplementalTarget{{ID: "one", Name: "Example Network", Country: "US", Feed: east, Labels: []SupplementalLabel{{Value: "Example Network", Evidence: "station"}}}}
	suggestions, complete := RetrieveSupplemental(targets, scoped, nil)
	if !complete || len(suggestions) != 1 || suggestions[0].Stream.AccountID != 1 || suggestions[0].Stream.Name != raw[0].Name {
		t.Fatalf("supplemental evidence leaked: %+v", suggestions)
	}
}

func TestProviderPrefixConflictingPhysicalScopeFailsClosed(t *testing.T) {
	a, b := int64(10), int64(20)
	streams := []Stream{{ID: 1, M3UAccountID: 1, ChannelGroupID: &a, Name: "ZetaNet: Alpha Network"}, {ID: 1, M3UAccountID: 1, ChannelGroupID: &b, Name: "ZetaNet: Alpha Network"}}
	if _, _, err := MatchStreamCandidatesWithLineuparr(context.Background(), "source", "US", nil, streams, nil); err == nil {
		t.Fatal("ambiguous physical scope accepted")
	}
}
