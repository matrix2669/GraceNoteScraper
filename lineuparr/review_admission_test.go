package lineuparr

import (
	"errors"
	"os"
	"reflect"
	"testing"
)

func TestReviewPersistenceFailureNeverPublishesMemory(t *testing.T) {
	for _, stage := range []string{"encode", "write", "sync", "rename"} {
		t.Run(stage, func(t *testing.T) {
			service := newTestService(t, "", "")
			if err := service.store.SetAliasSuppressed("source", "one", "Raw One", true); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(service.store.path)
			if err != nil {
				t.Fatal(err)
			}
			token, inputs := service.ReviewStateToken(), service.ReviewInputToken()
			override := service.store.Snapshot("source")
			service.store.beforeSave = func(at string) error {
				if at == stage {
					return errors.New("isolated failure")
				}
				return nil
			}
			d := MatchDecision{Key: "one", Decision: "confirmed", DispatcharrFingerprint: "conn", StreamFingerprint: "raw-one", StreamKey: "1:1", ChannelID: "one", StreamName: "Raw One", Score: 100, NameScore: 100}
			if err := service.SetAdmittedMatchDecisions("source", inputs, []MatchDecision{d}, nil); err == nil {
				t.Fatal("failure accepted")
			}
			after, _ := os.ReadFile(service.store.path)
			if string(before) != string(after) || token != service.ReviewStateToken() || !reflect.DeepEqual(override, service.store.Snapshot("source")) {
				t.Fatal("failed save changed durable or visible state")
			}
			service.store.beforeSave = nil
			if err := service.SetMatchDecision("source", d); err != nil {
				t.Fatal(err)
			}
			before, _ = os.ReadFile(service.store.path)
			token = service.ReviewStateToken()
			service.store.beforeSave = func(at string) error {
				if at == stage {
					return errors.New("isolated failure")
				}
				return nil
			}
			if err := service.ClearMatchDecisions("source", []string{"one"}); err == nil {
				t.Fatal("failed undo accepted")
			}
			after, _ = os.ReadFile(service.store.path)
			if string(before) != string(after) || token != service.ReviewStateToken() {
				t.Fatal("failed undo changed visible state")
			}
		})
	}
}

func TestAdmittedRetryIsDurableAndCannotReverseChoice(t *testing.T) {
	s := newTestService(t, "", "")
	d := MatchDecision{Key: "one", Decision: "denied", DispatcharrFingerprint: "conn", StreamFingerprint: "raw-one", StreamKey: "1:1", ChannelID: "one", StreamName: "Raw One", Score: 100, NameScore: 100}
	token := s.ReviewInputToken()
	if err := s.SetAdmittedMatchDecisions("source", token, []MatchDecision{d}, nil); err != nil {
		t.Fatal(err)
	}
	durable := s.ReviewStateToken()
	s.store.beforeSave = func(string) error { t.Fatal("identical retry rewrote state"); return nil }
	if err := s.SetAdmittedMatchDecisions("source", token, []MatchDecision{d}, nil); err != nil {
		t.Fatal(err)
	}
	if s.ReviewStateToken() != durable {
		t.Fatal("retry changed timestamp")
	}
	d.Decision = "confirmed"
	if err := s.SetAdmittedMatchDecisions("source", token, []MatchDecision{d}, nil); !errors.Is(err, ErrStaleReview) {
		t.Fatal("conflicting retry accepted", err)
	}
}
