package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

type lifecycleFakeAPI struct {
	*fakeDispatcharrAPI
	inventory *dispatcharr.Client
	captures  int
	hook      func()
}

func TestLifecycleDraftFailureInvalidationAndRetry(t *testing.T) {
	for _, failOn := range []int{1, 2} {
		t.Run(fmt.Sprintf("draft-%d", failOn), func(t *testing.T) {
			s, f := newDispatcharrTestServer(t, true)
			s.client = &lifecycleFakeAPI{fakeDispatcharrAPI: f, inventory: lifecycleTestClient()}
			w := httptest.NewRecorder()
			draft, cfg, _, ok := s.lineup.buildDraft(w, httptest.NewRequest("GET", "/draft", nil))
			if !ok {
				t.Fatal(w.Code, w.Body.String())
			}
			conn, _ := s.config.Get()
			d := builder.MatchDecision{Key: "saved", Decision: "confirmed", DispatcharrFingerprint: conn.Fingerprint(), StreamFingerprint: strings.Repeat("a", 64), StreamKey: "3:99", StreamID: 99, M3UAccountID: 3, ChannelID: draft.Channels[0].ID, StreamName: "missing", Score: 100, NameScore: 100}
			if err := s.lineup.builder.SetMatchDecision(cfg.Fingerprint(), d); err != nil {
				t.Fatal(err)
			}
			w = httptest.NewRecorder()
			if _, ok := s.buildReview(w, httptest.NewRequest("GET", "/review", nil), true); !ok {
				t.Fatal(w.Code, w.Body.String())
			}
			calls := 0
			s.lineup.automaticProjection = func(draft *builder.Draft, c appconfig.Config) {
				draft.AutomaticLease = &builder.AutomaticMatchLease{}
				calls++
				if calls == failOn {
					guideCopy := *s.lineup.state.Get()
					s.lineup.state.UpdateForSource(&guideCopy, c.Fingerprint())
				}
			}
			w = httptest.NewRecorder()
			if _, ok := s.buildReview(w, httptest.NewRequest("GET", "/review", nil), true); ok {
				t.Fatal("draft failure unexpectedly succeeded")
			}
			if !s.lifecycleIsUnsafe() || len(s.lineup.builder.MatchDecisions(cfg.Fingerprint())) != 1 {
				t.Fatal("failed fenced invalidation lost choices or failed to mark unsafe")
			}
			s.lineup.automaticProjection = nil
			w = httptest.NewRecorder()
			if _, ok := s.buildReview(w, httptest.NewRequest("GET", "/review", nil), true); !ok {
				t.Fatal(w.Code, w.Body.String())
			}
			if s.lifecycleIsUnsafe() || len(s.lineup.builder.MatchDecisions(cfg.Fingerprint())) != 1 {
				t.Fatal("retry did not safely reset continuity")
			}
		})
	}
}

