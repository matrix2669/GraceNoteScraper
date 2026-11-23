package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	lineuparrbuilder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

func snapshotReview(t *testing.T, s *dispatcharrServer, refresh bool) dispatcharrReviewResponse {
	t.Helper()
	url := "/api/lineuparr/dispatcharr/review"
	if refresh {
		url += "?refresh=true"
	}
	w := httptest.NewRecorder()
	s.handleReview(w, httptest.NewRequest("GET", url, nil))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var result dispatcharrReviewResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestSnapshotOnlyExplicitRefreshInvokesMatcher(t *testing.T) {
	s, api := newDispatcharrTestServer(t, true)
	runs := 0
	matcher := s.matcher
	s.matcher = func(ctx context.Context, source, country string, channels []dispatcharr.MatchChannel, streams []dispatcharr.Stream, decisions map[string]dispatcharr.Decision) (dispatcharr.CandidateSet, string, error) {
		runs++
		return matcher(ctx, source, country, channels, streams, decisions)
	}
	initial := snapshotReview(t, s, false)
	if initial.Warning == "" || runs != 0 || api.streamCalls != 0 {
		t.Fatal("read triggered refresh")
	}
	refreshed := snapshotReview(t, s, true)
	if len(refreshed.Candidates) == 0 || runs != 1 || api.streamCalls != 1 {
		t.Fatalf("%+v runs %d fetches %d", refreshed, runs, api.streamCalls)
	}
	s.snapshot.fetchedAt = time.Now().Add(-24 * time.Hour)
	for i := 0; i < 3; i++ {
		snapshotReview(t, s, false)
	}
	if runs != 1 || api.streamCalls != 1 {
		t.Fatal("aged snapshot triggered refresh")
	}
	key := refreshed.Candidates[0].Key
	for _, method := range []string{"POST", "DELETE"} {
		req := httptest.NewRequest(method, "/api/lineuparr/dispatcharr/decision", strings.NewReader(`{"key":"`+key+`","decision":"confirmed","tvgIds":[]}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handleDecision(w, req)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", method, w.Code, w.Body.String())
		}
		review := snapshotReview(t, s, false)
		if method == "POST" && review.CandidateCount != refreshed.CandidateCount-1 {
			t.Fatalf("confirm: %+v", review)
		}
		if method == "DELETE" && review.CandidateCount != refreshed.CandidateCount {
			t.Fatalf("undo: %+v", review)
		}
	}
	if runs != 1 || api.streamCalls != 1 {
		t.Fatal("decisions triggered refresh")
	}
	s.clearCandidateCache()
	missing := snapshotReview(t, s, false)
	if missing.CandidateCount != 0 || missing.Warning == "" || runs != 1 {
		t.Fatal("missing snapshot auto refreshed")
	}
}

func TestSnapshotFailedRefreshRetainsPreviousAndStaleInputsRequireRefresh(t *testing.T) {
	s, api := newDispatcharrTestServer(t, true)
	before := snapshotReview(t, s, true)
	api.streamErr = errors.New("unavailable")
	after := snapshotReview(t, s, true)
	if after.CandidateCount != before.CandidateCount || !after.FetchedAt.Equal(before.FetchedAt) || after.Warning == "" {
		t.Fatalf("lost snapshot: %+v", after)
	}
	s.snapshot.channels[0].Name = "different-draft"
	s.snapshot.digest = "different-draft"
	stale := snapshotReview(t, s, false)
	if stale.CandidateCount != 0 || stale.Warning == "" {
		t.Fatal("stale pairs offered")
	}
	key := before.Candidates[0].Key
	req := httptest.NewRequest("POST", "/api/lineuparr/dispatcharr/decision", strings.NewReader(`{"key":"`+key+`","decision":"confirmed","tvgIds":[]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleDecision(w, req)
	if w.Code != 409 {
		t.Fatalf("stale save %d", w.Code)
	}
	if api.streamCalls != 2 {
		t.Fatal("stale save fetched streams")
	}
}

func TestSnapshotRejectsConcurrentRefresh(t *testing.T) {
	s, _ := newDispatcharrTestServer(t, true)
	entered, release := make(chan struct{}), make(chan struct{})
	matcher := s.matcher
	s.matcher = func(ctx context.Context, source, country string, channels []dispatcharr.MatchChannel, streams []dispatcharr.Stream, decisions map[string]dispatcharr.Decision) (dispatcharr.CandidateSet, string, error) {
		close(entered)
		<-release
		return matcher(ctx, source, country, channels, streams, decisions)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		s.handleReview(w, httptest.NewRequest("GET", "/api/lineuparr/dispatcharr/review?refresh=true", nil))
		done <- w
	}()
	<-entered
	w := httptest.NewRecorder()
	s.handleReview(w, httptest.NewRequest("GET", "/api/lineuparr/dispatcharr/review?refresh=true", nil))
	if w.Code != 409 {
		t.Errorf("concurrent refresh %d", w.Code)
	}
	close(release)
	if result := <-done; result.Code != 200 {
		t.Fatal(result.Body.String())
	}
}

func TestSnapshotRealPythonDecisionExportContract(t *testing.T) {
	s, api := newDispatcharrTestServer(t, true)
	s.matcher = nil
	config, _, _ := s.lineup.store.Get()
	name := "National Geographic Documentary Television Channel"
	s.lineup.state.UpdateForSource(&guide.TVGuide{LineupChannels: []guide.Channel{
		{ID: "100", PlacementID: "1001", ChannelNo: "2", CallSign: name},
		{ID: "200", PlacementID: "2001", ChannelNo: "3", CallSign: "CNN"},
	}}, config.Fingerprint())
	api.streams = []dispatcharr.Stream{
		{ID: 1, Name: name + " Live", M3UAccountID: 3},
		{ID: 2, Name: "US CNN HD", M3UAccountID: 3},
	}
	review := snapshotReview(t, s, true)
	if len(review.Candidates) != 2 {
		t.Fatalf("real review %+v", review)
	}
	for _, c := range review.Candidates {
		decision := "denied"
		if c.ChannelID == "1001" {
			decision = "confirmed"
			if c.MaximumScore < 70 || c.MaximumScore >= 95 {
				t.Fatalf("expected low match %+v", c)
			}
		} else if c.MinimumScore < 95 {
			t.Fatalf("expected high match %+v", c)
		}
		req := httptest.NewRequest("POST", "/api/lineuparr/dispatcharr/decision", strings.NewReader(`{"key":"`+c.Key+`","decision":"`+decision+`","tvgIds":[]}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handleDecision(w, req)
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	if after := snapshotReview(t, s, false); after.CandidateCount != 0 {
		t.Fatal(after)
	}
	if api.streamCalls != 1 {
		t.Fatal("decisions refreshed")
	}
	stored := s.lineup.builder.MatchDecisions(config.Fingerprint())
	for _, d := range stored {
		if d.MatcherVersion == "" || d.NameScore != d.Score {
			t.Fatalf("lost matcher evidence %+v", d)
		}
	}
	draft, _, _, ok := s.lineup.buildDraft(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/lineuparr/draft", nil))
	if !ok {
		t.Fatal("draft unavailable")
	}
	file := lineuparrbuilder.ExportFromDraft(draft)
	payload, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), name+" Live") || !strings.Contains(string(payload), `"excluded_aliases":["US CNN HD"]`) {
		t.Fatalf("export %s", payload)
	}
}

func TestSnapshotRemovalsReuseButAdditionsHideCachedResults(t *testing.T) {
	s, api := newDispatcharrTestServer(t, true)
	before := snapshotReview(t, s, true)
	target := before.Candidates[0].ChannelID
	setMatchTargetIncluded(t, s, target, false)
	hidden := snapshotReview(t, s, false)
	if hidden.CandidateCount != before.CandidateCount-1 || hidden.StreamCount != before.StreamCount || strings.Contains(hidden.Warning, "Refresh required") {
		t.Fatalf("remaining cached data was not reused: %+v", hidden)
	}
	if api.streamCalls != 1 {
		t.Fatal("inclusion auto refreshed")
	}
	setMatchTargetIncluded(t, s, target, true)
	hidden = snapshotReview(t, s, false)
	if hidden.CandidateCount != 0 || hidden.StreamCount != 0 || !strings.Contains(hidden.Warning, "Refresh required") {
		t.Fatal(hidden)
	}
	if api.streamCalls != 1 {
		t.Fatal("reinclusion auto refreshed")
	}
	fresh := snapshotReview(t, s, true)
	if fresh.CandidateCount != before.CandidateCount {
		t.Fatal(fresh)
	}
}

func TestSnapshotRefreshRejectsPreviousGenerationDecision(t *testing.T) {
	s, _ := newDispatcharrTestServer(t, true)
	before := snapshotReview(t, s, true)
	after := snapshotReview(t, s, true)
	if before.Candidates[0].Key == after.Candidates[0].Key {
		t.Fatal("refresh reused generation key")
	}
	req := httptest.NewRequest("POST", "/api/lineuparr/dispatcharr/decision", strings.NewReader(`{"key":"`+before.Candidates[0].Key+`","decision":"confirmed","tvgIds":[]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleDecision(w, req)
	if w.Code != 409 {
		t.Fatalf("stale generation %d", w.Code)
	}
}
