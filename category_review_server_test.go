package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	lineuparrbuilder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

func TestCategoryApprovalRejectsInvalidRequests(t *testing.T) {
	s := newLineuparrTestServer(t, true)
	for _, test := range []struct {
		method, body string
		status       int
	}{{"GET", "", 405}, {"POST", `{"channels":[]}`, 400}, {"POST", `{"sourceFingerprint":"old-provider","channels":[{"id":"unknown","category":"Movies"}]}`, 409}} {
		r := httptest.NewRequest(test.method, "/api/lineuparr/approve-categories", strings.NewReader(test.body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handleApproveCategories(w, r)
		if w.Code != test.status {
			t.Fatalf("%s: %d %s", test.body, w.Code, w.Body.String())
		}
	}
}

func TestCategoryApprovalAcceptsCurrentSelectionsAndRetainsProposals(t *testing.T) {
	s := newLineuparrTestServer(t, true)
	config, _, _ := s.store.Get()
	if err := s.builder.SaveTMDBCategoryScan(config.Fingerprint(), lineuparrbuilder.TMDBCategoryScan{
		Revision:  "test",
		ScannedAt: time.Now().UTC(),
		Categories: map[string]lineuparrbuilder.AttributedCategory{
			"100": {Priority: 4, Value: "Movies", Source: "tmdb-schedule", Label: "TMDB test", Method: "test evidence"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/lineuparr/approve-categories", strings.NewReader(`{"sourceFingerprint":"`+config.Fingerprint()+`","channels":[{"id":"1001","category":"Entertainment"},{"id":"1002","category":"Movies"}]}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	s.handleApproveCategories(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("approval response = %d %s", recorder.Code, recorder.Body.String())
	}

	draftRecorder := httptest.NewRecorder()
	s.handleDraft(draftRecorder, httptest.NewRequest(http.MethodGet, "/api/lineuparr/draft", nil))
	if draftRecorder.Code != http.StatusOK {
		t.Fatalf("draft response = %d %s", draftRecorder.Code, draftRecorder.Body.String())
	}
	var draft lineuparrbuilder.Draft
	if err := json.Unmarshal(draftRecorder.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	if len(draft.Channels) != 2 {
		t.Fatalf("draft channels = %+v", draft.Channels)
	}
	want := map[string]string{"1001": "Entertainment", "1002": "Movies"}
	for _, channel := range draft.Channels {
		if channel.Category != want[channel.ID] || channel.NeedsCategoryReview || channel.CategoryReview == nil || channel.CategoryReview.Proposed != "Movies" || channel.CategoryReview.Chosen != want[channel.ID] {
			t.Fatalf("reviewed channel = %+v", channel)
		}
	}
}
