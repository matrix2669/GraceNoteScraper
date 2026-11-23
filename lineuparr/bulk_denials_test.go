package lineuparr

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBulkDenialsAtomicFailureAndPersistence(t *testing.T) {
	s := newTestService(t, "", "")
	source := "bulk-source"
	decision := MatchDecision{Key: "one", Decision: "denied", DispatcharrFingerprint: "dispatch", StreamFingerprint: "hash", StreamKey: "stream", ChannelID: "target", StreamName: `US: CBS (KTVT)`, NameScore: 100, Score: 100}
	token := s.ReviewStateToken()
	original := s.store.path
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	s.store.path = filepath.Join(blocker, "state.json")
	if err := s.ApplyReviewedDenials(source, token, []MatchDecision{decision}); err == nil {
		t.Fatal("expected write failure")
	}
	if s.ReviewStateToken() != token || len(s.MatchDecisions(source)) != 0 {
		t.Fatal("failed write changed memory")
	}
	s.store.path = original
	if err := s.ApplyReviewedDenials(source, token, []MatchDecision{decision}); err != nil {
		t.Fatal(err)
	}
	store, err := LoadStateStore(original)
	if err != nil {
		t.Fatal(err)
	}
	if len(NewService(store, ServiceOptions{}).MatchDecisions(source)) != 1 {
		t.Fatal("restart lost denial")
	}
	if err := s.ApplyReviewedDenials(source, token, []MatchDecision{decision}); err != ErrStaleReview {
		t.Fatalf("stale token: %v", err)
	}
}

func TestBulkDenialsNewSourceUsesExistingSourceIsolation(t *testing.T) {
	s := newTestService(t, "", "")
	d := MatchDecision{Key: "one", Decision: "confirmed", DispatcharrFingerprint: "dispatch", StreamFingerprint: "hash", StreamKey: "stream", ChannelID: "target", StreamName: "US: CBS (KTVT)", NameScore: 100, Score: 100}
	if err := s.SetMatchDecision("source-A", d); err != nil {
		t.Fatal(err)
	}
	token := s.ReviewStateToken()
	d.Decision = "denied"
	if err := s.ApplyReviewedDenials("source-B", token, []MatchDecision{d}); err != nil {
		t.Fatal(err)
	}
	if len(s.MatchDecisions("source-A")) != 0 || s.MatchDecisions("source-B")["one"].Decision != "denied" {
		t.Fatal("source decisions leaked")
	}
}
