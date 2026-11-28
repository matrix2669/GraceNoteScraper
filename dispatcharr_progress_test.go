package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
)

func TestProgressEndpointObservesWithoutStartingWork(t *testing.T) {
	s, api := newDispatcharrTestServer(t, true)
	read := func() dispatcharrMatchProgress {
		w := httptest.NewRecorder()
		s.handleProgress(w, httptest.NewRequest("GET", "/api/lineuparr/dispatcharr/progress", nil))
		var p dispatcharrMatchProgress
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w)
		}
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	read()
	if api.streamCalls != 0 {
		t.Fatal("progress fetched streams")
	}
	matcher := s.matcher
	s.matcher = func(ctx context.Context, source, country string, channels []dispatcharr.MatchChannel, streams []dispatcharr.Stream, decisions map[string]dispatcharr.Decision) (dispatcharr.CandidateSet, string, error) {
		if p := read(); p.Stage != "matching" || p.Total != len(channels) {
			t.Fatal(p)
		}
		return matcher(ctx, source, country, channels, streams, decisions)
	}
	snapshotReview(t, s, true)
	if p := read(); p.Stage != "idle" {
		t.Fatal(p)
	}
	if api.streamCalls != 1 {
		t.Fatal(api.streamCalls)
	}
}
