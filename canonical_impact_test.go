package main

import (
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	matcher "github.com/daniel-widrick/GraceNoteScraper/lineuparr_matcher"
	"testing"
)

func TestCanonicalImpactRequiresEveryPhysicalAdditionConfirmed(t *testing.T) {
	e := builder.ExportFile{Categories: map[string][]builder.ExportChannel{"Entertainment": {{ChannelID: "t", Name: "OLD", Number: 1}}}}
	p := e
	p.Categories = map[string][]builder.ExportChannel{"Entertainment": {{ChannelID: "t", Name: "New", Number: 1}}}
	r := dispatcharr.ImpactResult{Revision: matcher.Revision, Baseline: []dispatcharr.ImpactMatch{{Category: "Entertainment", EntryIndex: 0}}, Proposed: []dispatcharr.ImpactMatch{{Category: "Entertainment", EntryIndex: 0, Names: []string{"raw"}}}}
	streams := []dispatcharr.IdentityStream{{ID: 1, AccountID: 2, Name: "raw"}}
	target := builder.DraftChannel{ID: "t", StationID: "s", PlacementID: "p", CallSign: "c"}
	delta, err := canonicalImpactDelta(e, p, r, streams, target, "src", "conn", nil, nil)
	if err != nil || len(delta.Unresolved) != 1 {
		t.Fatalf("unresolved = %+v err %v", delta, err)
	}
	decisions := map[string]builder.SupplementalDecision{"x": {SourceFingerprint: "src", DispatcharrFingerprint: "conn", ChannelID: "t", StationID: "s", PlacementID: "p", RawCallSign: "c", StreamName: "raw", Decision: "confirmed"}}
	delta, err = canonicalImpactDelta(e, p, r, streams, target, "src", "conn", nil, decisions)
	if err != nil || len(delta.Unresolved) != 0 || len(delta.Conflict) != 0 {
		t.Fatalf("confirmation = %+v err %v", delta, err)
	}
	primary := map[string]builder.MatchDecision{"x": {ChannelID: "t", DispatcharrFingerprint: "conn", StreamName: "raw", Decision: "denied"}}
	delta, err = canonicalImpactDelta(e, p, r, streams, target, "src", "conn", primary, decisions)
	if err != nil || len(delta.Conflict) != 1 {
		t.Fatalf("deny = %+v err %v", delta, err)
	}
}
func TestCanonicalImpactHoldsLossAndRejectsIncomplete(t *testing.T) {
	e := builder.ExportFile{Categories: map[string][]builder.ExportChannel{"A": {{ChannelID: "a", Name: "old"}}}}
	r := dispatcharr.ImpactResult{Revision: matcher.Revision, Baseline: []dispatcharr.ImpactMatch{{Category: "A", EntryIndex: 0, Names: []string{"raw"}}}, Proposed: []dispatcharr.ImpactMatch{{Category: "A", EntryIndex: 0}}}
	d, err := canonicalImpactDelta(e, e, r, []dispatcharr.IdentityStream{{ID: 1, AccountID: 1, Name: "raw"}}, builder.DraftChannel{ID: "a"}, "s", "c", nil, nil)
	if err != nil || len(d.Lost) != 1 {
		t.Fatalf("loss = %+v err %v", d, err)
	}
	r.Proposed = nil
	if _, err = canonicalImpactDelta(e, e, r, nil, builder.DraftChannel{ID: "a"}, "s", "c", nil, nil); err == nil {
		t.Fatal("accepted partial result")
	}
}

func TestCanonicalSafeStreamSnapshotIsDeepCopy(t *testing.T) {
	id := int64(7)
	num := 14.5
	in := []dispatcharr.Stream{{ID: 1, Name: "stream", ChannelGroupID: &id, StreamChannelNo: &num}}
	snapshot := copySafeStreams(in)
	id = 8
	num = 22
	in[0].Name = "changed"
	if snapshot[0].Name != "stream" || *snapshot[0].ChannelGroupID != 7 || *snapshot[0].StreamChannelNo != 14.5 {
		t.Fatalf("snapshot aliased source: %+v", snapshot[0])
	}
}
