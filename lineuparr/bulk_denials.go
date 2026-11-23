package lineuparr

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrStaleReview = errors.New("review changed; preview affiliate exclusions again")

func (s *StateStore) reviewTokenLocked() string {
	data, _ := json.Marshal(s.state)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// ReviewStateToken fences every persisted builder choice, not only decisions.
func (s *Service) ReviewStateToken() string {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	return s.store.reviewTokenLocked()
}

// ApplyReviewedDenials atomically appends a reviewed batch without replacing
// manual choices. Disk errors restore memory; saveLocked commits by atomic rename.
func (s *Service) ApplyReviewedDenials(fingerprint, token string, decisions []MatchDecision) error {
	if len(decisions) == 0 {
		return errors.New("select at least one exclusion")
	}
	normalized := make([]MatchDecision, len(decisions))
	for i, d := range decisions {
		var err error
		normalized[i], err = normalizeMatchDecision(d, time.Now().UTC())
		if err != nil {
			return err
		}
		if d.Decision != "denied" || decisionNameScore(d) < 95 {
			return errors.New("bulk review accepts only eligible denials")
		}
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if s.store.reviewTokenLocked() != token {
		return ErrStaleReview
	}
	previous := s.store.state
	old := s.store.state.MatchDecisions
	if s.store.state.SourceFingerprint != fingerprint {
		old = nil
	}
	next := make(map[string]MatchDecision, len(old)+len(normalized))
	for k, v := range old {
		next[k] = v
	}
	for _, d := range normalized {
		if _, exists := next[d.Key]; exists {
			return ErrStaleReview
		}
		if included := s.store.state.Channels[d.ChannelID].Included; s.store.state.SourceFingerprint == fingerprint && included != nil && !*included {
			return ErrMatchChannelExcluded
		}
		next[d.Key] = d
	}
	s.store.ensureSourceLocked(fingerprint)
	s.store.state.MatchDecisions = next
	if err := s.store.saveLocked(); err != nil {
		s.store.state = previous
		return err
	}
	return nil
}
