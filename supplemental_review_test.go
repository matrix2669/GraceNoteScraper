package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

func supplementalServerFixture(t *testing.T) (*dispatcharrServer, dispatcharrReviewResponse) {
	t.Helper()
	s, f := newDispatcharrTestServer(t, true)
	f.streams = []dispatcharr.Stream{{ID: 1, M3UAccountID: 3, Name: "US| Two Network HD", GroupName: "US| Entertainment"}, {ID: 2, M3UAccountID: 3, Name: "US| Two Network West", GroupName: "US| Entertainment"}, {ID: 3, M3UAccountID: 4, Name: "US| Two Network HD", GroupName: "US| Entertainment"}}
	s.matcher = func(context.Context, string, string, []dispatcharr.MatchChannel, []dispatcharr.Stream, map[string]dispatcharr.Decision) (dispatcharr.CandidateSet, string, error) {
		return dispatcharr.CandidateSet{}, "test", nil
	}
	s.lineup.automaticProjection = s.projectAutomaticExclusions
	s.lineup.derivedRevision = s.derivedRevision
	r := httptest.NewRecorder()
	b, ok := s.buildReview(r, httptest.NewRequest("GET", "/review", nil), true)
	if !ok || b.response.Supplemental.CandidateCount == 0 {
		t.Fatalf("fixture %d %s %+v", r.Code, r.Body.String(), b.response.Supplemental)
	}
	return s, b.response
}
func supplementalRequest(t *testing.T, s *dispatcharrServer, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, strings.NewReader(string(data)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	if strings.Contains(path, "impact") {
		s.handleSupplementalImpact(w, r)
	} else {
		s.handleDecision(w, r)
	}
	return w
}
func supplementalSaveBody(review dispatcharrReviewResponse, g supplementalGroup) map[string]string {
	return map[string]string{"key": g.Key, "operation": review.DecisionTokens[g.Key], "decision": "confirmed"}
}
func TestSupplementalServerReceiptsAndSnapshotFences(t *testing.T) {
	for _, change := range []string{"lost-ack-undo", "inventory", "evidence", "guide", "connection"} {
		t.Run(change, func(t *testing.T) {
			s, review := supplementalServerFixture(t)
			g := review.Supplemental.Candidates[0]
			body := supplementalSaveBody(review, g)
			if change == "lost-ack-undo" {
				for i := 0; i < 2; i++ {
					w := supplementalRequest(t, s, "POST", "/decision", body)
					if w.Code != 200 {
						t.Fatalf("save/retry %d %s", w.Code, w.Body.String())
					}
				}
				cfg, _, _ := s.lineup.store.Get()
				saved := s.lineup.builder.SupplementalDecisions(cfg.Fingerprint())
				if len(saved) != g.StreamCount {
					t.Fatal("physical group lost", saved)
				}
				w := supplementalRequest(t, s, "DELETE", "/decision", map[string]string{"key": g.Key})
				if w.Code != 200 {
					t.Fatal(w.Body.String())
				}
				w = supplementalRequest(t, s, "POST", "/decision", body)
				if w.Code != 409 {
					t.Fatal("late request resurrected Undo", w.Code)
				}
				return
			}
			switch change {
			case "inventory":
				s.cache.mu.Lock()
				s.cache.digest = "changed"
				s.cache.fetchedAt = time.Now().Add(time.Second)
				s.cache.mu.Unlock()
			case "evidence":
				s.lineup.tmdbEvidenceRevision = func() uint64 { return 99 }
			case "guide":
				cfg, _, _ := s.lineup.store.Get()
				old := *s.lineup.state.Get()
				s.lineup.state.UpdateForSource(&old, cfg.Fingerprint())
			case "connection":
				conn, _ := s.config.Get()
				conn.BaseURL = "https://changed.dispatcharr.test"
				if err := s.config.Save(conn); err != nil {
					t.Fatal(err)
				}
			}
			w := supplementalRequest(t, s, "POST", "/decision", body)
			if w.Code != 409 {
				t.Fatalf("%s accepted stale %d %s", change, w.Code, w.Body.String())
			}
		})
	}
}
func awaitImpact(t *testing.T, s *dispatcharrServer, state string) supplementalImpactResponse {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r := s.impactResponse()
		if r.State == state {
			return r
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("impact wanted %s got %+v", state, s.impactResponse())
	return supplementalImpactResponse{}
}
func TestSupplementalImpactExplicitCoalescedCancelledRetriedAndStale(t *testing.T) {
	s, review := supplementalServerFixture(t)
	var calls atomic.Int32
	hold := make(chan struct{})
	s.impactRunner = func(ctx context.Context, req dispatcharr.ImpactRequest) (dispatcharr.ImpactResult, error) {
		calls.Add(1)
		select {
		case <-ctx.Done():
			return dispatcharr.ImpactResult{}, ctx.Err()
		case <-hold:
			return dispatcharr.ImpactResult{}, nil
		}
	}
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		if _, ok := s.buildReview(w, httptest.NewRequest("GET", "/review", nil), false); !ok {
			t.Fatal(w.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal("ordinary read ran consumer")
	}
	first := supplementalRequest(t, s, "POST", "/impact", map[string]string{})
	if first.Code != 202 {
		t.Fatalf("start %d %s", first.Code, first.Body.String())
	}
	var initial supplementalImpactResponse
	json.Unmarshal(first.Body.Bytes(), &initial)
	second := supplementalRequest(t, s, "POST", "/impact", map[string]string{})
	var duplicate supplementalImpactResponse
	json.Unmarshal(second.Body.Bytes(), &duplicate)
	if initial.ID != duplicate.ID {
		t.Fatal("not coalesced")
	}
	// Independent admitted durable writes can proceed while the process is blocked.
	start := time.Now()
	w := supplementalRequest(t, s, "POST", "/decision", supplementalSaveBody(review, review.Supplemental.Candidates[0]))
	if w.Code != 200 || time.Since(start) > time.Second {
		t.Fatalf("impact blocked choice %d %s", w.Code, w.Body.String())
	}
	close(hold)
	awaitImpact(t, s, "stale")
	entered := make(chan struct{})
	s.impactRunner = func(ctx context.Context, req dispatcharr.ImpactRequest) (dispatcharr.ImpactResult, error) {
		calls.Add(1)
		close(entered)
		<-ctx.Done()
		return dispatcharr.ImpactResult{}, ctx.Err()
	}
	w = supplementalRequest(t, s, "POST", "/impact", map[string]string{})
	json.Unmarshal(w.Body.Bytes(), &initial)
	<-entered
	w = supplementalRequest(t, s, "DELETE", "/impact", map[string]string{"id": initial.ID})
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	awaitImpact(t, s, "cancelled")
	s.impactRunner = func(context.Context, dispatcharr.ImpactRequest) (dispatcharr.ImpactResult, error) {
		return dispatcharr.ImpactResult{}, errors.New("injected consumer failure")
	}
	supplementalRequest(t, s, "POST", "/impact", map[string]string{})
	awaitImpact(t, s, "failed")
	s.impactRunner = func(context.Context, dispatcharr.ImpactRequest) (dispatcharr.ImpactResult, error) {
		return dispatcharr.ImpactResult{}, nil
	}
	supplementalRequest(t, s, "POST", "/impact", map[string]string{})
	done := awaitImpact(t, s, "complete")
	w = supplementalRequest(t, s, "POST", "/impact", map[string]string{})
	json.Unmarshal(w.Body.Bytes(), &duplicate)
	if duplicate.ID != done.ID {
		t.Fatal("complete cache not reused")
	}
}
func TestSupplementalImpactPhysicalConflictsAndExtraAdmission(t *testing.T) {
	s, review := supplementalServerFixture(t)
	g := review.Supplemental.Candidates[0]
	if w := supplementalRequest(t, s, "POST", "/decision", supplementalSaveBody(review, g)); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	// A consumer extra that did not pass supplemental label retrieval becomes an
	// explicit proposal only after this check; no decision is auto-created.
	s.reviewMu.Lock()
	s.snapshot.identityStreams = append(s.snapshot.identityStreams, dispatcharr.IdentityStream{ID: 99, AccountID: 3, Name: "Unexpected literal * extra", Fingerprint: "extra"})
	s.reviewMu.Unlock()
	s.impactRunner = func(_ context.Context, req dispatcharr.ImpactRequest) (dispatcharr.ImpactResult, error) {
		var exported builder.ExportFile
		if err := json.Unmarshal([]byte(req.Proposed), &exported); err != nil {
			t.Fatal(err)
		}
		result := dispatcharr.ImpactResult{}
		for category, entries := range exported.Categories {
			for i, entry := range entries {
				result.Proposed = append(result.Proposed, dispatcharr.ImpactMatch{Category: category, EntryIndex: i, Name: entry.Name, Number: fmt.Sprint(entry.Number), Names: []string{g.StreamNames[0], "Unexpected literal * extra"}})
			}
		}
		return result, nil
	}
	supplementalRequest(t, s, "POST", "/impact", map[string]string{})
	done := awaitImpact(t, s, "complete")
	if len(done.Rows) == 0 || len(done.Rows[0].Extra) == 0 {
		t.Fatal("extra absent", done)
	}
	w := httptest.NewRecorder()
	built, ok := s.buildReview(w, httptest.NewRequest("GET", "/review", nil), false)
	if !ok {
		t.Fatal(w.Body.String())
	}
	found := false
	collateral := map[string]bool{}
	for _, row := range done.Rows {
		if row.ChannelID != g.ChannelID && len(row.Extra) > 0 {
			collateral[row.ChannelID] = false
		}
	}
	if len(collateral) == 0 {
		t.Fatal("unsaved sibling collateral omitted")
	}
	for _, row := range built.response.Supplemental.Candidates {
		if row.StreamName == "Unexpected literal * extra" {
			found = true
			if _, ok := collateral[row.ChannelID]; ok {
				collateral[row.ChannelID] = true
			}
			body := supplementalSaveBody(built.response, row)
			body["decision"] = "denied"
			w := supplementalRequest(t, s, "POST", "/decision", body)
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
		}
	}
	for channel, reviewed := range collateral {
		if !reviewed {
			t.Fatalf("collateral target %s has no literal review path", channel)
		}
	}
	if !found {
		t.Fatal("impact extra not actionable")
	}
	if s.impactResponse().State != "stale" {
		t.Fatal("choice did not stale impact")
	}
}
func TestSupplementalTenHzBurstDuringImpact(t *testing.T) {
	s, f := newDispatcharrTestServer(t, true)
	for i := 0; i < 20; i++ {
		f.streams = append(f.streams, dispatcharr.Stream{ID: int64(100 + i), M3UAccountID: 3, Name: fmt.Sprintf("Two Network Service %d", i)})
	}
	s.matcher = func(context.Context, string, string, []dispatcharr.MatchChannel, []dispatcharr.Stream, map[string]dispatcharr.Decision) (dispatcharr.CandidateSet, string, error) {
		return dispatcharr.CandidateSet{}, "test", nil
	}
	w := httptest.NewRecorder()
	b, ok := s.buildReview(w, httptest.NewRequest("GET", "/review", nil), true)
	if !ok || len(b.response.Supplemental.Candidates) < 10 {
		t.Fatal("burst fixture", w.Body.String())
	}
	hold := make(chan struct{})
	s.impactRunner = func(ctx context.Context, _ dispatcharr.ImpactRequest) (dispatcharr.ImpactResult, error) {
		select {
		case <-hold:
			return dispatcharr.ImpactResult{}, nil
		case <-ctx.Done():
			return dispatcharr.ImpactResult{}, ctx.Err()
		}
	}
	supplementalRequest(t, s, "POST", "/impact", map[string]string{})
	defer close(hold)
	max := time.Duration(0)
	for _, g := range b.response.Supplemental.Candidates[:10] {
		start := time.Now()
		w = supplementalRequest(t, s, "POST", "/decision", supplementalSaveBody(b.response, g))
		elapsed := time.Since(start)
		if elapsed > max {
			max = elapsed
		}
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if elapsed < 100*time.Millisecond {
			time.Sleep(100*time.Millisecond - elapsed)
		}
	}
	if max > 100*time.Millisecond {
		t.Fatalf("10Hz save exceeded interval: %v", max)
	}
	t.Logf("10Hz independent impact burst maximum durable save %v", max)
}

func TestSupplementalImpactSameNameAccountAndProtectedBaseline(t *testing.T) {
	c := builder.DraftChannel{ID: "one", StationID: "station", PlacementID: "placement", CallSign: "RAW", Name: "Canonical", Number: "1", Included: true}
	draft := &builder.Draft{Channels: []builder.DraftChannel{c}}
	selected := builder.SupplementalDecision{ChannelID: c.ID, StationID: c.StationID, PlacementID: c.PlacementID, RawCallSign: c.CallSign, DispatcharrFingerprint: "conn", Decision: "confirmed", StreamName: "Equal Name", StreamID: 1, M3UAccountID: 2, StreamFingerprint: "first"}
	denied := selected
	denied.Decision = "denied"
	denied.StreamID = 2
	denied.M3UAccountID = 3
	denied.StreamFingerprint = "second"
	inventory := []dispatcharr.IdentityStream{{Name: "Equal Name", ID: 1, AccountID: 2, Fingerprint: "first"}, {Name: "Equal Name", ID: 2, AccountID: 3, Fingerprint: "second"}, {Name: "Protected Prior", ID: 4, AccountID: 5, Fingerprint: "protected"}}
	result := dispatcharr.ImpactResult{Baseline: []dispatcharr.ImpactMatch{{Category: "Uncategorized", Name: "Canonical", Number: "1", Names: []string{"Protected Prior"}}}, Proposed: []dispatcharr.ImpactMatch{{Category: "Uncategorized", Name: "Canonical", Number: "1", Names: []string{"Equal Name"}}}}
	rows := compareSupplementalImpact(draft, inventory, map[string]builder.SupplementalDecision{"a": selected, "b": denied}, nil, result, "conn")
	if len(rows) != 1 || len(rows[0].Extra) != 1 || len(rows[0].SameNameConflicts) != 2 || len(rows[0].ProtectedConflicts) != 1 {
		t.Fatalf("account/baseline conflicts hidden: %+v", rows)
	}
}
func TestSupplementalProjectionDoesNotSeedPrimary(t *testing.T) {
	s, review := supplementalServerFixture(t)
	g := review.Supplemental.Candidates[0]
	recorder := httptest.NewRecorder()
	before, _, _, ok := s.lineup.buildDraft(recorder, httptest.NewRequest("GET", "/draft", nil))
	if !ok {
		t.Fatal(recorder.Body.String())
	}
	base, _ := dispatcharrSnapshotChannels(before)
	if w := supplementalRequest(t, s, "POST", "/decision", supplementalSaveBody(review, g)); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	recorder = httptest.NewRecorder()
	after, _, _, ok := s.lineup.buildDraft(recorder, httptest.NewRequest("GET", "/draft", nil))
	if !ok {
		t.Fatal(recorder.Body.String())
	}
	next, _ := dispatcharrSnapshotChannels(after)
	a, _ := json.Marshal(base)
	b, _ := json.Marshal(next)
	if string(a) != string(b) {
		t.Fatal("supplemental alias contaminated primary prediction input")
	}
	if after.SupplementalDecisionCount == 0 {
		t.Fatal("supplemental alias missing from export draft")
	}
}
