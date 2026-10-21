package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestShortReviewSaveReloadUndoDoesNotEraseUnrelatedNames(t *testing.T) {
	s, _ := automaticServer(t)
	names := []string{"US| FX HD", "US: FX HD", "US: A&E HD", "US| A&E HD", "US| ID HD"}
	base := s.snapshot.candidates[0]
	s.snapshot.candidates = nil
	for _, name := range names {
		c := base
		c.Key = name
		c.StreamName = name
		c.StreamHash = name
		c.StreamKey = name
		c.NormalizedAlias = ""
		s.snapshot.candidates = append(s.snapshot.candidates, c)
	}
	review := automaticReview(t, s)
	if review.CandidateCount != 5 {
		t.Fatalf("short names hidden: %+v", review.Candidates)
	}
	cfg, _, _ := s.lineup.store.Get()
	for _, group := range review.Candidates {
		body, _ := json.Marshal(map[string]any{"key": group.Key, "decision": "denied", "tvgIds": []string{}})
		req := httptest.NewRequest("POST", "/", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handleDecision(w, req)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if automaticReview(t, s).CandidateCount != 0 {
		t.Fatal("saved decisions not restored")
	}
	saved := s.lineup.builder.MatchDecisions(cfg.Fingerprint())
	if len(saved) != 5 {
		t.Fatal("constituents lost", len(saved))
	}
	key := ""
	for k, d := range saved {
		if d.StreamName == names[0] {
			key = k
		}
		if d.NormalizedAlias != "" {
			t.Fatal("opaque identity became positive alias")
		}
	}
	body, _ := json.Marshal(map[string]string{"key": key})
	req := httptest.NewRequest("DELETE", "/", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleDecision(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(s.lineup.builder.MatchDecisions(cfg.Fingerprint())) != 4 || automaticReview(t, s).CandidateCount != 1 {
		t.Fatal("Undo cleared unrelated short names")
	}
}
