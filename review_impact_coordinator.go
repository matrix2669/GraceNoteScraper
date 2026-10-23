package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

// One server-owned worker coalesces changes. The signal is cheap and may only be
// sent after a successful durable write/publication, with all save locks released.
func (s *dispatcharrServer) startImpactCoordinator(ctx context.Context) {
	s.impactMu.Lock()
	defer s.impactMu.Unlock()
	if s.impactSignal != nil {
		return
	}
	s.impactContext, s.impactStop = context.WithCancel(ctx)
	s.impactSignal = make(chan struct{}, 1)
	s.impactStopped = make(chan struct{})
	go s.coordinateImpact(s.impactContext, s.impactSignal, s.impactStopped)
}
func (s *dispatcharrServer) signalImpact()  { s.requestImpact(false) }
func (s *dispatcharrServer) recoverImpact() { s.requestImpact(true) }
func (s *dispatcharrServer) requestImpact(recovery bool) {
	// Read owner tokens before the scheduler mutex, preserving save lock order.
	inputs := s.lineup.reviewInputsRevision()
	state := s.lineup.builder.ReviewStateToken()
	inventory := s.inventoryObservationToken()
	conn, _ := s.config.Get()
	s.reviewMu.RLock()
	snapshot := snapshotIdentity(s.snapshot)
	s.reviewMu.RUnlock()
	key := supplementalHash([]any{configurationToken(inputs.config), fmt.Sprintf("%p", inputs.guide), inputs.policy, inputs.index, inputs.tmdb, inputs.choices, state, inventory, snapshot, conn.Fingerprint()})
	s.impactMu.Lock()
	defer s.impactMu.Unlock()
	if s.impactSignal == nil || s.impactContext.Err() != nil {
		return
	}
	if !recovery && s.impactRequestedKey == key {
		return
	}
	s.impactRequestedKey = key
	s.impactDirty++
	s.impactLifecycle = "queued"
	if s.impact != nil && s.impact.response.State == "running" {
		s.impact.cancel()
	}
	select {
	case s.impactSignal <- struct{}{}:
	default:
	}
}
func (s *dispatcharrServer) interruptImpact() {
	s.impactMu.Lock()
	job := s.impact
	if job != nil && job.cancel != nil {
		job.cancel()
	}
	s.impactMu.Unlock()
	if job != nil && job.done != nil {
		<-job.done
	}
}
func (s *dispatcharrServer) stopImpactCoordinator() {
	s.impactMu.Lock()
	stop, done := s.impactStop, s.impactStopped
	s.impactMu.Unlock()
	if stop != nil {
		stop()
	}
	s.interruptImpact()
	if done != nil {
		<-done
	}
}
func (s *dispatcharrServer) coordinateImpact(ctx context.Context, signal <-chan struct{}, stopped chan<- struct{}) {
	defer close(stopped)
	for {
		select {
		case <-ctx.Done():
			return
		case <-signal:
		}
		// Quiet period resets on every mutation, bounding a burst to one preparation.
		timer := time.NewTimer(750 * time.Millisecond)
	quiet:
		for {
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-signal:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(750 * time.Millisecond)
			case <-timer.C:
				break quiet
			}
		}
		s.impactMu.Lock()
		revision := s.impactDirty
		s.impactLifecycle = "preparing"
		s.impactMu.Unlock()
		for attempt := 0; attempt < 2; attempt++ {
			// Primary refresh owns its own flag. Background preparation never
			// reserves refreshMu, so a slow draft cannot reject a user Refresh.

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/background-validation", nil).WithContext(ctx)
			s.startSupplementalImpact(recorder, request, true)
			var prepared supplementalImpactResponse
			_ = json.Unmarshal(recorder.Body.Bytes(), &prepared)
			s.impactMu.Lock()
			job := s.impact
			if s.impactDirty == revision {
				s.impactLifecycle = ""
			}
			if recorder.Code >= 400 {
				state := "failed"
				if recorder.Code == 409 {
					state = "refresh-required"
				}
				job = &supplementalImpactJob{response: supplementalImpactResponse{State: state, Message: "Validation could not prepare the current review. Reload review to retry, or Refresh streams when inputs changed. Generation remains available."}}
				s.impact = job
			} else if prepared.State == "idle" {
				s.impact = &supplementalImpactJob{response: prepared}
				s.impactCompleted = nil
				job = s.impact
			}
			s.impactMu.Unlock()
			if job != nil && job.done != nil {
				select {
				case <-ctx.Done():
					s.interruptImpact()
					return
				case <-job.done:
				}
			}
			s.impactMu.Lock()
			changed := s.impactDirty != revision
			failed := job != nil && job.response.State == "failed"
			s.impactMu.Unlock()
			if changed || !failed || attempt == 1 {
				break
			}
			// One transient retry; polling can never create an infinite retry loop.
			select {
			case <-ctx.Done():
				return
			case <-time.After(750 * time.Millisecond):
			}
		}
	}
}

// Observe successful mutating handlers once they have released their own locks.
// A read/status/refill/export GET cannot schedule validation.
type reviewMutationResponse struct {
	http.ResponseWriter
	status int
}

func (w *reviewMutationResponse) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *reviewMutationResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(data)
}
func (s *dispatcharrServer) observeReviewMutations(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		observed := &reviewMutationResponse{ResponseWriter: w}
		next.ServeHTTP(observed, r)
		if observed.status >= 200 && observed.status < 300 && r.Method != http.MethodGet && r.Method != http.MethodHead && strings.HasPrefix(r.URL.Path, "/api/") && !strings.Contains(r.URL.Path, "supplemental-impact") {
			s.signalImpact()
		}
	})
}

// Primary refresh takes priority even between an obsolete child exiting and a
// pending replacement preparing. Count concurrent attempts until each returns.
func (s *dispatcharrServer) beginImpactRefresh() {
	s.impactMu.Lock()
	s.impactRefreshes++
	s.impactMu.Unlock()
	s.interruptImpact()
}
func (s *dispatcharrServer) endImpactRefresh() {
	s.impactMu.Lock()
	s.impactRefreshes--
	resume := s.impactRefreshes == 0
	s.impactMu.Unlock()
	if resume {
		s.recoverImpact()
	}
}
