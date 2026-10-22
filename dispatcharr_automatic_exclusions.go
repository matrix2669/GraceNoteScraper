package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"github.com/daniel-widrick/GraceNoteScraper/providersource"
)

const automaticMatchPolicy = "current-snapshot-identity-v2"

var errAutomaticStale = errors.New("automatic matching inputs changed; reload the draft and refresh streams if required")

type automaticMatchRow struct {
	Key            string `json:"key"`
	ChannelID      string `json:"channelId"`
	ChannelNumber  string `json:"channelNumber"`
	ChannelName    string `json:"channelName"`
	StreamName     string `json:"streamName"`
	AccountID      int64  `json:"accountId"`
	Rule           string `json:"rule"`
	Reason         string `json:"reason"`
	PendingReview  bool   `json:"pendingReview,omitempty"`
	Conflict       bool   `json:"conflict,omitempty"`
	ManualDecision string `json:"manualDecision,omitempty"`
	Exported       bool   `json:"exported"`
	InventoryOnly  bool   `json:"inventoryOnly,omitempty"`
}
type automaticMatchView struct {
	Status    string              `json:"status"`
	Message   string              `json:"message"`
	Policy    string              `json:"policy"`
	FetchedAt time.Time           `json:"fetchedAt"`
	Rows      []automaticMatchRow `json:"rows"`
	Hidden    map[string]bool     `json:"-"`
}

