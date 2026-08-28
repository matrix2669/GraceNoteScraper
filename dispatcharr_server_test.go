package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	lineuparrbuilder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

type fakeDispatcharrAPI struct {
	tested      dispatcharr.Config
	streams     []dispatcharr.Stream
	streamErr   error
	testErr     error
	streamCalls int
	reset       int
}

func (f *fakeDispatcharrAPI) Test(_ context.Context, config dispatcharr.Config) error {
	f.tested = config
	return f.testErr
}

func (f *fakeDispatcharrAPI) Streams(_ context.Context, _ dispatcharr.Config) ([]dispatcharr.Stream, error) {
	f.streamCalls++
	if f.streamErr != nil {
		return nil, f.streamErr
	}
	return append([]dispatcharr.Stream(nil), f.streams...), nil
}

func TestDispatcharrStreamCacheRetainsEmptySuccessfulResult(t *testing.T) {
	config := dispatcharr.Config{BaseURL: "https://dispatcharr.test", Username: "admin", Password: "secret"}
	fake := &fakeDispatcharrAPI{}
	var cache dispatcharrStreamCache
	streams, fetchedAt, cached, warning, err := cache.get(t.Context(), fake, config, false)
	if err != nil || cached || warning != "" || len(streams) != 0 || fetchedAt.IsZero() {
		t.Fatalf("initial empty load = streams:%+v fetched:%v cached:%v warning:%q err:%v", streams, fetchedAt, cached, warning, err)
	}
	streams, cachedAt, cached, warning, err := cache.get(t.Context(), fake, config, false)
	if err != nil || !cached || warning != "" || len(streams) != 0 || !cachedAt.Equal(fetchedAt) || fake.streamCalls != 1 {
		t.Fatalf("cached empty load = streams:%+v fetched:%v cached:%v warning:%q calls:%d err:%v", streams, cachedAt, cached, warning, fake.streamCalls, err)
	}
	fake.streamErr = errors.New("offline")
	streams, staleAt, cached, warning, err := cache.get(t.Context(), fake, config, true)
	if err != nil || !cached || warning == "" || len(streams) != 0 || !staleAt.Equal(fetchedAt) {
		t.Fatalf("stale empty fallback = streams:%+v fetched:%v cached:%v warning:%q err:%v", streams, staleAt, cached, warning, err)
	}
}

func (f *fakeDispatcharrAPI) Reset() {
	f.reset++
}

func TestDispatcharrStreamCacheReportsStaleFallback(t *testing.T) {
	config := dispatcharr.Config{BaseURL: "https://dispatcharr.test", Username: "admin", Password: "secret"}
	fake := &fakeDispatcharrAPI{streams: []dispatcharr.Stream{{ID: 1, Name: "CNN", M3UAccountID: 3}}}
	var cache dispatcharrStreamCache
	streams, fetchedAt, cached, warning, err := cache.get(t.Context(), fake, config, false)
	if err != nil || cached || warning != "" || len(streams) != 1 || fetchedAt.IsZero() {
		t.Fatalf("initial cache load = streams:%+v fetched:%v cached:%v warning:%q err:%v", streams, fetchedAt, cached, warning, err)
	}
	fake.streamErr = errors.New("offline")
	streams, staleAt, cached, warning, err := cache.get(t.Context(), fake, config, true)
	if err != nil || !cached || warning == "" || len(streams) != 1 || !staleAt.Equal(fetchedAt) {
		t.Fatalf("stale fallback = streams:%+v fetched:%v cached:%v warning:%q err:%v", streams, staleAt, cached, warning, err)
	}
}

func newDispatcharrTestServer(t *testing.T, configured bool) (*dispatcharrServer, *fakeDispatcharrAPI) {
	t.Helper()
	lineup := newLineuparrTestServer(t, true)
	config, err := dispatcharr.LoadConfigStore(filepath.Join(t.TempDir(), "dispatcharr.json"))
	if err != nil {
		t.Fatal(err)
	}
	if configured {
		if err := config.Save(dispatcharr.Config{
			BaseURL: "https://dispatcharr.test", Username: "admin", Password: "secret",
		}); err != nil {
			t.Fatal(err)
		}
	}
	fake := &fakeDispatcharrAPI{streams: []dispatcharr.Stream{
		{ID: 10, Name: "US| TWO HD", TVGID: "Two.us", M3UAccountID: 3},
	}}
	return &dispatcharrServer{lineup: lineup, config: config, client: fake}, fake
}

