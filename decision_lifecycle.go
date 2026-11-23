package main

import (
	"context"
	"crypto/rand"
	"strings"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

type lifecycleInventoryAPI interface {
	CaptureLifecycleInventory(context.Context, dispatcharr.Config, []dispatcharr.LifecycleReference) (dispatcharr.LifecycleInventoryReceipt, error)
}

// lifecyclePrimaryCovered compares exactly the safe projection actually used by
// Streams, including its documented trim, without broadening decision equality.
func lifecyclePrimaryCovered(primary []dispatcharr.Stream, inventory []dispatcharr.LifecycleRow) bool {
	rows := map[int64]dispatcharr.LifecycleRow{}
	for _, row := range inventory {
		rows[row.ID] = row
	}
	for _, stream := range primary {
		row, ok := rows[stream.ID]
		if !ok || row.AccountID != stream.M3UAccountID || strings.TrimSpace(row.Name) != stream.Name {
			return false
		}
	}
	return true
}

// Called only by a successfully matched explicit refresh while refreshMu is
// held. Failure is advisory and never mutates choices or fails matching.
func (s *dispatcharrServer) prepareRefreshLifecycle(ctx context.Context, connection dispatcharr.Config, config appconfig.Config, stateToken string, draft *builder.Draft, streams []dispatcharr.Stream) (*builder.LifecycleProjection, builder.LifecycleResult) {
	result := builder.LifecycleResult{Status: "deferred", Reason: "Complete lifecycle inventory unavailable; choices preserved"}
	fail := func() (*builder.LifecycleProjection, builder.LifecycleResult) {
		s.lifecycleMu.Lock()
		s.lifecycleUnsafe = true
		s.lifecycleMu.Unlock()
		return nil, result
	}
	api, ok := s.client.(lifecycleInventoryAPI)
	if !ok {
		return fail()
	}
	started := time.Now().UTC()
	receipt, err := api.CaptureLifecycleInventory(ctx, connection, s.lineup.builder.LifecycleReferences(config.Fingerprint()))
	if err != nil || receipt.Coverage() != dispatcharr.LifecycleCoverageComplete || !lifecyclePrimaryCovered(streams, receipt.Streams()) {
		return fail()
	}
	epoch := s.lifecycleEpoch()
	if s.lifecycleIsUnsafe() {
		epoch = rand.Text()
		s.lifecycleMu.Lock()
		s.lifecycleProcessEpoch = epoch
		s.lifecycleMu.Unlock()
	}
	plan, err := s.lineup.builder.PrepareLifecycle(ctx, config.Fingerprint(), connection.Fingerprint(), stateToken, rand.Text(), epoch, started, time.Now().UTC(), draft.Channels, receipt)
	if err != nil {
		return fail()
	}
	return plan, plan.Result
}

func (s *dispatcharrServer) rememberLifecycle(source, connection string, result builder.LifecycleResult) {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.lifecycleSource, s.lifecycleConnection, s.lifecycleResult = source, connection, result
}
func (s *dispatcharrServer) previousLifecycle(source, connection string) builder.LifecycleResult {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.lifecycleSource == source && s.lifecycleConnection == connection && s.lifecycleResult.Status != "" {
		return s.lifecycleResult
	}
	return builder.LifecycleResult{Status: "not-checked", Reason: "Lifecycle is checked only during a successful explicit refresh"}
}

func (s *dispatcharrServer) retireLifecycleOperations(keys []string) {
	s.reviewMu.Lock()
	defer s.reviewMu.Unlock()
	s.retireLifecycleOperationsLocked(keys)
}
func (s *dispatcharrServer) retireLifecycleOperationsLocked(keys []string) {
	removed := map[string]bool{}
	for _, key := range keys {
		removed[key] = true
	}
	for token, op := range s.operations {
		for _, key := range op.Members {
			if removed[key] {
				op.State = "retired"
				s.operations[token] = op
				break
			}
		}
	}
}

func (s *dispatcharrServer) lifecycleIsUnsafe() bool {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	return s.lifecycleUnsafe
}
func (s *dispatcharrServer) invalidateRefreshLifecycle(connection dispatcharr.Config, config appconfig.Config, inputs reviewInputsRevision, token string) bool {
	changed := false
	dispatchCurrent, err := s.config.WhileCurrent(connection.Fingerprint(), func() error {
		lineupCurrent, inner := s.lineup.store.WhileConfiguration(config, func() error {
			return s.commitAdmittedDecision(reviewAdmission{inputs: inputs}, func() error {
				var e error
				changed, e = s.lineup.builder.InvalidateLifecycleContinuity(config.Fingerprint(), token)
				return e
			})
		})
		if !lineupCurrent && inner == nil {
			return builder.ErrStaleReview
		}
		return inner
	})
	s.lifecycleMu.Lock()
	s.lifecycleUnsafe = err != nil || !dispatchCurrent
	s.lifecycleMu.Unlock()
	return changed
}

func (s *dispatcharrServer) lifecycleEpoch() string {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.lifecycleProcessEpoch == "" {
		s.lifecycleProcessEpoch = rand.Text()
	}
	return s.lifecycleProcessEpoch
}
