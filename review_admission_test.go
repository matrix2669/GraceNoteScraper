package main

import (
	"context"
	"encoding/json"
	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWarmReviewBypassesDerivedBuildersAndRetriesAfterRead(t *testing.T) {
	s, keys, _ := syntheticReviewBurstFixture(t)
	s.lineup.preparationMu.Lock()
	s.lineup.draftMu.Lock()
	s.automaticMu.Lock()
	done := make(chan int, 1)
	go func() { done <- postReviewChoice(s, keys[0], "confirmed").Code }()
	select {
	case code := <-done:
		if code != 200 {
			t.Errorf("warm save status %d", code)
		}
	case <-time.After(time.Second):
		t.Error("warm save waited on derived work")
	}
	s.automaticMu.Unlock()
	s.lineup.draftMu.Unlock()
	s.lineup.preparationMu.Unlock()
	token := s.lineup.builder.ReviewStateToken()
	_ = automaticReview(t, s)
	if w := postReviewChoice(s, keys[0], "confirmed"); w.Code != 200 {
		t.Fatal("lost acknowledgement retry rejected", w.Code)
	}
	if s.lineup.builder.ReviewStateToken() != token {
		t.Fatal("retry rewrote state")
	}
	if w := postReviewChoice(s, keys[0], "denied"); w.Code != 409 {
		t.Fatal("conflicting retry accepted", w.Code)
	}
}

func TestReviewAdmissionInvalidation(t *testing.T) {
	for _, kind := range []string{"guide", "inventory", "snapshot", "excluded", "reinclude", "reset"} {
		t.Run(kind, func(t *testing.T) {
			s, keys, _ := syntheticReviewBurstFixture(t)
			config, _, _ := s.lineup.store.Get()
			target := s.snapshot.candidates[0].ChannelID
			switch kind {
			case "guide":
				next := *s.lineup.state.Get()
				s.lineup.state.UpdateForSource(&next, config.Fingerprint())
			case "inventory":
				s.cache.observe(s.snapshot.dispatcharrFingerprint, nil)
			case "snapshot":
				s.clearCandidateCache()
			case "excluded", "reinclude":
				excluded := false
				if err := s.lineup.builder.UpdateChannel(config.Fingerprint(), target, builder.ChannelUpdate{Included: &excluded}); err != nil {
					t.Fatal(err)
				}
				if kind == "reinclude" {
					_ = automaticReview(t, s)
					included := true
					if err := s.lineup.builder.UpdateChannel(config.Fingerprint(), target, builder.ChannelUpdate{Included: &included}); err != nil {
						t.Fatal(err)
					}
				}
			case "reset":
				s.lineup.afterBuilderReset = s.clearCandidateCache
				_, err := s.lineup.builder.ResetBuilder(config.Fingerprint(), s.lineup.builder.ReviewStateToken(), "", false)
				if err != nil {
					t.Fatal(err)
				}
				s.clearCandidateCache()
			}
			if w := postReviewChoice(s, keys[0], "denied"); w.Code != 409 {
				t.Fatalf("old request accepted after %s: %d", kind, w.Code)
			}
		})
	}
}

func TestReviewEvidenceChangeFencesAdmissionAndExport(t *testing.T) {
	s, keys, _ := syntheticReviewBurstFixture(t)
	var revision uint64
	s.lineup.tmdbCachedEvidence = func(string, bool, int) ([]int, []string, bool) { return nil, nil, false }
	s.lineup.tmdbEvidenceRevision = func() uint64 { return revision }
	s.lineup.tmdbWhileEvidence = func(want uint64, action func() error) (bool, error) {
		if revision != want {
			return false, nil
		}
		return true, action()
	}
	_ = automaticReview(t, s)
	draft := automaticDraft(t, s)
	_, admission, ok := s.admittedCandidates(keys[0])
	if !ok {
		t.Fatal("no admission")
	}
	revision++
	if _, _, ok = s.admittedCandidates(keys[0]); ok {
		t.Fatal("evidence update retained warm admission")
	}
	if err := s.commitAdmittedDecision(admission, func() error { t.Fatal("old admission committed"); return nil }); err == nil {
		t.Fatal("old evidence committed")
	}
	if _, err := s.commitAutomaticDraft(draft, func() error { t.Fatal("old evidence published"); return nil }); err == nil {
		t.Fatal("old evidence exported")
	}
}

func TestReviewCacheCopiesAndAliasExposureInvalidation(t *testing.T) {
	s, _, _ := syntheticReviewBurstFixture(t)
	cfg, _, _ := s.lineup.store.Get()
	first := automaticDraft(t, s)
	original := first.Channels[0].Name
	first.Channels[0].Name = "caller changed"
	if automaticDraft(t, s).Channels[0].Name != original {
		t.Fatal("caller mutated cached draft")
	}
	w := httptest.NewRecorder()
	_, inputs, _, ok := s.lineup.preparedInputs(w)
	if !ok {
		t.Fatal("preparation failed")
	}
	name := inputs[0].CallSign
	inputs[0].CallSign = "caller changed"
	_, again, _, ok := s.lineup.preparedInputs(w)
	if !ok || again[0].CallSign != name {
		t.Fatal("caller mutated preparation")
	}
	target := s.snapshot.candidates[0].ChannelID
	before := s.lineup.reviewInputsRevision()
	if err := s.lineup.builder.SetAliasSuppressed(cfg.Fingerprint(), target, "Fixture 000", true); err != nil {
		t.Fatal(err)
	}
	if before == s.lineup.reviewInputsRevision() {
		t.Fatal("alias suppression did not invalidate")
	}
	_ = automaticReview(t, s)
	var key string
	for k, ids := range s.admission.groups {
		for _, id := range ids {
			if id == "fixture-000" {
				key = k
			}
		}
	}
	if key == "" {
		t.Fatal("missing alias test group")
	}
	before = s.lineup.reviewInputsRevision()
	if w := postReviewChoice(s, key, "confirmed"); w.Code != 200 {
		t.Fatal("confirmation failed", w.Code)
	}
	if before == s.lineup.reviewInputsRevision() {
		t.Fatal("confirmation did not invalidate exposed alias inputs")
	}
	if w := postReviewChoice(s, key, "confirmed"); w.Code != 200 {
		t.Fatal("lost acknowledgement after alias exposure rejected", w.Code)
	}
}

func TestReviewResetHeaderRejectsQueuedOldRequest(t *testing.T) {
	s, keys, _ := syntheticReviewBurstFixture(t)
	cfg, _, _ := s.lineup.store.Get()
	if _, err := s.lineup.builder.ResetBuilder(cfg.Fingerprint(), s.lineup.builder.ReviewStateToken(), "", false); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"key": keys[0], "decision": "confirmed", "tvgIds": []string{}})
	r := httptest.NewRequest("POST", "/api/lineuparr/dispatcharr/decision", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Builder-Generation", "0")
	w := httptest.NewRecorder()
	s.lineup.guardBuilder(s.handleDecision)(w, r)
	if w.Code != 409 {
		t.Fatal("pre-reset request accepted", w.Code)
	}
}

