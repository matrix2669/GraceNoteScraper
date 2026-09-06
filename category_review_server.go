package main

import (
	lineuparrbuilder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"net/http"
)

func (s *lineuparrServer) handleApproveCategories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}
	var body struct {
		SourceFingerprint string `json:"sourceFingerprint"`
		Channels          []struct {
			ID       string `json:"id"`
			Category string `json:"category"`
		} `json:"channels"`
	}
	if !decodeLineuparrRequest(w, r, &body) {
		return
	}
	if len(body.Channels) == 0 || len(body.Channels) > 1000 {
		http.Error(w, "Select between 1 and 1000 channels", 400)
		return
	}
	draft, config, _, ok := s.buildDraft(w, r)
	if !ok {
		return
	}
	if body.SourceFingerprint != config.Fingerprint() {
		http.Error(w, "Provider changed; reload category review", 409)
		return
	}
	currentRows := map[string]lineuparrbuilder.DraftChannel{}
	for _, channel := range draft.Channels {
		currentRows[channel.ID] = channel
	}
	selected := []lineuparrbuilder.DraftChannel{}
	seen := map[string]bool{}
	for _, requested := range body.Channels {
		channel, exists := currentRows[requested.ID]
		if !exists || seen[requested.ID] || !channel.Included || !channel.NeedsCategoryReview || channel.Category != requested.Category {
			http.Error(w, "Category proposals changed; reload before approving. Nothing was saved.", 409)
			return
		}
		seen[requested.ID] = true
		selected = append(selected, channel)
	}
	current, err := s.store.WhileCurrent(config.Fingerprint(), func() error { return s.builder.ApproveReviewedCategories(config.Fingerprint(), selected) })
	if err != nil {
		http.Error(w, "Could not approve categories; reload and retry. Nothing was saved.", 409)
		return
	}
	if !current {
		http.Error(w, "Provider changed; nothing was saved", 409)
		return
	}
	writeLineuparrJSON(w, 200, map[string]any{"saved": true, "approved": len(selected)})
}
