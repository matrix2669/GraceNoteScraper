package main

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Display-only configuration: never fetch this URL or discover it via Docker.
type shareLinksServer struct {
	path string
	mu   sync.Mutex
}
type shareLinksConfig struct {
	InternalBaseURL string `json:"internalBaseURL"`
}

func normalizeInternalBase(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return "", errors.New("enter an HTTP(S) base URL without credentials, path, query or fragment")
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return "", errors.New("invalid port")
		}
	}
	u.Path = ""
	return u.String(), nil
}
func (s *shareLinksServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	config := shareLinksConfig{}
	if r.Method == http.MethodPost {
		media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if media != "application/json" {
			http.Error(w, "application/json required", 415)
			return
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		d.DisallowUnknownFields()
		if d.Decode(&config) != nil || d.Decode(&struct{}{}) != io.EOF {
			http.Error(w, "invalid settings", 400)
			return
		}
		base, err := normalizeInternalBase(config.InternalBaseURL)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		config.InternalBaseURL = base
		if err = s.save(config); err != nil {
			http.Error(w, "could not save internal URL settings", 500)
			return
		}
	} else {
		data, err := os.ReadFile(s.path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			http.Error(w, "could not read internal URL settings", 500)
			return
		}
		if err == nil && json.Unmarshal(data, &config) != nil {
			http.Error(w, "invalid internal URL settings", 500)
			return
		}
		base, err := normalizeInternalBase(config.InternalBaseURL)
		if err != nil {
			http.Error(w, "invalid internal URL settings", 500)
			return
		}
		config.InternalBaseURL = base
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(config)
}
func (s *shareLinksServer) save(config shareLinksConfig) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".share-links-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = json.NewEncoder(f).Encode(config); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), s.path)
}
