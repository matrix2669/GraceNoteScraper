package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	lineuparrbuilder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

const lineuparrPublishedPrefix = "/lineuparr/exports/"

var publishedLineuparrFilename = regexp.MustCompile(`^[A-Z]{2}_[\pL\pN-]+_lineup\.json$`)

// One atomic record per export filename keeps the last explicit export independent of
// guide availability, subsequent draft edits, and provider selection changes.
type publishedLineuparr struct {
	Version     int             `json:"version"`
	Filename    string          `json:"filename"`
	PublishedAt time.Time       `json:"publishedAt"`
	Data        json.RawMessage `json:"data"`
}

func (s *lineuparrServer) handlePublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}
	var request struct {
		SourceFingerprint string `json:"sourceFingerprint"`
	}
	if !decodeLineuparrRequest(w, r, &request) {
		return
	}
	config, configured, _ := s.store.Get()
	if !configured || request.SourceFingerprint != config.Fingerprint() {
		http.Error(w, "The active provider changed; reload the builder before exporting", http.StatusConflict)
		return
	}
	if s.exportDir == "" {
		http.Error(w, "Published exports are unavailable", http.StatusServiceUnavailable)
		return
	}
	draft, config, _, ok := s.buildDraft(w, r)
	if !ok {
		return
	}
	if request.SourceFingerprint != config.Fingerprint() {
		http.Error(w, "The active provider changed; reload the builder before exporting", http.StatusConflict)
		return
	}
	data, err := json.MarshalIndent(lineuparrbuilder.ExportFromDraft(draft), "", "  ")
	if err != nil {
		http.Error(w, "Unable to create Lineuparr JSON", http.StatusInternalServerError)
		return
	}
	record := publishedLineuparr{Version: 1, Filename: lineuparrbuilder.ExportFilename(draft), PublishedAt: time.Now().UTC(), Data: data}
	current, err := s.store.WhileCurrent(config.Fingerprint(), func() error {
		return savePublishedLineuparr(s.exportDir, record)
	})
	if err != nil {
		http.Error(w, "Unable to save the export. The previous published version is unchanged.", http.StatusInternalServerError)
		return
	}
	if !current {
		http.Error(w, "The active provider changed; reload the builder before exporting", http.StatusConflict)
		return
	}
	writeLineuparrJSON(w, http.StatusOK, map[string]any{
		"path":     lineuparrPublishedPrefix + url.PathEscape(record.Filename),
		"filename": record.Filename, "publishedAt": record.PublishedAt,
	})
}

func savePublishedLineuparr(dir string, record publishedLineuparr) error {
	if !publishedLineuparrFilename.MatchString(record.Filename) {
		return errors.New("invalid Lineuparr export filename")
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".lineuparr-export-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, record.Filename))
}

func (s *lineuparrServer) handlePublishedExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	filename := strings.TrimPrefix(r.URL.Path, lineuparrPublishedPrefix)
	if !strings.HasPrefix(r.URL.Path, lineuparrPublishedPrefix) || !publishedLineuparrFilename.MatchString(filename) || s.exportDir == "" {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(filepath.Join(s.exportDir, filename))
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	var record publishedLineuparr
	if err != nil || json.Unmarshal(data, &record) != nil || record.Version != 1 || record.Filename != filename || !json.Valid(record.Data) {
		http.Error(w, "The saved export could not be read; export the lineup again", http.StatusInternalServerError)
		return
	}
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": record.Filename}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Serve the same serialized document for URL imports and explicit downloads.
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, record.Data, "", "  "); err != nil {
		http.Error(w, "The saved export could not be read", http.StatusInternalServerError)
		return
	}
	pretty.WriteByte('\n')
	w.Header().Set("Content-Length", fmt.Sprint(pretty.Len()))
	if r.Method == http.MethodGet {
		_, _ = w.Write(pretty.Bytes())
	}
}
