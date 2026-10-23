package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

func TestUnifiedNamesAndFeedArePresentationOnly(t *testing.T) {
	c := builder.DraftChannel{ID: "target", StationID: "station", Name: "SHORT", CallSign: "SHORT", Aliases: []string{"Brand"}, AliasEvidence: []builder.AliasEvidence{{Value: "Brand", Sources: []string{"spectrum"}, Methods: []string{"exact Gracenote station ID"}}}}
	raw := "  US: BRAND HD  "
	group := dispatcharr.GroupCandidates([]dispatcharr.Candidate{{Key: "key", ChannelID: c.ID, StreamName: raw, Score: 99}})[0]
	exact := exactReviewGroupNames(group, map[string]dispatcharr.Candidate{"key": {StreamName: raw}})
	if exact.Alias != raw || exact.StreamNames[0] != raw {
		t.Fatal("raw whitespace was changed")
	}
	if reviewDisplayName(c) != "Brand" {
		t.Fatal("single-word exact-ID brand was discarded")
	}
	before, _ := json.Marshal(c)
	feeds := map[string]dispatcharr.FeedIdentity{c.ID: {Family: "brand", Region: "west", Evidence: "verified exact station"}}
	got := presentReviewIdentity(c, []string{"US: BRAND HD", "US: BRAND WEST HD", "US: BRAND EAST WEST HD"}, feeds, nil)
	if got.TargetFeed.Region != "west" || got.StreamFeeds[0].Region != "unknown" || got.StreamFeeds[1].Region != "west" || got.StreamFeeds[2].Region != "conflicting" {
		t.Fatalf("feed guesses: %+v", got)
	}
	after, _ := json.Marshal(c)
	if string(before) != string(after) {
		t.Fatal("presentation mutated identity")
	}
	c.SupplementalEvidence = []builder.ServiceIdentityEvidence{{RootStationID: c.StationID, Value: "News 12"}}
	c.AliasEvidence = append(c.AliasEvidence, builder.AliasEvidence{Value: "Estrella National TV", Sources: []string{"verizon"}, Methods: []string{"unique provider-local number/v3"}})
	if reviewDisplayName(c) != "News 12" || len(presentReviewIdentity(c, nil, feeds, nil).IdentityConflicts) != 1 {
		t.Fatal("provider conflict was hidden")
	}
}
func TestUnifiedQueueOneLimitAllScoresAndHistory(t *testing.T) {
	s, api := newDispatcharrTestServer(t, true)
	api.streams = nil
	for i := 0; i < 120; i++ {
		api.streams = append(api.streams, dispatcharr.Stream{ID: int64(i + 1), Name: func() string {
			if i < 60 {
				return fmt.Sprintf("Primary unique %d", i)
			}
			return "US| Two Network HD"
		}(), M3UAccountID: 3})
	}
	s.matcher = func(_ context.Context, source, _ string, channels []dispatcharr.MatchChannel, streams []dispatcharr.Stream, _ map[string]dispatcharr.Decision) (dispatcharr.CandidateSet, string, error) {
		rows := []dispatcharr.Candidate{}
		for i := 0; i < 60; i++ {
			stream := streams[i]
			rows = append(rows, dispatcharr.Candidate{Key: fmt.Sprint(i), ChannelID: channels[0].ID, ChannelNumber: channels[0].Number, ChannelName: channels[0].Name, StreamID: stream.ID, StreamName: stream.Name, StreamHash: stream.Fingerprint(), NormalizedAlias: stream.Name, M3UAccountID: 3, Source: source, Score: 70 + i%31, NameScore: 70 + i%31})
		}
		return dispatcharr.CandidateSet{All: rows}, "fixture", nil
	}
	w := httptest.NewRecorder()
	built, ok := s.buildReview(w, httptest.NewRequest("GET", "/review?limit=13", nil), true)
	if !ok {
		t.Fatal(w.Body.String())
	}
	if len(built.response.ReviewItems) != 13 || built.response.CandidateCount <= 60 {
		t.Fatalf("union pagination: %+v", built.response)
	}
	// A wider prefix exposes both kinds without fabricating a score on retrieval.
	w = httptest.NewRecorder()
	built, ok = s.buildReview(w, httptest.NewRequest("GET", "/review?limit=500", nil), false)
	if !ok {
		t.Fatal(w.Body.String())
	}
	high, unscored := false, false
	for _, row := range built.response.ReviewItems {
		switch g := row.(type) {
		case unifiedReviewItem:
			high = high || g.MaximumScore >= 95
		case supplementalGroup:
			unscored = true
			b, _ := json.Marshal(g)
			var payload map[string]any
			json.Unmarshal(b, &payload)
			if _, exists := payload["score"]; exists {
				t.Fatal("unscored row gained score")
			}
		}
	}
	if !high || !unscored {
		t.Fatal("missing high-score Deny or retrieval rows")
	}
}
func TestUnifiedCoordinatorBurstReadsCacheFailureAndShutdown(t *testing.T) {
	s, review := supplementalServerFixture(t)
	var calls, active, maximum atomic.Int32
	hold := make(chan struct{})
	s.impactRunner = func(ctx context.Context, _ dispatcharr.ImpactRequest) (dispatcharr.ImpactResult, error) {
		calls.Add(1)
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := maximum.Load()
			if n <= old || maximum.CompareAndSwap(old, n) {
				break
			}
		}
		select {
		case <-hold:
			return dispatcharr.ImpactResult{}, nil
		case <-ctx.Done():
			return dispatcharr.ImpactResult{}, ctx.Err()
		}
	}
	s.startImpactCoordinator(context.Background())
	defer s.stopImpactCoordinator()
	g := review.Supplemental.Candidates[0]
	for i := 0; i < 100; i++ {
		w := supplementalRequest(t, s, "POST", "/decision", supplementalSaveBody(review, g))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal("consumer ran per click")
	}
	deadline := time.Now().Add(3 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if calls.Load() != 1 {
		t.Fatalf("burst launched %d children", calls.Load())
	}
	start := time.Now()
	w := supplementalRequest(t, s, "POST", "/decision", supplementalSaveBody(review, g))
	if w.Code != 200 || time.Since(start) > 100*time.Millisecond {
		t.Fatal("blocked impact delayed durable retry", time.Since(start))
	}
	close(hold)
	awaitImpact(t, s, "complete")
	s.signalImpact()
	awaitImpact(t, s, "complete")
	if calls.Load() != 1 || maximum.Load() != 1 {
		t.Fatal("completed-key reuse/serial child violated", calls.Load(), maximum.Load())
	}
	for i := 0; i < 5; i++ {
		s.impactResponse()
		w := httptest.NewRecorder()
		s.buildReview(w, httptest.NewRequest("GET", "/review", nil), false)
	}
	if calls.Load() != 1 {
		t.Fatal("read launched consumer")
	}
}
func TestUnifiedCoordinatorFiniteRetry(t *testing.T) {
	s, review := supplementalServerFixture(t)
	var calls atomic.Int32
	s.impactRunner = func(context.Context, dispatcharr.ImpactRequest) (dispatcharr.ImpactResult, error) {
		calls.Add(1)
		return dispatcharr.ImpactResult{}, errors.New("transient fixture")
	}
	s.startImpactCoordinator(context.Background())
	defer s.stopImpactCoordinator()
	w := supplementalRequest(t, s, "POST", "/decision", supplementalSaveBody(review, review.Supplemental.Candidates[0]))
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	deadline := time.Now().Add(4 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if calls.Load() != 2 || s.impactResponse().State != "failed" {
		t.Fatalf("retry not bounded: %d %+v", calls.Load(), s.impactResponse())
	}
	for i := 0; i < 10; i++ {
		s.impactResponse()
	}
	if calls.Load() != 2 {
		t.Fatal("poll retries")
	}
}
func TestUnifiedCompletedOffersSurviveReplacementAndRetireOnIdentityChange(t *testing.T) {
	s, review := supplementalServerFixture(t)
	cfg, _, _ := s.lineup.store.Get()
	draft := automaticDraft(t, s)
	snapshot := s.snapshot
	extra := snapshot.identityStreams[0]
	completed := &supplementalImpactJob{inputs: s.lineup.reviewInputsRevision(), snapshot: snapshotIdentity(snapshot), connection: snapshot.dispatcharrFingerprint, response: supplementalImpactResponse{State: "complete", Fingerprint: "old", Rows: []supplementalImpactRow{{ChannelID: review.Supplemental.Candidates[0].ChannelID, Extra: []supplementalImpactPhysical{{Name: extra.Name, AccountID: extra.AccountID, StreamID: extra.ID}}}}}}
	s.impactCompleted = completed
	s.impact = &supplementalImpactJob{response: supplementalImpactResponse{State: "running"}}
	if len(s.impactExtraCandidates(draft, snapshot, s.lineup.reviewInputsRevision())) != 1 {
		t.Fatal("replacement erased unresolved offer")
	}
	if !s.impactResponse().FindingsStale {
		t.Fatal("prior diagnostics not marked updating")
	}
	changed := s.lineup.reviewInputsRevision()
	changed.index++
	if len(s.impactExtraCandidates(draft, snapshot, changed)) != 0 {
		t.Fatal("stale evidence kept offer")
	}
	snapshot.inventoryDigest = "new inventory"
	if len(s.impactExtraCandidates(draft, snapshot, s.lineup.reviewInputsRevision())) != 0 {
		t.Fatal("stale physical identity kept offer")
	}
	if len(s.lineup.builder.SupplementalDecisions(cfg.Fingerprint())) != 0 {
		t.Fatal("extra auto-decided")
	}
}
func TestUnifiedRunningCancellationSourceInputInventoryAndReset(t *testing.T) {
	for _, kind := range []string{"connection", "guide", "choices", "inventory", "reset"} {
		t.Run(kind, func(t *testing.T) {
			s, review := supplementalServerFixture(t)
			s.impactRunner = func(ctx context.Context, _ dispatcharr.ImpactRequest) (dispatcharr.ImpactResult, error) {
				<-ctx.Done()
				return dispatcharr.ImpactResult{}, ctx.Err()
			}
			w := supplementalRequest(t, s, "POST", "/decision", supplementalSaveBody(review, review.Supplemental.Candidates[0]))
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			supplementalRequest(t, s, "POST", "/impact", map[string]string{})
			s.impactMu.Lock()
			job := s.impact
			s.impactMu.Unlock()
			switch kind {
			case "connection":
				conn, _ := s.config.Get()
				conn.Username = "other"
				if err := s.config.Save(conn); err != nil {
					t.Fatal(err)
				}
			case "guide":
				cfg, _, _ := s.lineup.store.Get()
				s.lineup.state.UpdateForSource(&guide.TVGuide{}, cfg.Fingerprint())
			case "choices":
				cfg, _, _ := s.lineup.store.Get()
				name := "Movies"
				if err := s.lineup.builder.UpdateChannel(cfg.Fingerprint(), review.Supplemental.Candidates[0].ChannelID, builder.ChannelUpdate{Category: &name}); err != nil {
					t.Fatal(err)
				}
			case "inventory":
				s.cache.observe("different inventory", nil)
			case "reset":
				s.clearCandidateCache()
			}
			select {
			case <-job.done:
			case <-time.After(time.Second):
				t.Fatal("obsolete consumer was not cancelled")
			}
			if job.response.State == "complete" {
				t.Fatal("obsolete callback published success")
			}
			if s.impactCompleted != nil {
				t.Fatal("stale completed offers admitted")
			}
		})
	}
}

func TestUnifiedHundredDistinctDurableChoicesCoalesce(t *testing.T) {
	s, _ := supplementalServerFixture(t)
	api := s.client.(*fakeDispatcharrAPI)
	api.streams = nil
	for i := 0; i < 100; i++ {
		api.streams = append(api.streams, dispatcharr.Stream{ID: int64(1000 + i), M3UAccountID: 3, Name: fmt.Sprintf("%d Two Network HD", 100+i)})
	}
	w := httptest.NewRecorder()
	built, ok := s.buildReview(w, httptest.NewRequest("GET", "/review?limit=500", nil), true)
	if !ok || len(built.response.Supplemental.Candidates) != 200 {
		t.Fatal("100-choice fixture", w.Code, w.Body.String(), len(built.response.Supplemental.Candidates))
	}
	var calls atomic.Int32
	s.impactRunner = func(context.Context, dispatcharr.ImpactRequest) (dispatcharr.ImpactResult, error) {
		calls.Add(1)
		return dispatcharr.ImpactResult{}, nil
	}
	s.startImpactCoordinator(context.Background())
	defer s.stopImpactCoordinator()
	maximum := time.Duration(0)
	for _, g := range built.response.Supplemental.Candidates[:100] {
		start := time.Now()
		saved := supplementalRequest(t, s, "POST", "/decision", supplementalSaveBody(built.response, g))
		elapsed := time.Since(start)
		if elapsed > maximum {
			maximum = elapsed
		}
		if saved.Code != 200 {
			t.Fatal(saved.Code, saved.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal("individual click ran consumer")
	}
	awaitImpact(t, s, "complete")
	if calls.Load() != 1 {
		t.Fatal("burst did not coalesce", calls.Load())
	}
	cfg, _, _ := s.lineup.store.Get()
	if len(s.lineup.builder.SupplementalDecisions(cfg.Fingerprint())) != 100 {
		t.Fatal("durable choices lost")
	}
	t.Logf("100 distinct durable saves; one quiescent consumer; maximum save %v", maximum)
}

func TestUnifiedPrimaryRefreshWinsDuringBackgroundPreparation(t *testing.T) {
	s, review := supplementalServerFixture(t)
	if w := supplementalRequest(t, s, "POST", "/decision", supplementalSaveBody(review, review.Supplemental.Candidates[0])); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var calls atomic.Int32
	s.impactRunner = func(context.Context, dispatcharr.ImpactRequest) (dispatcharr.ImpactResult, error) {
		calls.Add(1)
		return dispatcharr.ImpactResult{}, nil
	}
	s.startImpactCoordinator(context.Background())
	s.lineup.draftMu.Lock()
	locked := true
	defer func() {
		if locked {
			s.lineup.draftMu.Unlock()
		}
		s.stopImpactCoordinator()
	}()
	s.signalImpact()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.impactMu.Lock()
		preparing := s.impactLifecycle == "preparing"
		s.impactMu.Unlock()
		if preparing {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !s.refreshMu.TryLock() {
		t.Fatal("background preparation reserved the primary Refresh lock")
	}
	s.refreshMu.Unlock()
	s.beginImpactRefresh()
	s.lineup.draftMu.Unlock()
	locked = false
	awaitImpact(t, s, "refresh-required")
	if calls.Load() != 0 {
		t.Fatal("consumer launched during primary refresh")
	}
	s.endImpactRefresh()
	awaitImpact(t, s, "complete")
	if calls.Load() != 1 {
		t.Fatal("latest validation did not resume after primary publication", calls.Load())
	}
}
