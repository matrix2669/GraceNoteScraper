package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func repairFixture(t *testing.T, number string) (*builder.Service, *builder.Draft, builder.SupplementalDecision, []dispatcharr.IdentityStream) {
	t.Helper()
	store, err := builder.LoadStateStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	svc := builder.NewService(store, builder.ServiceOptions{})
	c := builder.DraftChannel{ID: "one", StationID: "station-one", PlacementID: "placement-one", CallSign: "RAW", Name: "Fixture Canonical", Number: number, Included: true}
	stream := dispatcharr.Stream{ID: 1, M3UAccountID: 3, Name: "US| Qwertz Unique HD", GroupName: "US| Entertainment"}
	d := builder.SupplementalDecision{Origin: builder.SupplementalOrigin, SourceFingerprint: "source", DispatcharrFingerprint: "conn", ChannelID: c.ID, StationID: c.StationID, PlacementID: c.PlacementID, RawCallSign: c.CallSign, ChannelName: c.Name, ChannelNumber: c.Number, StreamID: stream.ID, M3UAccountID: stream.M3UAccountID, StreamName: stream.Name, StreamFingerprint: stream.Fingerprint(), RetrievalVersion: "v1", RetrievalRank: 1, RetrievalReason: "fixture", EvidenceRefs: []string{"fixture"}, AdmittedSnapshot: "snapshot", OperationID: "op", GroupKey: "supp:group", Decision: "confirmed"}
	d.Key = builder.SupplementalKey(d)
	return svc, &builder.Draft{CountryCode: "US", Channels: []builder.DraftChannel{c}}, d, dispatcharr.ProjectIdentityInventory([]dispatcharr.Stream{stream})
}
func repairSave(t *testing.T, svc *builder.Service, d builder.SupplementalDecision) {
	t.Helper()
	if err := svc.SaveSupplementalDecisions("source", svc.ReviewInputToken(), []builder.SupplementalDecision{d}, false, func(f func() error) error { return f() }); err != nil {
		t.Fatal(err)
	}
}
func repairImpact(t *testing.T, baseline []byte, draft *builder.Draft, inventory []dispatcharr.IdentityStream) dispatcharr.ImpactResult {
	t.Helper()
	proposed, _ := json.Marshal(builder.ExportFromDraft(draft))
	req := dispatcharr.ImpactRequest{Country: "US", Baseline: string(baseline), Proposed: string(proposed)}
	for _, s := range inventory {
		req.Streams = append(req.Streams, dispatcharr.ImpactStream{Name: s.Name, GroupName: s.GroupName})
	}
	result, err := dispatcharr.CheckSupplementalImpact(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestSupplementalRepairImpactReportsCollateralDuplicateNameTarget(t *testing.T) {
	svc, draft, d, inventory := repairFixture(t, "101")
	sibling := draft.Channels[0]
	sibling.ID = "two"
	sibling.StationID = "station-two"
	sibling.PlacementID = "placement-two"
	sibling.Number = "102"
	draft.Channels = append(draft.Channels, sibling)
	before, _ := json.Marshal(builder.ExportFromDraft(draft))
	repairSave(t, svc, d)
	svc.ProjectSupplementalDecisions(draft, "source", "conn")
	result := repairImpact(t, before, draft, inventory)
	rows := compareSupplementalImpact(draft, inventory, svc.SupplementalDecisions("source"), nil, result, "conn")
	t.Logf("actual baseline=%+v proposed=%+v reported=%+v", result.Baseline, result.Proposed, rows)
	if len(result.Proposed) != 2 || len(result.Proposed[1].Names) != 1 {
		t.Fatal("invalid cross-target fixture")
	}
	if len(rows) != 2 {
		t.Fatal("consumer additionally matched sibling target, but impact omitted it")
	}
}
func TestSupplementalRepairImpactJoinsSerializedChannelNumber(t *testing.T) {
	svc, draft, d, inventory := repairFixture(t, "005.10")
	before, _ := json.Marshal(builder.ExportFromDraft(draft))
	repairSave(t, svc, d)
	svc.ProjectSupplementalDecisions(draft, "source", "conn")
	result := repairImpact(t, before, draft, inventory)
	rows := compareSupplementalImpact(draft, inventory, svc.SupplementalDecisions("source"), nil, result, "conn")
	t.Logf("actual proposed=%+v reported=%+v", result.Proposed, rows)
	if len(result.Proposed[0].Names) != 1 {
		t.Fatal("invalid number fixture")
	}
	if len(rows) != 1 || len(rows[0].Matched) != 1 || len(rows[0].Missed) != 0 {
		t.Fatal("actual matching name reported as missed because raw number differs from exported number")
	}
}
func TestSupplementalRepairSuppressedPositiveDoesNotSuppressReviewedDeny(t *testing.T) {
	svc, draft, d, inventory := repairFixture(t, "101")
	d.Decision = "denied"
	draft.Channels[0].Aliases = []string{"Qwertz Unique"}
	draft.Channels[0].SuppressedAliasEvidence = []builder.AliasEvidence{{Value: d.StreamName, Sources: []string{"fixture"}}}
	before, _ := json.Marshal(builder.ExportFromDraft(draft))
	repairSave(t, svc, d)
	svc.ProjectSupplementalDecisions(draft, "source", "conn")
	result := repairImpact(t, before, draft, inventory)
	t.Logf("projected excludedAliases=%+v actual proposed=%+v", draft.Channels[0].ExcludedAliases, result.Proposed)
	if len(draft.Channels[0].ExcludedAliases) != 1 || len(result.Proposed[0].Names) != 0 {
		t.Fatal("reviewed supplemental Deny silently lost because same raw positive alias was suppressed")
	}
}

func TestSupplementalRepairImpactStartCancelRace(t *testing.T) {
	s, _ := supplementalServerFixture(t)
	s.impactRunner = func(ctx context.Context, req dispatcharr.ImpactRequest) (dispatcharr.ImpactResult, error) {
		<-ctx.Done()
		return dispatcharr.ImpactResult{}, ctx.Err()
	}
	prior := ""
	for i := 0; i < 150; i++ {
		finished := make(chan struct{})
		go func() { defer close(finished); supplementalRequest(t, s, "POST", "/impact", map[string]string{}) }()
		id := ""
		for id == "" {
			s.impactMu.Lock()
			if s.impact != nil && s.impact.response.ID != prior {
				id = s.impact.response.ID
			}
			s.impactMu.Unlock()
		}
		w := supplementalRequest(t, s, "DELETE", "/impact", map[string]string{"id": id})
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
		<-finished
		prior = id
	}
}

func TestSupplementalRepairSuppressConfirmedSupplementalAliasPersists(t *testing.T) {
	s, review := supplementalServerFixture(t)
	g := review.Supplemental.Candidates[0]
	if w := supplementalRequest(t, s, "POST", "/decision", supplementalSaveBody(review, g)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cfg, _, _ := s.lineup.store.Get()
	if err := s.lineup.builder.SetAliasSuppressed(cfg.Fingerprint(), g.ChannelID, g.StreamName, true); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	draft, _, _, ok := s.lineup.buildDraft(w, httptest.NewRequest("GET", "/draft", nil))
	if !ok {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, c := range draft.Channels {
		if c.ID == g.ChannelID {
			t.Logf("suppressed raw=%q aliases=%v suppressedEvidence=%v", g.StreamName, c.Aliases, c.SuppressedAliasEvidence)
			for _, a := range c.Aliases {
				if a == g.StreamName {
					t.Fatal("saved alias suppression silently ignored for supplemental-only full raw alias")
				}
			}
		}
	}
}

// Each serialized occurrence has an identity even when both the name and the
// normalized number collide. Different per-entry exclusions must stay separate.
func TestSupplementalRepairExportOccurrenceCollisions(t *testing.T) {
	svc, draft, d, inventory := repairFixture(t, "005.10")
	draft.Channels[0].Category = " Entertainment "
	for i, category := range []string{"Entertainment", "Other"} {
		c := draft.Channels[0]
		c.ID = fmt.Sprintf("sibling-%d", i)
		c.StationID = c.ID
		c.PlacementID = c.ID
		c.Number = "5.1"
		c.Category = category
		draft.Channels = append(draft.Channels, c)
	}
	before, _ := json.Marshal(builder.ExportFromDraft(draft))
	repairSave(t, svc, d)
	for _, c := range draft.Channels[1:] {
		negative := d
		negative.ChannelID = c.ID
		negative.StationID = c.StationID
		negative.PlacementID = c.PlacementID
		negative.ChannelNumber = c.Number
		negative.Decision = "denied"
		negative.OperationID = c.ID
		negative.GroupKey = "supp:" + c.ID
		negative.Key = builder.SupplementalKey(negative)
		repairSave(t, svc, negative)
	}
	svc.ProjectSupplementalDecisions(draft, "source", "conn")
	result := repairImpact(t, before, draft, inventory)
	rows := compareSupplementalImpact(draft, inventory, svc.SupplementalDecisions("source"), nil, result, "conn")
	if len(rows) != 3 {
		t.Fatalf("lost exported occurrence: %+v", rows)
	}
	for _, row := range rows {
		want := 0
		if row.ChannelID == d.ChannelID {
			want = 1
		}
		if len(row.Matched) != want || len(row.Missed) != 0 {
			t.Fatalf("colliding entry results mixed: %+v", rows)
		}
	}
	encoded, _ := json.Marshal(builder.ExportFromDraft(draft))
	if strings.Contains(string(encoded), "channelId") || strings.Contains(string(encoded), "sibling-") {
		t.Fatal("internal correlation leaked to export", string(encoded))
	}
}

func TestSupplementalRepairSerializedNumericForms(t *testing.T) {
	for _, number := range []string{"0005", "5.00", "5e2", " 005.10 ", "9007199254740993", "9.5e-7"} {
		t.Run(number, func(t *testing.T) {
			svc, draft, d, inventory := repairFixture(t, number)
			before, _ := json.Marshal(builder.ExportFromDraft(draft))
			repairSave(t, svc, d)
			svc.ProjectSupplementalDecisions(draft, "source", "conn")
			result := repairImpact(t, before, draft, inventory)
			rows := compareSupplementalImpact(draft, inventory, svc.SupplementalDecisions("source"), nil, result, "conn")
			if len(rows) != 1 || len(rows[0].Matched) != 1 || len(rows[0].Missed) != 0 {
				t.Fatalf("number formatting lost identity: %+v", rows)
			}
		})
	}
}

func TestSupplementalRepairUnsavedTargetProtectedConflict(t *testing.T) {
	_, draft, _, inventory := repairFixture(t, "1")
	result := dispatcharr.ImpactResult{Baseline: []dispatcharr.ImpactMatch{{Category: "Uncategorized", Name: draft.Channels[0].Name, Number: "1", Names: []string{inventory[0].Name}}}, Proposed: []dispatcharr.ImpactMatch{{Category: "Uncategorized", Name: draft.Channels[0].Name, Number: "1"}}}
	rows := compareSupplementalImpact(draft, inventory, nil, nil, result, "conn")
	if len(rows) != 1 || len(rows[0].ProtectedConflicts) != 1 {
		t.Fatalf("unsaved target's lost baseline hidden: %+v", rows)
	}
}
