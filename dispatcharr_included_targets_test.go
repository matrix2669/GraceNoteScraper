package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	lineuparrbuilder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

func inclusionReview(t *testing.T, server *dispatcharrServer) dispatcharrReviewResponse {
	t.Helper()
	w := httptest.NewRecorder()
	server.handleReview(w, httptest.NewRequest("GET", "/api/lineuparr/dispatcharr/review", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result dispatcharrReviewResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestOneStreamCanBeConfirmedForEveryIncludedTarget(t *testing.T) {
	s, _ := newDispatcharrTestServer(t, true)
	initial := inclusionReview(t, s)
	if initial.StreamCount != 1 || len(initial.Candidates) != 2 {
		t.Fatalf("missing one-to-many queue: %+v", initial)
	}
	for _, candidate := range initial.Candidates {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/lineuparr/dispatcharr/decision", strings.NewReader(`{"key":"`+candidate.Key+`","decision":"confirmed"}`))
		r.Header.Set("Content-Type", "application/json")
		s.handleDecision(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	after := inclusionReview(t, s)
	if after.CandidateCount != 0 || after.ConfirmedCount != 2 || after.StreamCount != 1 {
		t.Fatalf("one-to-many confirmations: %+v", after)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("DELETE", "/api/lineuparr/dispatcharr/decision", strings.NewReader(`{"key":"`+initial.Candidates[0].Key+`"}`))
	r.Header.Set("Content-Type", "application/json")
	s.handleDecision(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	after = inclusionReview(t, s)
	if after.ConfirmedCount != 1 || len(after.Candidates) != 1 || after.Candidates[0].ChannelID != initial.Candidates[0].ChannelID {
		t.Fatalf("undo crossed target scope: %+v", after)
	}
}

func setMatchTargetIncluded(t *testing.T, server *dispatcharrServer, id string, included bool) {
	t.Helper()
	config, _, _ := server.lineup.store.Get()
	if err := server.lineup.builder.UpdateChannel(config.Fingerprint(), id, lineuparrbuilder.ChannelUpdate{Included: &included}); err != nil {
		t.Fatal(err)
	}
}

func TestReviewTargetsOnlyIncludedChannelsWithoutFilteringStreams(t *testing.T) {
	s, _ := newDispatcharrTestServer(t, true)
	before := inclusionReview(t, s)
	target := before.Candidates[0].ChannelID
	setMatchTargetIncluded(t, s, target, false)
	after := inclusionReview(t, s)
	if after.StreamCount != before.StreamCount || len(after.Candidates) == 0 {
		t.Fatal("stream pool changed or included alternative lost")
	}
	for _, c := range after.Candidates {
		if c.ChannelID == target {
			t.Fatal("excluded primary target")
		}
		for _, alt := range c.Alternatives {
			if alt.ChannelID == target {
				t.Fatal("excluded alternative target")
			}
		}
		setMatchTargetIncluded(t, s, c.ChannelID, false)
	}
	empty := inclusionReview(t, s)
	if empty.CandidateCount != 0 || empty.StreamCount != before.StreamCount {
		t.Fatalf("all excluded: %+v", empty)
	}
	setMatchTargetIncluded(t, s, target, true)
	if restored := inclusionReview(t, s); len(restored.Candidates) != 1 || restored.Candidates[0].ChannelID != target {
		t.Fatalf("reinclusion: %+v", restored)
	}
}

func TestCachedDecisionCannotSaveExcludedTarget(t *testing.T) {
	for _, action := range []string{"confirmed", "denied"} {
		t.Run(action, func(t *testing.T) {
			s, _ := newDispatcharrTestServer(t, true)
			review := inclusionReview(t, s)
			c := review.Candidates[0]
			setMatchTargetIncluded(t, s, c.ChannelID, false)
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/lineuparr/dispatcharr/decision", strings.NewReader(`{"key":"`+c.Key+`","decision":"`+action+`"}`))
			r.Header.Set("Content-Type", "application/json")
			s.handleDecision(w, r)
			if w.Code != http.StatusConflict {
				t.Fatal(w.Code, w.Body.String())
			}
			config, _, _ := s.lineup.store.Get()
			if len(s.lineup.builder.MatchDecisions(config.Fingerprint())) != 0 {
				t.Fatal("excluded decision saved")
			}
		})
	}
}

func TestExcludedConfirmationDoesNotReserveStreamAndRemainsUndoable(t *testing.T) {
	s, _ := newDispatcharrTestServer(t, true)
	c := inclusionReview(t, s).Candidates[0]
	r := httptest.NewRequest("POST", "/api/lineuparr/dispatcharr/decision", strings.NewReader(`{"key":"`+c.Key+`","decision":"confirmed"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleDecision(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	setMatchTargetIncluded(t, s, c.ChannelID, false)
	remaining := inclusionReview(t, s)
	if len(remaining.Candidates) == 0 || remaining.ConfirmedCount != 1 {
		t.Fatalf("stream reserved or history lost: %+v", remaining)
	}
	for _, candidate := range remaining.Candidates {
		if candidate.ChannelID == c.ChannelID {
			t.Fatal("excluded target restored")
		}
	}
	setMatchTargetIncluded(t, s, c.ChannelID, true)
	if restored := inclusionReview(t, s); restored.CandidateCount != 1 || restored.ConfirmedCount != 1 || restored.Candidates[0].ChannelID == c.ChannelID {
		t.Fatalf("reinclusion lost saved confirmation: %+v", restored)
	}
	setMatchTargetIncluded(t, s, c.ChannelID, false)
	r = httptest.NewRequest("DELETE", "/api/lineuparr/dispatcharr/decision", strings.NewReader(`{"key":"`+c.Key+`"}`))
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	s.handleDecision(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if got := inclusionReview(t, s); got.ConfirmedCount != 0 {
		t.Fatal("excluded history cannot be undone")
	}
}
