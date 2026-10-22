package lineuparr

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func supplementalFixture() SupplementalDecision {
	d := SupplementalDecision{Origin: SupplementalOrigin, SourceFingerprint: "source", DispatcharrFingerprint: "conn", ChannelID: "one", StationID: "station", PlacementID: "placement", RawCallSign: "RAW", StreamID: 1, M3UAccountID: 2, StreamName: "US| Raw * Name", StreamFingerprint: "physical", RetrievalVersion: "v1", RetrievalRank: 2, RetrievalReason: "attributed label", EvidenceRefs: []string{"source-row"}, AdmittedSnapshot: "snapshot", OperationID: "op", GroupKey: "supp:group", Decision: "confirmed"}
	d.Key = SupplementalKey(d)
	return d
}
func TestSupplementalDurabilityUndoResetAndNoScore(t *testing.T) {
	svc := newTestService(t, "", "")
	d := supplementalFixture()
	input := svc.ReviewInputToken()
	guard := func(write func() error) error { return write() }
	if err := svc.SaveSupplementalDecisions("source", input, []SupplementalDecision{d}, false, guard); err != nil {
		t.Fatal(err)
	}
	durable := svc.ReviewStateToken()
	if svc.ReviewInputToken() != input {
		t.Fatal("choice invalidated cheap preparation")
	}
	if err := svc.SaveSupplementalDecisions("source", input, []SupplementalDecision{d}, true, guard); err != nil || svc.ReviewStateToken() != durable {
		t.Fatal("lost ack retry mutated", err)
	}
	d.Decision = "denied"
	if err := svc.SaveSupplementalDecisions("source", input, []SupplementalDecision{d}, true, guard); !errors.Is(err, ErrStaleReview) {
		t.Fatal("reverse retry accepted", err)
	}
	d.Decision = "confirmed"
	reloaded, err := LoadStateStore(svc.store.path)
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewService(reloaded, ServiceOptions{})
	if !reflect.DeepEqual(svc.SupplementalDecisions("source"), restarted.SupplementalDecisions("source")) {
		t.Fatal("restart lost physical history")
	}
	raw, _ := json.Marshal(restarted.SupplementalDecisions("source"))
	if strings.Contains(string(raw), "score") {
		t.Fatal("fake score persisted")
	}
	backup, err := svc.ResetBuilder("source", svc.ReviewStateToken(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(svc.SupplementalDecisions("source")) != 0 {
		t.Fatal("reset left decisions")
	}
	if _, err = svc.ResetBuilder("source", svc.ReviewStateToken(), backup, false); err != nil {
		t.Fatal(err)
	}
	if len(svc.SupplementalDecisions("source")) != 1 {
		t.Fatal("restore lost decisions")
	}
	if _, err = svc.ClearSupplementalDecisions("source", d.GroupKey); err != nil {
		t.Fatal(err)
	}
	if err = svc.SaveSupplementalDecisions("source", svc.ReviewInputToken(), []SupplementalDecision{d}, true, guard); !errors.Is(err, ErrStaleReview) {
		t.Fatal("late receipt replay resurrected Undo", err)
	}
}
func TestSupplementalPreservesPrimaryAndExactIdentity(t *testing.T) {
	svc := newTestService(t, "", "")
	d := supplementalFixture()
	guard := func(write func() error) error { return write() }
	primary := MatchDecision{Key: "prior", StreamKey: "2:1", Decision: "denied", ChannelID: d.ChannelID, StreamName: d.StreamName, StreamFingerprint: d.StreamFingerprint, DispatcharrFingerprint: "conn", Score: 100, NameScore: 100}
	if err := svc.SetMatchDecision("source", primary); err != nil {
		t.Fatal(err)
	}
	old := svc.MatchDecisions("source")
	if err := svc.SaveSupplementalDecisions("source", svc.ReviewInputToken(), []SupplementalDecision{d}, false, guard); !errors.Is(err, ErrStaleReview) {
		t.Fatal("primary denial overwritten")
	}
	if !reflect.DeepEqual(old, svc.MatchDecisions("source")) {
		t.Fatal("prior decision mutated")
	}
	d.StreamName = "Other Raw Name"
	d.StreamFingerprint = "other"
	d.Key = SupplementalKey(d)
	if err := svc.SaveSupplementalDecisions("source", svc.ReviewInputToken(), []SupplementalDecision{d}, false, guard); err != nil {
		t.Fatal(err)
	}
	channel := DraftChannel{ID: d.ChannelID, StationID: d.StationID, PlacementID: d.PlacementID, CallSign: d.RawCallSign, Name: "Canonical", Included: true}
	for _, which := range []string{"current", "connection", "station", "placement", "callsign"} {
		t.Run(which, func(t *testing.T) {
			c := channel
			conn := "conn"
			switch which {
			case "connection":
				conn = "new"
			case "station":
				c.StationID = "new"
			case "placement":
				c.PlacementID = "new"
			case "callsign":
				c.CallSign = "new"
			}
			draft := &Draft{Channels: []DraftChannel{c}}
			svc.ProjectSupplementalDecisions(draft, "source", conn)
			if (len(draft.Channels[0].Aliases) > 0) != (which == "current") {
				t.Fatal("identity fence failed", draft)
			}
			if draft.Channels[0].Name != "Canonical" {
				t.Fatal("canonical export name changed")
			}
		})
	}
	future := d
	future.StreamID++
	future.Key = SupplementalKey(future)
	if _, ok := svc.SupplementalDecisions("source")[future.Key]; ok {
		t.Fatal("future physical auto-approved")
	}
}
func TestSupplementalPersistenceFailureRollsBack(t *testing.T) {
	for _, stage := range []string{"encode", "write", "sync", "rename"} {
		t.Run(stage, func(t *testing.T) {
			s := newTestService(t, "", "")
			before := s.ReviewStateToken()
			s.store.beforeSave = func(at string) error {
				if at == stage {
					return errors.New("injected")
				}
				return nil
			}
			if err := s.SaveSupplementalDecisions("source", s.ReviewInputToken(), []SupplementalDecision{supplementalFixture()}, false, func(write func() error) error { return write() }); err == nil {
				t.Fatal("failed durable save reported success")
			}
			if before != s.ReviewStateToken() || len(s.SupplementalDecisions("source")) > 0 {
				t.Fatal("failed save published")
			}
		})
	}
}
