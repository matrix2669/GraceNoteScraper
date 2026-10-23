package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

type impactSourceTransport func(*http.Request) (*http.Response, error)

func (f impactSourceTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Retains the reviewer's instrumented-acquisition probe, now as a negative
// transport assertion for each preflight failure and a valid captured replay.
func TestAutomaticImpactPreflightAndCapturedInputsNeverAcquireSources(t *testing.T) {
	for _, kind := range []string{"missing", "warning", "source", "guide", "evidence", "inventory", "valid"} {
		t.Run(kind, func(t *testing.T) {
			s, review := supplementalServerFixture(t)
			if w := supplementalRequest(t, s, "POST", "/decision", supplementalSaveBody(review, review.Supplemental.Candidates[0])); w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			cfg, _, _ := s.lineup.store.Get()
			prior := s.lineup.builder.SupplementalDecisions(cfg.Fingerprint())
			store, err := builder.LoadStateStore(filepath.Join(t.TempDir(), "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			var acquired, consumed atomic.Int32
			svc := builder.NewService(store, builder.ServiceOptions{CacheDir: t.TempDir(), CatalogURLs: []string{"https://fixture.invalid/catalog.json"}, HTTPClient: &http.Client{Transport: impactSourceTransport(func(r *http.Request) (*http.Response, error) {
				acquired.Add(1)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"categories":{}}`)), Header: http.Header{}}, nil
			})}})
			rows := []builder.SupplementalDecision{}
			for _, d := range prior {
				rows = append(rows, d)
			}
			if err := svc.SaveSupplementalDecisions(cfg.Fingerprint(), svc.ReviewInputToken(), rows, false, func(f func() error) error { return f() }); err != nil {
				t.Fatal(err)
			}
			s.lineup.builder = svc
			switch kind {
			case "missing":
				s.clearCandidateCache()
			case "warning":
				s.snapshot.warning = "unsuccessful refresh"
			case "source":
				s.snapshot.lineupFingerprint = "other source"
			case "guide":
				s.lineup.state.UpdateForSource(&guide.TVGuide{}, cfg.Fingerprint())
			case "evidence":
				s.lineup.tmdbEvidenceRevision = func() uint64 { return 1 }
			case "inventory":
				conn, _ := s.config.Get()
				s.cache.observe(conn.Fingerprint(), nil)
			}
			s.impactRunner = func(context.Context, dispatcharr.ImpactRequest) (dispatcharr.ImpactResult, error) {
				consumed.Add(1)
				return dispatcharr.ImpactResult{}, nil
			}
			w := httptest.NewRecorder()
			s.startSupplementalImpact(w, httptest.NewRequest("POST", "/background-validation", nil), true)
			if acquired.Load() != 0 {
				t.Fatal("automatic preparation acquired optional source", acquired.Load())
			}
			if kind == "valid" {
				if w.Code != 202 {
					t.Fatal(w.Code, w.Body.String())
				}
				awaitImpact(t, s, "complete")
				if consumed.Load() != 1 {
					t.Fatal("captured local preparation failed to run", consumed.Load())
				}
			} else if w.Code != 409 || consumed.Load() != 0 {
				t.Fatal("invalid preflight launched work", w.Code, consumed.Load(), w.Body.String())
			}
			t.Logf("%s: source acquisitions=%d; consumer calls=%d; HTTP=%d", kind, acquired.Load(), consumed.Load(), w.Code)
		})
	}
}

func TestUnifiedRepairBrowserIntegration(t *testing.T) {
	node := os.Getenv("REVIEW_BROWSER_NODE")
	if node == "" {
		t.Skip("browser driver not requested")
	}
	for _, prior := range []string{"idle", "complete"} {
		t.Run(prior, func(t *testing.T) { unifiedRepairBrowser(t, node, prior) })
	}
}

func unifiedRepairBrowser(t *testing.T, node, prior string) {
	s, review := supplementalServerFixture(t)
	choiceTarget := review.Supplemental.Candidates[0].ChannelID
	cfg, _, _ := s.lineup.store.Get()
	// The generic family label does not authorize a West proposal. Give this
	// isolated lifecycle fixture explicit guide attribution for its West stream;
	// retrieval/stream names and the unrelated target remain unchanged.
	g := *s.lineup.state.Get()
	g.LineupChannels = append([]guide.Channel(nil), g.LineupChannels...)
	for i := range g.LineupChannels {
		if g.LineupChannels[i].PlacementID == choiceTarget {
			g.LineupChannels[i].EventCallSigns = []string{"Two Network West"}
		}
	}
	s.lineup.state.UpdateForSource(&g, cfg.Fingerprint())
	recorder := httptest.NewRecorder()
	rebuilt, ok := s.buildReview(recorder, httptest.NewRequest("GET", "/review", nil), true)
	if !ok {
		t.Fatal(recorder.Body.String())
	}
	review = rebuilt.response
	var initial supplementalGroup
	westOffered := false
	for _, candidate := range review.Supplemental.Candidates {
		if candidate.ChannelID == choiceTarget && candidate.StreamCount == 1 && len(candidate.Constituents) == 1 && candidate.Constituents[0].M3UAccountID == 3 && candidate.Constituents[0].StreamID == 2 && review.DecisionTokens[candidate.Key] != "" {
			westOffered = true
		}
		if candidate.ChannelID == choiceTarget && candidate.StreamCount == 2 {
			initial = candidate
		}
	}
	if initial.Key == "" || !westOffered {
		t.Fatal("same-target HD fixture missing")
	}
	if w := supplementalRequest(t, s, "POST", "/decision", supplementalSaveBody(review, initial)); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	s.lineup.exportDir = t.TempDir()
	s.lineup.commitDraft = s.commitAutomaticDraft
	mux := regressionMux(s)
	mux.HandleFunc("/api/lineuparr/publish", s.lineup.guardBuilder(s.lineup.handlePublish))
	mux.HandleFunc("/fixture", func(w http.ResponseWriter, r *http.Request) {
		recorder := httptest.NewRecorder()
		b, ok := s.buildReview(recorder, httptest.NewRequest("GET", "/review", nil), false)
		if !ok {
			http.Error(w, recorder.Body.String(), recorder.Code)
			return
		}
		draft := automaticDraft(t, s)
		if strings.Contains(draft.SupplementalMessage, "Check supplemental export impact") || !strings.Contains(draft.SupplementalMessage, "automatically") {
			t.Error("obsolete generation producer", draft.SupplementalMessage)
		}
		writeLineuparrJSON(w, 200, map[string]any{"draft": draft, "review": b.response, "choiceTarget": choiceTarget})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<!doctype html><title>Isolated repair</title>"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	cmd := exec.Command(node, "tools/test_unified_repair_browser.cjs")
	cmd.Env = append(os.Environ(), "UNIFIED_REPAIR_URL="+server.URL, "UNIFIED_REPAIR_PRIOR="+prior)
	data, err := cmd.CombinedOutput()
	t.Log(string(data))
	if err != nil {
		t.Fatal(err)
	}
	// Two literal name groups contain three distinct physical constituents.
	saved := s.lineup.builder.SupplementalDecisions(cfg.Fingerprint())
	if len(saved) != 3 {
		t.Fatal("real durable browser choices missing", saved)
	}
	want := map[[2]int64]bool{{3, 1}: true, {3, 2}: true, {4, 3}: true}
	for _, row := range saved {
		if row.ChannelID != choiceTarget {
			t.Fatal("other target was modified", row.ChannelID)
		}
		key := [2]int64{row.M3UAccountID, row.StreamID}
		if !want[key] {
			t.Fatal("unexpected physical choice", key)
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatal("missing physical choices", want)
	}
}
