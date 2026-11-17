package main

import (
	"net/http/httptest"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

// Exercise the real server queue using captured-style synthetic evidence and
// a private temporary store. No live state, consumer export, or refresh is used.
func TestSupplementalRetrievalEffectiveQueueFilters(t *testing.T) {
	s, review := supplementalServerFixture(t)
	g := review.Supplemental.Candidates[0]
	if w := supplementalRequest(t, s, "POST", "/decision", supplementalSaveBody(review, g)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cfg, _, _ := s.lineup.store.Get()
	conn, _ := s.config.Get()
	recorder := httptest.NewRecorder()
	draft, _, _, ok := s.lineup.buildDraft(recorder, httptest.NewRequest("GET", "/draft", nil))
	if !ok {
		t.Fatal(recorder.Code, recorder.Body.String())
	}
	var target builder.DraftChannel
	for _, c := range draft.Channels {
		if c.ID == g.ChannelID {
			target = c
			break
		}
	}
	inputs := s.lineup.reviewInputsRevision()
	snapshot := s.snapshot
	original, complete := s.prepareSupplemental(draft, cfg, snapshot, inputs)
	// Real retrieval yields five short-label candidates. Then actual primary,
	// automatic, and saved-decision filters leave just the eligible candidate.
	st := dispatcharr.SupplementalTarget{ID: target.ID, StationID: target.StationID, PlacementID: target.PlacementID, CallSign: target.CallSign, Country: "US", Labels: []dispatcharr.SupplementalLabel{{Value: "FX - East", Evidence: "test fixture"}}}
	streams := []dispatcharr.Stream{{ID: 91, M3UAccountID: 3, Name: "FX"}, {ID: 92, M3UAccountID: 4, Name: "FX"}, {ID: 93, M3UAccountID: 5, Name: "FX"}, {ID: 94, M3UAccountID: 6, Name: "FX SD"}, {ID: 95, M3UAccountID: 7, Name: "FX FHD"}}
	rows, ok := dispatcharr.RetrieveSupplemental([]dispatcharr.SupplementalTarget{st}, dispatcharr.ProjectIdentityInventory(streams), nil)
	if !ok || len(rows) != 5 {
		t.Fatalf("retrieval: %+v", rows)
	}
	primary := builder.MatchDecision{Key: "test-primary", Decision: "denied", DispatcharrFingerprint: conn.Fingerprint(), StreamFingerprint: streams[0].Fingerprint(), StreamKey: streams[0].Key(), ChannelID: target.ID, StreamName: "FX", Score: 100, NameScore: 100}
	if err := s.lineup.builder.SetMatchDecision(cfg.Fingerprint(), primary); err != nil {
		t.Fatal(err)
	}
	// Primary literal-name authority deliberately covers all three equal names.
	s.supplementalPreparation = supplementalPreparation{inputs: inputs, snapshot: snapshotIdentity(snapshot), candidates: append(append([]dispatcharr.SupplementalCandidate{}, original...), rows...), complete: complete}
	automatic := dispatcharr.AutomaticResult{InventoryRows: []dispatcharr.InventoryFeedExclusion{{ChannelID: target.ID, StreamName: "FX SD", AccountID: 6, Rule: "inventory-opposing-feed-v1"}}}
	got, _ := s.buildSupplementalReview(draft, cfg, snapshot, inputs, automatic, true, 1000)
	for _, candidate := range got.Candidates {
		if candidate.ChannelID == target.ID && supplementalLiteral(candidate.StreamName) == supplementalLiteral(g.StreamName) {
			t.Fatal("saved decision leaked into effective queue")
		}
		if candidate.ChannelID == target.ID && supplementalLiteral(candidate.StreamName) == "fx" {
			t.Fatalf("primary-covered pair leaked into queue: %+v", candidate)
		}
	}
	foundEligible := false
	for _, candidate := range got.Candidates {
		if candidate.ChannelID == target.ID && candidate.StreamName == "FX SD" {
			t.Fatal("automatically excluded candidate leaked")
		}
		if candidate.ChannelID == target.ID && candidate.StreamName == "FX FHD" {
			foundEligible = true
		}
	}
	if !foundEligible {
		t.Fatalf("eligible candidate absent from effective queue: %+v", got)
	}
	if got.PreviouslyReviewed < 3 {
		t.Fatalf("primary coverage not counted: %+v", got)
	}
}
