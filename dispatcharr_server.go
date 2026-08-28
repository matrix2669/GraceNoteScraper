package main

import (
	"context"
	"net/http"
	"sort"
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
	Score         int       `json:"score"`
	Reason        string    `json:"reason"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type dispatcharrReviewResponse struct {
	StreamCount    int                         `json:"streamCount"`
	CandidateCount int                         `json:"candidateCount"`
	ConfirmedCount int                         `json:"confirmedCount"`
	DeniedCount    int                         `json:"deniedCount"`
	FetchedAt      time.Time                   `json:"fetchedAt"`
	Cached         bool                        `json:"cached"`
	Warning        string                      `json:"warning,omitempty"`
	Candidates     []dispatcharr.Candidate     `json:"candidates"`
	Decisions      []dispatcharrReviewDecision `json:"decisions"`
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
			Username:   config.Username,
		})
	case http.MethodPost:
		if !requireJSONContentType(w, r) {
			return
		}
		var body struct {
			BaseURL  string `json:"baseUrl"`
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !decodeLineuparrRequest(w, r, &body) {
			return
		}
		s.configMu.Lock()
		defer s.configMu.Unlock()
		existing, configured := s.config.Get()
		if strings.TrimSpace(body.Password) == "" && configured &&
			strings.TrimRight(strings.TrimSpace(body.BaseURL), "/") == existing.BaseURL &&
			strings.TrimSpace(body.Username) == existing.Username {
			body.Password = existing.Password
		}
		config, err := (dispatcharr.Config{
			BaseURL: body.BaseURL, Username: body.Username, Password: body.Password,
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
		writeLineuparrJSON(w, http.StatusOK, dispatcharrConfigResponse{
			Configured: true, BaseURL: config.BaseURL, Username: config.Username,
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
		Key      string `json:"key"`
		Decision string `json:"decision,omitempty"`
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
		existing := s.lineup.builder.MatchDecisions(lineupConfig.Fingerprint())[body.Key]
		if existing.Key == "" || existing.DispatcharrFingerprint != dispatchConfig.Fingerprint() {
			http.Error(w, "match decision does not belong to the active sources", http.StatusNotFound)
			return
		}
		if !s.saveWhileCurrent(dispatchConfig, lineupConfig, func() error {
			return s.lineup.builder.ClearMatchDecision(lineupConfig.Fingerprint(), body.Key)
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
	build, ok := s.buildReview(w, r, false)
	if !ok {
		return
	}
	var candidate *dispatcharr.Candidate
	for index := range build.candidates {
		if build.candidates[index].Key == body.Key {
			candidate = &build.candidates[index]
			break
		}
	}
	if candidate == nil {
		http.Error(w, "match candidate is no longer current", http.StatusConflict)
		return
	}
	decision := lineuparrbuilder.MatchDecision{
		Key: candidate.Key, Decision: body.Decision,
		DispatcharrFingerprint: candidate.Source, StreamFingerprint: candidate.StreamHash,
		StreamKey: candidate.StreamKey, StreamID: candidate.StreamID, M3UAccountID: candidate.M3UAccountID,
		ChannelID: candidate.ChannelID, ChannelNumber: candidate.ChannelNumber, ChannelName: candidate.ChannelName,
		StreamName: candidate.StreamName, TVGID: candidate.TVGID, Score: candidate.Score, Reason: candidate.Reason,
	}
	if !s.saveWhileCurrent(build.dispatchConfig, build.lineupConfig, func() error {
		return s.lineup.builder.SetMatchDecision(build.lineupConfig.Fingerprint(), decision)
	}, w) {
		return
	}
	writeLineuparrJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

func (s *dispatcharrServer) buildReview(w http.ResponseWriter, r *http.Request, force bool) (dispatcharrReviewBuild, bool) {
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
			ID: channel.ID, Number: channel.Number, Name: channel.Name,
			Aliases: append([]string(nil), channel.Aliases...), EPGIDs: append([]string(nil), channel.EPGIDs...),
		})
	}
	stored := s.lineup.builder.MatchDecisions(lineupConfig.Fingerprint())
	matcherDecisions := make(map[string]dispatcharr.Decision, len(stored))
	history := make([]dispatcharrReviewDecision, 0)
	confirmed := 0
	denied := 0
	for key, decision := range stored {
		matcherDecisions[key] = dispatcharr.Decision{
			Key: key, Decision: decision.Decision, Source: decision.DispatcharrFingerprint,
			StreamHash: decision.StreamFingerprint, ChannelID: decision.ChannelID,
		}
		if decision.DispatcharrFingerprint != dispatchConfig.Fingerprint() {
			continue
		}
		if decision.Decision == "confirmed" {
			confirmed++
		} else if decision.Decision == "denied" {
			denied++
		}
		history = append(history, dispatcharrReviewDecision{
			Key: decision.Key, Decision: decision.Decision,
			ChannelID: decision.ChannelID, ChannelNumber: decision.ChannelNumber, ChannelName: decision.ChannelName,
			StreamID: decision.StreamID, StreamName: decision.StreamName, TVGID: decision.TVGID,
			M3UAccountID: decision.M3UAccountID, Score: decision.Score, Reason: decision.Reason, UpdatedAt: decision.UpdatedAt,
		})
	}
	sort.SliceStable(history, func(i, j int) bool { return history[i].UpdatedAt.After(history[j].UpdatedAt) })
	if len(history) > dispatcharrReviewLimit {
		history = history[:dispatcharrReviewLimit]
	}
	candidates := dispatcharr.MatchStreams(dispatchConfig.Fingerprint(), channels, streams, matcherDecisions)
	visible := candidates
	if len(visible) > dispatcharrReviewLimit {
		visible = visible[:dispatcharrReviewLimit]
	}
	return dispatcharrReviewBuild{
		response: dispatcharrReviewResponse{
			StreamCount: len(streams), CandidateCount: len(candidates), ConfirmedCount: confirmed, DeniedCount: denied,
			FetchedAt: fetchedAt, Cached: cached, Warning: warning, Candidates: visible, Decisions: history,
		},
		candidates: candidates, dispatchConfig: dispatchConfig, lineupConfig: lineupConfig,
	}, true
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