func snapshotIdentity(snapshot dispatcharrMatchSnapshot) string {
	data, _ := json.Marshal([]any{snapshot.dispatcharrFingerprint, snapshot.lineupFingerprint, snapshot.version, snapshot.fetchedAt, snapshot.digest, snapshot.inventoryDigest, snapshot.warning, fmt.Sprintf("%p", snapshot.guide)})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// Feed facts come from the same already-eligible station-bound projection as
// aliases. Flattened strings, user names and unauditable historical evidence
// cannot become regional identity merely because a name contains West or P.
func stationFeedCatalog(draft *builder.Draft, config appconfig.Config) (map[string]dispatcharr.FeedIdentity, map[string]dispatcharr.FeedIdentity) {
	feeds := map[string]dispatcharr.FeedIdentity{}
	tentative := map[string]dispatcharr.FeedIdentity{}
	ambiguous := map[string]bool{}
	for _, channel := range draft.Channels {
		for _, alias := range channel.AliasEvidence {
			if parsed := dispatcharr.ParseFeedHint(alias.Value); parsed.Region != "" {
				parsed.Region = "unknown"
				parsed.Evidence = "Regional label lacks an unambiguous station-bound feed assertion"
				tentative[channel.ID] = parsed
			}
		}
		for _, fact := range channel.ServiceEvidence {
			currentKey := config.Gracenote.LineupID
			if config.Gracenote.Device != "" {
				currentKey += "@device=" + config.Gracenote.Device
			}
			bound := fact.BindingKind == "station-id-catalog-v1" || (fact.SourceID == providersource.OfficialSourceID(config.Gracenote.ProviderName) && fact.RootLineupKey == currentKey)
			if !bound || fact.RootStationID != channel.StationID || fact.SourceID == "" || fact.SourceRowID == "" || fact.SourceRevision == "" {
				continue
			}
			active := false
			for _, alias := range channel.AliasEvidence {
				if alias.Value == fact.Value {
					for _, source := range alias.Sources {
						if source == fact.SourceID {
							active = true
						}
					}
				}
			}
			if !active {
				continue
			}
			feed := dispatcharr.ParseStationFeedLabel(fact.Value)
			if feed.Region == "" {
				continue
			}
			if previous, ok := feeds[channel.ID]; ok && (previous.Family != feed.Family || previous.Region != feed.Region) {
				ambiguous[channel.ID] = true
			}
			// Equivalent family labels must come from the very same typed source
			// row/revision, never flattened aliases or provider positions.
			for _, related := range channel.ServiceEvidence {
				if related.RootStationID != fact.RootStationID || related.SourceID != fact.SourceID || related.SourceRowID != fact.SourceRowID || related.SourceRevision != fact.SourceRevision {
					continue
				}
				activeRelated := false
				for _, a := range channel.AliasEvidence {
					if a.Value == related.Value {
						for _, src := range a.Sources {
							if src == related.SourceID {
								activeRelated = true
							}
						}
					}
				}
				if !activeRelated {
					continue
				}
				family := dispatcharr.StreamFeed(related.Value, nil)
				if family.Region == "" && family.Family != "" {
					feed.FamilyAliases = append(feed.FamilyAliases, family.Family)
				}
			}
			feeds[channel.ID] = feed
		}
	}
	for key := range ambiguous {
		feed := feeds[key]
		feed.Region = "unknown"
		feed.Evidence = "Conflicting station-bound feed assertions require manual review"
		feeds[key] = feed
	}
	for key, feed := range tentative {
		if _, ok := feeds[key]; !ok {
			feeds[key] = feed
		}
	}
	codes := map[string]dispatcharr.FeedIdentity{}
	badCodes := map[string]bool{}
	for _, channel := range draft.Channels {
		feed, ok := feeds[channel.ID]
		if !ok || feed.Region == "unknown" {
			continue
		}
		code := strings.ToUpper(strings.TrimSpace(channel.CallSign))
		// A bare service-family label is not a regional station-code assertion.
		if code == "" || strings.EqualFold(code, strings.ReplaceAll(feed.Family, " ", "")) {
			continue
		}
		if old, ok := codes[code]; ok && (old.Family != feed.Family || old.Region != feed.Region) {
			badCodes[code] = true
		}
		codes[code] = feed
	}
	for code := range badCodes {
		delete(codes, code)
	}
	return feeds, codes
}

func (s *dispatcharrServer) automaticView(draft *builder.Draft, config appconfig.Config, snapshot dispatcharrMatchSnapshot) (automaticMatchView, dispatcharr.AutomaticResult) {
	view := automaticMatchView{Status: "refresh-required", Message: "Refresh streams to derive current channel-scoped exclusions. Saved manual decisions remain unchanged.", Policy: automaticMatchPolicy, Rows: []automaticMatchRow{}, Hidden: map[string]bool{}}
	empty := dispatcharr.AutomaticResult{Hidden: map[string]bool{}}
	conn, connected := s.config.Get()
	channels, included := dispatcharrSnapshotChannels(draft)
	valid := connected && snapshot.dispatcharrFingerprint == conn.Fingerprint() && snapshot.lineupFingerprint == config.Fingerprint() && !snapshot.fetchedAt.IsZero() && snapshot.warning == "" && snapshot.guide == s.lineup.state.GetForSource(config.Fingerprint()) && dispatcharrInputsSubset(channels, snapshot.channels)
	if !valid || s.inventoryDrift(snapshot) {
		return view, empty
	}
	targets := map[string]dispatcharr.AffiliateTarget{}
	for _, c := range draft.Channels {
		targets[c.ID] = dispatcharr.AffiliateTarget{Country: affiliateCountry(config.Gracenote.Country), CallSign: c.CallSign}
	}
	feeds, codes := stationFeedCatalog(draft, config)
	candidates := []dispatcharr.Candidate{}
	for _, c := range snapshot.candidates {
		if included[c.ChannelID] && c.Source == conn.Fingerprint() {
			candidates = append(candidates, c)
		}
	}
	stored := s.lineup.builder.MatchDecisions(config.Fingerprint())
	decisions := []dispatcharr.Decision{}
	for _, d := range stored {
		decisions = append(decisions, dispatcharr.Decision{ChannelID: d.ChannelID, Source: d.DispatcharrFingerprint, StreamHash: d.StreamFingerprint, StreamName: d.StreamName, NormalizedAlias: d.NormalizedAlias, Decision: d.Decision})
	}
	result := dispatcharr.DeriveAutomaticExclusions(candidates, targets, feeds, codes, decisions)
	feedTargets := []dispatcharr.FeedTarget{}
	for _, c := range draft.Channels {
		if included[c.ID] {
			feedTargets = append(feedTargets, dispatcharr.FeedTarget{ID: c.ID, Name: c.Name, Number: c.Number, Country: affiliateCountry(config.Gracenote.Country), Feed: feeds[c.ID]})
		}
	}
	result.InventoryRows = dispatcharr.DeriveInventoryFeedExclusions(conn.Fingerprint(), snapshot.identityStreams, feedTargets, codes, decisions, result.Rows)
	view.Status = "current"
	view.Message = "Definite opposing feeds are checked across this successful stream inventory; inventory-only exclusions have no match score. Uncertain scored feed names remain manual and withheld from JSON until confirmed. Saved confirmations win and conflicts are shown. No manual Deny records are created. Recomputed on snapshot, lineup, identity or decision changes; restart requires Refresh streams."
	view.FetchedAt = snapshot.fetchedAt
	view.Hidden = result.Hidden
	for _, row := range result.Rows {
		c := row.Candidate
		view.Rows = append(view.Rows, automaticMatchRow{Key: c.Key, ChannelID: c.ChannelID, ChannelNumber: c.ChannelNumber, ChannelName: c.ChannelName, StreamName: c.StreamName, AccountID: c.M3UAccountID, Rule: row.Rule, Reason: row.Reason, PendingReview: row.PendingReview, Conflict: row.Conflict, ManualDecision: row.ManualDecision, Exported: !row.Conflict})
	}
	for _, row := range result.InventoryRows {
		view.Rows = append(view.Rows, automaticMatchRow{Key: row.Key, ChannelID: row.ChannelID, ChannelNumber: row.ChannelNumber, ChannelName: row.ChannelName, StreamName: row.StreamName, AccountID: row.AccountID, Rule: row.Rule, Reason: row.Reason, Conflict: row.Conflict, ManualDecision: row.ManualDecision, Exported: !row.Conflict, InventoryOnly: true})
	}
	sort.Slice(view.Rows, func(i, j int) bool { return view.Rows[i].Key < view.Rows[j].Key })
	return view, result
}

func (s *dispatcharrServer) projectAutomaticExclusions(draft *builder.Draft, config appconfig.Config) {
	stateToken := s.lineup.builder.ReviewStateToken()
	inventoryToken := s.inventoryObservationToken()
	s.reviewMu.RLock()
	snapshot := s.snapshot
	s.reviewMu.RUnlock()
	view, result := s.automaticView(draft, config, snapshot)
	conn, connected := s.config.Get()
	fingerprint := ""
	if connected {
		fingerprint = conn.Fingerprint()
	}
	draft.AutomaticLease = &builder.AutomaticMatchLease{StateToken: stateToken, SnapshotToken: snapshotIdentity(snapshot), InventoryToken: inventoryToken, DispatchFingerprint: fingerprint, GuideToken: fmt.Sprintf("%p", s.lineup.state.GetForSource(config.Fingerprint()))}
	draft.AutomaticMatchingStatus = view.Status
	draft.AutomaticMatchingMessage = view.Message
	byID := map[string]*builder.DraftChannel{}
	for i := range draft.Channels {
		byID[draft.Channels[i].ID] = &draft.Channels[i]
	}
	appendExclusion := func(channelID, rawName string, conflict bool) {
		if conflict {
			return
		}
		channel := byID[channelID]
		if channel == nil {
			return
		}
		name := strings.Join(strings.Fields(rawName), " ")
		exists := false
		for _, old := range channel.ExcludedAliases {
			if strings.EqualFold(old, name) {
				exists = true
			}
		}
		if !exists {
			channel.ExcludedAliases = append(channel.ExcludedAliases, name)
			channel.AutomaticExcludedAliases = append(channel.AutomaticExcludedAliases, name)
			draft.AutomaticExclusionCount++
		}
	}
	for _, row := range result.Rows {
		appendExclusion(row.Candidate.ChannelID, row.Candidate.StreamName, row.Conflict)
	}
	for _, row := range result.InventoryRows {
		appendExclusion(row.ChannelID, row.StreamName, row.Conflict)
	}
	// A concurrent decision change makes this projection stale, never publishable.
	if stateToken != s.lineup.builder.ReviewStateToken() {
		draft.AutomaticLease.StateToken = "stale"
	}
	if inventoryToken != s.inventoryObservationToken() {
		draft.AutomaticLease.InventoryToken = "stale"
	}
	_ = builder.AttachDraftSignatures(draft)
}

// Lock order matches existing review writes: refresh -> connection -> source ->
// guide -> saved builder state -> snapshot -> inventory. No matcher/provider call occurs here.
func (s *dispatcharrServer) commitAutomaticDraft(draft *builder.Draft, action func() error) (bool, error) {
	if draft.AutomaticLease == nil {
		return s.lineup.store.WhileCurrent(draft.SourceFingerprint, action)
	}
	if !s.refreshMu.TryLock() {
		return false, errAutomaticStale
	}
	defer s.refreshMu.Unlock()
	lease := draft.AutomaticLease
	conn, connected := s.config.Get()
	fp := ""
	if connected {
		fp = conn.Fingerprint()
	}
	if fp != lease.DispatchFingerprint {
		return false, errAutomaticStale
	}
	commit := func() error {
		current, err := s.lineup.store.WhileCurrent(draft.SourceFingerprint, func() error {
			s.lineup.state.mu.RLock()
			defer s.lineup.state.mu.RUnlock()
			if fmt.Sprintf("%p", s.lineup.state.guide) != lease.GuideToken || s.lineup.state.sourceFingerprint != draft.SourceFingerprint {
				return errAutomaticStale
			}
			return s.lineup.builder.WhileReviewState(lease.StateToken, func() error {
				s.reviewMu.RLock()
				defer s.reviewMu.RUnlock()
				s.cache.mu.Lock()
				defer s.cache.mu.Unlock()
				if snapshotIdentity(s.snapshot) != lease.SnapshotToken || s.cache.observationTokenLocked() != lease.InventoryToken {
					return errAutomaticStale
				}
				return action()
			})
		})
		if !current && err == nil {
			return errAutomaticStale
		}
		return err
	}
	if connected {
		current, err := s.config.WhileCurrent(fp, commit)
		return current && err == nil, err
	}
	err := commit()
	return err == nil, err
}

func (s *dispatcharrServer) handleAutomaticExclusions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "automatic exclusions are derived; use existing Confirm/Deny for manual review", 405)
		return
	}
	build, ok := s.buildReview(w, r, false)
	if !ok {
		return
	}
	writeLineuparrJSON(w, 200, build.response.Automatic)
}
