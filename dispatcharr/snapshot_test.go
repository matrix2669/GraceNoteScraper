package dispatcharr

import "testing"

func TestSnapshotDecisionsDoNotHideNewRawVariants(t *testing.T) {
	for _, kind := range []string{"", "inclusion", "qualified", "feed-review"} {
		candidates := []Candidate{
			{Key: "saved", StreamHash: "saved", StreamName: "US: NETWORK HD", NormalizedAlias: "network", ChannelID: "a", ReviewKind: kind},
			{Key: "same-raw-account", StreamHash: "another-account", StreamName: " us:   network hd ", NormalizedAlias: "network", ChannelID: "a", ReviewKind: kind},
			{Key: "new-prefix", StreamHash: "new-prefix", StreamName: "TV: NETWORK HD", NormalizedAlias: "network", ChannelID: "a", ReviewKind: kind},
			{Key: "new-quality", StreamHash: "new-quality", StreamName: "US: NETWORK SD", NormalizedAlias: "network", ChannelID: "a", ReviewKind: kind},
			{Key: "other-target", StreamHash: "saved", StreamName: "US: NETWORK HD", NormalizedAlias: "network", ChannelID: "b", ReviewKind: kind},
		}
		for _, decision := range []string{"confirmed", "denied"} {
			got := FilterSnapshotCandidates(candidates, map[string]bool{"a": true, "b": true}, map[string]Decision{"saved": {StreamHash: "saved", StreamName: "US: NETWORK HD", ChannelID: "a", NormalizedAlias: "network", ReviewKind: kind, Decision: decision}})
			if len(got) != 3 {
				t.Fatalf("%s/%s: new raw variants hidden: %+v", kind, decision, got)
			}
			for _, c := range got {
				if c.Key == "saved" || c.Key == "same-raw-account" {
					t.Fatalf("retained literal decision lost: %+v", c)
				}
			}
		}
	}
}
