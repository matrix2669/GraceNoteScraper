package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewKindsKeepPerConstituentThresholdAndFeedException(t *testing.T) {
	candidates := []dispatcharr.Candidate{
		{Key: "low", ChannelID: "a", NormalizedAlias: "network", Score: 94, StreamName: "Network HD"},
		{Key: "exact", ChannelID: "a", NormalizedAlias: "network", Score: 95, StreamName: "Network SD"},
		{Key: "perfect", ChannelID: "a", NormalizedAlias: "network", Score: 100, StreamName: "Network UHD"},
		{Key: "pending", ChannelID: "a", NormalizedAlias: "network", Score: 100, StreamName: "Network"},
	}
	classifyReviewCandidates(candidates, automaticMatchView{Rows: []automaticMatchRow{{Key: "pending", PendingReview: true}}})
	want := []string{"inclusion", "qualified", "qualified", "feed-qualified"}
	for i, c := range candidates {
		if c.ReviewKind != want[i] {
			t.Fatalf("%s: %s", c.Key, c.ReviewKind)
		}
	}
	groups := dispatcharr.GroupCandidates(candidates)
	if len(groups) != 3 {
		t.Fatalf("mixed threshold classes combined: %+v", groups)
	}
	// A saved low-score inclusion must not make a different qualified or feed-review
	// constituent disappear merely because normalization produced the same alias.
	d := dispatcharr.Decision{ChannelID: "a", NormalizedAlias: "network", ReviewKind: "inclusion", Decision: "confirmed", StreamName: "Network HD"}
	for i := range candidates {
		candidates[i].StreamHash = candidates[i].Key
	}
	d.StreamHash = "low"
	remaining := dispatcharr.FilterSnapshotCandidates(candidates, map[string]bool{"a": true}, map[string]dispatcharr.Decision{"low": d})
	if len(remaining) != 3 {
		t.Fatalf("decision suppressed other classes: %+v", remaining)
	}
}

