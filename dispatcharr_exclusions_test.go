package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	lineuparrbuilder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

func TestGroupedDenialRetainsEveryNameAcrossRestartAndGroupUndo(t *testing.T) {
	server, fake := newDispatcharrTestServer(t, true)
	path := filepath.Join(t.TempDir(), "decisions.json")
	restart := func() {
		t.Helper()
		store, err := lineuparrbuilder.LoadStateStore(path)
		if err != nil {
			t.Fatal(err)
		}
		server.lineup.builder = lineuparrbuilder.NewService(store, lineuparrbuilder.ServiceOptions{})
		server.clearCandidateCache()
		server.cache.clear()
	}
	restart()
	fake.streams = []dispatcharr.Stream{
		{ID: 10, Name: "US| TWO HD", TVGID: "Two.us", M3UAccountID: 3},
		{ID: 11, Name: "US: TWO", TVGID: "Two.us", M3UAccountID: 7},
		{ID: 12, Name: "TWO", TVGID: "Two.us", M3UAccountID: 11},
		{ID: 13, Name: " us|  two HD ", TVGID: "Two.us", M3UAccountID: 12},
	}
	review := func() dispatcharrReviewResponse {
		t.Helper()
		w := httptest.NewRecorder()
		server.handleReview(w, httptest.NewRequest(http.MethodGet, "/api/lineuparr/dispatcharr/review", nil))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var result dispatcharrReviewResponse
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	initial := review()
	if len(initial.Candidates) != 2 || initial.CandidateStreamCount != 8 {
		t.Fatalf("initial review: %+v", initial)
	}
	key := initial.Candidates[0].Key
	decide := func(method string) {
		t.Helper()
		r := httptest.NewRequest(method, "/api/lineuparr/dispatcharr/decision", strings.NewReader(`{"key":"`+key+`","decision":"denied"}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.handleDecision(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	decide(http.MethodPost)
	restart()
	config, _, _ := server.lineup.store.Get()
	stored := server.lineup.builder.MatchDecisions(config.Fingerprint())
	if len(stored) != 4 {
		t.Fatalf("group lost names: %+v", stored)
	}
	for _, stream := range fake.streams {
		found := false
		for _, decision := range stored {
			if decision.StreamID == stream.ID {
				found = true
				if decision.StreamName != strings.Join(strings.Fields(stream.Name), " ") || decision.NameScore < 95 {
					t.Fatalf("changed constituent: %+v", decision)
				}
			}
		}
		if !found {
			t.Fatalf("missing constituent %d", stream.ID)
		}
	}
	readDraft := func() lineuparrbuilder.Draft {
		t.Helper()
		w := httptest.NewRecorder()
		server.lineup.handleDraft(w, httptest.NewRequest(http.MethodGet, "/api/lineuparr/draft", nil))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var d lineuparrbuilder.Draft
		if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	draft := readDraft()
	var names []string
	for _, channel := range draft.Channels {
		names = append(names, channel.ExcludedAliases...)
	}
	if len(names) != 3 {
		t.Fatalf("want three distinct full names, got %q", names)
	}
	for _, want := range []string{"US| TWO HD", "US: TWO", "TWO"} {
		found := false
		for _, name := range names {
			found = found || strings.EqualFold(name, want)
		}
		if !found {
			t.Fatalf("missing %q in %q", want, names)
		}
	}
	if got := review(); got.DeniedCount != 1 {
		t.Fatalf("review grouping changed: %+v", got)
	}
	decide(http.MethodDelete)
	restart()
	if len(server.lineup.builder.MatchDecisions(config.Fingerprint())) != 0 {
		t.Fatal("group undo retained decisions")
	}
	for _, channel := range readDraft().Channels {
		if len(channel.ExcludedAliases) > 0 {
			t.Fatal("group undo retained exclusions")
		}
	}
	if got := review(); got.CandidateStreamCount != 8 {
		t.Fatalf("undo did not restore all variants: %+v", got)
	}
}
