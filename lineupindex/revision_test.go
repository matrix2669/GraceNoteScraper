package lineupindex

import (
	"testing"
	"time"
)

func TestEvidenceRevisionFencesEveryWriter(t *testing.T) {
	s := &Service{}
	revision := s.EvidenceRevision()
	done := make(chan struct{})
	current, err := s.WhileEvidenceRevision(revision, func() error {
		go func() { s.mu.Lock(); s.mu.Unlock(); close(done) }()
		select {
		case <-done:
			t.Fatal("writer crossed evidence fence")
		case <-time.After(time.Millisecond):
		}
		return nil
	})
	if !current || err != nil {
		t.Fatal(current, err)
	}
	<-done
	if s.EvidenceRevision() == revision {
		t.Fatal("write did not invalidate evidence")
	}
	if current, _ := s.WhileEvidenceRevision(revision, func() error { t.Fatal("stale evidence action ran"); return nil }); current {
		t.Fatal("stale revision accepted")
	}
}