func (f *lifecycleFakeAPI) CaptureLifecycleInventory(ctx context.Context, cfg dispatcharr.Config, refs []dispatcharr.LifecycleReference) (dispatcharr.LifecycleInventoryReceipt, error) {
	f.captures++
	if f.hook != nil {
		f.hook()
	}
	return f.inventory.CaptureLifecycleInventory(ctx, cfg, refs)
}
func lifecycleTestClient() *dispatcharr.Client {
	return dispatcharr.NewClient(&http.Client{Transport: impactSourceTransport(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.URL.Path {
		case "/api/accounts/token/":
			body = `{"access":"x","refresh":"y"}`
		case "/api/core/version/":
			body = `{"version":"0.33.0"}`
		case "/api/accounts/users/me/":
			body = `{"id":1,"user_level":10}`
		case "/api/m3u/accounts/":
			body = `[{"id":3,"is_active":true,"status":"success"}]`
		case "/api/channels/streams/":
			body = `{"count":1,"next":null,"results":[{"id":10,"m3u_account":3,"name":"US| TWO HD"}]}`
		case "/api/channels/streams/by-ids/":
			body = `[]`
		default:
			body = `{}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
}
func TestLifecycleRunsOnlyAfterSuccessfulExplicitRefresh(t *testing.T) {
	s, f := newDispatcharrTestServer(t, true)
	wrapped := &lifecycleFakeAPI{fakeDispatcharrAPI: f, inventory: lifecycleTestClient()}
	s.client = wrapped
	w := httptest.NewRecorder()
	first, ok := s.buildReview(w, httptest.NewRequest("GET", "/review", nil), true)
	if !ok {
		t.Fatal(w.Code, w.Body.String())
	}
	if wrapped.captures != 1 || first.response.Lifecycle.Status == "not-checked" {
		t.Fatalf("capture not integrated %+v", first.response.Lifecycle)
	}
	w = httptest.NewRecorder()
	_, ok = s.buildReview(w, httptest.NewRequest("GET", "/review", nil), false)
	if !ok || wrapped.captures != 1 {
		t.Fatalf("ordinary read acquired lifecycle %d %s", w.Code, w.Body.String())
	}
	f.streamErr = io.ErrUnexpectedEOF
	cfg, _, _ := s.lineup.store.Get()
	before := len(s.lineup.builder.MatchDecisions(cfg.Fingerprint()))
	w = httptest.NewRecorder()
	later, ok := s.buildReview(w, httptest.NewRequest("GET", "/review", nil), true)
	if !ok || wrapped.captures != 1 || later.response.Lifecycle.Status != "deferred" || before != len(s.lineup.builder.MatchDecisions(cfg.Fingerprint())) {
		t.Fatalf("failed refresh changed choices %d %+v", w.Code, later.response.Lifecycle)
	}
}
func TestLifecyclePrimaryProjectionMustMatch(t *testing.T) {
	streams := []dispatcharr.Stream{{ID: 1, M3UAccountID: 3, Name: "FX"}}
	for _, rows := range [][]dispatcharr.LifecycleRow{{{ID: 1, AccountID: 3, Name: " FX "}}, {{ID: 1, AccountID: 4, Name: "FX"}}, {{ID: 1, AccountID: 3, Name: "FX HD"}}, nil} {
		got := lifecyclePrimaryCovered(streams, rows)
		want := len(rows) > 0 && rows[0].AccountID == 3 && strings.TrimSpace(rows[0].Name) == "FX"
		if got != want {
			t.Fatalf("coverage %v %v", rows, got)
		}
	}
}

func TestFailedRefreshInvalidatesPendingWithoutDeletingChoices(t *testing.T) {
	s, f := newDispatcharrTestServer(t, true)
	wrapped := &lifecycleFakeAPI{fakeDispatcharrAPI: f, inventory: lifecycleTestClient()}
	s.client = wrapped
	w := httptest.NewRecorder()
	if _, ok := s.buildReview(w, httptest.NewRequest("GET", "/review", nil), true); !ok {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	draft, cfg, _, ok := s.lineup.buildDraft(w, httptest.NewRequest("GET", "/draft", nil))
	if !ok {
		t.Fatal(w.Code, w.Body.String())
	}
	conn, _ := s.config.Get()
	var target string
	for _, c := range draft.Channels {
		if c.Included && c.StationID != "" {
			target = c.ID
			break
		}
	}
	if target == "" {
		t.Fatal("missing target")
	}
	d := builder.MatchDecision{Key: "saved", Decision: "confirmed", DispatcharrFingerprint: conn.Fingerprint(), StreamFingerprint: strings.Repeat("a", 64), StreamKey: "3:99", StreamID: 99, M3UAccountID: 3, ChannelID: target, StreamName: "missing", Score: 100, NameScore: 100}
	if err := s.lineup.builder.SetMatchDecision(cfg.Fingerprint(), d); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	result, ok := s.buildReview(w, httptest.NewRequest("GET", "/review", nil), true)
	if !ok || result.response.Lifecycle.Deferred < 1 {
		t.Fatalf("pending not seeded %d %+v %s", w.Code, result.response.Lifecycle, w.Body.String())
	}
	before := s.lineup.builder.ReviewStateToken()
	f.streamErr = io.ErrUnexpectedEOF
	w = httptest.NewRecorder()
	result, ok = s.buildReview(w, httptest.NewRequest("GET", "/review", nil), true)
	if !ok || result.response.Lifecycle.Status != "deferred" || s.lineup.builder.ReviewStateToken() == before || len(s.lineup.builder.MatchDecisions(cfg.Fingerprint())) != 1 {
		t.Fatalf("failed refresh did not safely invalidate pending %d %+v %s", w.Code, result.response.Lifecycle, w.Body.String())
	}
}

func TestLifecycleConcurrentChoiceInvalidatesCapture(t *testing.T) {
	s, f := newDispatcharrTestServer(t, true)
	entered := make(chan struct{})
	release := make(chan struct{})
	wrapped := &lifecycleFakeAPI{fakeDispatcharrAPI: f, inventory: lifecycleTestClient(), hook: func() { close(entered); <-release }}
	s.client = wrapped
	w := httptest.NewRecorder()
	done := make(chan bool, 1)
	go func() { _, ok := s.buildReview(w, httptest.NewRequest("GET", "/review", nil), true); done <- ok }()
	<-entered
	cfg, _, _ := s.lineup.store.Get()
	conn, _ := s.config.Get()
	d := builder.MatchDecision{Key: "concurrent", Decision: "confirmed", DispatcharrFingerprint: conn.Fingerprint(), StreamFingerprint: strings.Repeat("a", 64), StreamKey: "3:99", StreamID: 99, M3UAccountID: 3, ChannelID: "some-target", StreamName: "manual", Score: 100, NameScore: 100}
	if err := s.lineup.builder.SetMatchDecision(cfg.Fingerprint(), d); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-done
	if len(s.lineup.builder.MatchDecisions(cfg.Fingerprint())) != 1 {
		t.Fatal("concurrent choice was lost")
	}
}

func TestLifecycleRetiresOldOperationReceipt(t *testing.T) {
	s, _ := newDispatcharrTestServer(t, true)
	s.operations = map[string]reviewOperation{"token": {Key: "group", State: "saved", Members: []string{"keep", "retired"}}}
	s.retireLifecycleOperations([]string{"retired"})
	if s.operations["token"].State != "retired" {
		t.Fatal("old save operation can be retried")
	}
}
