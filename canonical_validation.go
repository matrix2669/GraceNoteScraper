package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sort"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	matcher "github.com/daniel-widrick/GraceNoteScraper/lineuparr_matcher"
)

type canonicalLead struct {
	TargetID, Name, Proposal string
	AccountID, StreamID      int64
}

// Immutable after publication, owned by reviewMu with the corresponding snapshot.
// This is derived proof, not a durable user choice.
type canonicalValidation struct {
	key, generation, state, inventory, rawKey, activeSnapshot string
	inputs                                                    reviewInputsRevision
	base                                                      dispatcharrMatchSnapshot
	accepted                                                  map[string]bool
	reasons                                                   map[string]string
	leads                                                     []canonicalLead
	failed                                                    bool
}

func cloneCanonicalDraft(d *builder.Draft) (*builder.Draft, error) {
	data, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	var out builder.Draft
	if err = json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	out.SourceCapture = d.SourceCapture
	return &out, nil
}
func canonicalDraftKey(d *builder.Draft) string {
	e := builder.ExportFromDraft(d)
	e.Date = ""
	return supplementalHash([]any{e, d.CanonicalNameProposals})
}
func canonicalLineup(config appconfig.Config) builder.LineupContext {
	return builder.LineupContext{SourceFingerprint: config.Fingerprint(), Country: config.Gracenote.Country, PostalCode: config.Gracenote.PostalCode, ProviderName: config.Gracenote.ProviderName, LineupID: config.Gracenote.LineupID}
}

// Called only by projection, never schedules work. A stale proof cannot rename
// a draft. The retained canonical-free snapshot preserves baseline protections.
func (s *dispatcharrServer) canonicalProjection(d *builder.Draft, config appconfig.Config, actual dispatcharrMatchSnapshot) (dispatcharrMatchSnapshot, string) {
	inputs := s.lineup.reviewInputsRevision()
	state := s.lineup.builder.ReviewStateToken()
	inventory := s.inventoryObservationToken()
	s.reviewMu.RLock()
	proof := s.canonical
	s.reviewMu.RUnlock()
	if proof == nil {
		return actual, ""
	}
	token := proof.generation
	relevant := proof.inputs.config == inputs.config && supplementalEvidenceCurrent(proof.base, inputs) && proof.inventory == inventory && proof.base.dispatcharrFingerprint == actual.dispatcharrFingerprint && proof.base.inventoryDigest == actual.inventoryDigest
	if !relevant {
		return actual, token
	}
	valid := proof.state == state && proof.inputs == inputs && proof.rawKey == canonicalDraftKey(d) && proof.activeSnapshot == snapshotIdentity(actual)
	if valid && len(proof.accepted) > 0 {
		if err := builder.ApplyCanonicalProposalsForValidation(d, proof.accepted); err == nil {
			for i := range d.CanonicalNameProposals {
				p := &d.CanonicalNameProposals[i]
				if proof.accepted[p.Hash] {
					p.Status = "validated"
					p.Reason = "Exact consumer effects covered by existing stream choices"
				} else if reason := proof.reasons[p.Hash]; reason != "" {
					p.Reason = reason
				}
			}
			_ = builder.AttachDraftSignatures(d)
			return actual, token
		}
	}
	for i := range d.CanonicalNameProposals {
		p := &d.CanonicalNameProposals[i]
		if p.Status == "pending" {
			if valid {
				if r := proof.reasons[p.Hash]; r != "" {
					p.Reason = r
				}
			} else {
				p.Reason = "Inputs changed; awaiting current consumer validation"
			}
		}
	}
	channels, _ := dispatcharrSnapshotChannels(d)
	if dispatcharrInputsSubset(channels, proof.base.channels) {
		return proof.base, token
	}
	return actual, token
}

