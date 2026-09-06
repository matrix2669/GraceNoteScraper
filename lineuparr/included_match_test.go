package lineuparr

import (
	"errors"
	"testing"
)

func TestExcludedMatchBatchRejectedBeforeAnySave(t *testing.T) {
	s := newTestService(t, "", "")
	included := false
	if err := s.UpdateChannel("source", "excluded", ChannelUpdate{Included: &included}); err != nil {
		t.Fatal(err)
	}
	decisions := []MatchDecision{
		{Key: "first", Decision: "confirmed", DispatcharrFingerprint: "d", StreamFingerprint: "a", StreamKey: "a", ChannelID: "included", StreamName: "A"},
		{Key: "last", Decision: "denied", DispatcharrFingerprint: "d", StreamFingerprint: "b", StreamKey: "b", ChannelID: "excluded", StreamName: "B"},
	}
	if err := s.SetMatchDecisions("source", decisions); !errors.Is(err, ErrMatchChannelExcluded) {
		t.Fatalf("got %v", err)
	}
	reopened, err := LoadStateStore(s.store.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.MatchDecisions("source")) != 0 || len(reopened.MatchDecisionSnapshot("source")) != 0 {
		t.Fatal("partial batch saved")
	}
}
