package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

func regressionReviewFixture(t *testing.T, names ...string) (*dispatcharrServer, dispatcharrReviewResponse, string, *int) {
	t.Helper()
	s, api := newDispatcharrTestServer(t, true)
	path := filepath.Join(t.TempDir(), "state.json")
	store, err := builder.LoadStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s.lineup.builder = builder.NewService(store, builder.ServiceOptions{CacheDir: filepath.Join(t.TempDir(), "cache")})
	cfg, _, _ := s.lineup.store.Get()
	s.lineup.state.UpdateForSource(&guide.TVGuide{LineupChannels: []guide.Channel{{ID: "59368", PlacementID: "regression-hits", ChannelNo: "902", CallSign: "HBOHTHD"}}}, cfg.Fingerprint())
	api.streams = nil
	for i, name := range names {
		api.streams = append(api.streams, dispatcharr.Stream{ID: int64(i + 1), M3UAccountID: int64(i + 1), Name: name})
	}
	calls := new(int)
	s.matcher = func(ctx context.Context, source, country string, channels []dispatcharr.MatchChannel, streams []dispatcharr.Stream, decisions map[string]dispatcharr.Decision) (dispatcharr.CandidateSet, string, error) {
		*calls++
		return dispatcharr.MatchStreamCandidatesWithLineuparr(ctx, source, country, channels, streams, decisions)
	}
	s.lineup.automaticProjection = s.projectAutomaticExclusions
	s.lineup.commitDraft = s.commitAutomaticDraft
	s.lineup.derivedRevision = s.derivedRevision
	w := httptest.NewRecorder()
	built, ok := s.buildReview(w, httptest.NewRequest("GET", "/review", nil), true)
	if !ok || len(built.response.Candidates) == 0 {
		t.Fatalf("fixture: %d %s %+v", w.Code, w.Body.String(), built.response)
	}
	return s, built.response, path, calls
}

func regressionMux(s *dispatcharrServer) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/lineuparr/dispatcharr/decision", s.lineup.guardBuilder(s.handleDecision))
	mux.HandleFunc("/api/lineuparr/dispatcharr/review", s.lineup.guardBuilder(s.handleReview))
	mux.HandleFunc("/api/lineuparr/draft", s.lineup.guardBuilder(s.lineup.handleDraft))
	return mux
}

