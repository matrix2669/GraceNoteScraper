package main

import (
	"context"
	"encoding/json"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"net/http"
	"net/http/httptest"
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
	want := []string{"inclusion", "qualified", "qualified", "feed-review"}
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
				server.handleDecision(out, request)
				if out.Code != http.StatusOK {
					t.Fatalf("decision: %d %s", out.Code, out.Body.String())
				}
			}
			initial := read("?refresh=true&limit=1")
			if initial.CandidateCount != 2 || len(initial.Candidates) != 1 || initial.Candidates[0].ReviewKind != "qualified" || initial.Candidates[0].StreamCount != 2 {
				t.Fatalf("high-score group hidden, deprioritized or lost raw constituents: %+v", initial)
			}
			high := initial.Candidates[0]
			act(http.MethodPost, high.Key, decision)
			remaining := read("")
			if remaining.CandidateCount != 1 || remaining.Candidates[0].MinimumScore != 94 {
				t.Fatalf("mixed-score review lost: %+v", remaining)
			}
			act(http.MethodPost, remaining.Candidates[0].Key, "confirmed")
			if got := read(""); got.CandidateCount != 0 {
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
			if got := read(""); got.CandidateCount != 1 || got.Candidates[0].StreamCount != 2 || got.Candidates[0].MinimumScore != 95 {
				t.Fatalf("Undo failed to restore reviewed high-score group: %+v", got)
			}
			if fake.streamCalls != 1 {
				t.Fatalf("decision/Undo refreshed inventory: %d", fake.streamCalls)
			}
		})
	}
}
