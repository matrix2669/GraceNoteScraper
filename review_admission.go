package main

import (
	"fmt"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

type reviewAdmission struct {
	inputs              reviewInputsRevision
	snapshot, inventory string
	candidates          map[string]dispatcharr.Candidate
	groups              map[string][]string
	settled             map[string]bool
	retryOnly           bool
}

func (s *dispatcharrServer) derivedRevision() string {
	s.reviewMu.RLock()
	token := snapshotIdentity(s.snapshot)
	s.reviewMu.RUnlock()
	conn, connected := s.config.Get()
	return fmt.Sprintf("%t:%s:%s:%s", connected, conn.Fingerprint(), token, s.inventoryObservationToken())
}
func (s *dispatcharrServer) publishAdmission(inputs reviewInputsRevision, snapshot dispatcharrMatchSnapshot, candidates []dispatcharr.Candidate) {
	inventory := s.inventoryObservationToken()
	token := snapshotIdentity(snapshot)
	s.reviewMu.Lock()
	defer s.reviewMu.Unlock()
	if snapshotIdentity(s.snapshot) != token {
		return
	}
	old := s.admission
	next := reviewAdmission{inputs: inputs, snapshot: token, inventory: inventory, candidates: map[string]dispatcharr.Candidate{}, groups: map[string][]string{}, settled: map[string]bool{}}
	// Keep immutable admitted groups for identical lost-acknowledgement retries.
	// The store rejects conflicting settled constituents under its write lock.
	if old.inputs == inputs && old.snapshot == token && old.inventory == inventory {
		for k, v := range old.candidates {
			next.candidates[k] = v
		}
		for k, v := range old.groups {
			next.groups[k] = v
		}
		for k, v := range old.settled {
			next.settled[k] = v
		}
	}
	for _, c := range candidates {
		next.candidates[c.Key] = c
	}
	for _, g := range dispatcharr.GroupCandidates(candidates) {
		next.groups[g.Key] = append([]string(nil), g.CandidateKeys...)
		delete(next.settled, g.Key)
	}
	s.admission = next
}
func (s *dispatcharrServer) admittedCandidates(key string) ([]dispatcharr.Candidate, reviewAdmission, bool) {
	inputs := s.lineup.reviewInputsRevision()
	inventory := s.inventoryObservationToken()
	s.reviewMu.RLock()
	defer s.reviewMu.RUnlock()
	a := s.admission
	if a.candidates == nil || a.inputs != inputs || a.inventory != inventory || a.snapshot != snapshotIdentity(s.snapshot) {
		return nil, a, false
	}
	keys, found := a.groups[key]
	if !found {
		if _, ok := a.candidates[key]; ok {
			keys = []string{key}
		}
	}
	if len(keys) == 0 {
		return nil, a, false
	}
	result := make([]dispatcharr.Candidate, 0, len(keys))
	for _, k := range keys {
		c, ok := a.candidates[k]
		if !ok {
			return nil, a, false
		}
		result = append(result, c)
	}
	a.retryOnly = a.settled[key]
	return result, a, true
}

// Configuration is already held by saveWhileCurrent; lock evidence and guide
// before the builder write lock. The write checks choices itself and takes the
// snapshot/inventory locks last, matching automatic export publication.
func (s *dispatcharrServer) commitAdmittedDecision(a reviewAdmission, update func() error) error {
	guarded := func() error {
		s.lineup.state.mu.RLock()
		defer s.lineup.state.mu.RUnlock()
		if s.lineup.state.guide != a.inputs.guide || s.lineup.state.sourceFingerprint != a.inputs.config.Fingerprint() {
			return builder.ErrStaleReview
		}
		return update()
	}
	return s.lineup.whileEvidence(a.inputs.index, a.inputs.tmdb, guarded)
}
func (s *dispatcharrServer) whileAdmissionSnapshot(a reviewAdmission, write func() error) error {
	s.reviewMu.RLock()
	defer s.reviewMu.RUnlock()
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()
	if snapshotIdentity(s.snapshot) != a.snapshot || s.cache.observationTokenLocked() != a.inventory {
		return builder.ErrStaleReview
	}
	return write()
}

func (s *dispatcharrServer) recordDecisionReceipt(key, committedInputs string, a reviewAdmission, candidates []dispatcharr.Candidate) {
	s.reviewMu.Lock()
	defer s.reviewMu.Unlock()
	if snapshotIdentity(s.snapshot) != a.snapshot {
		return
	}
	next := s.admission
	if committedInputs != a.inputs.choices {
		a.inputs.choices = committedInputs
		next = reviewAdmission{inputs: a.inputs, snapshot: a.snapshot, inventory: a.inventory, candidates: map[string]dispatcharr.Candidate{}, groups: map[string][]string{}}
		for _, c := range candidates {
			next.candidates[c.Key] = c
			next.groups[key] = append(next.groups[key], c.Key)
		}
	} else if next.inputs != a.inputs || next.snapshot != a.snapshot || next.inventory != a.inventory {
		return
	}
	settled := map[string]bool{}
	for k, v := range next.settled {
		settled[k] = v
	}
	settled[key] = true
	next.settled = settled
	s.admission = next
}
