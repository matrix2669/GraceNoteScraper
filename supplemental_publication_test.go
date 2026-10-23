package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

// The scheduling callback exists only in test builds. An isolated Go overlay
// calls it after saveWhileCurrent returns and before the handler processes that
// result. No production field/hook is added, and the returned result is unchanged.
var supplementalPublicationAfterSave = func(*dispatcharrServer, bool) {}

func runSupplementalPublicationOverlay(t *testing.T) bool {
	t.Helper()
	if os.Getenv("SUPPLEMENTAL_PUBLICATION_OVERLAY") == "1" {
		return false
	}
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "supplemental_review.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	begin := strings.Index(text, "func (s *dispatcharrServer) handleSupplementalDecision(")
	if begin < 0 {
		t.Fatal("supplemental handler boundary changed")
	}
	end := strings.Index(text[begin:], "\n// Evidence captured by refresh") + begin
	if end < begin {
		t.Fatal("supplemental handler boundary changed")
	}
	handler := text[begin:end]
	start := strings.LastIndex(handler, "if !s.saveWhileCurrent(")
	if start < 0 {
		t.Fatal("supplemental persistence boundary changed")
	}
	guardEnd := strings.Index(handler[start:], "}, w) {\n\t\treturn\n\t}")
	if guardEnd < 0 {
		t.Fatal("supplemental persistence guard changed")
	}
	guardEnd += start
	// Preserve the exact save call and boolean condition; only schedule a read
	// after its locks have released. The hook also observes actual failed writes.
	call := handler[start+len("if !") : guardEnd+len("}, w)")]
	replacement := "if !func() bool {\n\t\tsaved := " + call + "\n\t\tsupplementalPublicationAfterSave(s, saved)\n\t\treturn saved\n\t}()"
	handler = handler[:start] + replacement + handler[guardEnd+len("}, w)"):]
	temp := t.TempDir()
	copy := filepath.Join(temp, "supplemental_review.go")
	if err := os.WriteFile(copy, []byte(text[:begin]+handler+text[end:]), 0600); err != nil {
		t.Fatal(err)
	}
	overlay, _ := json.Marshal(map[string]any{"Replace": map[string]string{path: copy}})
	overlayPath := filepath.Join(temp, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-race", "-overlay", overlayPath, ".", "-run", "^"+t.Name()+"$", "-count=1", "-timeout=60s", "-v")
	cmd.Env = append(os.Environ(), "SUPPLEMENTAL_PUBLICATION_OVERLAY=1")
	out, err := cmd.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatalf("deterministic handler interleaving: %v", err)
	}
	return true
}

// Pause the real handler after its guarded store action has returned, before the
// HTTP acknowledgement. Ordinary GET runs concurrently at the exact R2 boundary.
func supplementalPublicationPause(t *testing.T, s *dispatcharrServer) (<-chan error, func()) {
	t.Helper()
	arrived := make(chan error, 1)
	release := make(chan struct{})
	var first atomic.Bool
	var once sync.Once
	resume := func() { once.Do(func() { close(release) }) }
	t.Cleanup(resume)
	supplementalPublicationAfterSave = func(server *dispatcharrServer, saved bool) {
		if server != s {
			return
		}
		if first.CompareAndSwap(false, true) {
			var err error
			if !saved {
				err = os.ErrInvalid
			}
			arrived <- err
			<-release
		}
	}
	return arrived, resume
}

func waitSupplementalPublication(t *testing.T, arrived <-chan error, failed bool) {
	t.Helper()
	select {
	case err := <-arrived:
		if (err != nil) != failed {
			t.Fatalf("guarded persistence failure=%v, wanted %v", err, failed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not reach guarded persistence boundary")
	}
}

func supplementalPublicationOperation(s *dispatcharrServer, token string) reviewOperation {
	s.reviewMu.RLock()
	defer s.reviewMu.RUnlock()
	return s.operations[token]
}

func TestSupplementalPersistAndReceiptPublicationConcurrentRead(t *testing.T) {
	if runSupplementalPublicationOverlay(t) {
		return
	}
	for _, choice := range []string{"confirmed", "denied"} {
		t.Run(choice, func(t *testing.T) {
			s, r := supplementalAutomaticFixture(t,
				dispatcharr.Stream{ID: 1, Name: "US: HBO 2 HD", M3UAccountID: 3},
				dispatcharr.Stream{ID: 2, Name: "US: HBO 2 HD", M3UAccountID: 4})
			g := r.Supplemental.Candidates[0]
			body := supplementalSaveBody(r, g)
			body["decision"] = choice
			arrived, resume := supplementalPublicationPause(t, s)
			completed := make(chan *httptest.ResponseRecorder, 1)
			go func() { completed <- supplementalRequest(t, s, "POST", "/decision", body) }()
			waitSupplementalPublication(t, arrived, false)
			cfg, _, _ := s.lineup.store.Get()
			if len(s.lineup.builder.SupplementalDecisions(cfg.Fingerprint())) != 2 {
				t.Fatal("boundary is not after the atomic durable group")
			}
			read := automaticReview(t, s)
			op := supplementalPublicationOperation(s, body["operation"])
			s.reviewMu.RLock()
			retained := len(s.supplementalAdmission.groups[g.Key])
			s.reviewMu.RUnlock()
			t.Logf("interleaved GET pending=%d history=%d receipt=%s retryMembers=%d", read.Supplemental.CandidateCount, len(read.Supplemental.Decisions), op.State, retained)
			if read.Supplemental.CandidateCount != 0 || len(read.Supplemental.Decisions) != 1 || op.State != "saved" || retained != 2 || len(op.Members) != 2 {
				t.Error("durable group exposed before saved receipt/admission publication")
			}
			// Undo and receipt resolution cannot overtake an in-flight mutation.
			if w := supplementalRequest(t, s, "DELETE", "/decision", map[string]string{"key": g.Key}); w.Code != 409 {
				t.Fatal("Undo overtook the unfinished save", w.Code)
			}
			resume()
			first := <-completed
			state := s.lineup.builder.ReviewStateToken()
			retry := supplementalRequest(t, s, "POST", "/decision", body)
			t.Logf("initialHTTP=%d receipt=%s retryHTTP=%d", first.Code, receiptFor(t, s, body["operation"])["state"], retry.Code)
			if first.Code != 200 || retry.Code != 200 || state != s.lineup.builder.ReviewStateToken() {
				t.Fatalf("durable retry failed or mutated state: initial=%d retry=%d %s", first.Code, retry.Code, retry.Body.String())
			}
			if w := supplementalRequest(t, s, "DELETE", "/decision", map[string]string{"key": g.Key}); w.Code != 200 {
				t.Fatal("Undo after save failed", w.Code)
			}
			_ = automaticReview(t, s)
			if receiptFor(t, s, body["operation"])["state"] != "undone" {
				t.Fatal("Undo receipt lost")
			}
			if w := supplementalRequest(t, s, "POST", "/decision", body); w.Code != 409 || len(s.lineup.builder.SupplementalDecisions(cfg.Fingerprint())) != 0 {
				t.Fatal("old committed retry resurrected Undo", w.Code)
			}
		})
	}
}

func TestSupplementalFailedPersistenceDoesNotPublishSavedReceipt(t *testing.T) {
	if runSupplementalPublicationOverlay(t) {
		return
	}
	s, _ := supplementalAutomaticFixture(t, dispatcharr.Stream{ID: 1, Name: "US: HBO 2 HD", M3UAccountID: 3})
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := builder.LoadStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s.lineup.builder = builder.NewService(store, builder.ServiceOptions{})
	r := automaticReview(t, s)
	g := r.Supplemental.Candidates[0]
	body := supplementalSaveBody(r, g)
	// A directory at the destination makes the real atomic rename fail.
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	state := s.lineup.builder.ReviewStateToken()
	arrived, resume := supplementalPublicationPause(t, s)
	completed := make(chan *httptest.ResponseRecorder, 1)
	go func() { completed <- supplementalRequest(t, s, "POST", "/decision", body) }()
	waitSupplementalPublication(t, arrived, true)
	read := automaticReview(t, s)
	if op := supplementalPublicationOperation(s, body["operation"]); op.State != "offered" || len(op.Members) != 0 || read.Supplemental.CandidateCount != 1 || len(read.Supplemental.Decisions) != 0 || state != s.lineup.builder.ReviewStateToken() {
		t.Fatal("failed write published success or changed state", op, read.Supplemental)
	}
	resume()
	if failed := <-completed; failed.Code != 400 {
		t.Fatal("persistence failure acknowledged", failed.Code)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if retry := supplementalRequest(t, s, "POST", "/decision", body); retry.Code != 200 {
		t.Fatal("unchanged offered choice could not retry failed write", retry.Code, retry.Body.String())
	}
	if receiptFor(t, s, body["operation"])["state"] != "saved" {
		t.Fatal("successful retry receipt missing")
	}
}

func TestSupplementalPublishedReceiptStillHonorsLifecycleFences(t *testing.T) {
	if runSupplementalPublicationOverlay(t) {
		return
	}
	for _, change := range []string{"source", "connection", "snapshot", "inventory", "reset", "evidence", "guide"} {
		t.Run(change, func(t *testing.T) {
			s, r := supplementalAutomaticFixture(t, dispatcharr.Stream{ID: 1, Name: "US: HBO 2 HD", M3UAccountID: 3})
			body := supplementalSaveBody(r, r.Supplemental.Candidates[0])
			arrived, resume := supplementalPublicationPause(t, s)
			completed := make(chan *httptest.ResponseRecorder, 1)
			go func() { completed <- supplementalRequest(t, s, "POST", "/decision", body) }()
			waitSupplementalPublication(t, arrived, false)
			_ = automaticReview(t, s)
			resume()
			if first := <-completed; first.Code != 200 {
				t.Fatal("initial persistence failed", first.Code)
			}
			cfg, _, _ := s.lineup.store.Get()
			switch change {
			case "source":
				cfg.Gracenote.PostalCode = "different-source"
				if err := s.lineup.store.Save(cfg); err != nil {
					t.Fatal(err)
				}
			case "connection":
				conn, _ := s.config.Get()
				conn.BaseURL = "https://different.fixture.invalid"
				if err := s.config.Save(conn); err != nil {
					t.Fatal(err)
				}
			case "snapshot":
				s.reviewMu.Lock()
				s.snapshot.fetchedAt = s.snapshot.fetchedAt.Add(time.Second)
				s.reviewMu.Unlock()
			case "inventory":
				s.cache.observe(s.snapshot.dispatcharrFingerprint, nil)
			case "reset":
				if _, err := s.lineup.builder.ResetBuilder(cfg.Fingerprint(), s.lineup.builder.ReviewStateToken(), "", false); err != nil {
					t.Fatal(err)
				}
				s.clearCandidateCache()
			case "evidence":
				s.lineup.tmdbEvidenceRevision = func() uint64 { return 1 }
			case "guide":
				copy := *s.lineup.state.Get()
				s.lineup.state.UpdateForSource(&copy, cfg.Fingerprint())
			}
			state := s.lineup.builder.ReviewStateToken()
			if retry := supplementalRequest(t, s, "POST", "/decision", body); retry.Code != 409 || state != s.lineup.builder.ReviewStateToken() {
				t.Fatal("saved receipt bypassed lifecycle fence", change, retry.Code)
			}
		})
	}
}
