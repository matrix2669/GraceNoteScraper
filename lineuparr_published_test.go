package main

import (
	"encoding/json"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func publishLineuparrForTest(t *testing.T, server *lineuparrServer) string {
	t.Helper()
	config, _, _ := server.store.Get()
	request := httptest.NewRequest(http.MethodPost, "/api/lineuparr/publish", strings.NewReader(`{"sourceFingerprint":"`+config.Fingerprint()+`"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.handlePublish(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("publish = %d: %s", recorder.Code, recorder.Body.String())
	}
	var result struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || result.Path == "" {
		t.Fatalf("publish result = %s, error = %v", recorder.Body.String(), err)
	}
	return result.Path
}

func readLineuparrExport(server *lineuparrServer, method, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	server.handlePublishedExport(recorder, httptest.NewRequest(method, path, nil))
	return recorder
}

func TestPublishedLineuparrSnapshotSurvivesEditsAndRestart(t *testing.T) {
	server := newLineuparrTestServer(t, true)
	server.exportDir = t.TempDir()
	path := publishLineuparrForTest(t, server)
	if path != "/lineuparr/exports/US_Test-Cable-11743_lineup.json" {
		t.Fatalf("unexpected public URL: %s", path)
	}
	first := readLineuparrExport(server, http.MethodGet, path)
	if first.Code != http.StatusOK || !json.Valid(first.Body.Bytes()) || first.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("first export = %d %s", first.Code, first.Body.String())
	}
	disposition, params, err := mime.ParseMediaType(first.Header().Get("Content-Disposition"))
	if err != nil || disposition != "inline" || params["filename"] != "US_Test-Cable-11743_lineup.json" {
		t.Fatalf("Lineuparr filename contract = %s", first.Header().Get("Content-Disposition"))
	}
	config, _, _ := server.store.Get()
	if err := server.builder.UpdateChannelsCategory(config.Fingerprint(), []string{"1001"}, "Sports"); err != nil {
		t.Fatal(err)
	}
	// The endpoint needs neither an active guide nor a builder after restarting.
	restarted := &lineuparrServer{exportDir: server.exportDir}
	unchanged := readLineuparrExport(restarted, http.MethodGet, path)
	if unchanged.Body.String() != first.Body.String() {
		t.Fatal("draft edits or restart changed the published export")
	}
	download := readLineuparrExport(restarted, http.MethodGet, path+"?download=1")
	if download.Body.String() != first.Body.String() || !strings.HasPrefix(download.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatal("download differs from published JSON")
	}
	head := readLineuparrExport(restarted, http.MethodHead, path)
	if head.Code != http.StatusOK || head.Body.Len() != 0 || head.Header().Get("Content-Length") != first.Header().Get("Content-Length") {
		t.Fatal("HEAD must describe the saved JSON without returning its body")
	}
	if nextPath := publishLineuparrForTest(t, server); nextPath != path {
		t.Fatalf("republishing changed URL: %s", nextPath)
	}
	updated := readLineuparrExport(restarted, http.MethodGet, path)
	if updated.Body.String() == first.Body.String() || !strings.Contains(updated.Body.String(), `"Sports"`) {
		t.Fatal("explicit export did not publish the category edit")
	}
	config.Gracenote.ProviderName = "Renamed Cable"
	if err := server.store.Save(config); err != nil {
		t.Fatal(err)
	}
	renamedPath := publishLineuparrForTest(t, server)
	if renamedPath != "/lineuparr/exports/US_Renamed-Cable-11743_lineup.json" {
		t.Fatal("public URL did not follow export filename")
	}
	updated = readLineuparrExport(restarted, http.MethodGet, renamedPath)
	_, renamedParams, err := mime.ParseMediaType(updated.Header().Get("Content-Disposition"))
	if err != nil || renamedParams["filename"] != "US_Renamed-Cable-11743_lineup.json" {
		t.Fatal("response filename did not follow provider name")
	}

	config.Gracenote.LineupID = "USA-SECOND"
	config.Gracenote.ProviderName = "Second Cable"
	if err := server.store.Save(config); err != nil {
		t.Fatal(err)
	}
	server.state.UpdateForSource(server.state.Get(), config.Fingerprint())
	secondPath := publishLineuparrForTest(t, server)
	if secondPath == path {
		t.Fatal("different source reused the previous lineup URL")
	}
	if got := readLineuparrExport(restarted, http.MethodGet, renamedPath); got.Body.String() != updated.Body.String() {
		t.Fatal("changing provider replaced the previously exported lineup")
	}
}

func TestPublishedLineuparrRejectsStaleSourceAndMissingGuide(t *testing.T) {
	server := newLineuparrTestServer(t, true)
	server.exportDir = t.TempDir()
	config, _, _ := server.store.Get()
	for _, test := range []struct {
		fingerprint string
		status      int
	}{{"stale", http.StatusConflict}, {config.Fingerprint(), http.StatusServiceUnavailable}} {
		server.state.UpdateForSource(nil, "")
		r := httptest.NewRequest(http.MethodPost, "/api/lineuparr/publish", strings.NewReader(`{"sourceFingerprint":"`+test.fingerprint+`"}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.handlePublish(w, r)
		if w.Code != test.status {
			t.Fatalf("status = %d, want %d", w.Code, test.status)
		}
	}
	files, err := os.ReadDir(server.exportDir)
	if err != nil || len(files) != 0 {
		t.Fatalf("failed publication wrote files: %v, %v", files, err)
	}
}

func TestPublishedLineuparrDescriptiveFilename(t *testing.T) {
	server := newLineuparrTestServer(t, true)
	server.exportDir = t.TempDir()
	config, _, _ := server.store.Get()
	config.Gracenote.ProviderName = "Optimum of Woodbury - Digital"
	if err := server.store.Save(config); err != nil {
		t.Fatal(err)
	}
	path := publishLineuparrForTest(t, server)
	if path != "/lineuparr/exports/US_Optimum-of-Woodbury-Digital-11743_lineup.json" {
		t.Fatalf("Optimum URL = %s", path)
	}
	if got := readLineuparrExport(server, http.MethodGet, lineuparrPublishedPrefix+config.Fingerprint()+"/lineup.json"); got.Code != http.StatusNotFound {
		t.Fatalf("legacy fingerprint URL remains accessible: %d", got.Code)
	}
	for _, filename := range []string{"../US_Test_lineup.json", "US_../Test_lineup.json", "config.json"} {
		if err := savePublishedLineuparr(server.exportDir, publishedLineuparr{Filename: filename, Data: json.RawMessage(`{}`)}); err == nil {
			t.Fatalf("accepted unsafe export filename %q", filename)
		}
	}
	// Filenames, not source fingerprints, identify published URLs. A deliberate
	// export with the same filename replaces the previous snapshot.
	config.Gracenote.LineupID = "USA-SECOND"
	if err := server.store.Save(config); err != nil {
		t.Fatal(err)
	}
	server.state.UpdateForSource(server.state.Get(), config.Fingerprint())
	if replacementPath := publishLineuparrForTest(t, server); replacementPath != path {
		t.Fatalf("same filename changed URL: %s", replacementPath)
	}
	files, err := os.ReadDir(server.exportDir)
	if err != nil || len(files) != 1 {
		t.Fatalf("expected one filename-scoped snapshot: %v %v", files, err)
	}
}

func TestPublishedLineuparrReadValidationAndFailedReplacement(t *testing.T) {
	server := newLineuparrTestServer(t, true)
	server.exportDir = t.TempDir()
	path := publishLineuparrForTest(t, server)
	before := readLineuparrExport(server, http.MethodGet, path).Body.String()
	if err := savePublishedLineuparr(server.exportDir, publishedLineuparr{Filename: "US_Test-Cable-11743_lineup.json", Data: json.RawMessage(`invalid`)}); err == nil {
		t.Fatal("invalid replacement succeeded")
	}
	if after := readLineuparrExport(server, http.MethodGet, path).Body.String(); before != after {
		t.Fatal("failed replacement lost prior export")
	}
	for _, badPath := range []string{lineuparrPublishedPrefix + "../config.json", path + "/extra", path + "missing", lineuparrPublishedPrefix + strings.Repeat("a", 64) + "/lineup.json", lineuparrPublishedPrefix + "US_.._lineup.json", lineuparrPublishedPrefix + "US_%2fconfig_lineup.json"} {
		if got := readLineuparrExport(server, http.MethodGet, badPath); got.Code != http.StatusNotFound {
			t.Fatalf("%s = %d", badPath, got.Code)
		}
	}
	if got := readLineuparrExport(server, http.MethodDelete, path); got.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE = %d", got.Code)
	}
	if err := os.WriteFile(filepath.Join(server.exportDir, "US_Test-Cable-11743_lineup.json"), []byte(`broken`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := readLineuparrExport(server, http.MethodGet, path); got.Code != http.StatusInternalServerError {
		t.Fatalf("corrupt snapshot = %d", got.Code)
	}
}
