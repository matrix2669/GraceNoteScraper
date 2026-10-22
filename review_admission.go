package main

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"net/http"
	"slices"
)

type reviewAdmission struct {
	inputs              reviewInputsRevision
	snapshot, inventory string
	candidates          map[string]dispatcharr.Candidate
	groups              map[string][]string
	tokens              map[string]string
	operation           string
	retryOnly           bool
}

func (s *dispatcharrServer) derivedRevision() string {
	s.reviewMu.RLock()
	token := snapshotIdentity(s.snapshot)
	s.reviewMu.RUnlock()
	conn, connected := s.config.Get()
	return fmt.Sprintf("%t:%s:%s:%s", connected, conn.Fingerprint(), token, s.inventoryObservationToken())
}

// A token identifies one explicit choice, never a displayed history representative.
// Receipts live only in this process and review scope. Unknown/retired scopes are
// superseded, not evidence that a prior request failed to save.
type reviewOperation struct {
	Key, Decision, State, Scope string
	Members                     []string
}

func reviewOperationScope(config, connection, snapshot, inventory string, reset uint64, guide any) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%s:%s:%d:%p", config, connection, snapshot, inventory, reset, guide))))
}

// Caller holds builder, reviewMu and cache.mu in that order. No derived work is
// performed here; this is the linearization point for cache and response data.
func (s *dispatcharrServer) publishAdmissionLocked(inputs reviewInputsRevision, snapshot, inventory, scope string, candidates []dispatcharr.Candidate) map[string]string {
	old := s.admission
	next := reviewAdmission{inputs: inputs, snapshot: snapshot, inventory: inventory, candidates: map[string]dispatcharr.Candidate{}, groups: map[string][]string{}, tokens: map[string]string{}}
	if s.operationScope != scope {
		s.operationScope = scope
		s.operations = map[string]reviewOperation{}
	}
	if old.inputs == inputs && old.snapshot == snapshot && old.inventory == inventory {
		for k, v := range old.candidates {
			next.candidates[k] = v
		}
		for k, v := range old.groups {
			next.groups[k] = v
		}
		for k, v := range old.tokens {
			next.tokens[k] = v
		}
	}
	for _, c := range candidates {
		next.candidates[c.Key] = c
	}
	result := map[string]string{}
	for _, g := range dispatcharr.GroupCandidates(candidates) {
		unchanged := slices.Equal(old.groups[g.Key], g.CandidateKeys)
		for _, key := range g.CandidateKeys {
			if old.candidates[key] != next.candidates[key] {
				unchanged = false
			}
		}
		next.groups[g.Key] = append([]string(nil), g.CandidateKeys...)
		token := next.tokens[g.Key]
		// An ordinary GET can expose a fresh choice after Undo, but cannot rearm
		// any old request. Its old opaque token remains retired forever.
		if op, ok := s.operations[token]; !ok || op.State != "offered" || !unchanged {
			if ok && op.State == "offered" {
				op.State = "superseded"
				s.operations[token] = op
			}
			token = rand.Text()
			s.operations[token] = reviewOperation{Key: g.Key, State: "offered", Scope: scope}
		}
		next.tokens[g.Key] = token
		result[g.Key] = token
	}
	s.admission = next
	return result
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
	keys := a.groups[key]
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
	a.operation = a.tokens[key]
	op := s.operations[a.operation]
	a.retryOnly = op.State == "saved" || op.State == "undone"
	return result, a, a.operation != "" && (op.State == "offered" || op.State == "saved")
}

func (s *dispatcharrServer) retireDecisionOperations(keys []string) {
	removed := map[string]bool{}
	for _, key := range keys {
		removed[key] = true
	}
	s.reviewMu.Lock()
	defer s.reviewMu.Unlock()
	for token, op := range s.operations {
		for _, key := range op.Members {
			if removed[key] {
				op.State = "undone"
				s.operations[token] = op
				break
			}
		}
	}
}

func (s *dispatcharrServer) handleDecisionReceipt(w http.ResponseWriter, r *http.Request) {
	// Serialize the small receipt lookup with the durable mutation, not derived
	// reads: offered then authoritatively means this operation has not committed.
	if !s.refreshMu.TryLock() {
		http.Error(w, "A save or stream refresh is still running; check saved state again", 409)
		return
	}
	defer s.refreshMu.Unlock()
	config, _, _ := s.lineup.store.Get()
	connection, _ := s.config.Get()
	generation := s.lineup.builder.ResetGeneration()
	guide := s.lineup.state.Get()
	s.reviewMu.Lock()
	defer s.reviewMu.Unlock()
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()
	scope := reviewOperationScope(configurationToken(config), connection.Fingerprint(), snapshotIdentity(s.snapshot), s.cache.observationTokenLocked(), generation, guide)
	token := r.URL.Query().Get("operation")
	op, ok := s.operations[token]
	state := "superseded"
	if ok && op.Scope == scope {
		state = op.State
		if state == "offered" {
			// Retire an uncommitted token before reporting absence. A delayed POST
			// must not arrive after this receipt and invalidate that conclusion.
			state = "not-saved"
			op.State = state
			s.operations[token] = op
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeLineuparrJSON(w, 200, map[string]string{"operation": token, "key": op.Key, "decision": op.Decision, "state": state})
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

func (s *dispatcharrServer) recordDecisionReceipt(key, decision, committedInputs string, a reviewAdmission, candidates []dispatcharr.Candidate, members []string) {
	s.reviewMu.Lock()
	defer s.reviewMu.Unlock()
	if snapshotIdentity(s.snapshot) != a.snapshot {
		return
	}
	op := s.operations[a.operation]
	op.Decision = decision
	op.State = "saved"
	op.Members = append([]string(nil), members...)
	s.operations[a.operation] = op
	next := s.admission
	if committedInputs != a.inputs.choices {
		a.inputs.choices = committedInputs
		next = reviewAdmission{inputs: a.inputs, snapshot: a.snapshot, inventory: a.inventory, candidates: map[string]dispatcharr.Candidate{}, groups: map[string][]string{}, tokens: map[string]string{key: a.operation}}
		for _, c := range candidates {
			next.candidates[c.Key] = c
			next.groups[key] = append(next.groups[key], c.Key)
		}
	} else if next.inputs != a.inputs || next.snapshot != a.snapshot || next.inventory != a.inventory {
		return
	}
	s.admission = next
}
