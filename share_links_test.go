package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInternalBaseValidation(t *testing.T) {
	for _, raw := range []string{"", "http://gracenote-dev:8080", "https://guide.example", "http://[::1]:8080"} {
		got, err := normalizeInternalBase(" " + raw + " ")
		if err != nil || got != raw {
			t.Fatalf("%q: %q %v", raw, got, err)
		}
	}
	if got, err := normalizeInternalBase("http://gracenote-dev:8080/"); err != nil || got != "http://gracenote-dev:8080" {
		t.Fatal(got, err)
	}
	for _, raw := range []string{"gracenote-dev:8080", "ftp://host", "http://user:pass@host", "http://host/path", "http://host/?x=1", "http://host/?", "http://host/#part", "http://host:0", "http://host:65536", "http://host:bad", "http://", "//host", "http://host/\n"} {
		// Surrounding whitespace is intentionally trimmed; embedded newline is invalid.
		if raw == "http://host/\n" {
			raw = "http://ho\nst/"
		}
		if _, err := normalizeInternalBase(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestShareLinksPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json.links.json")
	s := &shareLinksServer{path: path}
	call := func(method, body, ct string, status int) shareLinksConfig {
		t.Helper()
		r := httptest.NewRequest(method, "/api/setup/share-links", strings.NewReader(body))
		r.Header.Set("Content-Type", ct)
		w := httptest.NewRecorder()
		s.handle(w, r)
		if w.Code != status {
			t.Fatalf("%s: %d %s", method, w.Code, w.Body.String())
		}
		var c shareLinksConfig
		if status == 200 {
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cacheable settings")
			}
			if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
				t.Fatal(err)
			}
		}
		return c
	}
	if c := call("GET", "", "", 200); c.InternalBaseURL != "" {
		t.Fatal(c)
	}
	call("POST", `{"internalBaseURL":"http://gracenote-dev:8080/"}`, "application/json", 200)
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	s = &shareLinksServer{path: path}
	if c := call("GET", "", "", 200); c.InternalBaseURL != "http://gracenote-dev:8080" {
		t.Fatal(c)
	}
	call("POST", `{"internalBaseURL":"http://bad/path"}`, "application/json", 400)
	call("POST", `{"other":true}`, "application/json", 400)
	call("POST", `{} {}`, "application/json", 400)
	call("POST", `{`, "application/json", 400)
	call("POST", `{}`, "text/plain", 415)
	call("DELETE", "", "", 405)
	if c := call("GET", "", "", 200); c.InternalBaseURL != "http://gracenote-dev:8080" {
		t.Fatal("invalid request changed saved settings")
	}
	call("POST", `{"internalBaseURL":""}`, "application/json", 200)
	if c := call("GET", "", "", 200); c.InternalBaseURL != "" {
		t.Fatal(c)
	}
	s.path = filepath.Join(path, "invalid-child")
	call("POST", `{}`, "application/json", 500)
}
