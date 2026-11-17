package lineuparr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
)

// ReviewProjection exposes only immutable reads and captured-source projection.
// A staged projection never substitutes a store on the live Service.
type ReviewProjection interface {
	BuildCaptured(context.Context, LineupContext, []InputChannel, *SourceCapture) (*Draft, error)
	MatchDecisions(string) map[string]MatchDecision
	SupplementalDecisions(string) map[string]SupplementalDecision
	ProjectSupplementalDecisions(*Draft, string, string)
	ReviewStateToken() string
	ReviewInputToken() string
	ResetGeneration() uint64
	PublicationGeneration() uint64
}

// LifecycleProjection owns a private Service with fresh mutexes. Its state and
// mutation methods are deliberately not exported to the server.
type LifecycleProjection struct {
	owner     *StateStore
	view      *Service
	expected  string
	completed time.Time
	Result    LifecycleResult
	keys      []string
}

func (p *LifecycleProjection) BuildCaptured(c context.Context, l LineupContext, i []InputChannel, s *SourceCapture) (*Draft, error) {
	return p.view.BuildCaptured(c, l, i, s)
}
func (p *LifecycleProjection) MatchDecisions(s string) map[string]MatchDecision {
	return p.view.MatchDecisions(s)
}
func (p *LifecycleProjection) SupplementalDecisions(s string) map[string]SupplementalDecision {
	return p.view.SupplementalDecisions(s)
}
func (p *LifecycleProjection) ProjectSupplementalDecisions(d *Draft, s, c string) {
	p.view.ProjectSupplementalDecisions(d, s, c)
}
func (p *LifecycleProjection) ReviewStateToken() string      { return p.view.ReviewStateToken() }
func (p *LifecycleProjection) ReviewInputToken() string      { return p.view.ReviewInputToken() }
func (p *LifecycleProjection) ResetGeneration() uint64       { return p.view.ResetGeneration() }
func (p *LifecycleProjection) PublicationGeneration() uint64 { return p.view.PublicationGeneration() }
func (p *LifecycleProjection) RetiredKeys() []string         { return append([]string(nil), p.keys...) }

func lifecycleBackupIdentity(state State) (string, string, error) {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(append(data, '\n'))
	digest := hex.EncodeToString(sum[:])
	return "lifecycle-" + digest + ".json", digest, nil
}

func (s *Service) PrepareLifecycle(ctx context.Context, source, connection, token, operation, epoch string, started, now time.Time, targets []DraftChannel, receipt dispatcharr.LifecycleInventoryReceipt) (*LifecycleProjection, error) {
	s.store.mu.RLock()
	if s.store.reviewTokenLocked() != token {
		s.store.mu.RUnlock()
		return nil, ErrStaleReview
	}
	state := cloneReviewState(s.store.state)
	s.store.mu.RUnlock()
	view := NewService(&StateStore{state: state}, s.options)
	result, keys, err := view.reconcileLifecycle(ctx, source, connection, token, operation, epoch, started, now, targets, receipt)
	if err != nil {
		return nil, err
	}
	return &LifecycleProjection{owner: s.store, view: view, expected: token, completed: receipt.CompletedAt(), Result: result, keys: keys}, nil
}

// CommitLifecycle executes the only durable write inside the caller's final
// publication fence. The guard must do all checks before write, then publish
// precomputed memory values without returning a new failure after write succeeds.
func (s *Service) CommitLifecycle(ctx context.Context, p *LifecycleProjection, guard func(func() error) error) error {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if p == nil || p.owner != s.store || s.store.reviewTokenLocked() != p.expected {
		return ErrStaleReview
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !p.completed.IsZero() && (time.Since(p.completed) > time.Minute || time.Now().Before(p.completed)) {
		return ErrStaleReview
	}
	next := cloneReviewState(p.view.store.state)
	write := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(p.keys) > 0 {
			if _, _, err := s.store.writeLifecycleBackup(s.store.state); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if p.view.ReviewStateToken() == p.expected {
			return nil
		}
		previous := s.store.state
		s.store.state = next
		if err := s.store.saveLocked(); err != nil {
			s.store.state = previous
			return err
		}
		return nil
	}
	if guard != nil {
		return guard(write)
	}
	return write()
}