func TestDispatcharrConfigNeverReturnsPassword(t *testing.T) {
	server, fake := newDispatcharrTestServer(t, false)
	payload := `{"baseUrl":"https://dispatcharr.test","username":"admin","password":"secret"}`
	request := httptest.NewRequest(http.MethodPost, "/api/lineuparr/dispatcharr/config", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.handleConfig(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("save config response = %d body %s", recorder.Code, recorder.Body.String())
	}
	if fake.tested.Password != "secret" || fake.reset != 1 {
		t.Fatalf("tested config/reset = %+v / %d", fake.tested, fake.reset)
	}
	if strings.Contains(recorder.Body.String(), "secret") || strings.Contains(recorder.Body.String(), "password") {
		t.Fatalf("password leaked in save response: %s", recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/lineuparr/dispatcharr/config", nil)
	recorder = httptest.NewRecorder()
	server.handleConfig(recorder, request)
	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), "secret") {
		t.Fatalf("config status response = %d body %s", recorder.Code, recorder.Body.String())
	}
}

func TestDispatcharrReviewConfirmAndClearUpdatesAliases(t *testing.T) {
	server, _ := newDispatcharrTestServer(t, true)
	request := httptest.NewRequest(http.MethodGet, "/api/lineuparr/dispatcharr/review", nil)
	recorder := httptest.NewRecorder()
	server.handleReview(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("review response = %d body %s", recorder.Code, recorder.Body.String())
	}
	var review dispatcharrReviewResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	if review.StreamCount != 1 || review.CandidateCount != 1 || len(review.Candidates) != 1 {
		t.Fatalf("review = %+v", review)
	}
	if strings.Contains(recorder.Body.String(), "streamFingerprint") || strings.Contains(recorder.Body.String(), "dispatcharrFingerprint") {
		t.Fatalf("internal fingerprints leaked: %s", recorder.Body.String())
	}
	key := review.Candidates[0].Key

	payload := `{"key":"` + key + `","decision":"confirmed"}`
	request = httptest.NewRequest(http.MethodPost, "/api/lineuparr/dispatcharr/decision", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	server.handleDecision(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("confirm response = %d body %s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/lineuparr/draft", nil)
	recorder = httptest.NewRecorder()
	server.lineup.handleDraft(recorder, request)
	var draft lineuparrbuilder.Draft
	if err := json.Unmarshal(recorder.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	channel := draft.Channels[0]
	if !containsString(channel.Aliases, "US| TWO HD") || !containsString(channel.EPGIDs, "Two.us") {
		t.Fatalf("confirmed draft channel = %+v", channel)
	}

	request = httptest.NewRequest(http.MethodDelete, "/api/lineuparr/dispatcharr/decision", strings.NewReader(`{"key":"`+key+`"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	server.handleDecision(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("clear response = %d body %s", recorder.Code, recorder.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/lineuparr/draft", nil)
	recorder = httptest.NewRecorder()
	server.lineup.handleDraft(recorder, request)
	if err := json.Unmarshal(recorder.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	if containsString(draft.Channels[0].Aliases, "US| TWO HD") || containsString(draft.Channels[0].EPGIDs, "Two.us") {
		t.Fatalf("cleared decision still applied: %+v", draft.Channels[0])
	}
}

func TestDispatcharrDenyPersistsNegativeDecision(t *testing.T) {
	server, _ := newDispatcharrTestServer(t, true)
	request := httptest.NewRequest(http.MethodGet, "/api/lineuparr/dispatcharr/review", nil)
	recorder := httptest.NewRecorder()
	server.handleReview(recorder, request)
	var review dispatcharrReviewResponse
	_ = json.Unmarshal(recorder.Body.Bytes(), &review)
	key := review.Candidates[0].Key

	request = httptest.NewRequest(http.MethodPost, "/api/lineuparr/dispatcharr/decision", strings.NewReader(`{"key":"`+key+`","decision":"denied"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	server.handleDecision(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("deny response = %d body %s", recorder.Code, recorder.Body.String())
	}
	config, _, _ := server.lineup.store.Get()
	decision := server.lineup.builder.MatchDecisions(config.Fingerprint())[key]
	if decision.Decision != "denied" {
		t.Fatalf("stored denial = %+v", decision)
	}
}
