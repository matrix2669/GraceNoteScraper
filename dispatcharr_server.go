package main

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	lineuparrbuilder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

const (
	dispatcharrStreamCacheAge = 5 * time.Minute
	dispatcharrReviewLimit    = 100
	dispatcharrReviewMaxLimit = 5000
)

type dispatcharrAPI interface {
	Test(context.Context, dispatcharr.Config) error
	Streams(context.Context, dispatcharr.Config) ([]dispatcharr.Stream, error)
	Reset()
}

type dispatcharrServer struct {
	lineup   *lineuparrServer
	config   *dispatcharr.ConfigStore
	client   dispatcharrAPI
	configMu sync.Mutex
	cache    dispatcharrStreamCache
	reviewMu sync.RWMutex
	review   dispatcharrCandidateCache
}

type dispatcharrCandidateCache struct {
	dispatcharrFingerprint string
	lineupFingerprint      string
	candidates             map[string]dispatcharr.Candidate
	groups                 map[string][]string
}

type dispatcharrStreamCache struct {
	mu          sync.Mutex
	fingerprint string
	fetchedAt   time.Time
	streams     []dispatcharr.Stream
}

type dispatcharrConfigResponse struct {
	Configured bool   `json:"configured"`
	BaseURL    string `json:"baseUrl,omitempty"`
	AuthMethod string `json:"authMethod,omitempty"`
	Username   string `json:"username,omitempty"`
}