// Visibility, saved review decisions and export eligibility are separate contracts.
// Scores are supplied by an isolated matcher stub, never fabricated in production.
func TestHighScoreReviewRemainsVisibleAndDecisionExportSemantics(t *testing.T) {
	for _, decision := range []string{"confirmed", "denied"} {
		t.Run(decision, func(t *testing.T) {
			server, fake := newDispatcharrTestServer(t, true)
			server.matcher = func(_ context.Context, source, _ string, _ []dispatcharr.MatchChannel, _ []dispatcharr.Stream, _ map[string]dispatcharr.Decision) (dispatcharr.CandidateSet, string, error) {
				rows := []dispatcharr.Candidate{}
				for i, score := range []int{94, 95, 100} {
					key := []string{"low", "boundary", "high"}[i]
					rows = append(rows, dispatcharr.Candidate{Key: key, ChannelID: "1001", ChannelNumber: "2", ChannelName: "TWO", StreamKey: key, StreamHash: key, StreamName: "US: Fixture " + key, M3UAccountID: int64(i + 1), Source: source, Score: score, NameScore: score, NormalizedAlias: "samegroup", MatcherVersion: "test-threshold"})
				}
				return dispatcharr.CandidateSet{All: rows}, "test-threshold", nil
			}
			read := func(query string) dispatcharrReviewResponse {
				t.Helper()
				out := httptest.NewRecorder()
				server.handleReview(out, httptest.NewRequest(http.MethodGet, "/api/lineuparr/dispatcharr/review"+query, nil))
				if out.Code != http.StatusOK {
					t.Fatalf("review: %d %s", out.Code, out.Body.String())
				}
				var view dispatcharrReviewResponse
				if err := json.Unmarshal(out.Body.Bytes(), &view); err != nil {
					t.Fatal(err)
				}
				return view
			}
			act := func(method, key, choice string) {
				t.Helper()
				body, _ := json.Marshal(map[string]string{"key": key, "decision": choice})
				out := httptest.NewRecorder()
				request := httptest.NewRequest(method, "/api/lineuparr/dispatcharr/decision", strings.NewReader(string(body)))
				request.Header.Set("Content-Type", "application/json")
				server.handleDecision(out, withTestReviewOperation(server, request))
				if out.Code != http.StatusOK {
					t.Fatalf("decision: %d %s", out.Code, out.Body.String())
				}
			}
			initial := read("?refresh=true&limit=1")
			if initial.CandidateCount != 2+initial.Supplemental.CandidateCount || len(initial.Candidates) != 1 || initial.Candidates[0].ReviewKind != "qualified" || initial.Candidates[0].StreamCount != 2 {
				t.Fatalf("high-score group hidden, deprioritized or lost raw constituents: %+v", initial)
			}
			high := initial.Candidates[0]
			act(http.MethodPost, high.Key, decision)
			remaining := read("")
			if remaining.CandidateCount != 1+remaining.Supplemental.CandidateCount || remaining.Candidates[0].MinimumScore != 94 {
				t.Fatalf("mixed-score review lost: %+v", remaining)
			}
			act(http.MethodPost, remaining.Candidates[0].Key, "confirmed")
			if got := read(""); got.CandidateCount != got.Supplemental.CandidateCount || len(got.Candidates) != 0 {
				t.Fatalf("resolved review: %+v", got)
			}
			draft, _, _, ok := server.lineup.buildDraft(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/lineuparr/draft", nil))
			if !ok {
				t.Fatal("draft unavailable")
			}
			channel := findDraftChannel(t, *draft, "1001")
			aliases := strings.Join(channel.Aliases, "\n")
			if !strings.Contains(aliases, "US: Fixture low") || strings.Contains(aliases, "US: Fixture boundary") || strings.Contains(aliases, "US: Fixture high") {
				t.Fatalf("per-name confirmation threshold changed: %v", channel.Aliases)
			}
			if decision == "denied" && (len(channel.ExcludedAliases) != 2 || !strings.Contains(strings.Join(channel.ExcludedAliases, "\n"), "US: Fixture boundary") || !strings.Contains(strings.Join(channel.ExcludedAliases, "\n"), "US: Fixture high")) {
				t.Fatalf("literal high-score exclusions: %v", channel.ExcludedAliases)
			}
			if decision == "confirmed" && len(channel.ExcludedAliases) != 0 {
				t.Fatalf("confirmation became exclusion: %v", channel.ExcludedAliases)
			}
			act(http.MethodDelete, high.Key, "")
			if got := read(""); got.CandidateCount != 1+got.Supplemental.CandidateCount || got.Candidates[0].StreamCount != 2 || got.Candidates[0].MinimumScore != 95 {
				t.Fatalf("Undo failed to restore reviewed high-score group: %+v", got)
			}
			if fake.streamCalls != 1 {
				t.Fatalf("decision/Undo refreshed inventory: %d", fake.streamCalls)
			}
		})
	}
}

func TestPendingFeedGroupsKeepActualThresholdBoundary(t *testing.T) {
	candidates := []dispatcharr.Candidate{}
	automatic := automaticMatchView{}
	for i, score := range []int{70, 94, 95, 100} {
		key := fmt.Sprintf("raw-%d", i)
		candidates = append(candidates, dispatcharr.Candidate{Key: key, StreamHash: key, StreamName: key, ChannelID: "station", NormalizedAlias: "same-family", Score: score, NameScore: score})
		automatic.Rows = append(automatic.Rows, automaticMatchRow{Key: key, PendingReview: true})
	}
	classifyReviewCandidates(candidates, automatic)
	groups := dispatcharr.GroupCandidates(candidates)
	if len(groups) != 2 {
		t.Fatalf("pending feed score classes merged: %+v", groups)
	}
	var high dispatcharr.CandidateGroup
	for _, group := range groups {
		if group.MinimumScore >= 95 {
			high = group
			if group.StreamCount != 2 || group.ReviewKind != "feed-qualified" {
				t.Fatal(group)
			}
		} else if group.MaximumScore != 94 || group.MinimumScore != 70 || group.StreamCount != 2 || group.ReviewKind != "feed-inclusion" {
			t.Fatal(group)
		}
	}
	for _, choice := range []string{"confirmed", "denied"} {
		decisions := map[string]dispatcharr.Decision{}
		for _, c := range candidates {
			if c.Score >= 95 {
				decisions[c.Key] = dispatcharr.Decision{ChannelID: c.ChannelID, StreamHash: c.StreamHash, StreamName: c.StreamName, ReviewKind: c.ReviewKind, Decision: choice}
			}
		}
		remaining := dispatcharr.FilterSnapshotCandidates(candidates, map[string]bool{"station": true}, decisions)
		if len(remaining) != 2 || remaining[0].Score >= 95 || high.MinimumScore != 95 {
			t.Fatalf("%s high-score group consumed below95 inclusion choices: %+v", choice, remaining)
		}
		// Undo exposes the same immutable candidates and scores; no matcher rerun.
		if got := dispatcharr.GroupCandidates(dispatcharr.FilterSnapshotCandidates(candidates, map[string]bool{"station": true}, nil)); len(got) != 2 {
			t.Fatalf("undo lost score classes: %+v", got)
		}
	}
}

func TestPendingFeedDecisionRestartAndUndoKeepScoreClasses(t *testing.T) {
	for _, choice := range []string{"confirmed", "denied"} {
		t.Run(choice, func(t *testing.T) {
			s, _ := automaticFeedFixture(t, 95, -1)
			statePath := filepath.Join(t.TempDir(), "state.json")
			state, err := builder.LoadStateStore(statePath)
			if err != nil {
				t.Fatal(err)
			}
			s.lineup.builder = builder.NewService(state, builder.ServiceOptions{})
			s.lineup.automaticProjection = func(d *builder.Draft, c appconfig.Config) {
				for i := range d.Channels {
					if d.Channels[i].CallSign == "HBOHDP" {
						proof := feedDraftChannel(d.Channels[i].ID, d.Channels[i].StationID, "HBOHDP", "HBO - West")
						d.Channels[i].AliasEvidence = proof.AliasEvidence
						d.Channels[i].ServiceEvidence = proof.ServiceEvidence
					}
				}
				s.projectAutomaticExclusions(d, c)
			}
			actual := automaticDraft(t, s)
			s.snapshot.channels, _ = dispatcharrSnapshotChannels(actual)
			s.snapshot.digest = dispatcharrInputDigest(s.snapshot.channels)
			low := s.snapshot.candidates[1]
			low.Key = "low"
			low.StreamKey = "low"
			low.StreamHash = "low"
			low.Score = 94
			low.NameScore = 94
			high := low
			high.Key = "high"
			high.StreamKey = "high"
			high.StreamHash = "high"
			high.StreamName = "TV: HBO HD"
			high.Score = 95
			high.NameScore = 95
			s.snapshot.candidates = []dispatcharr.Candidate{low, high}
			view := automaticReview(t, s)
			if len(view.Candidates) != 2 || view.Candidates[0].MinimumScore != 95 {
				t.Fatalf("feed classes merged: %+v", view.Candidates)
			}
			highKey := view.Candidates[0].Key
			act := func(method, key, decision string) {
				t.Helper()
				data, _ := json.Marshal(map[string]string{"key": key, "decision": decision})
				r := httptest.NewRequest(method, "/api/lineuparr/dispatcharr/decision", strings.NewReader(string(data)))
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				s.handleDecision(w, withTestReviewOperation(s, r))
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			act("POST", highKey, choice)
			reloaded, err := builder.LoadStateStore(statePath)
			if err != nil {
				t.Fatal(err)
			}
			s.lineup.builder = builder.NewService(reloaded, builder.ServiceOptions{})
			view = automaticReview(t, s)
			if len(view.Candidates) != 1 || view.Candidates[0].MinimumScore != 94 {
				t.Fatalf("reloaded high choice consumed low: %+v", view.Candidates)
			}
			act("POST", view.Candidates[0].Key, "confirmed")
			cfg, _, _ := s.lineup.store.Get()
			saved := s.lineup.builder.MatchDecisions(cfg.Fingerprint())
			if len(saved) != 2 {
				t.Fatal("saved class cardinality", saved)
			}
			for _, d := range saved {
				if d.StreamName == high.StreamName && d.NameScore != 95 || d.StreamName == low.StreamName && d.NameScore != 94 {
					t.Fatal("score mutated", d)
				}
			}
			act("DELETE", highKey, "")
			view = automaticReview(t, s)
			if len(view.Candidates) != 1 || view.Candidates[0].MinimumScore != 95 {
				t.Fatalf("Undo crossed score class: %+v", view.Candidates)
			}
			if len(s.lineup.builder.MatchDecisions(cfg.Fingerprint())) != 1 {
				t.Fatal("Undo removed low choice")
			}
		})
	}
}
