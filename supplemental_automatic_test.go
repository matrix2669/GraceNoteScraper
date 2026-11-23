package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

// Adapted from independent R1's synthetic reproduction; no retained data or live
// endpoint is used. Deliberately no primary pair for the opposing inventory name.
func supplementalAutomaticFixture(t *testing.T, streams ...dispatcharr.Stream) (*dispatcharrServer, dispatcharrReviewResponse) {
	t.Helper()
	s, api := newDispatcharrTestServer(t, true)
	cfg, _, _ := s.lineup.store.Get()
	s.lineup.state.UpdateForSource(&guide.TVGuide{LineupChannels: []guide.Channel{{ID: "59368", PlacementID: "auto-hits", ChannelNo: "902", CallSign: "HBOHTHD"}}}, cfg.Fingerprint())
	api.streams = streams
	s.matcher = func(context.Context, string, string, []dispatcharr.MatchChannel, []dispatcharr.Stream, map[string]dispatcharr.Decision) (dispatcharr.CandidateSet, string, error) {
		return dispatcharr.CandidateSet{}, "isolated", nil
	}
	s.lineup.automaticProjection = s.projectAutomaticExclusions
	s.lineup.derivedRevision = s.derivedRevision
	w := httptest.NewRecorder()
	built, ok := s.buildReview(w, httptest.NewRequest("GET", "/review", nil), true)
	if !ok {
		t.Fatalf("build %d %s", w.Code, w.Body.String())
	}
	return s, built.response
}

func TestSupplementalDefiniteOpposingFeedsRemainAutomatic(t *testing.T) {
	s, r := supplementalAutomaticFixture(t, dispatcharr.Stream{ID: 1, Name: "US: HBO HITS WEST HD", M3UAccountID: 3})
	if len(r.Automatic.Rows) != 1 || !r.Automatic.Rows[0].Exported || r.Automatic.Rows[0].PendingReview || r.Automatic.Rows[0].Conflict {
		t.Fatalf("expected active inventory exclusion: %+v", r.Automatic)
	}
	if r.CandidateCount != 0 || r.Supplemental.CandidateCount != 0 || r.Supplemental.CandidateStreamCount != 0 || len(r.Supplemental.Candidates) != 0 || len(s.supplementalAdmission.groups) != 0 || len(r.DecisionTokens) != 0 {
		t.Fatalf("automatic pair still pending/admitted: %+v", r)
	}
	cfg, _, _ := s.lineup.store.Get()
	state := s.lineup.builder.ReviewStateToken()
	before, _ := json.Marshal(builder.ExportFromDraft(automaticDraft(t, s)))
	for i := 0; i < 2; i++ {
		again := automaticReview(t, s)
		if !reflect.DeepEqual(r.Automatic.Rows, again.Automatic.Rows) || again.Supplemental.CandidateCount != 0 {
			t.Fatal("inspection or filtering changed on read")
		}
	}
	after, _ := json.Marshal(builder.ExportFromDraft(automaticDraft(t, s)))
	if string(before) != string(after) || state != s.lineup.builder.ReviewStateToken() || len(s.lineup.builder.SupplementalDecisions(cfg.Fingerprint())) != 0 {
		t.Fatal("filtering changed export or saved a decision")
	}
}

func TestSupplementalAutomaticFilterLiteralAccountTargetAndUncertainty(t *testing.T) {
	candidate := func(target, name string, account, id int64) dispatcharr.SupplementalCandidate {
		return dispatcharr.SupplementalCandidate{Target: dispatcharr.SupplementalTarget{ID: target}, Stream: dispatcharr.IdentityStream{Name: name, AccountID: account, ID: id}}
	}
	rows := []dispatcharr.SupplementalCandidate{
		candidate("east", "US: HBO HITS WEST HD", 3, 1),
		candidate("east", " us:  hbo hits west hd ", 3, 2), // equivalent literal, distinct physical ID
		candidate("east", "US: HBO HITS WEST HD", 4, 3),    // different account
		candidate("west", "US: HBO HITS WEST HD", 3, 4),    // different target
		candidate("east", "US| HBO HITS WEST HD", 3, 5),    // punctuation is significant
		candidate("east", "US: HBO HITS WEST SD", 3, 6),    // quality is significant
		candidate("east", "US: HBO HITS", 3, 7),
		candidate("east", "US: HBO HITS EAST WEST", 3, 8),
		candidate("unknown-target", "US: HBO HITS WEST HD", 3, 9),
	}
	automatic := dispatcharr.AutomaticResult{InventoryRows: []dispatcharr.InventoryFeedExclusion{{ChannelID: "east", StreamName: rows[0].Stream.Name, AccountID: 3, Rule: "inventory-opposing-feed-v1"}}}
	for _, i := range []int{6, 7, 8} {
		c := rows[i]
		automatic.Rows = append(automatic.Rows, dispatcharr.AutomaticExclusion{Candidate: dispatcharr.Candidate{ChannelID: c.Target.ID, StreamName: c.Stream.Name, M3UAccountID: c.Stream.AccountID}, Rule: "feed-identity-v1", PendingReview: true})
	}
	if got := filterSupplementalAutomatic(rows, automatic); !reflect.DeepEqual(got, rows[2:]) {
		t.Fatalf("scope/uncertainty lost: %+v", got)
	}
	// A saved confirmation conflict is not an active automatic negative.
	automatic.InventoryRows[0].Conflict = true
	if got := filterSupplementalAutomatic(rows, automatic); !reflect.DeepEqual(got, rows) {
		t.Fatal("confirmation conflict filtered")
	}
	if got := filterSupplementalAutomatic(rows, dispatcharr.AutomaticResult{}); !reflect.DeepEqual(got, rows) {
		t.Fatal("absent current automatic evidence suppressed proposals")
	}
	// Scored definite feed constraints also cover consumer-impact extras; unrelated
	// affiliate rules and unresolved holds do not acquire this filter's authority.
	automatic.Rows[0].PendingReview = false
	if got := filterSupplementalAutomatic(rows, automatic); len(got) != len(rows)-1 || got[6].Stream.ID != 8 {
		t.Fatal("scored feed constraint not filtered", got)
	}
	automatic.Rows[0].Rule = "affiliate-callsign-v1"
	if got := filterSupplementalAutomatic(rows, automatic); !reflect.DeepEqual(got, rows) {
		t.Fatal("filter widened to another rule")
	}
}

