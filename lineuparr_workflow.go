package main

import (
	"net/http"
	"strings"
	"time"
)

type lineuparrWorkflowRequest struct {
	Action            string `json:"action"`
	SourceFingerprint string `json:"sourceFingerprint"`
}

func (s *lineuparrServer) handleWorkflow(w http.ResponseWriter, r *http.Request) {
	config, configured, _ := s.store.Get()
	if !configured {
		http.Error(w, "Choose a provider at /setup first", http.StatusConflict)
		return
	}
	fingerprint := config.Fingerprint()
	if r.Method == http.MethodGet {
		writeLineuparrJSON(w, http.StatusOK, s.builder.WorkflowProgress(fingerprint))
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}
	var request lineuparrWorkflowRequest
	if !decodeLineuparrRequest(w, r, &request) {
		return
	}
	request.Action = strings.TrimSpace(request.Action)
	request.SourceFingerprint = strings.TrimSpace(request.SourceFingerprint)
	if request.SourceFingerprint != fingerprint {
		http.Error(w, "The active provider changed; reload the builder before saving", http.StatusConflict)
		return
	}

	now := time.Now().UTC()
	var update func() error
	switch request.Action {
	case "skip-tmdb":
		update = func() error { return s.builder.SetTMDBDisposition(fingerprint, "skipped", now) }
	case "complete-tmdb":
		update = func() error { return s.builder.SetTMDBDisposition(fingerprint, "complete", now) }
	case "complete-customization":
		draft, currentConfig, _, ok := s.buildDraft(w, r)
		if !ok {
			return
		}
		if currentConfig.Fingerprint() != fingerprint || draft.CustomizationSignature == "" {
			http.Error(w, "The lineup changed; reload before completing customization", http.StatusConflict)
			return
		}
		update = func() error { return s.builder.CompleteCustomization(fingerprint, draft.CustomizationSignature, now) }
	default:
		http.Error(w, "Unknown workflow action", http.StatusBadRequest)
		return
	}
	current, err := s.store.WhileCurrent(fingerprint, update)
	if err != nil {
		http.Error(w, "Unable to save workflow progress: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !current {
		http.Error(w, "The active provider changed; reload the builder before saving", http.StatusConflict)
		return
	}
	writeLineuparrJSON(w, http.StatusOK, s.builder.WorkflowProgress(fingerprint))
}
