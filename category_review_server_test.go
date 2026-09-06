package main

import (
	"net/http/httptest"
	"strings"
	"testing"
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