func seedSupplementalAutomaticHistory(t *testing.T, s *dispatcharrServer, choice string) supplementalGroup {
	t.Helper()
	cfg, _, _ := s.lineup.store.Get()
	draft := automaticDraft(t, s)
	rows, _ := s.prepareSupplemental(draft, cfg, s.snapshot, s.lineup.reviewInputsRevision())
	for _, c := range rows {
		if c.Stream.ID != 1 {
			continue
		}
		d := supplementalDecision(c, cfg.Fingerprint(), s.snapshot.dispatcharrFingerprint, snapshotIdentity(s.snapshot))
		group := supplementalGroups([]builder.SupplementalDecision{d}, false)[0]
		d = group.Constituents[0]
		d.Decision, d.OperationID = choice, "preexisting-operation"
		if err := s.lineup.builder.SaveSupplementalDecisions(cfg.Fingerprint(), s.lineup.builder.ReviewInputToken(), []builder.SupplementalDecision{d}, false, func(write func() error) error { return write() }); err != nil {
			t.Fatal(err)
		}
		return group
	}
	t.Fatal("seed identity missing")
	return supplementalGroup{}
}

func TestSupplementalAutomaticPreservesActualChoicesAndUndo(t *testing.T) {
	for _, choice := range []string{"confirmed", "denied"} {
		t.Run(choice, func(t *testing.T) {
			s, _ := supplementalAutomaticFixture(t, dispatcharr.Stream{ID: 1, Name: "US: HBO HITS WEST HD", M3UAccountID: 3})
			g := seedSupplementalAutomaticHistory(t, s, choice)
			state := s.lineup.builder.ReviewStateToken()
			r := automaticReview(t, s)
			if len(r.Supplemental.Decisions) != 1 || r.Supplemental.Decisions[0].Decision != choice || r.Supplemental.CandidateCount != 0 || r.Supplemental.ConfirmedCount+r.Supplemental.DeniedCount != 1 || state != s.lineup.builder.ReviewStateToken() {
				t.Fatal("saved history changed", r.Supplemental)
			}
			conflict := choice == "confirmed"
			if r.Automatic.Rows[0].Conflict != conflict || r.Automatic.Rows[0].Exported == conflict {
				t.Fatal("saved choice automatic projection changed", r.Automatic)
			}
			if w := supplementalRequest(t, s, "DELETE", "/decision", map[string]string{"key": g.Key}); w.Code != 200 {
				t.Fatal("Undo failed", w.Code, w.Body.String())
			}
			after := automaticReview(t, s)
			if after.Supplemental.CandidateCount != 0 || len(after.Supplemental.Decisions) != 0 || !after.Automatic.Rows[0].Exported || after.Automatic.Rows[0].Conflict {
				t.Fatal("Undo did not restore automatic resolution", after)
			}
		})
	}
}