func (s *dispatcharrServer) canonicalLeads(d *builder.Draft, snapshot dispatcharrMatchSnapshot, inputs reviewInputsRevision) []dispatcharr.SupplementalCandidate {
	s.reviewMu.RLock()
	proof := s.canonical
	s.reviewMu.RUnlock()
	if proof == nil || !supplementalEvidenceCurrent(proof.base, inputs) || proof.base.inventoryDigest != snapshot.inventoryDigest || proof.base.dispatcharrFingerprint != snapshot.dispatcharrFingerprint || proof.inventory != s.inventoryObservationToken() {
		return nil
	}
	targets := map[string]builder.DraftChannel{}
	for _, c := range d.Channels {
		targets[c.ID] = c
	}
	proposals := map[string]builder.CanonicalNameProposal{}
	for _, p := range d.CanonicalNameProposals {
		proposals[p.Hash] = p
	}
	out := []dispatcharr.SupplementalCandidate{}
	for _, lead := range proof.leads {
		c, ok := targets[lead.TargetID]
		p, present := proposals[lead.Proposal]
		if !ok || !c.Included || !present {
			continue
		}
		for _, stream := range snapshot.identityStreams {
			if stream.ID == lead.StreamID && stream.AccountID == lead.AccountID && stream.Name == lead.Name {
				out = append(out, dispatcharr.SupplementalCandidate{Target: dispatcharr.SupplementalTarget{ID: c.ID, StationID: c.StationID, PlacementID: c.PlacementID, Name: c.Name, Number: c.Number, CallSign: c.CallSign}, Stream: stream, Rank: 4, Reason: "Proposed canonical name would add this stream; Confirm/Deny its full name", Evidence: "canonical-consumer:" + proof.generation, Descriptor: fmt.Sprintf("%s → %s", p.BeforeName, p.ProposedName)})
			}
		}
	}
	return out
}

func canonicalRequest(before, after *builder.Draft, snapshot dispatcharrMatchSnapshot) (dispatcharr.ImpactRequest, builder.ExportFile, builder.ExportFile, error) {
	b, p := builder.ExportFromDraft(before), builder.ExportFromDraft(after)
	b.Date = ""
	p.Date = ""
	bb, err := json.Marshal(b)
	if err != nil {
		return dispatcharr.ImpactRequest{}, b, p, err
	}
	pp, err := json.Marshal(p)
	if err != nil {
		return dispatcharr.ImpactRequest{}, b, p, err
	}
	r := dispatcharr.ImpactRequest{Country: before.CountryCode, Baseline: string(bb), Proposed: string(pp)}
	for _, stream := range snapshot.identityStreams {
		r.Streams = append(r.Streams, dispatcharr.ImpactStream{Name: stream.Name, GroupName: stream.GroupName})
	}
	return r, b, p, nil
}
func canonicalSafeDelta(d canonicalDelta) bool {
	return len(d.Lost) == 0 && len(d.Conflict) == 0 && len(d.Unresolved) == 0
}