func TestSettledReceiptCannotRecreateUndoWithoutFreshReview(t *testing.T) {
	s, keys, _ := syntheticReviewBurstFixture(t)
	cfg, _, _ := s.lineup.store.Get()
	if w := postReviewChoice(s, keys[0], "confirmed"); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if err := s.lineup.builder.ClearMatchDecisions(cfg.Fingerprint(), []string{keys[0]}); err != nil {
		t.Fatal(err)
	}
	if w := postReviewChoice(s, keys[0], "confirmed"); w.Code != 409 {
		t.Fatal("old receipt recreated undo", w.Code)
	}
	_ = automaticReview(t, s)
	if w := postReviewChoice(s, keys[0], "denied"); w.Code != 200 {
		t.Fatal("new explicit review blocked", w.Code)
	}
}

func TestDerivedReadCannotDelaySaveOrPublishOverNewChoice(t *testing.T) {
	s, keys, _ := syntheticReviewBurstFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	original := s.lineup.automaticProjection
	var once sync.Once
	s.lineup.automaticProjection = func(d *builder.Draft, c appconfig.Config) {
		once.Do(func() { close(entered); <-release })
		original(d, c)
	}
	s.lineup.draftMu.Lock()
	s.lineup.draftCache = cachedReviewDraft{}
	s.lineup.draftMu.Unlock()
	read := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		s.lineup.handleDraft(w, httptest.NewRequest("GET", "/api/lineuparr/draft", nil))
		read <- w.Code
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("read did not enter projection")
	}
	saved := make(chan int, 1)
	go func() { saved <- postReviewChoice(s, keys[0], "denied").Code }()
	select {
	case code := <-saved:
		if code != 200 {
			t.Error("save failed", code)
		}
	case <-time.After(time.Second):
		t.Error("read blocked warm save")
	}
	close(release)
	if code := <-read; code != 409 {
		t.Fatal("superseded read published", code)
	}
	_ = automaticDraft(t, s)
}

func TestCancelledRequestBeforePersistenceLeavesChoicesUnchanged(t *testing.T) {
	s, keys, _ := syntheticReviewBurstFixture(t)
	token := s.lineup.builder.ReviewStateToken()
	body, _ := json.Marshal(map[string]any{"key": keys[0], "decision": "confirmed", "tvgIds": []string{}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("POST", "/api/lineuparr/dispatcharr/decision", strings.NewReader(string(body))).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.lineup.guardBuilder(s.handleDecision)(w, r)
	if w.Code == 200 || s.lineup.builder.ReviewStateToken() != token {
		t.Fatal("cancelled request committed")
	}
}