func TestSupplementalAutomaticUndoRetiresDependentOffersBeforeAndAfterRead(t *testing.T) {
	s, _ := supplementalAutomaticFixture(t,
		dispatcharr.Stream{ID: 1, Name: "US: HBO HITS WEST HD", M3UAccountID: 3},
		dispatcharr.Stream{ID: 2, Name: "US: HBO HITS WEST HD", M3UAccountID: 4},
		dispatcharr.Stream{ID: 3, Name: "US: HBO 2 HD", M3UAccountID: 3})
	saved := seedSupplementalAutomaticHistory(t, s, "confirmed")
	r := automaticReview(t, s)
	var dependent, unknown supplementalGroup
	for _, g := range r.Supplemental.Candidates {
		if g.StreamName == saved.StreamName {
			dependent = g
		} else {
			unknown = g
		}
	}
	if dependent.Key == "" || unknown.Key == "" || dependent.StreamCount != 1 {
		t.Fatal("expected current conflict and unknown proposals", r.Supplemental)
	}
	body := supplementalSaveBody(r, dependent)
	if w := supplementalRequest(t, s, "DELETE", "/decision", map[string]string{"key": saved.Key}); w.Code != 200 {
		t.Fatal(w.Code)
	}
	for i := 0; i < 2; i++ {
		if w := supplementalRequest(t, s, "POST", "/decision", body); w.Code != 409 {
			t.Fatal("old dependent offer bypassed restored constraint", i, w.Code)
		}
		after := automaticReview(t, s)
		if after.Supplemental.CandidateCount != 1 || after.Supplemental.Candidates[0].Key != unknown.Key {
			t.Fatal("filter failed to retain only unknown feed", after.Supplemental)
		}
	}
	if _, ok := s.supplementalAdmission.groups[dependent.Key]; ok {
		t.Fatal("filtered group remains admitted")
	}
	if receiptFor(t, s, body["operation"])["state"] != "superseded" {
		t.Fatal("removed offer receipt not retired")
	}
	if w := supplementalRequest(t, s, "POST", "/decision", supplementalSaveBody(r, unknown)); w.Code != 200 {
		t.Fatal("unrelated unknown offer invalidated", w.Code, w.Body.String())
	}
	// Read hides the saved group but must preserve its idempotent durable retry.
	_ = automaticReview(t, s)
	if w := supplementalRequest(t, s, "POST", "/decision", supplementalSaveBody(r, unknown)); w.Code != 200 {
		t.Fatal("saved retry lost on filtered read", w.Code)
	}
}

func TestSupplementalAutomaticUnknownOfferSourceFence(t *testing.T) {
	s, r := supplementalAutomaticFixture(t,
		dispatcharr.Stream{ID: 1, Name: "US: HBO HITS WEST HD", M3UAccountID: 3},
		dispatcharr.Stream{ID: 2, Name: "US: HBO 2 HD", M3UAccountID: 3})
	if r.Supplemental.CandidateCount != 1 || r.Supplemental.Candidates[0].StreamName != "US: HBO 2 HD" {
		t.Fatal("unknown proposal lost", r.Supplemental)
	}
	body := supplementalSaveBody(r, r.Supplemental.Candidates[0])
	cfg, _, _ := s.lineup.store.Get()
	cfg.Gracenote.PostalCode = "changed-source"
	if err := s.lineup.store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if w := supplementalRequest(t, s, "POST", "/decision", body); w.Code != 409 {
		t.Fatal("old source offer accepted", w.Code)
	}
}

func TestSupplementalAutomaticReadRetiresPreviouslyOfferedOpposingGroup(t *testing.T) {
	s, r := supplementalAutomaticFixture(t, dispatcharr.Stream{ID: 1, Name: "US: HBO HITS WEST HD", M3UAccountID: 3})
	cfg, _, _ := s.lineup.store.Get()
	rows, _ := s.prepareSupplemental(automaticDraft(t, s), cfg, s.snapshot, s.lineup.reviewInputsRevision())
	if len(rows) != 1 {
		t.Fatal("expected raw retrieval before automatic filter")
	}
	g := supplementalGroups([]builder.SupplementalDecision{supplementalDecision(rows[0], cfg.Fingerprint(), s.snapshot.dispatcharrFingerprint, snapshotIdentity(s.snapshot))}, false)[0]
	// Simulate an older offered generation with the same snapshot/input scope.
	// A filtered reread must not merge its uncommitted token back into admission.
	s.reviewMu.Lock()
	s.supplementalAdmission.groups[g.Key] = g.Constituents
	s.supplementalAdmission.tokens[g.Key] = "old-uncommitted-offer"
	s.operations["old-uncommitted-offer"] = reviewOperation{Key: g.Key, State: "offered", Scope: r.OperationScope}
	s.reviewMu.Unlock()
	state := s.lineup.builder.ReviewStateToken()
	after := automaticReview(t, s)
	if after.Supplemental.CandidateCount != 0 || len(s.supplementalAdmission.groups) != 0 || len(s.supplementalAdmission.tokens) != 0 {
		t.Fatal("filtered offer was retained in admission")
	}
	if w := supplementalRequest(t, s, "POST", "/decision", map[string]string{"key": g.Key, "operation": "old-uncommitted-offer", "decision": "confirmed"}); w.Code != 409 {
		t.Fatal("old opposing offer replayed", w.Code)
	}
	if receiptFor(t, s, "old-uncommitted-offer")["state"] != "superseded" || state != s.lineup.builder.ReviewStateToken() {
		t.Fatal("receipt or saved state changed")
	}
}
