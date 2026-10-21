package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

type affiliateExclusionRow struct {
	Key           string `json:"key"`
	ChannelNumber string `json:"channelNumber"`
	ChannelName   string `json:"channelName"`
	StreamName    string `json:"streamName"`
	AccountID     int64  `json:"accountId"`
	Reason        string `json:"reason"`
	Group         string `json:"group"`
}
type affiliateExclusionPreview struct {
	guide       *guide.TVGuide
	Token       string                  `json:"token"`
	Rows        []affiliateExclusionRow `json:"rows"`
	ManualCount int                     `json:"manualCount"`
}

// A token commits to concrete current candidates, raw station identities,
// matcher/snapshot generation and all saved builder choices. It is not stored.
func (s *dispatcharrServer) affiliatePreview(w http.ResponseWriter, r *http.Request) (affiliateExclusionPreview, map[string]dispatcharr.Candidate, dispatcharrReviewBuild, string, bool) {
	currentGuide := s.lineup.state.Get()
	build, ok := s.buildReview(w, r, false)
	if !ok {
		return affiliateExclusionPreview{}, nil, build, "", false
	}
	if build.response.ReviewStatus != "current" {
		http.Error(w, "Refresh a current stream review before previewing exclusions", http.StatusConflict)
		return affiliateExclusionPreview{}, nil, build, "", false
	}
	stateToken := s.lineup.builder.ReviewStateToken()
	config, inputs, ok := s.lineup.activeInputs(w)
	if !ok {
		return affiliateExclusionPreview{}, nil, build, "", false
	}
	targets := map[string]dispatcharr.AffiliateTarget{}
	for _, input := range inputs {
		targets[input.Key] = dispatcharr.AffiliateTarget{Country: affiliateCountry(config.Gracenote.Country), CallSign: input.CallSign}
	}
	stored := s.lineup.builder.MatchDecisions(config.Fingerprint())
	decisions := []dispatcharr.Decision{}
	for _, d := range stored {
		decisions = append(decisions, dispatcharr.Decision{ChannelID: d.ChannelID, Source: d.DispatcharrFingerprint, StreamHash: d.StreamFingerprint, StreamName: d.StreamName, NormalizedAlias: d.NormalizedAlias, Decision: d.Decision})
	}
	plan := dispatcharr.PlanAffiliateExclusions(build.candidates, targets, decisions)
	preview := affiliateExclusionPreview{guide: currentGuide, Rows: []affiliateExclusionRow{}, ManualCount: len(plan.Manual)}
	selected := map[string]dispatcharr.Candidate{}
	for _, suggestion := range plan.Suggestions {
		c := suggestion.Candidate
		selected[c.Key] = c
		preview.Rows = append(preview.Rows, affiliateExclusionRow{Key: c.Key, ChannelNumber: c.ChannelNumber, ChannelName: c.ChannelName, StreamName: c.StreamName, AccountID: c.M3UAccountID, Group: c.ChannelID + "\x00" + c.NormalizedAlias, Reason: fmt.Sprintf("Explicit %s differs from target station %s", suggestion.StreamCallSign, suggestion.TargetCallSign)})
	}
	sort.Slice(preview.Rows, func(i, j int) bool { return preview.Rows[i].Key < preview.Rows[j].Key })
	s.reviewMu.RLock()
	snapshot := s.snapshot
	s.reviewMu.RUnlock()
	data, _ := json.Marshal([]any{preview.Rows, targets, stateToken, snapshot.version, snapshot.fetchedAt, build.dispatchConfig.Fingerprint(), build.lineupConfig.Fingerprint(), snapshot.digest, fmt.Sprintf("%p", currentGuide)})
	preview.Token = fmt.Sprintf("%x", sha256.Sum256(data))
	if currentGuide != s.lineup.state.Get() || stateToken != s.lineup.builder.ReviewStateToken() || config.Fingerprint() != build.lineupConfig.Fingerprint() {
		http.Error(w, "Review changed; preview again", http.StatusConflict)
		return preview, nil, build, "", false
	}
	return preview, selected, build, stateToken, true
}

func (s *dispatcharrServer) handleAffiliateExclusions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	if !s.refreshMu.TryLock() {
		http.Error(w, "A refresh or connection change is running", 409)
		return
	}
	defer s.refreshMu.Unlock()
	var body struct {
		Token string   `json:"token"`
		Keys  []string `json:"keys"`
	}
	if r.Method == http.MethodPost {
		if !requireJSONContentType(w, r) || !decodeLineuparrRequest(w, r, &body) {
			return
		}
		if len(body.Keys) == 0 || len(body.Keys) > 250000 {
			http.Error(w, "Select exclusions from a current preview", 400)
			return
		}
	}
	preview, candidates, build, stateToken, ok := s.affiliatePreview(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		writeLineuparrJSON(w, 200, preview)
		return
	}
	if body.Token != preview.Token {
		http.Error(w, "Review changed; preview affiliate exclusions again", 409)
		return
	}
	batch := []builder.MatchDecision{}
	seen := map[string]bool{}
	for _, key := range body.Keys {
		c, found := candidates[key]
		if !found || seen[key] {
			http.Error(w, "Invalid or duplicate preview selection", 409)
			return
		}
		seen[key] = true
		batch = append(batch, builder.MatchDecision{Key: c.Key, Decision: "denied", MatcherVersion: c.MatcherVersion, DispatcharrFingerprint: c.Source, StreamFingerprint: c.StreamHash, StreamKey: c.StreamKey, StreamID: c.StreamID, M3UAccountID: c.M3UAccountID, ChannelID: c.ChannelID, ChannelNumber: c.ChannelNumber, ChannelName: c.ChannelName, StreamName: c.StreamName, NormalizedAlias: c.NormalizedAlias, Score: c.Score, NameScore: c.NameScore, Reason: c.Reason})
	}
	selectedGroups := map[string]bool{}
	for _, d := range batch {
		selectedGroups[d.ChannelID+"\x00"+d.NormalizedAlias] = true
	}
	for _, c := range candidates {
		if selectedGroups[c.ChannelID+"\x00"+c.NormalizedAlias] && !seen[c.Key] {
			http.Error(w, "Select all raw variants in a review group", 409)
			return
		}
	}
	if !s.saveWhileCurrent(build.dispatchConfig, build.lineupConfig, func() error {
		return s.applyAffiliatePreview(preview, build.lineupConfig.Fingerprint(), stateToken, batch)
	}, w) {
		return
	}
	writeLineuparrJSON(w, 200, map[string]int{"saved": len(batch)})
}

func affiliateCountry(raw string) string {
	if strings.EqualFold(raw, "USA") || strings.EqualFold(raw, "US") {
		return "US"
	}
	return ""
}

// Hold current raw station identity stable until the atomic decision commit.
func (s *dispatcharrServer) applyAffiliatePreview(preview affiliateExclusionPreview, fingerprint, stateToken string, batch []builder.MatchDecision) error {
	s.lineup.state.mu.RLock()
	defer s.lineup.state.mu.RUnlock()
	if s.lineup.state.guide != preview.guide || s.lineup.state.sourceFingerprint != fingerprint {
		return builder.ErrStaleReview
	}
	return s.lineup.builder.ApplyReviewedDenials(fingerprint, stateToken, batch)
}
