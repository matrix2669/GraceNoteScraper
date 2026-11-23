package main

import (
	"context"
	"encoding/json"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

// These scheduling gates exist only in the test request/response. The production
// handler, admission locks, atomic store write and receipt publication all run.
type acknowledgementContext struct {
	context.Context
	once    sync.Once
	entered chan struct{}
	release chan struct{}
	cancel  context.CancelFunc
}

func (c *acknowledgementContext) Err() error {
	before := c.Context.Err()
	c.once.Do(func() { close(c.entered); <-c.release; c.cancel() })
	return before // Cancellation occurs just after the final successful check.
}

type suppressedAcknowledgement struct {
	header  http.Header
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *suppressedAcknowledgement) Header() http.Header { return w.header }
func (w *suppressedAcknowledgement) WriteHeader(int)     {}
func (w *suppressedAcknowledgement) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered); <-w.release })
	return 0, io.ErrClosedPipe
}
func awaitAcknowledgementGate(t *testing.T, gate <-chan struct{}) {
	t.Helper()
	select {
	case <-gate:
	case <-time.After(5 * time.Second):
		t.Fatal("real handler did not reach acknowledgement gate")
	}
}
func acknowledgementFixture(t *testing.T, supplemental bool) (*dispatcharrServer, string, string, string) {
	t.Helper()
	var s *dispatcharrServer
	var key string
	if supplemental {
		var review dispatcharrReviewResponse
		s, review = supplementalServerFixture(t)
		key = review.Supplemental.Candidates[0].Key
	} else {
		var keys []string
		s, keys, _ = syntheticReviewBurstFixture(t)
		key = keys[0]
	}
	path := filepath.Join(t.TempDir(), "choices.json")
	store, err := builder.LoadStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s.lineup.builder = builder.NewService(store, builder.ServiceOptions{CacheDir: filepath.Join(t.TempDir(), "cache")})
	review := automaticReview(t, s)
	operation := review.DecisionTokens[key]
	if operation == "" {
		t.Fatal("fixture lost actual admission")
	}
	return s, key, operation, path
}
func acknowledgementRequest(key, operation string, ctx context.Context) *http.Request {
	data, _ := json.Marshal(map[string]any{"key": key, "operation": operation, "decision": "confirmed", "tvgIds": []string{}})
	r := httptest.NewRequest("POST", "/api/lineuparr/dispatcharr/decision", strings.NewReader(string(data))).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	return r
}
func TestSaveAcknowledgementPrimaryAndSupplementalInterleavings(t *testing.T) {
	for _, kind := range []string{"primary", "supplemental"} {
		t.Run(kind, func(t *testing.T) {
			s, key, op, path := acknowledgementFixture(t, kind == "supplemental")
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := &acknowledgementContext{Context: base, entered: make(chan struct{}), release: make(chan struct{}), cancel: cancel}
			w := &suppressedAcknowledgement{header: make(http.Header), entered: make(chan struct{}), release: make(chan struct{})}
			var releaseContext, releaseResponse sync.Once
			unblockContext := func() { releaseContext.Do(func() { close(ctx.release) }) }
			defer unblockContext()
			unblockResponse := func() { releaseResponse.Do(func() { close(w.release) }) }
			defer unblockResponse()
			done := make(chan struct{})
			go func() {
				defer close(done)
				s.lineup.guardBuilder(s.handleDecision)(w, acknowledgementRequest(key, op, ctx))
			}()
			awaitAcknowledgementGate(t, ctx.entered)
			receipt := httptest.NewRecorder()
			s.handleDecisionReceipt(receipt, httptest.NewRequest("GET", "/decision?operation="+op, nil))
			if receipt.Code != 409 {
				t.Fatalf("admitted running write receipt=%d %s", receipt.Code, receipt.Body.String())
			}
			unblockContext()
			awaitAcknowledgementGate(t, w.entered)
			// Persistence has completed but the acknowledgement cannot leave the handler.
			// Receipt lookup still cannot retire the token through this lock interval.
			receipt = httptest.NewRecorder()
			s.handleDecisionReceipt(receipt, httptest.NewRequest("GET", "/decision?operation="+op, nil))
			if receipt.Code != 409 {
				t.Fatalf("committed blocked ACK receipt=%d", receipt.Code)
			}
			persisted, err := builder.LoadStateStore(path)
			if err != nil {
				t.Fatal(err)
			}
			config, _, _ := s.lineup.store.Get()
			if kind == "primary" {
				if len(persisted.MatchDecisionSnapshot(config.Fingerprint())) == 0 {
					t.Fatal("primary ACK preceded durable readback")
				}
			} else {
				if len(builder.NewService(persisted, builder.ServiceOptions{}).SupplementalDecisions(config.Fingerprint())) == 0 {
					t.Fatal("supplemental ACK preceded durable readback")
				}
			}
			if base.Err() != context.Canceled {
				t.Fatal("post-check cancellation did not run")
			}
			unblockResponse()
			awaitAcknowledgementGate(t, done)
			got := receiptFor(t, s, op)
			if got["state"] != "saved" || got["key"] != key || got["decision"] != "confirmed" {
				t.Fatalf("lost ACK/cancel became not-saved: %+v", got)
			}
			undoReview(t, s, key)
			if receiptFor(t, s, op)["state"] != "undone" {
				t.Fatal("Undo did not retire receipt")
			}
			if late := postReviewOperation(s, key, "confirmed", op); late.Code != 409 {
				t.Fatal("late response/request resurrected Undo", late.Code)
			}
		})
	}
}
func TestSaveAcknowledgementPrimaryAndSupplementalRetirement(t *testing.T) {
	for _, kind := range []string{"primary", "supplemental"} {
		t.Run(kind, func(t *testing.T) {
			s, key, op, _ := acknowledgementFixture(t, kind == "supplemental")
			if receiptFor(t, s, op)["state"] != "not-saved" {
				t.Fatal("offered operation not fenced")
			}
			for i := 0; i < 2; i++ {
				if late := postReviewOperation(s, key, "confirmed", op); late.Code != 409 {
					t.Fatal("retired operation admitted", late.Code)
				}
				_ = automaticReview(t, s)
			}
			review := automaticReview(t, s)
			fresh := review.DecisionTokens[key]
			if fresh == op || fresh == "" {
				t.Fatal("explicit retry lacks fresh admission")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			failed := httptest.NewRecorder()
			s.lineup.guardBuilder(s.handleDecision)(failed, acknowledgementRequest(key, fresh, ctx))
			if failed.Code == 200 {
				t.Fatal("cancel before write acknowledged")
			}
			if receiptFor(t, s, fresh)["state"] != "not-saved" {
				t.Fatal("pre-write cancellation became saved")
			}
		})
	}
}

// A request-only gate holds a real admitted write at its final context check.
// No production hook or backend behavior is changed by these browser schedules.
type browserWriteGate struct {
	context.Context
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *browserWriteGate) Err() error {
	before := g.Context.Err()
	g.once.Do(func() { close(g.entered); <-g.release })
	return before
}

type browserSaveFixture struct {
	server            *dispatcharrServer
	path, fingerprint string
	gate              *browserWriteGate
	release           sync.Once
}

func newBrowserSaveFixture(t *testing.T, kind string) *browserSaveFixture {
	t.Helper()
	var s *dispatcharrServer
	var path string
	if kind == "supplemental" {
		var fake *fakeDispatcharrAPI
		s, fake = newDispatcharrTestServer(t, true)
		fake.streams = []dispatcharr.Stream{{ID: 1, M3UAccountID: 3, Name: "US| Two Network HD"}, {ID: 2, M3UAccountID: 3, Name: "US| Two Network West"}, {ID: 3, M3UAccountID: 3, Name: "US| Two Network East"}, {ID: 4, M3UAccountID: 4, Name: "US| Two Network HD"}}
		s.matcher = func(context.Context, string, string, []dispatcharr.MatchChannel, []dispatcharr.Stream, map[string]dispatcharr.Decision) (dispatcharr.CandidateSet, string, error) {
			return dispatcharr.CandidateSet{}, "test", nil
		}
		cfg, _, _ := s.lineup.store.Get()
		next := *s.lineup.state.Get()
		extra := next.LineupChannels[0]
		extra.PlacementID = "1003"
		extra.ChannelNo = "503"
		next.LineupChannels = append(next.LineupChannels[:len(next.LineupChannels):len(next.LineupChannels)], extra)
		s.lineup.state.UpdateForSource(&next, cfg.Fingerprint())
		s.lineup.automaticProjection = s.projectAutomaticExclusions
		s.lineup.derivedRevision = s.derivedRevision
		w := httptest.NewRecorder()
		if _, ok := s.buildReview(w, httptest.NewRequest("GET", "/review", nil), true); !ok {
			t.Fatal(w.Body.String())
		}
		path = filepath.Join(t.TempDir(), "choices.json")
		store, err := builder.LoadStateStore(path)
		if err != nil {
			t.Fatal(err)
		}
		s.lineup.builder = builder.NewService(store, builder.ServiceOptions{CacheDir: filepath.Join(t.TempDir(), "cache")})
	} else {
		s, _, _, path = acknowledgementFixture(t, false)
		member := s.snapshot.candidates[0]
		member.Key += "-physical-copy"
		member.StreamID += 1000
		member.StreamKey += "-physical-copy"
		member.StreamHash += "-physical-copy"
		member.Score, member.NameScore = 100, 100
		s.snapshot.candidates = append(s.snapshot.candidates, member)
	}
	cfg, _, _ := s.lineup.store.Get()
	return &browserSaveFixture{server: s, path: path, fingerprint: cfg.Fingerprint(), gate: &browserWriteGate{Context: context.Background(), entered: make(chan struct{}), release: make(chan struct{})}}
}
func TestSaveRetirementBrowserIntegration(t *testing.T) {
	node := os.Getenv("REVIEW_BROWSER_NODE")
	if node == "" {
		t.Skip("browser driver not requested")
	}
	var mu sync.RWMutex
	var active *browserSaveFixture
	mux := http.NewServeMux()
	mux.HandleFunc("/fixture", func(w http.ResponseWriter, r *http.Request) {
		next := newBrowserSaveFixture(t, r.URL.Query().Get("kind"))
		mu.Lock()
		if active != nil {
			active.release.Do(func() { close(active.gate.release) })
		}
		active = next
		mu.Unlock()
		writeLineuparrJSON(w, 200, map[string]any{"review": automaticReview(t, next.server), "draft": automaticDraft(t, next.server)})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		f := active
		mu.RUnlock()
		if f == nil {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!doctype html><title>Isolated save retirement</title>"))
			return
		}
		switch r.URL.Path {
		case "/api/lineuparr/dispatcharr/decision":
			if r.Method == "POST" && r.Header.Get("X-Test-Hold") == "true" {
				r = r.WithContext(&browserWriteGate{Context: r.Context(), entered: f.gate.entered, release: f.gate.release})
			}
			f.server.lineup.guardBuilder(f.server.handleDecision)(w, r)
		case "/api/lineuparr/dispatcharr/review":
			f.server.lineup.guardBuilder(f.server.handleReview)(w, r)
		case "/api/lineuparr/draft":
			f.server.lineup.guardBuilder(f.server.lineup.handleDraft)(w, r)
		case "/test/release":
			f.release.Do(func() { close(f.gate.release) })
			writeLineuparrJSON(w, 200, map[string]bool{"released": true})
		case "/test/exclude":
			included := false
			if err := f.server.lineup.builder.UpdateChannel(f.fingerprint, r.URL.Query().Get("id"), builder.ChannelUpdate{Included: &included}); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			writeLineuparrJSON(w, 200, map[string]bool{"excluded": true})
		case "/test/source":
			cfg, _, _ := f.server.lineup.store.Get()
			cfg.Gracenote.LineupID = "DIFFERENT-TEST-SOURCE"
			if err := f.server.lineup.store.Save(cfg); err != nil {
				t.Error(err)
			}
			writeLineuparrJSON(w, 200, map[string]string{"source": cfg.Fingerprint()})
		case "/test/inspection":
			store, err := builder.LoadStateStore(f.path)
			if err != nil {
				t.Error(err)
				http.Error(w, err.Error(), 500)
				return
			}
			held := false
			select {
			case <-f.gate.entered:
				held = true
			default:
			}
			writeLineuparrJSON(w, 200, map[string]any{"primary": len(store.MatchDecisionSnapshot(f.fingerprint)), "supplemental": len(builder.NewService(store, builder.ServiceOptions{}).SupplementalDecisions(f.fingerprint)), "held": held})
		default:
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<!doctype html><title>Isolated save retirement</title>"))
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	defer func() {
		mu.Lock()
		if active != nil {
			active.release.Do(func() { close(active.gate.release) })
		}
		mu.Unlock()
	}()
	cmd := exec.Command(node, "tools/test_save_retirement_integration.cjs")
	cmd.Env = append(os.Environ(), "SAVE_RETIREMENT_URL="+server.URL)
	output, err := cmd.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatalf("actual renderer/handler save retirement: %v", err)
	}
}