// Runs within the existing single coalesced validation worker. At most two
// consumer passes and one unchanged primary pass per input revision; all names
// are checked together, not one full process per proposal.
func (s *dispatcharrServer) validateCanonicalNames(parent context.Context) {
	s.impactMu.Lock()
	if s.impactRefreshes > 0 {
		s.impactMu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	done := make(chan struct{})
	s.canonicalCancel, s.canonicalDone = cancel, done
	s.impactMu.Unlock()
	defer func() {
		cancel()
		s.impactMu.Lock()
		s.canonicalCancel = nil
		s.canonicalDone = nil
		s.impactMu.Unlock()
		close(done)
	}()
	inputs := s.lineup.reviewInputsRevision()
	state := s.lineup.builder.ReviewStateToken()
	inventory := s.inventoryObservationToken()
	config, configured, _ := s.lineup.store.Get()
	conn, connected := s.config.Get()
	s.reviewMu.RLock()
	actual := s.snapshot
	old := s.canonical
	s.reviewMu.RUnlock()
	base := actual
	if old != nil && old.base.inventoryDigest == actual.inventoryDigest && old.inventory == inventory && old.inputs.config == inputs.config && supplementalEvidenceCurrent(old.base, inputs) {
		base = old.base
	}
	if !configured || !connected || !s.automaticImpactSnapshotCurrent(base, inputs, config, conn) || base.reviewSources == nil || len(base.safeStreams) != base.streamCount || base.streamCount == 0 {
		return
	}
	// A current complete proof needs no repeat work on status/recovery signals.
	if old != nil && !old.failed && old.state == state && old.inputs == inputs && old.inventory == inventory && old.activeSnapshot == snapshotIdentity(actual) {
		return
	}
	// Withdraw a stale name-bearing projection before expensive preparation.
	// This only restores its already captured/scored baseline; choices are untouched.
	if snapshotIdentity(base) != snapshotIdentity(actual) {
		expected := snapshotIdentity(actual)
		restored := s.saveWhileCurrent(conn, config, func() error {
			return s.commitAdmittedDecision(reviewAdmission{inputs: inputs}, func() error {
				return s.lineup.builder.WhileReviewState(state, func() error {
					s.reviewMu.Lock()
					defer s.reviewMu.Unlock()
					s.cache.mu.Lock()
					defer s.cache.mu.Unlock()
					if ctx.Err() != nil || snapshotIdentity(s.snapshot) != expected || s.cache.observationTokenLocked() != inventory {
						return builder.ErrStaleReview
					}
					s.snapshot = base
					return nil
				})
			})
		}, httptest.NewRecorder())
		if !restored {
			return
		}
		actual = base
	}
	expectedSnapshot := snapshotIdentity(actual)
	unchanged := func() bool {
		return inputs == s.lineup.reviewInputsRevision() && state == s.lineup.builder.ReviewStateToken() && inventory == s.inventoryObservationToken() && func() bool {
			s.reviewMu.RLock()
			defer s.reviewMu.RUnlock()
			return snapshotIdentity(s.snapshot) == expectedSnapshot
		}()
	}
	current := func() bool { return ctx.Err() == nil && unchanged() }
	watcherDone := make(chan struct{})
	defer close(watcherDone)
	go func() {
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-watcherDone:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				if !current() {
					cancel()
					return
				}
			}
		}
	}()
	raw, err := s.lineup.builder.BuildCaptured(ctx, canonicalLineup(config), copyReviewInputs(base.reviewInputs), base.reviewSources)
	if err != nil || !current() {
		return
	}
	raw.SourceFingerprint = config.Fingerprint()
	raw.ResetGeneration = s.lineup.builder.ResetGeneration()
	raw.PublicationGeneration = s.lineup.builder.PublicationGeneration()
	rawKey := canonicalDraftKey(raw)
	key := supplementalHash([]any{rawKey, state, inventory, configurationToken(config), conn.Fingerprint(), snapshotIdentity(base), matcher.Revision, inputs.index, inputs.tmdb, inputs.policy})
	if old != nil && !old.failed && old.key == key {
		return
	}
	channels, _ := dispatcharrSnapshotChannels(raw)
	if !dispatcharrInputsSubset(channels, base.channels) {
		return
	}
	selected := map[string]bool{}
	proposalFor := map[string]builder.CanonicalNameProposal{}
	reasons := map[string]string{}
	for _, p := range raw.CanonicalNameProposals {
		if p.Status == "pending" {
			selected[p.Hash] = true
			proposalFor[p.TargetID] = p
		}
	}
	if len(selected) == 0 && old == nil {
		return
	}

	completed := false
	defer func() {
		if completed || parent.Err() != nil || !unchanged() {
			return
		}
		if ctx.Err() != nil && ctx.Err() != context.DeadlineExceeded {
			return
		}
		// Errors and timeouts never activate names. Keep a visible retryable derived
		// status, without persisting a decision or presenting old proof as current.
		failedReasons := map[string]string{}
		for _, p := range proposalFor {
			failedReasons[p.Hash] = "Consumer validation unavailable; existing name retained. Retry validation or refresh streams."
		}
		proof := &canonicalValidation{key: key, generation: supplementalHash([]any{key, "unavailable"}), state: state, inventory: inventory, rawKey: rawKey, activeSnapshot: snapshotIdentity(base), inputs: inputs, base: base, accepted: map[string]bool{}, reasons: failedReasons, failed: true}
		s.publishCanonicalValidation(parent, conn, config, inputs, state, inventory, expectedSnapshot, base, proof)
	}()
	baseline, err := cloneCanonicalDraft(raw)
	if err != nil {
		return
	}
	s.projectAutomaticExclusionsFrom(baseline, config, base, s.lineup.builder, inputs)
	primary := s.lineup.builder.MatchDecisions(config.Fingerprint())
	supp := s.lineup.builder.SupplementalDecisions(config.Fingerprint())
	targets := map[string]builder.DraftChannel{}
	for _, c := range raw.Channels {
		targets[c.ID] = c
	}
	accepted := map[string]bool{}
	leads := []canonicalLead{}
	run := s.impactRunner
	if run == nil {
		run = dispatcharr.CheckSupplementalImpact
	}
	if len(selected) > 0 {
		all, err := cloneCanonicalDraft(baseline)
		if err != nil {
			return
		}
		if builder.ApplyCanonicalProposalsForValidation(all, selected) != nil {
			return
		}
		request, b, p, err := canonicalRequest(baseline, all, base)
		if err != nil {
			return
		}
		result, err := run(ctx, request)
		if err != nil || !current() {
			return
		}
		deltas, err := canonicalImpactDeltas(b, p, result, base.identityStreams, targets, config.Fingerprint(), conn.Fingerprint(), primary, supp)
		if err != nil {
			return
		}
		collateral := false
		for id, d := range deltas {
			if _, ok := proposalFor[id]; !ok && (len(d.Added) > 0 || len(d.Lost) > 0) {
				collateral = true
			}
		}
		for id, p := range proposalFor {
			delta := deltas[id]
			if !collateral && canonicalSafeDelta(delta) {
				accepted[p.Hash] = true
			} else {
				reasons[p.Hash] = "Consumer effects unresolved; existing exported name retained"
				for _, a := range delta.Unresolved {
					leads = append(leads, canonicalLead{id, a.Name, p.Hash, a.AccountID, a.StreamID})
				}
			}
		}
	}
	staged := base
	if len(accepted) > 0 {
		proposed, err := cloneCanonicalDraft(raw)
		if err != nil {
			return
		}
		if builder.ApplyCanonicalProposalsForValidation(proposed, accepted) != nil {
			return
		}
		newChannels, _ := dispatcharrSnapshotChannels(proposed)
		match := s.matcher
		if match == nil {
			match = dispatcharr.MatchStreamCandidatesWithLineuparr
		}
		matched, revision, err := match(ctx, conn.Fingerprint(), proposed.CountryCode, newChannels, copySafeStreams(base.safeStreams), nil)
		if err != nil || revision != matcher.Revision || !current() {
			return
		}
		staged.channels = newChannels
		staged.digest = dispatcharrInputDigest(newChannels)
		staged.version = revision
		staged.candidates = append([]dispatcharr.Candidate(nil), matched.All...)
		for i := range staged.candidates {
			c := &staged.candidates[i]
			c.ReviewGeneration = key
			c.Key = supplementalHash([]string{c.Key, key})
		}
		staged.supplementalInputs = inputs
		s.projectAutomaticExclusionsFrom(proposed, config, staged, s.lineup.builder, inputs)
		if builder.AttachDraftSignatures(proposed) != nil {
			return
		}
		request, b, p, err := canonicalRequest(baseline, proposed, base)
		if err != nil {
			return
		}
		result, err := run(ctx, request)
		if err != nil || !current() {
			return
		}
		deltas, err := canonicalImpactDeltas(b, p, result, base.identityStreams, targets, config.Fingerprint(), conn.Fingerprint(), primary, supp)
		if err != nil {
			return
		}
		safe := true
		for id, d := range deltas {
			if !canonicalSafeDelta(d) {
				safe = false
				for _, a := range d.Unresolved {
					if p, ok := proposalFor[id]; ok {
						leads = append(leads, canonicalLead{id, a.Name, p.Hash, a.AccountID, a.StreamID})
					}
				}
			}
		}
		if !safe {
			for hash := range accepted {
				reasons[hash] = "Rebuilt full consumer effects unresolved; existing name retained"
			}
			accepted = map[string]bool{}
			staged = base
		}
	}
	if !current() {
		return
	}
	hashes := []string{}
	for h := range accepted {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)
	proof := &canonicalValidation{key: key, generation: supplementalHash([]any{key, hashes, reasons, leads}), state: state, inventory: inventory, rawKey: rawKey, activeSnapshot: snapshotIdentity(staged), inputs: inputs, base: base, accepted: accepted, reasons: reasons, leads: leads}
	completed = true
	s.publishCanonicalValidation(ctx, conn, config, inputs, state, inventory, expectedSnapshot, staged, proof)
}

