package lineuparr

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

// ReviewInputToken excludes only saved decisions and workflow bookkeeping.
// Confirm can expose a suppressed base alias; Channels therefore stays in it.
func (s *StateStore) reviewInputTokenLocked() string {
	state := s.state
	state.SourceFingerprint = ""
	state.MatchDecisions = nil
	state.SupplementalDecisions = nil
	state.Workflow = WorkflowProgress{}
	data, _ := json.Marshal(state)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
func (s *Service) ReviewInputToken() string {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	return s.store.reviewInputTokenLocked()
}
func (s *Service) SetAdmittedMatchDecisions(fingerprint, inputToken string, decisions []MatchDecision, guard func(func() error) error) error {
	_, err := s.SaveAdmittedMatchDecisions(fingerprint, inputToken, decisions, false, guard)
	return err
}

// Return the committed input token while still under the state write lock. A
// confirmation may expose a suppressed alias; only its durable receipt can be
// retried against that new input state, never the other old admitted groups.
func (s *Service) SaveAdmittedMatchDecisions(fingerprint, inputToken string, decisions []MatchDecision, retryOnly bool, guard func(func() error) error) (string, error) {
	normalized := make([]MatchDecision, len(decisions))
	for i, d := range decisions {
		var err error
		normalized[i], err = normalizeMatchDecision(d, time.Now().UTC())
		if err != nil {
			return "", err
		}
	}
	committed := ""
	publish := func(write func() error) error {

		var err error
		if guard != nil {
			err = guard(write)
		} else {
			err = write()
		}
		if err == nil {
			committed = s.store.reviewInputTokenLocked()
		}
		return err
	}
	err := s.store.setMatchDecisions(fingerprint, inputToken, normalized, true, retryOnly, publish)
	return committed, err
}

// Optional remote catalogs keep their existing read/expiry behavior. Only
// immutable local evidence can admit a choice without rebuilding those sources.
func (s *Service) LocalReviewSources(country, provider string) bool {
	return len(s.sourceURLs(LineupContext{Country: country, ProviderName: provider})) == 0
}
