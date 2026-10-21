package main

import (
	"errors"
	"net/http"
	"strconv"

	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

// A reset never races a builder request. Clients opened before a reset must
// reload; a queued old mutation cannot recreate discarded choices afterwards.
func (s *lineuparrServer) guardBuilder(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.resetMu.RLock()
		defer s.resetMu.RUnlock()
		mutates := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
		mutates = mutates || (r.URL.Path == "/api/lineuparr/dispatcharr/review" && r.URL.Query().Get("refresh") == "true")
		if mutates {
			generation := strconv.FormatUint(s.builder.ResetGeneration(), 10)
			supplied := r.Header.Get("X-Builder-Generation")
			if supplied == "" {
				supplied = "0"
			}
			if supplied != generation {
				http.Error(w, "Builder was reset; reload before changing it", 409)
				return
			}
		}
		next(w, r)
	}
}

func (s *lineuparrServer) handleBuilderReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if !s.resetMu.TryLock() {
		http.Error(w, "A builder operation is in progress. Wait for it to finish and retry.", 409)
		return
	}
	defer s.resetMu.Unlock()
	config, configured, _ := s.store.Get()
	if !configured {
		http.Error(w, "Choose a lineup first", 409)
		return
	}
	if r.Method == http.MethodGet {
		writeLineuparrJSON(w, 200, map[string]any{"sourceFingerprint": config.Fingerprint(), "token": s.builder.ReviewStateToken(), "generation": s.builder.ResetGeneration(), "recovery": s.builder.LatestResetRecovery(config.Fingerprint()), "clears": []string{"builder channel inclusion and category overrides", "duplicate keeper choices and alias suppressions", "stream confirmations, denials and inclusion/exclusion decisions", "stream snapshot and active published exports"}, "preserves": []string{"downloaded guide and schedules", "TMDB lookup/category caches and completion settings", "verified reusable station evidence", "provider/source settings and Dispatcharr connection credentials"}})
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}
	var body struct {
		SourceFingerprint string `json:"sourceFingerprint"`
		Token             string `json:"token"`
		Confirm           bool   `json:"confirm"`
		Restore           string `json:"restore,omitempty"`
	}
	if !decodeLineuparrRequest(w, r, &body) {
		return
	}
	if !body.Confirm || body.SourceFingerprint != config.Fingerprint() {
		http.Error(w, "Confirm the current builder reset summary", 409)
		return
	}
	restoreAvailable := s.builder.ResetRecoveryMatchesSource(config.Fingerprint())
	var backup string
	current, err := s.store.WhileCurrent(config.Fingerprint(), func() error {
		var e error
		backup, e = s.builder.ResetBuilder(config.Fingerprint(), body.Token, body.Restore)
		return e
	})
	if err != nil {
		if errors.Is(err, builder.ErrStaleReview) {
			http.Error(w, "Builder choices changed; review the reset summary again", 409)
		} else {
			http.Error(w, "Builder reset failed; previous choices are unchanged", 500)
		}
		return
	}
	if !current {
		http.Error(w, "Provider changed; reload", 409)
		return
	}
	if s.afterBuilderReset != nil {
		s.afterBuilderReset()
	}
	writeLineuparrJSON(w, 200, map[string]any{"reset": true, "backup": backup, "restoreAvailable": restoreAvailable, "generation": s.builder.ResetGeneration()})
}
