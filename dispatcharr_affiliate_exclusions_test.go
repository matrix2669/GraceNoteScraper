package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

func affiliateTestServer(t *testing.T) (*dispatcharrServer, *fakeDispatcharrAPI) {
	s, api := newDispatcharrTestServer(t, true)
	store, err := builder.LoadStateStore(filepath.Join(t.TempDir(), "bulk-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	s.lineup.builder = builder.NewService(store, builder.ServiceOptions{})
	config, _, _ := s.lineup.store.Get()
	s.lineup.state.UpdateForSource(&guide.TVGuide{LineupChannels: []guide.Channel{{ID: "16689", PlacementID: "target", ChannelNo: "502", CallSign: "WCBSDT"}}}, config.Fingerprint())
	api.streams = nil
	for i, call := range []string{"KTVT", "KTXA", "KWTV", "KYW", "WBBM", "WBTV", "WBZ"} {
		api.streams = append(api.streams, dispatcharr.Stream{ID: int64(i + 1), Name: "US: CBS (" + call + ")", M3UAccountID: 3})
	}
	api.streams = append(api.streams, dispatcharr.Stream{ID: 20, Name: "US: CBS (KTVT) HD", M3UAccountID: 4}, dispatcharr.Stream{ID: 21, Name: "US: CBS (WCBS)", M3UAccountID: 3})
	s.matcher = func(_ context.Context, source, _ string, channels []dispatcharr.MatchChannel, streams []dispatcharr.Stream, _ map[string]dispatcharr.Decision) (dispatcharr.CandidateSet, string, error) {
		all := []dispatcharr.Candidate{}
		for _, stream := range streams {
			all = append(all, dispatcharr.Candidate{MatcherVersion: "c766a6bb582ca8ef28ae9548d83af1e61f6cde5d", Key: stream.Key(), ChannelID: channels[0].ID, ChannelName: channels[0].Name, ChannelNumber: channels[0].Number, StreamName: stream.Name, StreamID: stream.ID, StreamKey: stream.Key(), StreamHash: stream.Fingerprint(), Source: source, M3UAccountID: stream.M3UAccountID, Score: 100, NameScore: 100, NormalizedAlias: dispatcharr.NormalizeAliasName(stream.Name), Reason: "Lineuparr alias"})
		}
		return dispatcharr.CandidateSet{All: all}, "c766a6bb582ca8ef28ae9548d83af1e61f6cde5d", nil
	}
	snapshotReview(t, s, true)
	return s, api
}
func affiliateRequest(s *dispatcharrServer, method string, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	r := httptest.NewRequest(method, "/api/lineuparr/dispatcharr/affiliate-exclusions", strings.NewReader(string(data)))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleAffiliateExclusions(w, r)
	return w
}
func affiliatePreviewTest(t *testing.T, s *dispatcharrServer) affiliateExclusionPreview {
	t.Helper()
	w := affiliateRequest(s, http.MethodGet, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var p affiliateExclusionPreview
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}
func affiliateKeys(p affiliateExclusionPreview) []string {
	keys := []string{}
	for _, row := range p.Rows {
		keys = append(keys, row.Key)
	}
	return keys
}

func TestAffiliateBulkPreviewApplyExportRestartUndo(t *testing.T) {
	s, api := affiliateTestServer(t)
	config, _, _ := s.lineup.store.Get()
	p := affiliatePreviewTest(t, s)
	if len(p.Rows) != 8 || p.ManualCount != 1 {
		t.Fatalf("preview %+v", p)
	}
	if len(s.lineup.builder.MatchDecisions(config.Fingerprint())) != 0 || api.streamCalls != 1 {
		t.Fatal("preview changed data or fetched streams")
	}
	w := affiliateRequest(s, http.MethodPost, map[string]any{"token": p.Token, "keys": affiliateKeys(p)})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if api.streamCalls != 1 {
		t.Fatal("bulk apply fetched streams")
	}
	stored := s.lineup.builder.MatchDecisions(config.Fingerprint())
	if len(stored) != 8 {
		t.Fatal(stored)
	}
	draft, _, _, ok := s.lineup.buildDraft(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if !ok {
		t.Fatal("draft")
	}
	exported := builder.ExportFromDraft(draft)
	count := 0
	for _, channels := range exported.Categories {
		for _, c := range channels {
			count += len(c.ExcludedAliases)
		}
	}
	if count != 8 {
		t.Fatalf("exported %d names", count)
	}
	// Persisted restart retains decisions, using existing history and group Undo.
	key := p.Rows[0].Key
	r := httptest.NewRequest(http.MethodDelete, "/", strings.NewReader(`{"key":"`+key+`"}`))
	r.Header.Set("Content-Type", "application/json")
	undo := httptest.NewRecorder()
	s.handleDecision(undo, r)
	if undo.Code != 200 {
		t.Fatal(undo.Body.String())
	}
	if len(s.lineup.builder.MatchDecisions(config.Fingerprint())) >= 8 {
		t.Fatal("undo did not clear group")
	}
	if len(affiliatePreviewTest(t, s).Rows) == 0 {
		t.Fatal("undo did not restore preview")
	}

}

func TestAffiliateBulkStaleAndConcurrentApply(t *testing.T) {
	s, _ := affiliateTestServer(t)
	p := affiliatePreviewTest(t, s)
	payload := map[string]any{"token": p.Token, "keys": affiliateKeys(p)}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- affiliateRequest(s, http.MethodPost, payload).Code }()
	}
	wg.Wait()
	close(codes)
	success := 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code != 409 {
			t.Fatal(code)
		}
	}
	if success != 1 {
		t.Fatalf("%d successful duplicate applies", success)
	}
	if w := affiliateRequest(s, http.MethodPost, payload); w.Code != 409 {
		t.Fatal(w.Code)
	}
}
func TestAffiliateBulkRejectsManualChangeAndInvalidSelection(t *testing.T) {
	s, _ := affiliateTestServer(t)
	p := affiliatePreviewTest(t, s)
	bad := append(affiliateKeys(p), "not-previewed")
	if w := affiliateRequest(s, http.MethodPost, map[string]any{"token": p.Token, "keys": bad}); w.Code != 409 {
		t.Fatal(w.Code)
	}
	config, _, _ := s.lineup.store.Get()
	if len(s.lineup.builder.MatchDecisions(config.Fingerprint())) != 0 {
		t.Fatal("partial invalid save")
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"key":"`+p.Rows[0].Key+`","decision":"confirmed"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleDecision(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := affiliateRequest(s, http.MethodPost, map[string]any{"token": p.Token, "keys": affiliateKeys(p)}); w.Code != 409 {
		t.Fatal(w.Code)
	}
	for _, d := range s.lineup.builder.MatchDecisions(config.Fingerprint()) {
		if d.Decision != "confirmed" {
			t.Fatal("overwrote confirmation")
		}
	}
}

func TestAffiliateBulkRejectsRefreshPartialGroupAndGuideChange(t *testing.T) {
	s, _ := affiliateTestServer(t)
	p := affiliatePreviewTest(t, s)
	// KTVT and KTVT HD form one ordinary review/undo group.
	keys := affiliateKeys(p)
	partial := []string{}
	for _, row := range p.Rows {
		if row.StreamName == "US: CBS (KTVT)" {
			partial = append(partial, row.Key)
		}
	}
	if w := affiliateRequest(s, http.MethodPost, map[string]any{"token": p.Token, "keys": partial}); w.Code != 409 {
		t.Fatal("accepted partial group", w.Code, w.Body.String())
	}
	snapshotReview(t, s, true)
	if w := affiliateRequest(s, http.MethodPost, map[string]any{"token": p.Token, "keys": keys}); w.Code != 409 {
		t.Fatal("accepted refreshed preview", w.Code)
	}
	p = affiliatePreviewTest(t, s)
	config, _, _ := s.lineup.store.Get()
	old := s.lineup.state.Get()
	s.lineup.state.UpdateForSource(&guide.TVGuide{LineupChannels: []guide.Channel{{ID: "16689", PlacementID: "target", ChannelNo: "502", CallSign: "KTVTDT"}}}, config.Fingerprint())
	if w := affiliateRequest(s, http.MethodPost, map[string]any{"token": p.Token, "keys": affiliateKeys(p)}); w.Code != 409 {
		t.Fatal("accepted changed station identity", w.Code)
	}
	if len(s.lineup.builder.MatchDecisions(config.Fingerprint())) != 0 {
		t.Fatal("stale apply changed choices")
	}
	// Exercise the final check after preview, with a replacement waiting at the
	// same-source guide lock boundary rather than only comparing HTTP tokens.
	s.lineup.state.UpdateForSource(old, config.Fingerprint())
	preview := affiliateExclusionPreview{guide: old}
	token := s.lineup.builder.ReviewStateToken()
	s.lineup.state.mu.Lock()
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() { close(entered); done <- s.applyAffiliatePreview(preview, config.Fingerprint(), token, nil) }()
	<-entered
	s.lineup.state.guide = &guide.TVGuide{}
	s.lineup.state.mu.Unlock()
	if err := <-done; err != builder.ErrStaleReview {
		t.Fatal("guide replacement crossed commit fence", err)
	}
}
