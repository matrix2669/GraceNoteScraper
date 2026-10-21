package main

import (
	"context"
	"errors"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWorkflowConnectionIsNotSnapshot(t *testing.T) {
	s, api := newDispatcharrTestServer(t, false)
	save := func(url string) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/lineuparr/dispatcharr/config", strings.NewReader(`{"baseUrl":"`+url+`","authMethod":"api-key","apiKey":"synthetic-fixture"}`))
		r.Header.Set("Content-Type", "application/json")
		s.handleConfig(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	save("https://fixture.invalid")
	r := snapshotReview(t, s, false)
	if api.streamCalls != 0 || r.CandidateCount != 0 || !r.FetchedAt.IsZero() || r.Warning == "" || r.ReviewStatus != "refresh-required" {
		t.Fatalf("unexpected fresh state %+v", r)
	}
	t.Log("fresh Save/Test: zero inventory calls, zero candidates, zero fetchedAt, refresh warning")
	api.streams = nil
	s.matcher = func(context.Context, string, string, []dispatcharr.MatchChannel, []dispatcharr.Stream, map[string]dispatcharr.Decision) (dispatcharr.CandidateSet, string, error) {
		return dispatcharr.CandidateSet{}, "fixture", nil
	}
	r = snapshotReview(t, s, true)
	if r.FetchedAt.IsZero() || r.StreamCount != 0 || r.CandidateCount != 0 || r.ReviewStatus != "current" {
		t.Fatalf("empty success %+v", r)
	}
	t.Log("explicit valid empty refresh: nonzero fetchedAt, zero streams and candidates")
	api.streamErr = errors.New("offline")
	r = snapshotReview(t, s, true)
	if r.FetchedAt.IsZero() || r.Warning == "" || r.ReviewStatus != "stale" {
		t.Fatal("failed refresh lost last success")
	}
	t.Log("failed refresh retains successful timestamp and warning")
	save("https://fixture.invalid")
	r = snapshotReview(t, s, false)
	if !r.FetchedAt.IsZero() {
		t.Fatal("same endpoint save retained snapshot")
	}
	t.Log("same endpoint Save/Test clears prior snapshot")
	save("https://other-fixture.invalid")
	r = snapshotReview(t, s, false)
	if !r.FetchedAt.IsZero() {
		t.Fatal("new endpoint retained snapshot")
	}
	t.Log("new endpoint is unrefreshed")
}

func TestWorkflowReviewStatusReloadRestartFailureAndLoading(t *testing.T) {
	s, api := newDispatcharrTestServer(t, true)
	api.streamErr = errors.New("offline")
	w := httptest.NewRecorder()
	s.handleReview(w, httptest.NewRequest("GET", "/api/lineuparr/dispatcharr/review?refresh=true", nil))
	if w.Code != 502 {
		t.Fatal(w.Code)
	}
	if r := snapshotReview(t, s, false); r.ReviewStatus != "refresh-required" {
		t.Fatal(r.ReviewStatus)
	}
	api.streamErr = nil
	s.matcher = func(context.Context, string, string, []dispatcharr.MatchChannel, []dispatcharr.Stream, map[string]dispatcharr.Decision) (dispatcharr.CandidateSet, string, error) {
		return dispatcharr.CandidateSet{}, "fixture", nil
	}
	r := snapshotReview(t, s, true)
	if r.ReviewStatus != "current" || r.StreamCount == 0 {
		t.Fatalf("nonempty inventory/no proposals: %+v", r)
	}
	if r = snapshotReview(t, s, false); r.ReviewStatus != "current" {
		t.Fatal("page reload lost current review")
	}
	s.setMatchProgress("matching", 0, 1)
	if r = snapshotReview(t, s, false); r.ReviewStatus != "refreshing" {
		t.Fatal(r.ReviewStatus)
	}
	s.setMatchProgress("idle", 0, 0)
	api.testErr = errors.New("invalid fixture credentials")
	w = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/lineuparr/dispatcharr/config", strings.NewReader(`{"baseUrl":"https://other.invalid","authMethod":"api-key","apiKey":"synthetic"}`))
	req.Header.Set("Content-Type", "application/json")
	s.handleConfig(w, req)
	if w.Code != 502 {
		t.Fatal(w.Code)
	}
	if r = snapshotReview(t, s, false); r.ReviewStatus != "current" {
		t.Fatal("failed test changed retained connection/review")
	}
	s.snapshot.digest = "changed"
	s.snapshot.channels[0].Name = "changed"
	if r = snapshotReview(t, s, false); r.ReviewStatus != "refresh-required" {
		t.Fatal("changed input completed")
	}
	s.clearCandidateCache()
	if r = snapshotReview(t, s, false); r.ReviewStatus != "refresh-required" {
		t.Fatal("server restart completed")
	}
}

func TestWorkflowConnectionCannotRaceActiveRefresh(t *testing.T) {
	s, api := newDispatcharrTestServer(t, true)
	s.refreshMu.Lock()
	for _, method := range []string{"POST", "DELETE"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/api/lineuparr/dispatcharr/config", strings.NewReader(`{"baseUrl":"https://fixture.invalid","authMethod":"api-key","apiKey":"synthetic"}`))
		r.Header.Set("Content-Type", "application/json")
		s.handleConfig(w, r)
		if w.Code != 409 {
			t.Fatalf("%s concurrent config %d", method, w.Code)
		}
	}
	s.refreshMu.Unlock()
	if api.tested.BaseURL != "" {
		t.Fatal("busy connection operation touched client")
	}
}

// A refresh blocked on the guide must already reserve the operation, so a
// same-endpoint credential save cannot slip ahead of its captured configuration.
func TestWorkflowRefreshReservesBeforeDraftRead(t *testing.T) {
	s, _ := newDispatcharrTestServer(t, true)
	s.lineup.state.mu.Lock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleReview(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/lineuparr/dispatcharr/review?refresh=true", nil))
	}()
	defer func() { s.lineup.state.mu.Unlock(); <-done }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s.progressMu.Lock()
		started := s.progress.Stage == "fetching"
		s.progressMu.Unlock()
		if started {
			break
		}
		time.Sleep(time.Millisecond)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/lineuparr/dispatcharr/config", strings.NewReader(`{"baseUrl":"https://dispatcharr.test","authMethod":"password","username":"admin","password":"synthetic-new"}`))
	r.Header.Set("Content-Type", "application/json")
	s.handleConfig(w, r)
	if w.Code != 409 {
		t.Fatalf("credential save passed blocked refresh: %d", w.Code)
	}
}
