package main

import (
	"encoding/json"
	"fmt"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuilderResetEndpointConfirmationGenerationAndBusyFence(t *testing.T) {
	server, _ := newDispatcharrTestServer(t, true)
	s := server.lineup
	config, _, _ := s.store.Get()
	token := s.builder.ReviewStateToken()
	request := func(confirm bool) *httptest.ResponseRecorder {
		data, _ := json.Marshal(map[string]any{"sourceFingerprint": config.Fingerprint(), "token": token, "confirm": confirm})
		r := httptest.NewRequest("POST", "/api/lineuparr/reset", strings.NewReader(string(data)))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handleBuilderReset(w, r)
		return w
	}
	if w := request(false); w.Code != 409 || token != s.builder.ReviewStateToken() {
		t.Fatalf("cancel changed state: %d", w.Code)
	}
	s.resetMu.RLock()
	if w := request(true); w.Code != 409 {
		t.Fatalf("reset raced active operation: %d", w.Code)
	}
	s.resetMu.RUnlock()
	cleared := 0
	s.afterBuilderReset = func() { cleared++ }
	if w := request(true); w.Code != 200 {
		t.Fatalf("reset: %d %s", w.Code, w.Body.String())
	}
	if cleared != 1 || s.builder.ResetGeneration() != 1 {
		t.Fatal("snapshot or durable reset fence not invalidated")
	}
	called := false
	guard := s.guardBuilder(func(w http.ResponseWriter, r *http.Request) { called = true })
	old := httptest.NewRequest("POST", "/api/lineuparr/channel", nil)
	w := httptest.NewRecorder()
	guard(w, old)
	if w.Code != 409 || called {
		t.Fatal("old pending client recreated choices")
	}
	old.Header.Set("X-Builder-Generation", "1")
	w = httptest.NewRecorder()
	guard(w, old)
	if !called {
		t.Fatal("new generation rejected")
	}
}

func TestResetGenerationFencesQueuedRefreshGET(t *testing.T) {
	server, _ := newDispatcharrTestServer(t, true)
	s := server.lineup
	config, _, _ := s.store.Get()
	if _, err := s.builder.ResetBuilder(config.Fingerprint(), s.builder.ReviewStateToken(), "", false); err != nil {
		t.Fatal(err)
	}
	calls := 0
	guard := s.guardBuilder(func(w http.ResponseWriter, r *http.Request) { calls++ })
	for _, tc := range []struct {
		path, generation string
		want             int
	}{
		{"/api/lineuparr/dispatcharr/review?refresh=true", "", 409},
		{"/api/lineuparr/dispatcharr/review?refresh=true", "0", 409},
		{"/api/lineuparr/dispatcharr/review?refresh=TRUE", "0", 409},
		{"/api/lineuparr/dispatcharr/review?refresh=true", "1", 200},
		{"/api/lineuparr/dispatcharr/review", "", 200},
	} {
		req := httptest.NewRequest("GET", tc.path, nil)
		req.Header.Set("X-Builder-Generation", tc.generation)
		w := httptest.NewRecorder()
		guard(w, req)
		if w.Code != tc.want {
			t.Fatalf("%s generation %s: %d", tc.path, tc.generation, w.Code)
		}
	}
	if calls != 2 {
		t.Fatalf("stale requests executed: %d", calls)
	}
}

func TestResetAndRestorePublicationChoices(t *testing.T) {
	for _, withdrawReset := range []bool{false, true} {
		for _, withdrawRestore := range []bool{false, true} {
			t.Run(fmt.Sprintf("reset-withdraw-%v/restore-withdraw-%v", withdrawReset, withdrawRestore), func(t *testing.T) {
				s := newLineuparrTestServer(t, true)
				s.exportDir = t.TempDir()
				path := publishLineuparrForTest(t, s)
				recordPath := filepath.Join(s.exportDir, strings.TrimPrefix(path, lineuparrPublishedPrefix))
				before, err := os.ReadFile(recordPath)
				if err != nil {
					t.Fatal(err)
				}
				config, _, _ := s.store.Get()
				include := true
				if err := s.builder.UpdateChannel(config.Fingerprint(), "fixture", builder.ChannelUpdate{Included: &include}); err != nil {
					t.Fatal(err)
				}
				backup, err := s.builder.ResetBuilder(config.Fingerprint(), s.builder.ReviewStateToken(), "", withdrawReset)
				if err != nil {
					t.Fatal(err)
				}
				check := func(withdrawn bool) {
					t.Helper()
					want := 200
					if withdrawn {
						want = 409
					}
					for _, method := range []string{"GET", "HEAD"} {
						if got := readLineuparrExport(s, method, path); got.Code != want {
							t.Fatalf("%s: got %d want %d", method, got.Code, want)
						}
					}
					after, err := os.ReadFile(recordPath)
					if err != nil || string(after) != string(before) {
						t.Fatal("reset/restore rewrote retained published JSON")
					}
				}
				check(withdrawReset)
				if _, err = s.builder.ResetBuilder(config.Fingerprint(), s.builder.ReviewStateToken(), backup, withdrawRestore); err != nil {
					t.Fatal(err)
				}
				check(withdrawReset || withdrawRestore)
				// Keep after withdrawal never resurrects a file, including after restart.
				if _, err = s.builder.ResetBuilder(config.Fingerprint(), s.builder.ReviewStateToken(), "", false); err != nil {
					t.Fatal(err)
				}
				check(withdrawReset || withdrawRestore)
				if next := publishLineuparrForTest(t, s); next != path {
					t.Fatal("regeneration changed URL")
				}
				if got := readLineuparrExport(s, "GET", path); got.Code != 200 {
					t.Fatal("fresh generation rejected")
				}
			})
		}
	}
}

func TestRecoverySummaryAvailableDuringConcurrentStartupReads(t *testing.T) {
	server, _ := newDispatcharrTestServer(t, true)
	s := server.lineup
	s.resetMu.RLock()
	defer s.resetMu.RUnlock()
	w := httptest.NewRecorder()
	s.handleBuilderReset(w, httptest.NewRequest("GET", "/api/lineuparr/reset", nil))
	if w.Code != 200 {
		t.Fatalf("read-only startup operation hid recovery: %d %s", w.Code, w.Body.String())
	}
}