func regressionHTTP(t *testing.T, server *httptest.Server, method, path string, body any, expected int) []byte {
	t.Helper()
	data, _ := json.Marshal(body)
	r, err := http.NewRequest(method, server.URL+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err = io.ReadAll(response.Body)
	if err != nil || response.StatusCode != expected {
		t.Fatalf("%s %s: %d %s %v", method, path, response.StatusCode, data, err)
	}
	return data
}

func TestReviewRegressionActualHTTPConfirmDenyAndRestart(t *testing.T) {
	for _, name := range []string{"RK: HBO EAST 2", "US: HBO 2 HD"} {
		for _, decision := range []string{"confirmed", "denied"} {
			t.Run(name+"/"+decision, func(t *testing.T) {
				s, review, path, calls := regressionReviewFixture(t, name, name)
				g := review.Candidates[0]
				if g.StreamCount != 2 || g.ChannelDisplayName != "HBO Hits" || g.ChannelCallSign != "HBOHTHD" || len(g.ExactAliases) == 0 {
					t.Fatalf("presentation/group: %+v", g)
				}
				if name == "US: HBO 2 HD" {
					if len(g.ReviewWarnings) != 1 || !strings.Contains(g.ReviewWarnings[0], "USER-ATTESTED exact station/feed: user-attested/2026-10-22#59368:east") {
						t.Fatalf("warning: %+v", g)
					}
					oldReason := g.Reasons[0] + "; " + strings.Replace(g.ReviewWarnings[0], "Manual", "manual", 1)
					if len(oldReason) != 215 {
						t.Fatalf("expected actual215byte regression reason, got %d: %s", len(oldReason), oldReason)
					}
				} else if len(g.ReviewWarnings) != 0 {
					t.Fatalf("explicit East still unresolved: %+v", g)
				}
				server := httptest.NewServer(regressionMux(s))
				defer server.Close()
				body := map[string]string{"key": g.Key, "operation": review.DecisionTokens[g.Key], "decision": decision}
				regressionHTTP(t, server, "POST", "/api/lineuparr/dispatcharr/decision", body, 200)
				cfg, _, _ := s.lineup.store.Get()
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				for _, d := range s.lineup.builder.MatchDecisions(cfg.Fingerprint()) {
					if d.Reason != "Lineuparr alias" || d.Decision != decision {
						t.Fatalf("persisted decoration: %+v", d)
					}
				}
				if len(s.lineup.builder.MatchDecisions(cfg.Fingerprint())) != 2 {
					t.Fatal("constituents not atomic")
				}
				regressionHTTP(t, server, "POST", "/api/lineuparr/dispatcharr/decision", body, 200)
				after, _ := os.ReadFile(path)
				if !bytes.Equal(before, after) {
					t.Fatal("idempotent retry rewrote state")
				}
				data := regressionHTTP(t, server, "GET", "/api/lineuparr/dispatcharr/decision?operation="+body["operation"], nil, 200)
				if !bytes.Contains(data, []byte(`"state":"saved"`)) {
					t.Fatalf("receipt: %s", data)
				}
				data = regressionHTTP(t, server, "GET", "/api/lineuparr/dispatcharr/review", nil, 200)
				var saved dispatcharrReviewResponse
				json.Unmarshal(data, &saved)
				if saved.ConfirmedCount+saved.DeniedCount != 1 || len(saved.Decisions) != 1 || saved.Decisions[0].StreamCount != 2 || saved.Decisions[0].ChannelDisplayName != "HBO Hits" {
					t.Fatalf("group counts: %+v", saved)
				}
				reloaded, err := builder.LoadStateStore(path)
				if err != nil {
					t.Fatal(err)
				}
				s.lineup.builder = builder.NewService(reloaded, builder.ServiceOptions{})
				s.clearCandidateCache()
				data = regressionHTTP(t, server, "GET", "/api/lineuparr/dispatcharr/review", nil, 200)
				json.Unmarshal(data, &saved)
				if saved.ConfirmedCount+saved.DeniedCount != 1 || *calls != 1 {
					t.Fatal("restart history or one-shot matcher", saved, *calls)
				}
				regressionHTTP(t, server, "DELETE", "/api/lineuparr/dispatcharr/decision", map[string]string{"key": g.Key}, 200)
				regressionHTTP(t, server, "POST", "/api/lineuparr/dispatcharr/decision", body, 409)
				if len(s.lineup.builder.MatchDecisions(cfg.Fingerprint())) != 0 {
					t.Fatal("Undo did not preserve empty state")
				}
			})
		}
	}
}

func TestReviewRegressionInvalidMetadataRejectsWholeGroup(t *testing.T) {
	s, review, path, _ := regressionReviewFixture(t, "US: HBO 2 HD", "US: HBO 2 HD")
	g := review.Candidates[0]
	// Corrupt one admitted persisted reason, not its independent display warning.
	s.reviewMu.Lock()
	keys := s.admission.groups[g.Key]
	bad := s.admission.candidates[keys[1]]
	bad.Reason = strings.Repeat("x", 201)
	s.admission.candidates[keys[1]] = bad
	s.reviewMu.Unlock()
	before, _ := os.ReadFile(path)
	server := httptest.NewServer(regressionMux(s))
	defer server.Close()
	regressionHTTP(t, server, "POST", "/api/lineuparr/dispatcharr/decision", map[string]string{"key": g.Key, "operation": review.DecisionTokens[g.Key], "decision": "confirmed"}, 400)
	after, _ := os.ReadFile(path)
	cfg, _, _ := s.lineup.store.Get()
	if !bytes.Equal(before, after) || len(s.lineup.builder.MatchDecisions(cfg.Fingerprint())) != 0 {
		t.Fatal("invalid metadata partially mutated group")
	}
}

func TestReviewRegressionBrowser(t *testing.T) {
	node := os.Getenv("REVIEW_BROWSER_NODE")
	if node == "" {
		t.Skip("browser driver not requested")
	}
	s, review, _, _ := regressionReviewFixture(t, "US: HBO 2 HD", "US: HBO 2 HD", "RK: HBO EAST 2")
	mux := regressionMux(s)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!doctype html><title>Isolated regression</title>"))
	})
	mux.HandleFunc("/fixture", func(w http.ResponseWriter, r *http.Request) {
		writeLineuparrJSON(w, 200, map[string]any{"draft": automaticDraft(t, s), "review": review})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	cmd := exec.Command(node, "tools/test_review_regression_browser.cjs")
	cmd.Env = append(os.Environ(), "REVIEW_REGRESSION_URL="+server.URL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser: %v\n%s", err, out)
	}
	t.Log(string(out))
}