// A stale naming lease selects the retained scored baseline until the next
// atomic naming transition. Never label old scored pairs with new target names.
func (s *dispatcharrServer) canonicalReviewSnapshot(channels []dispatcharr.MatchChannel, actual dispatcharrMatchSnapshot, inputs reviewInputsRevision) dispatcharrMatchSnapshot {
	if dispatcharrInputsSubset(channels, actual.channels) {
		return actual
	}
	s.reviewMu.RLock()
	proof := s.canonical
	s.reviewMu.RUnlock()
	if proof != nil && proof.base.inventoryDigest == actual.inventoryDigest && proof.inventory == s.inventoryObservationToken() && supplementalEvidenceCurrent(proof.base, inputs) && dispatcharrInputsSubset(channels, proof.base.channels) {
		return proof.base
	}
	return actual
}

// No work after this publication can fail the transition. All matching/rendering
// is complete before the owner locks are taken.
func (s *dispatcharrServer) publishCanonicalValidation(ctx context.Context, conn dispatcharr.Config, config appconfig.Config, inputs reviewInputsRevision, state, inventory, expectedSnapshot string, staged dispatcharrMatchSnapshot, proof *canonicalValidation) {
	// Configuration -> guide/evidence -> builder -> review/inventory. Publication
	// changes only the lease and derived primary snapshot, never persisted choices.
	s.saveWhileCurrent(conn, config, func() error {
		return s.commitAdmittedDecision(reviewAdmission{inputs: inputs}, func() error {
			return s.lineup.builder.WhileReviewState(state, func() error {
				s.reviewMu.Lock()
				defer s.reviewMu.Unlock()
				s.cache.mu.Lock()
				defer s.cache.mu.Unlock()
				if ctx.Err() != nil || snapshotIdentity(s.snapshot) != expectedSnapshot || s.cache.observationTokenLocked() != inventory {
					return builder.ErrStaleReview
				}
				s.snapshot = staged
				s.canonical = proof
				return nil
			})
		})
	}, httptest.NewRecorder())
}

// Combine status only. Reads never prepare or launch matching, and a successful
// supplemental-only check must not hide an unresolved canonical-name effect.
func (s *dispatcharrServer) canonicalImpactStatus(response supplementalImpactResponse) supplementalImpactResponse {
	s.reviewMu.RLock()
	proof := s.canonical
	snapshot := snapshotIdentity(s.snapshot)
	s.reviewMu.RUnlock()
	if proof == nil || (response.State != "idle" && response.State != "complete") {
		return response
	}
	if proof.state != s.lineup.builder.ReviewStateToken() || proof.inputs != s.lineup.reviewInputsRevision() || proof.inventory != s.inventoryObservationToken() || proof.activeSnapshot != snapshot {
		response.State = "stale"
		response.Message = "Name-effect validation requires current inputs; existing names remain in use until revalidated."
	} else if proof.failed {
		response.State = "failed"
		response.Message = "Name-effect validation was unavailable; existing names remain in use. Retry validation or refresh streams."
	} else if len(proof.reasons) > 0 {
		response.Unresolved = true
		response.State = "complete"
		response.Message = "Some proposed names have unresolved consumer effects and remain unchanged. Review their stream candidates with the existing Confirm/Deny controls. Generation remains available."
	}
	return response
}
