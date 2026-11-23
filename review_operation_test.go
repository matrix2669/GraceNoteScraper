package main

import (
	"bytes"
	"encoding/json"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Adapt pre-token functional fixtures to the current public transport. Tests of
// old replay below deliberately retain and submit their original opaque token.
func withTestReviewOperation(s *dispatcharrServer, r *http.Request) *http.Request {
	if r.Method != "POST" {
		return r
	}
	data, _ := io.ReadAll(r.Body)
	var body map[string]any
	if json.Unmarshal(data, &body) != nil {
		r.Body = io.NopCloser(bytes.NewReader(data))
		return r
	}
	if _, ok := body["operation"]; !ok {
		key, _ := body["key"].(string)
		s.reviewMu.RLock()
		operation := s.admission.tokens[key]
		s.reviewMu.RUnlock()
		if operation == "" {
			w := httptest.NewRecorder()
			s.handleReview(w, httptest.NewRequest("GET", "/api/lineuparr/dispatcharr/review", nil))
			s.reviewMu.RLock()
			operation = s.admission.tokens[key]
			s.reviewMu.RUnlock()
		}
		body["operation"] = operation
	}
	data, _ = json.Marshal(body)
	r.Body = io.NopCloser(bytes.NewReader(data))
	return r
}
func receiptFor(t *testing.T, s *dispatcharrServer, operation string) map[string]string {
	t.Helper()
	w := httptest.NewRecorder()
	s.lineup.guardBuilder(s.handleDecision)(w, httptest.NewRequest("GET", "/api/lineuparr/dispatcharr/decision?operation="+operation, nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}
func undoReview(t *testing.T, s *dispatcharrServer, key string) {
	t.Helper()
	r := httptest.NewRequest("DELETE", "/api/lineuparr/dispatcharr/decision", strings.NewReader(`{"key":"`+key+`"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.lineup.guardBuilder(s.handleDecision)(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestReviewOperationCannotReplayAfterUndoAndReads(t *testing.T) {
	s, keys, _ := syntheticReviewBurstFixture(t)
	key := keys[0]
	old := s.admission.tokens[key]
	if w := postReviewOperation(s, key, "confirmed", old); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	state := s.lineup.builder.ReviewStateToken()
	if w := postReviewOperation(s, key, "confirmed", old); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if state != s.lineup.builder.ReviewStateToken() {
		t.Fatal("retry rewrote durable state")
	}
	undoReview(t, s, key)
	if receiptFor(t, s, old)["state"] != "undone" {
		t.Fatal("Undo receipt missing")
	}
	for i := 0; i < 3; i++ {
		_ = automaticDraft(t, s)
		_ = automaticReview(t, s)
		if w := postReviewOperation(s, key, "confirmed", old); w.Code != 409 {
			t.Fatal("old request rearmed by read", w.Code)
		}
	}
	fresh := s.admission.tokens[key]
	if fresh == old || fresh == "" {
		t.Fatal("new choice did not receive a fresh token")
	}
	if w := postReviewOperation(s, key, "denied", fresh); w.Code != 200 {
		t.Fatal("new explicit choice blocked", w.Code, w.Body.String())
	}
	if w := postReviewOperation(s, key, "confirmed", old); w.Code != 409 {
		t.Fatal("old operation reversed new choice", w.Code)
	}
	if w := postReviewOperation(s, keys[1], "confirmed", ""); w.Code != 409 || !strings.Contains(w.Body.String(), "reload") {
		t.Fatal("old client must reload", w.Code)
	}
}

func groupedOperationFixture(t *testing.T) (*dispatcharrServer, string) {
	s, _, _ := syntheticReviewBurstFixture(t)
	c := s.snapshot.candidates[0]
	c.Key += "-z"
	c.StreamID += 1000
	c.StreamKey += "-second"
	c.StreamHash += "-second"
	c.Score = 96
	c.NameScore = 96
	s.snapshot.candidates = append(s.snapshot.candidates, c)
	review := automaticReview(t, s)
	for _, g := range review.Candidates {
		if g.StreamCount == 2 {
			return s, g.Key
		}
	}
	t.Fatal("missing two-member fixture")
	return nil, ""
}
func TestGroupedOperationReceiptSurvivesPaginationAndSupersession(t *testing.T) {
	for _, change := range []string{"reset", "snapshot", "inventory", "source"} {
		t.Run(change, func(t *testing.T) {
			s, key := groupedOperationFixture(t)
			operation := s.admission.tokens[key]
			if w := postReviewOperation(s, key, "confirmed", operation); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			review := automaticReview(t, s)
			cfg, _, _ := s.lineup.store.Get()
			stored := s.lineup.builder.MatchDecisions(cfg.Fingerprint())
			if len(stored) != 2 {
				t.Fatal("raw members lost")
			}
			scores := map[int]bool{}
			for _, d := range stored {
				scores[d.Score] = true
			}
			if !scores[96] || !scores[100] {
				t.Fatal("actual scores lost")
			}
			receipt := receiptFor(t, s, operation)
			if receipt["state"] != "saved" || receipt["key"] != key {
				t.Fatal("group receipt lost", receipt)
			}
			w := httptest.NewRecorder()
			s.handleReview(w, httptest.NewRequest("GET", "/api/lineuparr/dispatcharr/review?limit=1", nil))
			if w.Code != 200 {
				t.Fatal(w.Code)
			}
			if receiptFor(t, s, operation)["state"] != "saved" {
				t.Fatal("pagination changed receipt")
			}
			// Undo uses the display representative and still removes the entire group.
			undoReview(t, s, review.Decisions[0].Key)
			if len(s.lineup.builder.MatchDecisions(cfg.Fingerprint())) != 0 {
				t.Fatal("Undo lost raw members")
			}
			if receiptFor(t, s, operation)["state"] != "undone" {
				t.Fatal("group Undo receipt lost")
			}
			switch change {
			case "reset":
				if _, err := s.lineup.builder.ResetBuilder(cfg.Fingerprint(), s.lineup.builder.ReviewStateToken(), "", false); err != nil {
					t.Fatal(err)
				}
				s.clearCandidateCache()
			case "snapshot":
				s.reviewMu.Lock()
				s.snapshot.fetchedAt = time.Now().UTC()
				s.reviewMu.Unlock()
			case "inventory":
				s.cache.observe(s.snapshot.dispatcharrFingerprint, nil)
			case "source":
				cfg.Gracenote.PostalCode = "changed-source"
				if err := s.lineup.store.Save(cfg); err != nil {
					t.Fatal(err)
				}
			}
			if receiptFor(t, s, operation)["state"] != "superseded" {
				t.Fatal("old operation not superseded", change)
			}
			if w := postReviewOperation(s, key, "confirmed", operation); w.Code != 409 {
				t.Fatal("superseded operation accepted", change, w.Code)
			}
		})
	}
}

func TestReviewPublicationRejectsCompletedGenerationReplacement(t *testing.T) {
	for _, change := range []string{"snapshot", "inventory", "source", "reset"} {
		t.Run(change, func(t *testing.T) {
			s, _, _ := syntheticReviewBurstFixture(t)
			newer := s.snapshot
			streams := s.cache.streams
			s.cache.observe(newer.dispatcharrFingerprint, nil)
			old := newer
			old.candidates = nil
			old.identityStreams = nil
			old.streamCount = 0
			old.inventoryDigest = streamInventoryDigest(nil)
			old.fetchedAt = time.Now().UTC()
			s.reviewMu.Lock()
			s.snapshot = old
			s.reviewMu.Unlock()
			_ = automaticDraft(t, s)
			s.automaticMu.Lock()
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				w := httptest.NewRecorder()
				s.handleReview(w, httptest.NewRequest("GET", "/api/lineuparr/dispatcharr/review", nil))
				done <- w
			}()
			entered := false
			for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
				buf := make([]byte, 1<<20)
				n := runtime.Stack(buf, true)
				if strings.Contains(string(buf[:n]), "(*dispatcharrServer).automaticView(") {
					entered = true
					break
				}
				runtime.Gosched()
			}
			if !entered {
				s.automaticMu.Unlock()
				t.Fatal("review did not reach paused derived phase")
			}
			cfg, _, _ := s.lineup.store.Get()
			switch change {
			case "snapshot":
				s.cache.observe(newer.dispatcharrFingerprint, streams)
				newer.fetchedAt = time.Now().UTC()
				s.reviewMu.Lock()
				s.snapshot = newer
				s.reviewMu.Unlock()
			case "inventory":
				s.cache.observe(newer.dispatcharrFingerprint, streams)
			case "source":
				cfg.Gracenote.PostalCode = "changed-source"
				if err := s.lineup.store.Save(cfg); err != nil {
					t.Fatal(err)
				}
			case "reset":
				if _, err := s.lineup.builder.ResetBuilder(cfg.Fingerprint(), s.lineup.builder.ReviewStateToken(), "", false); err != nil {
					t.Fatal(err)
				}
				s.clearCandidateCache()
			}
			s.automaticMu.Unlock()
			select {
			case w := <-done:
				if w.Code != 409 {
					t.Fatalf("obsolete zero review published after %s: %d", change, w.Code)
				}
			case <-time.After(time.Second):
				t.Fatal("review did not finish")
			}
		})
	}
}

func TestReviewRecoveryBrowserIntegration(t *testing.T) {
	node := os.Getenv("REVIEW_BROWSER_NODE")
	if node == "" {
		t.Skip("browser driver not requested")
	}
	s, key := groupedOperationFixture(t)
	s.lineup.afterBuilderReset = s.clearCandidateCache
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!doctype html><title>Isolated grouped recovery fixture</title>"))
	})
	mux.HandleFunc("/fixture", func(w http.ResponseWriter, r *http.Request) {
		review := automaticReview(t, s)
		draft := automaticDraft(t, s)
		// Grouped key must be visible, regardless of fixture score ordering.
		selected := []dispatcharr.CandidateGroup{}
		for _, c := range review.Candidates {
			if c.Key == key {
				selected = append(selected, c)
			}
		}
		for _, c := range review.Candidates {
			if c.Key != key && len(selected) < 100 {
				selected = append(selected, c)
			}
		}
		review.Candidates = selected
		writeLineuparrJSON(w, 200, map[string]any{"draft": draft, "review": review, "recovery": true, "recoveryKey": key})
	})
	mux.HandleFunc("/fixture/refresh", func(w http.ResponseWriter, r *http.Request) {
		s.refreshMu.Lock()
		defer s.refreshMu.Unlock()
		// Emulate only the completed publication of an explicit refresh; no matcher
		// or external service is invoked by this isolated fault-interleaving fixture.
		s.reviewMu.Lock()
		s.snapshot.fetchedAt = time.Now().UTC()
		s.reviewMu.Unlock()
		writeLineuparrJSON(w, 200, map[string]bool{"refreshed": true})
	})
	mux.HandleFunc("/api/lineuparr/dispatcharr/decision", s.lineup.guardBuilder(s.handleDecision))
	mux.HandleFunc("/api/lineuparr/dispatcharr/review", s.lineup.guardBuilder(s.handleReview))
	mux.HandleFunc("/api/lineuparr/draft", s.lineup.guardBuilder(s.lineup.handleDraft))
	mux.HandleFunc("/api/lineuparr/reset", s.lineup.handleBuilderReset)
	server := httptest.NewServer(mux)
	defer server.Close()
	cmd := exec.Command(node, "tools/test_review_handler_integration.cjs")
	cmd.Env = append(os.Environ(), "REVIEW_HANDLER_URL="+server.URL)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("grouped browser recovery: %v\n%s", err, output)
	}
	t.Log(string(output))
}

func TestUncommittedReceiptFencesDelayedArrival(t *testing.T) {
	s, keys, _ := syntheticReviewBurstFixture(t)
	key := keys[0]
	operation := s.admission.tokens[key]
	if receiptFor(t, s, operation)["state"] != "not-saved" {
		t.Fatal("expected authoritative uncommitted receipt")
	}
	if w := postReviewOperation(s, key, "confirmed", operation); w.Code != 409 {
		t.Fatal("delayed request committed after not-saved receipt", w.Code)
	}
	_ = automaticReview(t, s)
	if w := postReviewOperation(s, key, "confirmed", operation); w.Code != 409 {
		t.Fatal("read rearmed retired uncommitted token", w.Code)
	}
	if w := postReviewChoice(s, key, "denied"); w.Code != 200 {
		t.Fatal("fresh choice after receipt blocked", w.Code)
	}
}