type dispatcharrReviewDecision struct {
	Key           string    `json:"key"`
	Decision      string    `json:"decision"`
	ChannelID     string    `json:"channelId"`
	ChannelNumber string    `json:"channelNumber"`
	ChannelName   string    `json:"channelName"`
	StreamID      int64     `json:"streamId"`
	StreamName    string    `json:"streamName"`
	TVGID         string    `json:"tvgId,omitempty"`
	M3UAccountID  int64     `json:"m3uAccountId"`
	M3UAccountIDs []int64   `json:"m3uAccountIds,omitempty"`
	StreamCount   int       `json:"streamCount"`
	StreamNames   []string  `json:"streamNames,omitempty"`
	TVGIDs        []string  `json:"tvgIds,omitempty"`
	Score         int       `json:"score"`
	Reason        string    `json:"reason"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type dispatcharrReviewResponse struct {
	StreamCount          int                          `json:"streamCount"`
	CandidateCount       int                          `json:"candidateCount"`
	CandidateStreamCount int                          `json:"candidateStreamCount"`
	ConfirmedCount       int                          `json:"confirmedCount"`
	DeniedCount          int                          `json:"deniedCount"`
	FetchedAt            time.Time                    `json:"fetchedAt"`
	Cached               bool                         `json:"cached"`
	Warning              string                       `json:"warning,omitempty"`
	VisibleLimit         int                          `json:"visibleLimit"`
	Candidates           []dispatcharr.CandidateGroup `json:"candidates"`
	Decisions            []dispatcharrReviewDecision  `json:"decisions"`
}

type dispatcharrReviewBuild struct {
	response       dispatcharrReviewResponse
	candidates     []dispatcharr.Candidate
	dispatchConfig dispatcharr.Config
	lineupConfig   appconfig.Config
}

func (s *dispatcharrServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		config, configured := s.config.Get()
		writeLineuparrJSON(w, http.StatusOK, dispatcharrConfigResponse{
			Configured: configured,
			BaseURL:    config.BaseURL,
			AuthMethod: config.AuthMethod,
			Username:   config.Username,
		})
	case http.MethodPost:
		if !requireJSONContentType(w, r) {
			return
		}
		var body struct {
			BaseURL    string `json:"baseUrl"`
			AuthMethod string `json:"authMethod"`
			Username   string `json:"username"`
			Password   string `json:"password"`
			APIKey     string `json:"apiKey"`
		}
		if !decodeLineuparrRequest(w, r, &body) {
			return
		}
		s.configMu.Lock()
		defer s.configMu.Unlock()
		existing, configured := s.config.Get()
		method := strings.ToLower(strings.TrimSpace(body.AuthMethod))
		if method == "" {
			method = dispatcharr.AuthPassword
		}
		sameConnection := configured && strings.TrimRight(strings.TrimSpace(body.BaseURL), "/") == existing.BaseURL && method == existing.AuthMethod
		if method == dispatcharr.AuthPassword && strings.TrimSpace(body.Password) == "" && sameConnection && strings.TrimSpace(body.Username) == existing.Username {
			body.Password = existing.Password
		}
		if method == dispatcharr.AuthAPIKey && strings.TrimSpace(body.APIKey) == "" && sameConnection {
			body.APIKey = existing.APIKey
		}
		config, err := (dispatcharr.Config{
			BaseURL: body.BaseURL, AuthMethod: method, Username: body.Username, Password: body.Password, APIKey: body.APIKey,
		}).Normalized()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		testContext, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if err := s.client.Test(testContext, config); err != nil {
			http.Error(w, "Unable to connect: "+err.Error(), http.StatusBadGateway)
			return
		}
		if err := s.config.Save(config); err != nil {
			http.Error(w, "Unable to save Dispatcharr connection: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.client.Reset()
		s.cache.clear()
		s.clearCandidateCache()
		writeLineuparrJSON(w, http.StatusOK, dispatcharrConfigResponse{
			Configured: true, BaseURL: config.BaseURL, AuthMethod: config.AuthMethod, Username: config.Username,
		})
	case http.MethodDelete:
		s.configMu.Lock()
		defer s.configMu.Unlock()
		if err := s.config.Clear(); err != nil {
			http.Error(w, "Unable to remove Dispatcharr connection: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.client.Reset()
		s.cache.clear()
		s.clearCandidateCache()
		writeLineuparrJSON(w, http.StatusOK, dispatcharrConfigResponse{Configured: false})
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *dispatcharrServer) handleReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	build, ok := s.buildReview(w, r, strings.EqualFold(r.URL.Query().Get("refresh"), "true"))
	if !ok {
		return
	}
	writeLineuparrJSON(w, http.StatusOK, build.response)
}

func (s *dispatcharrServer) handleDecision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}
	var body struct {
		Key      string   `json:"key"`
		Decision string   `json:"decision,omitempty"`
		TVGIDs   []string `json:"tvgIds"`
	}
	if !decodeLineuparrRequest(w, r, &body) {
		return
	}
	body.Key = strings.TrimSpace(body.Key)
	if body.Key == "" {
		http.Error(w, "match decision key is required", http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodDelete {
		dispatchConfig, configured := s.config.Get()
		if !configured {
			http.Error(w, "Connect Dispatcharr before reviewing matches", http.StatusConflict)
			return
		}
		lineupConfig, _, ok := s.lineup.activeInputs(w)
		if !ok {
			return
		}
		stored := s.lineup.builder.MatchDecisions(lineupConfig.Fingerprint())
		existing := stored[body.Key]
		if existing.Key == "" {
			http.Error(w, "match decision does not belong to the active sources", http.StatusNotFound)
			return
		}
		alias := dispatcharr.NormalizeAliasName(existing.StreamName)
		keys := make([]string, 0)
		for key, decision := range stored {
			if decision.Decision == existing.Decision && decision.ChannelID == existing.ChannelID && dispatcharr.NormalizeAliasName(decision.StreamName) == alias {
				keys = append(keys, key)
			}
		}
		if !s.saveWhileCurrent(dispatchConfig, lineupConfig, func() error {
			return s.lineup.builder.ClearMatchDecisions(lineupConfig.Fingerprint(), keys)
		}, w) {
			return
		}
		writeLineuparrJSON(w, http.StatusOK, map[string]bool{"saved": true})
		return
	}

	body.Decision = strings.ToLower(strings.TrimSpace(body.Decision))
	if body.Decision != "confirmed" && body.Decision != "denied" {
		http.Error(w, "decision must be confirmed or denied", http.StatusBadRequest)
		return
	}
	dispatchConfig, configured := s.config.Get()
	if !configured {
		http.Error(w, "Connect Dispatcharr before reviewing matches", http.StatusConflict)
		return
	}
	lineupConfig, _, ok := s.lineup.activeInputs(w)
	if !ok {
		return
	}
	candidates, found := s.cachedCandidateGroup(dispatchConfig.Fingerprint(), lineupConfig.Fingerprint(), body.Key)
	if !found {
		if candidate, candidateFound := s.cachedCandidate(dispatchConfig.Fingerprint(), lineupConfig.Fingerprint(), body.Key); candidateFound {
			candidates, found = []dispatcharr.Candidate{candidate}, true
		}
	}
	if !found {
		build, built := s.buildReview(w, r, false)
		if !built {
			return
		}
		dispatchConfig = build.dispatchConfig
		lineupConfig = build.lineupConfig
		candidates, found = s.cachedCandidateGroup(dispatchConfig.Fingerprint(), lineupConfig.Fingerprint(), body.Key)
		if !found {
			if candidate, candidateFound := s.cachedCandidate(dispatchConfig.Fingerprint(), lineupConfig.Fingerprint(), body.Key); candidateFound {
				candidates, found = []dispatcharr.Candidate{candidate}, true
			}
		}
	}
	if !found {
		http.Error(w, "match candidate is no longer current", http.StatusConflict)
		return
	}
	selectedTVGIDs := make(map[string]bool)
	for _, value := range body.TVGIDs {
		if value = strings.ToLower(strings.TrimSpace(value)); value != "" {
			selectedTVGIDs[value] = true
		}
	}
	decisions := make([]lineuparrbuilder.MatchDecision, 0, len(candidates))
	for index, candidate := range candidates {
		tvgID := candidate.TVGID
		if body.TVGIDs != nil && !selectedTVGIDs[strings.ToLower(strings.TrimSpace(tvgID))] {
			tvgID = ""
		}
		decisionKey := candidate.Key
		if index == 0 {
			decisionKey = body.Key
		}
		decisions = append(decisions, lineuparrbuilder.MatchDecision{
			Key: decisionKey, Decision: body.Decision,
			DispatcharrFingerprint: candidate.Source, StreamFingerprint: candidate.StreamHash,
			StreamKey: candidate.StreamKey, StreamID: candidate.StreamID, M3UAccountID: candidate.M3UAccountID,
			ChannelID: candidate.ChannelID, ChannelNumber: candidate.ChannelNumber, ChannelName: candidate.ChannelName,
			StreamName: candidate.StreamName, NormalizedAlias: candidate.NormalizedAlias,
			TVGID: tvgID, Score: candidate.Score, NameScore: candidate.NameScore, Reason: candidate.Reason,
		})
	}
	if !s.saveWhileCurrent(dispatchConfig, lineupConfig, func() error {
		return s.lineup.builder.SetMatchDecisions(lineupConfig.Fingerprint(), decisions)
	}, w) {
		return
	}
	s.removeCachedGroup(dispatchConfig.Fingerprint(), lineupConfig.Fingerprint(), body.Key, candidates)
	writeLineuparrJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

func (s *dispatcharrServer) buildReview(w http.ResponseWriter, r *http.Request, force bool) (dispatcharrReviewBuild, bool) {
	visibleLimit, err := dispatcharrVisibleLimit(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return dispatcharrReviewBuild{}, false
	}
	dispatchConfig, configured := s.config.Get()
	if !configured {
		http.Error(w, "Connect Dispatcharr to review M3U matches", http.StatusConflict)
		return dispatcharrReviewBuild{}, false
	}
	draft, lineupConfig, _, ok := s.lineup.buildDraft(w, r)
	if !ok {
		return dispatcharrReviewBuild{}, false
	}
	streams, fetchedAt, cached, warning, err := s.cache.get(r.Context(), s.client, dispatchConfig, force)
	if err != nil {
		http.Error(w, "Unable to load M3U streams: "+err.Error(), http.StatusBadGateway)
		return dispatcharrReviewBuild{}, false
	}
	currentDispatch, stillConfigured := s.config.Get()
	if !stillConfigured || currentDispatch.Fingerprint() != dispatchConfig.Fingerprint() {
		http.Error(w, "The Dispatcharr connection changed; reload match review", http.StatusConflict)
		return dispatcharrReviewBuild{}, false
	}

	channels := make([]dispatcharr.MatchChannel, 0, len(draft.Channels))
	for _, channel := range draft.Channels {
		channels = append(channels, dispatcharr.MatchChannel{
			ID: channel.ID, Number: channel.Number, Name: channel.Name, Category: channel.Category,
			Aliases: append([]string(nil), channel.Aliases...), EPGIDs: append([]string(nil), channel.EPGIDs...),
		})
	}
	stored := s.lineup.builder.MatchDecisions(lineupConfig.Fingerprint())
	matcherDecisions := make(map[string]dispatcharr.Decision, len(stored))
	for key, decision := range stored {
		matcherDecisions[key] = dispatcharr.Decision{
			Key: key, Decision: decision.Decision, Source: decision.DispatcharrFingerprint,
			StreamHash: decision.StreamFingerprint, ChannelID: decision.ChannelID, StreamName: decision.StreamName,
			NormalizedAlias: decision.NormalizedAlias,
		}
	}
	history, confirmed, denied := groupReviewDecisions(stored)
	if len(history) > dispatcharrReviewLimit {
		history = history[:dispatcharrReviewLimit]
	}
	matches := dispatcharr.MatchStreamCandidates(dispatchConfig.Fingerprint(), channels, streams, matcherDecisions)
	groups := attachDispatcharrAlternatives(dispatcharr.GroupCandidates(matches.Primary), dispatcharr.GroupCandidates(matches.All))
	s.cacheCandidates(dispatchConfig.Fingerprint(), lineupConfig.Fingerprint(), matches.All)
	visible := groups
	if len(visible) > visibleLimit {
		visible = visible[:visibleLimit]
	}
	return dispatcharrReviewBuild{
		response: dispatcharrReviewResponse{
			StreamCount: len(streams), CandidateCount: len(groups), CandidateStreamCount: len(matches.Primary), ConfirmedCount: confirmed, DeniedCount: denied,
			FetchedAt: fetchedAt, Cached: cached, Warning: warning, VisibleLimit: visibleLimit, Candidates: visible, Decisions: history,
		},
		candidates: matches.Primary, dispatchConfig: dispatchConfig, lineupConfig: lineupConfig,
	}, true
}

func dispatcharrVisibleLimit(r *http.Request) (int, error) {
	value := strings.TrimSpace(r.URL.Query().Get("limit"))
	if value == "" {
		return dispatcharrReviewLimit, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > dispatcharrReviewMaxLimit {
		return 0, fmt.Errorf("review limit must be between 1 and %d", dispatcharrReviewMaxLimit)
	}
	return limit, nil
}

func attachDispatcharrAlternatives(primary, all []dispatcharr.CandidateGroup) []dispatcharr.CandidateGroup {
	byKey := make(map[string]dispatcharr.CandidateGroup, len(all))
	byAlias := make(map[string][]dispatcharr.CandidateGroup)
	for _, group := range all {
		byKey[group.Key] = group
		byAlias[group.NormalizedAlias] = append(byAlias[group.NormalizedAlias], group)
	}
	for index := range primary {
		current := primary[index]
		if complete, ok := byKey[current.Key]; ok {
			primary[index] = complete
		}
		if current.MinimumScore >= 95 {
			continue
		}
		for _, alternate := range byAlias[current.NormalizedAlias] {
			if alternate.Key != current.Key {
				primary[index].Alternatives = append(primary[index].Alternatives, alternate)
			}
		}
	}
	return primary
}

func groupReviewDecisions(stored map[string]lineuparrbuilder.MatchDecision) ([]dispatcharrReviewDecision, int, int) {
	groups := make(map[string]*dispatcharrReviewDecision)
	for _, decision := range stored {
		normalized := strings.TrimSpace(decision.NormalizedAlias)
		if normalized == "" {
			normalized = dispatcharr.NormalizeAliasName(decision.StreamName)
		}
		key := strings.Join([]string{decision.Decision, decision.ChannelID, normalized}, "\x00")
		group := groups[key]
		if group == nil {
			group = &dispatcharrReviewDecision{
				Key: decision.Key, Decision: decision.Decision,
				ChannelID: decision.ChannelID, ChannelNumber: decision.ChannelNumber, ChannelName: decision.ChannelName,
				StreamID: decision.StreamID, StreamName: decision.StreamName, TVGID: decision.TVGID,
				M3UAccountID: decision.M3UAccountID, Score: decision.Score, Reason: decision.Reason, UpdatedAt: decision.UpdatedAt,
			}
			groups[key] = group
		}
		group.StreamCount++
		group.StreamNames = appendUniqueReviewString(group.StreamNames, decision.StreamName)
		group.TVGIDs = appendUniqueReviewString(group.TVGIDs, decision.TVGID)
		group.M3UAccountIDs = appendUniqueReviewInt64(group.M3UAccountIDs, decision.M3UAccountID)
		if decision.UpdatedAt.After(group.UpdatedAt) {
			group.Key = decision.Key
			group.StreamID = decision.StreamID
			group.StreamName = decision.StreamName
			group.TVGID = decision.TVGID
			group.M3UAccountID = decision.M3UAccountID
			group.Score = decision.Score
			group.Reason = decision.Reason
			group.UpdatedAt = decision.UpdatedAt
		}
	}
	history := make([]dispatcharrReviewDecision, 0, len(groups))
	confirmed := 0
	denied := 0
	for _, group := range groups {
		sort.Slice(group.M3UAccountIDs, func(i, j int) bool { return group.M3UAccountIDs[i] < group.M3UAccountIDs[j] })
		sort.Slice(group.StreamNames, func(i, j int) bool {
			return strings.ToLower(group.StreamNames[i]) < strings.ToLower(group.StreamNames[j])
		})
		sort.Slice(group.TVGIDs, func(i, j int) bool { return strings.ToLower(group.TVGIDs[i]) < strings.ToLower(group.TVGIDs[j]) })
		history = append(history, *group)
		if group.Decision == "confirmed" {
			confirmed++
		} else if group.Decision == "denied" {
			denied++
		}
	}
	sort.SliceStable(history, func(i, j int) bool { return history[i].UpdatedAt.After(history[j].UpdatedAt) })
	return history, confirmed, denied
}

func appendUniqueReviewString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if strings.EqualFold(existing, value) {
			return values
		}
	}
	return append(values, value)
}

func appendUniqueReviewInt64(values []int64, value int64) []int64 {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func (s *dispatcharrServer) cacheCandidates(dispatchFingerprint, lineupFingerprint string, candidates []dispatcharr.Candidate) {
	byKey := make(map[string]dispatcharr.Candidate, len(candidates))
	for _, candidate := range candidates {
		byKey[candidate.Key] = candidate
	}
	groups := make(map[string][]string)
	for _, group := range dispatcharr.GroupCandidates(candidates) {
		groups[group.Key] = append([]string(nil), group.CandidateKeys...)
	}
	s.reviewMu.Lock()
	s.review = dispatcharrCandidateCache{dispatcharrFingerprint: dispatchFingerprint, lineupFingerprint: lineupFingerprint, candidates: byKey, groups: groups}
	s.reviewMu.Unlock()
}

func (s *dispatcharrServer) cachedCandidate(dispatchFingerprint, lineupFingerprint, key string) (dispatcharr.Candidate, bool) {
	s.reviewMu.RLock()
	defer s.reviewMu.RUnlock()
	if s.review.dispatcharrFingerprint != dispatchFingerprint || s.review.lineupFingerprint != lineupFingerprint {
		return dispatcharr.Candidate{}, false
	}
	candidate, ok := s.review.candidates[key]
	return candidate, ok
}

func (s *dispatcharrServer) cachedCandidateGroup(dispatchFingerprint, lineupFingerprint, key string) ([]dispatcharr.Candidate, bool) {
	s.reviewMu.RLock()
	defer s.reviewMu.RUnlock()
	if s.review.dispatcharrFingerprint != dispatchFingerprint || s.review.lineupFingerprint != lineupFingerprint {
		return nil, false
	}
	keys, ok := s.review.groups[key]
	if !ok || len(keys) == 0 {
		return nil, false
	}
	result := make([]dispatcharr.Candidate, 0, len(keys))
	for _, candidateKey := range keys {
		candidate, exists := s.review.candidates[candidateKey]
		if !exists {
			return nil, false
		}
		result = append(result, candidate)
	}
	return result, true
}

func (s *dispatcharrServer) removeCachedCandidate(dispatchFingerprint, lineupFingerprint, key string) {
	s.reviewMu.Lock()
	defer s.reviewMu.Unlock()
	if s.review.dispatcharrFingerprint == dispatchFingerprint && s.review.lineupFingerprint == lineupFingerprint {
		delete(s.review.candidates, key)
	}
}

func (s *dispatcharrServer) removeCachedGroup(dispatchFingerprint, lineupFingerprint, key string, candidates []dispatcharr.Candidate) {
	s.reviewMu.Lock()
	defer s.reviewMu.Unlock()
	if s.review.dispatcharrFingerprint != dispatchFingerprint || s.review.lineupFingerprint != lineupFingerprint {
		return
	}
	delete(s.review.groups, key)
	for _, candidate := range candidates {
		delete(s.review.candidates, candidate.Key)
	}
}

func (s *dispatcharrServer) clearCandidateCache() {
	s.reviewMu.Lock()
	s.review = dispatcharrCandidateCache{}
	s.reviewMu.Unlock()
}

func (s *dispatcharrServer) saveWhileCurrent(dispatchConfig dispatcharr.Config, lineupConfig appconfig.Config, update func() error, w http.ResponseWriter) bool {
	lineupCurrent := false
	dispatchCurrent, err := s.config.WhileCurrent(dispatchConfig.Fingerprint(), func() error {
		var innerErr error
		lineupCurrent, innerErr = s.lineup.store.WhileCurrent(lineupConfig.Fingerprint(), update)
		return innerErr
	})
	if err != nil {
		http.Error(w, "Unable to save match review: "+err.Error(), http.StatusBadRequest)
		return false
	}
	if !dispatchCurrent || !lineupCurrent {
		http.Error(w, "A source changed while saving; reload match review", http.StatusConflict)
		return false
	}
	return true
}

func (c *dispatcharrStreamCache) get(ctx context.Context, client dispatcharrAPI, config dispatcharr.Config, force bool) ([]dispatcharr.Stream, time.Time, bool, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fingerprint := config.Fingerprint()
	if !force && c.fingerprint == fingerprint && !c.fetchedAt.IsZero() && time.Since(c.fetchedAt) < dispatcharrStreamCacheAge {
		return append([]dispatcharr.Stream(nil), c.streams...), c.fetchedAt, true, "", nil
	}
	streams, err := client.Streams(ctx, config)
	if err != nil {
		if c.fingerprint == fingerprint && !c.fetchedAt.IsZero() {
			return append([]dispatcharr.Stream(nil), c.streams...), c.fetchedAt, true, "The latest Dispatcharr refresh failed. Review is using the last successful stream list.", nil
		}
		return nil, time.Time{}, false, "", err
	}
	c.fingerprint = fingerprint
	c.fetchedAt = time.Now().UTC()
	c.streams = append([]dispatcharr.Stream(nil), streams...)
	return streams, c.fetchedAt, false, "", nil
}

func (c *dispatcharrStreamCache) clear() {
	c.mu.Lock()
	c.fingerprint = ""
	c.fetchedAt = time.Time{}
	c.streams = nil
	c.mu.Unlock()
}
