package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/providersource"
)

func TestPastedAddressComponents(t *testing.T) {
	for _, text := range []string{
		"1 NE Example St, Sample City, FL 33308",
		"1 NE Example St, Sample City, Florida, 33308",
		"1 NE Example St, Sample City, FL 33308, USA",
	} {
		a, err := parsePastedProviderAddress(text, "33308", "us")
		if err != nil || a.StreetAddress != "1 NE Example St" || a.City != "Sample City" || a.State != "FL" || a.PostalCode != "33308" {
			t.Fatalf("%+v %v", a, err)
		}
	}
	a, err := parsePastedProviderAddress("1 NORTHEAST DR, Apt 2, Sample City, TX 76306-1234", "76306", "us")
	if err != nil || a.StreetAddress != "1 NORTHEAST DR, Apt 2" {
		t.Fatalf("street was rewritten: %+v %v", a, err)
	}
	for _, text := range []string{"https://maps.app.goo.gl/example", "Example Building", "1 Main St, FL 33308", "1 Main St, Sample City, ZZ 33308", "1 Main St, Sample City, FL 99999", "1 Main St, , FL 33308"} {
		if _, err := parsePastedProviderAddress(text, "33308", "us"); err == nil {
			t.Fatalf("accepted malformed address %q", text)
		}
	}
}

type manualAddressTester struct {
	change   func()
	verified bool
	calls    int
}

func (f *manualAddressTester) TestAddress(_ context.Context, request lineupindex.ProviderEvidenceRequest) providersource.AddressCheck {
	f.calls++
	if f.change != nil {
		f.change()
	}
	return providersource.AddressCheck{Provider: request.Provider.Name, Verified: f.verified, Channels: 4}
}

func TestManualSaveChecksPersistenceFailureAndStaleProvider(t *testing.T) {
	s := newLineuparrTestServer(t, true)
	c, _, _ := s.store.Get()
	c.Gracenote.ProviderName = "Xfinity"
	if err := s.store.Save(c); err != nil {
		t.Fatal(err)
	}
	f := &manualAddressTester{verified: true}
	s.addressTester = f
	post := func() *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"fingerprint": c.Fingerprint(), "addressText": "1 NE Example St, Test City, NY 11743"})
		r := httptest.NewRequest("POST", "/config", strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handleProviderAddressConfig(w, r)
		return w
	}
	w := post()
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	raw, err := s.store.Address(c.Fingerprint())
	var saved savedProviderAddress
	if err != nil || json.Unmarshal(raw, &saved) != nil || len(saved.Checks) != 1 || !saved.Checks[0].Verified || saved.TestedAt == "" {
		t.Fatal("verification not persisted")
	}
	if w := post(); w.Code != 429 || f.calls != 1 {
		t.Fatal("test rate limit missing", w.Code)
	}
	s.nextAddressTest = time.Time{}
	f.verified = false
	w = post()
	if w.Code != 200 {
		t.Fatal("failed lookup must preserve editable address", w.Body.String())
	}
	raw, _ = s.store.Address(c.Fingerprint())
	json.Unmarshal(raw, &saved)
	if saved.Checks[0].Verified {
		t.Fatal("failed lookup marked verified")
	}
	s.nextAddressTest = time.Time{}
	f.change = func() {
		updated := c
		updated.Gracenote.ProviderName = "DISH"
		updated.Gracenote.LineupID = "other"
		if err := s.store.Save(updated); err != nil {
			t.Fatal(err)
		}
	}
	if w := post(); w.Code != 409 {
		t.Fatal("stale provider test was saved", w.Code)
	}
	current, _, _ := s.store.Get()
	if raw, _ := s.store.Address(current.Fingerprint()); len(raw) != 0 {
		t.Fatal("old address leaked to new provider")
	}
}

func TestAddressHelpImage(t *testing.T) {
	s := newLineuparrTestServer(t, true)
	w := httptest.NewRecorder()
	s.handleAddressHelpImage(w, httptest.NewRequest("GET", "/lineuparr/address-help.png", nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || !strings.HasPrefix(w.Body.String(), "\x89PNG") {
		t.Fatal("missing embedded image")
	}
}
