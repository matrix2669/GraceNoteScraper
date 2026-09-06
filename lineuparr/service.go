package lineuparr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/daniel-widrick/GraceNoteScraper/channelcategory"
)

const (
	uncategorized              = "Uncategorized"
	lineuparrExactMatchMinimum = 95
)

type Service struct {
	store   *StateStore
	options ServiceOptions
	buildMu sync.Mutex
}

func NewService(store *StateStore, options ServiceOptions) *Service {
	if options.CacheDir == "" {
		options.CacheDir = "lineuparr_source_cache"
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Service{store: store, options: options}
}

type aliasWork struct {
	value   string
	sources map[string]bool
	methods map[string]bool
}

type channelWork struct {
	draft            DraftChannel
	input            InputChannel
	aliases          map[string]*aliasWork
	excludedAliases  map[string]string
	epgIDs           map[string]*aliasWork
	matchedSourceSet map[string]bool
}

func (s *Service) Build(ctx context.Context, lineup LineupContext, inputs []InputChannel) (*Draft, error) {
	if lineup.SourceFingerprint == "" {
		return nil, errors.New("active lineup fingerprint is required")
	}
	s.buildMu.Lock()
	defer s.buildMu.Unlock()

	overrides := s.store.Snapshot(lineup.SourceFingerprint)
	matchDecisions := s.store.MatchDecisionSnapshot(lineup.SourceFingerprint)
	channels := make([]*channelWork, 0, len(inputs))
	seenKeys := make(map[string]int)
	for _, input := range inputs {
		input = normalizeInput(input)
		if input.Key == "" {
			continue
		}
		baseKey := input.Key
		if count := seenKeys[baseKey]; count > 0 {
			input.Key = fmt.Sprintf("%s-%d", baseKey, count+1)
		}
		seenKeys[baseKey]++
		channels = append(channels, newChannelWork(input))
	}

	statuses := []SourceStatus{{
		ID:      "gracenote",
		Label:   "Gracenote active lineup",
		Status:  "active",
		Matched: len(channels),
		Message: "Authoritative lineup membership, channel numbers, callsigns, station IDs, and affiliate names",
	}}
	statuses = append(statuses, lineup.AdditionalSources...)

	for _, source := range s.fetchSources(ctx, lineup) {
		status := SourceStatus{
			ID:      source.ID,
			Label:   source.Label,
			URL:     publicSourceURL(source.URL),
			Status:  source.Status,
			Message: source.Message,
		}
		if source.Err == nil {
			switch source.Kind {
			case "catalog":
				var catalog catalogFile
				if err := json.Unmarshal(source.Data, &catalog); err != nil || len(catalog.Categories) == 0 {
					status.Status = "error"
					if err != nil {
						status.Message = "Could not decode this Lineuparr catalog: " + err.Error()
					} else {
						status.Message = "This JSON file does not contain Lineuparr categories"
					}
				} else {
					if strings.TrimSpace(catalog.Package) != "" {
						status.Label = "Lineuparr catalog: " + strings.TrimSpace(catalog.Package)
					}
					status.Matched, status.Ambiguous = applyCatalog(channels, catalog, source.ID, status.Label)
				}
			case "iptv-org":
				var entries []iptvOrgChannel
				if err := json.Unmarshal(source.Data, &entries); err != nil {
					status.Status = "error"
					status.Message = "Could not decode the iptv-org channel database: " + err.Error()
				} else {
					status.Matched, status.Ambiguous = applyIPTVOrg(channels, entries, countryAlpha2(lineup.Country), source.ID, status.Label)
				}
			}
		}
		statuses = append(statuses, status)
	}

	categoryHintMatches := make(map[string]int)
	categoryHintLabels := make(map[string]string)
	for _, channel := range channels {
		hint := channel.input.CategoryHint
		if hint == nil || channel.draft.CategorySource != "unresolved" {
			continue
		}
		channel.draft.Category = hint.Value
		channel.draft.CategorySource = hint.Source
		channel.draft.CategoryMethod = hint.Method
		channel.draft.CategoryPriority = hint.Priority
		channel.matchedSourceSet[hint.Source] = true
		categoryHintMatches[hint.Source]++
		categoryHintLabels[hint.Source] = hint.Label
	}
	categoryHintSources := make([]string, 0, len(categoryHintMatches))
	for source := range categoryHintMatches {
		categoryHintSources = append(categoryHintSources, source)
	}
	sort.Strings(categoryHintSources)
	for _, source := range categoryHintSources {
		label := categoryHintLabels[source]
		if label == "" {
			label = source
		}
		statuses = append(statuses, SourceStatus{
			ID: source, Label: label, Status: "derived", Matched: categoryHintMatches[source],
			Message: "Applied only when one Gracenote programme filter covers at least 70% of scheduled minutes; exact catalog and user categories take precedence",
		})
	}
	channelByID := make(map[string]*channelWork, len(channels))
	for _, channel := range channels {
		channelByID[channel.draft.ID] = channel
	}
	confirmedMatches := 0
	confirmedAliases := 0
	excludedMatches := 0
	for _, group := range groupMatchDecisions(matchDecisions) {
		channel := channelByID[group.ChannelID]
		if channel == nil {
			continue
		}
		switch group.Decision {
		case "confirmed":
			if group.NameScore < lineuparrExactMatchMinimum {
				channel.addAlias(group.Alias, "dispatcharr-confirmed", "user-confirmed M3U stream match below Lineuparr Exact threshold")
				confirmedAliases++
			}
			for _, tvgID := range group.TVGIDs {
				channel.addEPGID(tvgID, "dispatcharr-confirmed", "user-confirmed M3U tvg-id")
			}
			channel.matchedSourceSet["dispatcharr-confirmed"] = true
			confirmedMatches++
		}
	}
	// Negative matching uses full names, unlike positive review-group aliases.
	// Check each saved constituent's own name score before deduplication; a
	// stronger sibling or EPG-ID score must not make a weak name eligible.
	decisionKeys := make([]string, 0, len(matchDecisions))
	for key := range matchDecisions {
		decisionKeys = append(decisionKeys, key)
	}
	sort.Strings(decisionKeys)
	for _, key := range decisionKeys {
		decision := matchDecisions[key]
		channel := channelByID[decision.ChannelID]
		if channel != nil && decision.Decision == "denied" && decisionNameScore(decision) >= lineuparrExactMatchMinimum {
			if channel.addExcludedAlias(decision.StreamName) {
				channel.matchedSourceSet["dispatcharr-denied"] = true
				excludedMatches++
			}
		}
	}
	if confirmedMatches > 0 {
		statuses = append(statuses, SourceStatus{
			ID: "dispatcharr-confirmed", Label: "Confirmed Dispatcharr M3U matches", Status: "saved", Matched: confirmedMatches,
			Message: fmt.Sprintf("%d reviewed groups; %d names below 95%% were retained as aliases, while higher-scoring names rely on Lineuparr Exact matching", confirmedMatches, confirmedAliases),
		})
	}
	if excludedMatches > 0 {
		statuses = append(statuses, SourceStatus{
			ID: "dispatcharr-denied", Label: "Denied Dispatcharr M3U matches", Status: "saved", Matched: excludedMatches,
			Message: fmt.Sprintf("%d denied names at or above 95%% were retained as channel-scoped excluded_aliases", excludedMatches),
		})
	}

	resultChannels := make([]DraftChannel, 0, len(channels))
	for _, channel := range channels {
		finalizeChannel(channel)
		if channel.draft.Category != uncategorized && channel.draft.CategoryPriority == 0 {
			channel.draft.CategoryPriority = 2
		}
		channel.draft.NeedsCategoryReview = channel.draft.Category != uncategorized && channel.draft.CategoryPriority >= 3
		if override, ok := overrides[channel.draft.ID]; ok {
			if override.Included != nil {
				channel.draft.Included = *override.Included
			}
			if category := cleanCategory(override.Category); category != "" {
				channel.draft.Category = category
				channel.draft.CategorySource = "user"
				channel.draft.CategoryMethod = "user edit"
				channel.draft.CategoryPriority = 1
				channel.draft.NeedsCategoryReview = false
				channel.draft.CategoryReview = override.CategoryReview
			}
			applyAliasSuppressions(&channel.draft, override.SuppressedAliases)
		}
		resultChannels = append(resultChannels, channel.draft)
	}
	sort.SliceStable(resultChannels, func(i, j int) bool {
		if resultChannels[i].Number != resultChannels[j].Number {
			return numberLess(resultChannels[i].Number, resultChannels[j].Number)
		}
		return strings.ToLower(resultChannels[i].Name) < strings.ToLower(resultChannels[j].Name)
	})

	duplicates := findDuplicateSuggestions(resultChannels)
	duplicateByID := make(map[string]DuplicateSuggestion, len(duplicates))
	for _, suggestion := range duplicates {
		duplicateByID[suggestion.RemoveID] = suggestion
	}
	for index := range resultChannels {
		if suggestion, ok := duplicateByID[resultChannels[index].ID]; ok {
			resultChannels[index].DuplicateOf = suggestion.KeepID
			resultChannels[index].DuplicateReason = suggestion.Reason
		}
	}
	statuses = consolidateSourceStatuses(statuses)
	populateSourceMatches(statuses, resultChannels)

	draft := &Draft{
		GeneratedAt:          time.Now().UTC(),
		Package:              packageName(lineup),
		ProviderName:         lineup.ProviderName,
		PostalCode:           lineup.PostalCode,
		LineupID:             lineup.LineupID,
		CountryCode:          countryAlpha2(lineup.Country),
		Channels:             resultChannels,
		DuplicateSuggestions: duplicates,
		DuplicateGroups:      duplicateReviewGroups(resultChannels, duplicates),
		Sources:              statuses,
		Categories:           append(channelcategory.Categories(), uncategorized),
		Total:                len(resultChannels),
	}
	for _, channel := range resultChannels {
		if channel.Included {
			draft.Included++
			if channel.Category == uncategorized {
				draft.Uncategorized++
			} else {
				draft.Categorized++
			}
		} else {
			draft.Excluded++
		}
		draft.AliasCount += len(channel.Aliases)
	}
	return draft, nil
}

func consolidateSourceStatuses(statuses []SourceStatus) []SourceStatus {
	result := make([]SourceStatus, 0, len(statuses))
	byKey := make(map[string]int)
	for _, status := range statuses {
		status.ID = strings.TrimSpace(status.ID)
		if status.ID == "" {
			continue
		}
		status.RelatedIDs = appendUniqueStringFold(status.RelatedIDs, status.ID)
		key := sourceStatusFamily(status.ID)
		index, exists := byKey[key]
		if !exists {
			byKey[key] = len(result)
			result = append(result, status)
			continue
		}
		current := &result[index]
		current.RelatedIDs = appendUniqueStringFold(current.RelatedIDs, status.ID)
		for _, id := range status.RelatedIDs {
			current.RelatedIDs = appendUniqueStringFold(current.RelatedIDs, id)
		}
		replacingRegistration := strings.HasPrefix(current.ID, "provider-guide-") && !strings.HasPrefix(status.ID, "provider-guide-")
		if replacingRegistration {
			current.ID = status.ID
			current.Label = status.Label
			current.Status = status.Status
		}
		if sourceURLIsDocument(status.URL) || current.URL == "" {
			current.URL = status.URL
		}
		if !replacingRegistration && sourceStatusRank(status.Status) > sourceStatusRank(current.Status) {
			current.Status = status.Status
		}
		current.Matched = max(current.Matched, status.Matched)
		current.Ambiguous = max(current.Ambiguous, status.Ambiguous)
		if current.Access == "" {
			current.Access = status.Access
		}
		if current.LocationMode == "" {
			current.LocationMode = status.LocationMode
		}
		current.Message = appendUniqueMessage(current.Message, status.Message)
	}
	return result
}

func sourceStatusFamily(id string) string {
	value := strings.ToLower(strings.TrimSpace(id))
	provider := strings.TrimPrefix(value, "provider-guide-")
	provider = strings.TrimSuffix(provider, "-official-lineup")
	provider = strings.TrimSuffix(provider, "-official-guide")
	switch provider {
	case "verizon-fios", "directv", "dish", "optimum", "afn", "glorystar", "att-uverse", "xfinity", "spectrum", "broadstar":
		return "provider:" + provider
	default:
		return value
	}
}

func sourceStatusRank(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active", "complete", "saved", "live", "cached", "local":
		return 4
	case "derived", "maintained":
		return 3
	case "registered", "no-matches":
		return 2
	case "error":
		return 1
	default:
		return 0
	}
}

func sourceURLIsDocument(rawURL string) bool {
	clean := strings.ToLower(strings.TrimSpace(rawURL))
	if index := strings.IndexByte(clean, '?'); index >= 0 {
		clean = clean[:index]
	}
	return strings.HasSuffix(clean, ".pdf")
}

func appendUniqueMessage(current, addition string) string {
	addition = strings.TrimSpace(addition)
	if addition == "" || strings.Contains(current, addition) {
		return current
	}
	if strings.TrimSpace(current) == "" {
		return addition
	}
	return current + " · " + addition
}

func appendUniqueStringFold(values []string, value string) []string {
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

func populateSourceMatches(statuses []SourceStatus, channels []DraftChannel) {
	providerIDs := make(map[string]bool)
	for _, status := range statuses {
		if strings.HasPrefix(sourceStatusFamily(status.ID), "provider:") {
			for _, id := range append(append([]string(nil), status.RelatedIDs...), status.ID) {
				providerIDs[strings.ToLower(strings.TrimSpace(id))] = true
			}
		}
	}
	for index := range statuses {
		ids := make(map[string]bool)
		for _, id := range append(append([]string(nil), statuses[index].RelatedIDs...), statuses[index].ID) {
			ids[strings.ToLower(strings.TrimSpace(id))] = true
		}
		for _, channel := range channels {
			match, aliases, epgIDs, methods := sourceMatchForChannel(statuses[index].ID, ids, providerIDs, channel)
			if !match {
				continue
			}
			statuses[index].Matches = append(statuses[index].Matches, SourceMatch{
				ChannelID: channel.ID, Number: channel.Number, CallSign: channel.CallSign, Name: channel.Name,
				Category: channel.Category, Aliases: aliases, EPGIDs: epgIDs, Methods: methods,
			})
		}
		if len(statuses[index].Matches) > 0 {
			statuses[index].Matched = len(statuses[index].Matches)
		}
	}
}

func sourceMatchForChannel(statusID string, ids, providerIDs map[string]bool, channel DraftChannel) (bool, []string, []string, []string) {
	if statusID == "gracenote" {
		return true, nil, append([]string(nil), channel.EPGIDs...), []string{"active Gracenote lineup position"}
	}
	matchesID := func(value string) bool { return ids[strings.ToLower(strings.TrimSpace(value))] }
	marketSummary := statusID == "gracenote-market-index"
	matched := false
	aliases := make([]string, 0)
	epgIDs := make([]string, 0)
	methods := make([]string, 0)
	for _, source := range channel.MatchedSources {
		key := strings.ToLower(strings.TrimSpace(source))
		if matchesID(source) || (marketSummary && providerIDs[key]) {
			matched = true
		}
	}
	for _, evidence := range channel.AliasEvidence {
		for _, source := range evidence.Sources {
			key := strings.ToLower(strings.TrimSpace(source))
			if !matchesID(source) && !(marketSummary && providerIDs[key]) {
				continue
			}
			matched = true
			aliases = appendUniqueStringFold(aliases, evidence.Value)
			for _, method := range evidence.Methods {
				methods = appendUniqueStringFold(methods, method)
			}
		}
	}
	for _, evidence := range channel.EPGIDEvidence {
		for _, source := range evidence.Sources {
			key := strings.ToLower(strings.TrimSpace(source))
			if !matchesID(source) && !(marketSummary && providerIDs[key]) {
				continue
			}
			matched = true
			epgIDs = appendUniqueStringFold(epgIDs, evidence.Value)
			for _, method := range evidence.Methods {
				methods = appendUniqueStringFold(methods, method)
			}
		}
	}
	if matchesID(channel.CategorySource) {
		matched = true
		methods = appendUniqueStringFold(methods, channel.CategoryMethod)
	}
	return matched, aliases, epgIDs, methods
}

func (s *Service) UpdateChannel(fingerprint, channelID string, update ChannelUpdate) error {
	if update.Included == nil && update.Category == nil {
		return errors.New("included or category is required")
	}
	if update.Category != nil {
		category := cleanCategory(*update.Category)
		if category == "" {
			return errors.New("category must be one of the master categories or Uncategorized")
		}
		update.Category = &category
	}
	return s.store.Update(fingerprint, channelID, update)
}

func (s *Service) UpdateChannelsCategory(fingerprint string, channelIDs []string, category string) error {
	category = cleanCategory(category)
	if category == "" {
		category = uncategorized
	}
	if len(category) > 80 {
		return errors.New("category must be 80 characters or fewer")
	}
	return s.store.SetCategory(fingerprint, channelIDs, category)
}

func (s *Service) RemoveSuggestedDuplicates(fingerprint string, draft *Draft) error {
	ids := make([]string, 0, len(draft.DuplicateSuggestions))
	for _, suggestion := range draft.DuplicateSuggestions {
		if suggestion.Exact {
			continue
		}
		ids = append(ids, suggestion.RemoveID)
	}
	return s.RemoveSuggestedDuplicateIDs(fingerprint, draft, ids)
}

func (s *Service) RemoveSuggestedDuplicateIDs(fingerprint string, draft *Draft, requested []string) error {
	allowed := make(map[string]bool, len(draft.DuplicateSuggestions))
	for _, suggestion := range draft.DuplicateSuggestions {
		allowed[suggestion.RemoveID] = true
		allowed[suggestion.KeepID] = true
	}
	ids := make([]string, 0, len(requested))
	seen := make(map[string]bool, len(requested))
	for _, id := range requested {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if !allowed[id] {
			return fmt.Errorf("channel %q is not a current duplicate suggestion", id)
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	for _, group := range duplicateReviewGroups(draft.Channels, draft.DuplicateSuggestions) {
		remaining := false
		touched := false
		for _, id := range group.ChannelIDs {
			if seen[id] {
				touched = true
				continue
			}
			for _, channel := range draft.Channels {
				if channel.ID == id && channel.Included {
					remaining = true
				}
			}
		}
		if touched && !remaining {
			return errors.New("keep at least one included position in each duplicate group")
		}
	}
	return s.store.SetIncluded(fingerprint, ids, false)
}

func (s *Service) RestoreAll(fingerprint string) error {
	return s.store.RestoreAll(fingerprint)
}

func (s *Service) SetAliasSuppressed(fingerprint, channelID, alias string, suppressed bool) error {
	alias = cleanText(alias)
	if len(alias) > 512 {
		return errors.New("alias must be 512 characters or fewer")
	}
	return s.store.SetAliasSuppressed(fingerprint, channelID, alias, suppressed)
}

func (s *Service) MatchDecisions(fingerprint string) map[string]MatchDecision {
	return s.store.MatchDecisionSnapshot(fingerprint)
}

func (s *Service) SetMatchDecision(fingerprint string, decision MatchDecision) error {
	return s.SetMatchDecisions(fingerprint, []MatchDecision{decision})
}

func (s *Service) SetMatchDecisions(fingerprint string, decisions []MatchDecision) error {
	if len(decisions) == 0 {
		return errors.New("at least one match decision is required")
	}
	now := time.Now().UTC()
	normalized := make([]MatchDecision, 0, len(decisions))
	for _, decision := range decisions {
		cleaned, err := normalizeMatchDecision(decision, now)
		if err != nil {
			return err
		}
		normalized = append(normalized, cleaned)
	}
	return s.store.SetMatchDecisions(fingerprint, normalized)
}

func normalizeMatchDecision(decision MatchDecision, updatedAt time.Time) (MatchDecision, error) {
	decision.Key = strings.TrimSpace(decision.Key)
	decision.Decision = strings.ToLower(strings.TrimSpace(decision.Decision))
	decision.DispatcharrFingerprint = strings.TrimSpace(decision.DispatcharrFingerprint)
	decision.StreamFingerprint = strings.TrimSpace(decision.StreamFingerprint)
	decision.StreamKey = strings.TrimSpace(decision.StreamKey)
	decision.ChannelID = strings.TrimSpace(decision.ChannelID)
	decision.StreamName = cleanText(decision.StreamName)
	decision.NormalizedAlias = cleanText(decision.NormalizedAlias)
	decision.TVGID = cleanText(decision.TVGID)
	decision.ChannelName = cleanText(decision.ChannelName)
	decision.ChannelNumber = cleanText(decision.ChannelNumber)
	decision.Reason = cleanText(decision.Reason)
	if decision.Decision != "confirmed" && decision.Decision != "denied" {
		return MatchDecision{}, errors.New("match decision must be confirmed or denied")
	}
	if decision.Key == "" || decision.DispatcharrFingerprint == "" || decision.StreamFingerprint == "" || decision.StreamKey == "" || decision.ChannelID == "" || decision.StreamName == "" {
		return MatchDecision{}, errors.New("match decision is incomplete")
	}
	if len(decision.StreamName) > 512 || len(decision.NormalizedAlias) > 512 || len(decision.TVGID) > 255 || len(decision.Reason) > 200 {
		return MatchDecision{}, errors.New("match decision metadata is too long")
	}
	if decision.NameScore < 0 || decision.NameScore > 100 {
		return MatchDecision{}, errors.New("match decision name score must be between 0 and 100")
	}
	decision.UpdatedAt = updatedAt
	return decision, nil
}

type matchDecisionGroup struct {
	Decision  string
	ChannelID string
	Alias     string
	NameScore int
	TVGIDs    []string
}

func groupMatchDecisions(decisions map[string]MatchDecision) []matchDecisionGroup {
	grouped := make(map[string]*matchDecisionGroup)
	for _, decision := range decisions {
		alias := cleanText(decision.StreamName)
		if alias == "" {
			continue
		}
		normalized := strings.ToLower(cleanText(decision.NormalizedAlias))
		if normalized == "" {
			normalized = strings.ToLower(alias)
		}
		key := decision.Decision + "\x00" + decision.ChannelID + "\x00" + normalized
		group := grouped[key]
		if group == nil {
			group = &matchDecisionGroup{Decision: decision.Decision, ChannelID: decision.ChannelID, Alias: alias}
			grouped[key] = group
		}
		if preferredMatchAlias(alias, group.Alias) {
			group.Alias = alias
		}
		group.NameScore = max(group.NameScore, decisionNameScore(decision))
		if decision.Decision == "confirmed" {
			group.TVGIDs = appendUniqueFold(group.TVGIDs, cleanText(decision.TVGID))
		}
	}

	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]matchDecisionGroup, 0, len(keys))
	for _, key := range keys {
		result = append(result, *grouped[key])
	}
	return result
}

func decisionNameScore(decision MatchDecision) int {
	if decision.NameScore > 0 {
		return min(100, decision.NameScore)
	}
	if strings.EqualFold(strings.TrimSpace(decision.Reason), "Exact EPG ID") {
		return 0
	}
	score := decision.Score
	if strings.Contains(strings.ToLower(decision.Reason), "+ channel number") {
		score -= 4
	}
	return max(0, min(100, score))
}

func preferredMatchAlias(candidate, current string) bool {
	candidate = strings.TrimSpace(candidate)
	current = strings.TrimSpace(current)
	if current == "" || len([]rune(candidate)) != len([]rune(current)) {
		return current == "" || len([]rune(candidate)) < len([]rune(current))
	}
	return strings.ToLower(candidate) < strings.ToLower(current)
}

func appendUniqueFold(values []string, value string) []string {
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

func (s *Service) ClearMatchDecision(fingerprint, key string) error {
	return s.store.ClearMatchDecision(fingerprint, strings.TrimSpace(key))
}

func (s *Service) ClearMatchDecisions(fingerprint string, keys []string) error {
	cleaned := make([]string, 0, len(keys))
	for _, key := range keys {
		if key = strings.TrimSpace(key); key != "" {
			cleaned = append(cleaned, key)
		}
	}
	return s.store.ClearMatchDecisions(fingerprint, cleaned)
}

func applyAliasSuppressions(channel *DraftChannel, suppressed []string) {
	if len(suppressed) == 0 || len(channel.AliasEvidence) == 0 {
		return
	}
	suppressedSet := make(map[string]bool, len(suppressed))
	for _, alias := range suppressed {
		suppressedSet[strings.ToLower(cleanText(alias))] = true
	}
	aliases := make([]string, 0, len(channel.Aliases))
	evidence := make([]AliasEvidence, 0, len(channel.AliasEvidence))
	removed := make([]AliasEvidence, 0)
	for _, item := range channel.AliasEvidence {
		if suppressedSet[strings.ToLower(item.Value)] {
			removed = append(removed, item)
			continue
		}
		evidence = append(evidence, item)
		aliases = append(aliases, item.Value)
	}
	channel.Aliases = aliases
	channel.AliasEvidence = evidence
	channel.SuppressedAliasEvidence = removed
}

func normalizeInput(input InputChannel) InputChannel {
	input.StationID = strings.TrimSpace(input.StationID)
	input.PlacementID = strings.TrimSpace(input.PlacementID)
	input.Number = strings.TrimSpace(input.Number)
	input.CallSign = cleanText(input.CallSign)
	input.Affiliate = cleanText(input.Affiliate)
	if input.Key == "" {
		input.Key = input.PlacementID
	}
	if input.Key == "" {
		input.Key = strings.Join([]string{input.StationID, input.Number, input.CallSign}, "|")
	}
	input.Key = strings.TrimSpace(input.Key)
	seen := make(map[string]bool)
	events := make([]string, 0, len(input.EventCallSigns))
	for _, eventCallSign := range input.EventCallSigns {
		eventCallSign = cleanText(eventCallSign)
		key := strings.ToLower(eventCallSign)
		if eventCallSign == "" || seen[key] {
			continue
		}
		seen[key] = true
		events = append(events, eventCallSign)
	}
	input.EventCallSigns = events
	if input.PreferredName != nil {
		preferred := *input.PreferredName
		preferred.Value = cleanText(preferred.Value)
		preferred.Source = strings.TrimSpace(preferred.Source)
		preferred.Method = strings.TrimSpace(preferred.Method)
		if preferred.Value == "" || preferred.Source == "" || preferred.Method == "" {
			input.PreferredName = nil
		} else {
			input.PreferredName = &preferred
		}
	}
	if input.CategoryHint != nil {
		hint := *input.CategoryHint
		hint.Value = cleanCategory(hint.Value)
		hint.Source = strings.TrimSpace(hint.Source)
		hint.Label = cleanText(hint.Label)
		hint.Method = cleanText(hint.Method)
		if hint.Value == "" || hint.Value == uncategorized || hint.Source == "" || hint.Method == "" {
			input.CategoryHint = nil
		} else {
			input.CategoryHint = &hint
		}
	}
	external := make([]AttributedAlias, 0, len(input.ExternalAliases))
	for _, alias := range input.ExternalAliases {
		alias.Value = cleanText(alias.Value)
		alias.Source = strings.TrimSpace(alias.Source)
		alias.Method = strings.TrimSpace(alias.Method)
		if alias.Value == "" || alias.Source == "" || alias.Method == "" {
			continue
		}
		external = append(external, alias)
	}
	input.ExternalAliases = external
	return input
}

func newChannelWork(input InputChannel) *channelWork {
	name := input.CallSign
	nameMethod := "channel callsign"
	if name == "" && safeAffiliate(input.Affiliate) {
		name = input.Affiliate
		nameMethod = "safe affiliate name"
	}
	if name == "" {
		name = "Channel " + input.Number
		nameMethod = "generated fallback"
	}
	channel := &channelWork{
		input: input,
		draft: DraftChannel{
			ID:             input.Key,
			StationID:      input.StationID,
			PlacementID:    input.PlacementID,
			Number:         input.Number,
			Name:           name,
			OriginalName:   name,
			CallSign:       input.CallSign,
			Affiliate:      input.Affiliate,
			Category:       uncategorized,
			Included:       true,
			NameSource:     "gracenote",
			NameMethod:     nameMethod,
			CategorySource: "unresolved",
		},
		aliases:          make(map[string]*aliasWork),
		excludedAliases:  make(map[string]string),
		epgIDs:           make(map[string]*aliasWork),
		matchedSourceSet: map[string]bool{"gracenote": true},
	}
	if category, ok := channelcategory.ResolveIdentity(input.CallSign, input.Affiliate, input.EventCallSigns...); ok {
		channel.draft.Category = category.Category
		channel.draft.CategorySource = "gracenote"
		channel.draft.CategoryMethod = category.Method
	}
	channel.addAlias(input.CallSign, "gracenote", "channel callsign")
	channel.addAlias(input.StationID, "gracenote", "station ID")
	channel.addAlias(input.PlacementID, "gracenote", "lineup position ID")
	if input.Number != "" && input.CallSign != "" {
		channel.addAlias(input.Number+" "+input.CallSign, "gracenote", "channel number plus callsign")
	}
	if safeAffiliate(input.Affiliate) {
		channel.addAlias(input.Affiliate, "gracenote", "affiliate name")
	}
	for _, eventCallSign := range input.EventCallSigns {
		channel.addAlias(eventCallSign, "gracenote", "event callsign")
	}
	for _, alias := range input.ExternalAliases {
		channel.addAlias(alias.Value, alias.Source, alias.Method)
	}
	if input.PreferredName != nil {
		channel.draft.Name = input.PreferredName.Value
		channel.draft.NameSource = input.PreferredName.Source
		channel.draft.NameMethod = input.PreferredName.Method
		channel.matchedSourceSet[input.PreferredName.Source] = true
		channel.addAlias(input.PreferredName.Value, input.PreferredName.Source, input.PreferredName.Method)
	}
	if input.StationID != "" {
		channel.addEPGID(input.StationID, "gracenote", "station ID")
	}
	return channel
}

func (c *channelWork) addAlias(value, source, method string) {
	value = cleanText(value)
	if value == "" || strings.EqualFold(value, "null") {
		return
	}
	key := strings.ToLower(value)
	evidence, ok := c.aliases[key]
	if !ok {
		evidence = &aliasWork{value: value, sources: make(map[string]bool), methods: make(map[string]bool)}
		c.aliases[key] = evidence
	}
	if source != "" {
		evidence.sources[source] = true
	}
	if method != "" {
		evidence.methods[method] = true
	}
}

func (c *channelWork) addExcludedAlias(value string) bool {
	value = cleanText(value)
	if value == "" {
		return false
	}
	key := strings.ToLower(value)
	if _, exists := c.excludedAliases[key]; !exists {
		c.excludedAliases[key] = value
		return true
	}
	return false
}

func (c *channelWork) addEPGID(value, source, method string) {
	value = cleanText(value)
	if value == "" || strings.EqualFold(value, "null") {
		return
	}
	key := strings.ToLower(value)
	evidence, ok := c.epgIDs[key]
	if !ok {
		evidence = &aliasWork{value: value, sources: make(map[string]bool), methods: make(map[string]bool)}
		c.epgIDs[key] = evidence
	}
	if source != "" {
		evidence.sources[source] = true
	}
	if method != "" {
		evidence.methods[method] = true
	}
}

func finalizeChannel(channel *channelWork) {
	aliases := make([]string, 0, len(channel.aliases))
	evidence := make([]AliasEvidence, 0, len(channel.aliases))
	for _, alias := range channel.aliases {
		aliases = append(aliases, alias.value)
		sources := mapKeys(alias.sources)
		methods := mapKeys(alias.methods)
		evidence = append(evidence, AliasEvidence{Value: alias.value, Sources: sources, Methods: methods})
	}
	sort.SliceStable(aliases, func(i, j int) bool { return aliasLess(aliases[i], aliases[j]) })
	sort.SliceStable(evidence, func(i, j int) bool { return aliasLess(evidence[i].Value, evidence[j].Value) })
	channel.draft.Aliases = aliases
	channel.draft.AliasEvidence = evidence
	excludedAliases := make([]string, 0, len(channel.excludedAliases))
	for _, alias := range channel.excludedAliases {
		excludedAliases = append(excludedAliases, alias)
	}
	sort.SliceStable(excludedAliases, func(i, j int) bool { return aliasLess(excludedAliases[i], excludedAliases[j]) })
	channel.draft.ExcludedAliases = excludedAliases
	epgIDs := make([]string, 0, len(channel.epgIDs))
	epgEvidence := make([]IdentifierEvidence, 0, len(channel.epgIDs))
	for _, identifier := range channel.epgIDs {
		epgIDs = append(epgIDs, identifier.value)
		epgEvidence = append(epgEvidence, IdentifierEvidence{
			Value: identifier.value, Sources: mapKeys(identifier.sources), Methods: mapKeys(identifier.methods),
		})
	}
	sort.SliceStable(epgIDs, func(i, j int) bool { return aliasLess(epgIDs[i], epgIDs[j]) })
	sort.SliceStable(epgEvidence, func(i, j int) bool { return aliasLess(epgEvidence[i].Value, epgEvidence[j].Value) })
	channel.draft.EPGIDs = epgIDs
	channel.draft.EPGIDEvidence = epgEvidence
	channel.draft.MatchedSources = mapKeys(channel.matchedSourceSet)
}

type indexedEntry struct {
	name           string
	category       string
	categoryMethod string
	aliases        []string
	epgIDs         []string
	keys           map[string]bool
}

func applyCatalog(channels []*channelWork, catalog catalogFile, sourceID, sourceLabel string) (int, int) {
	entries := make([]indexedEntry, 0)
	textIndex := make(map[string][]int)
	epgIndex := make(map[string][]int)
	for category, catalogChannels := range catalog.Categories {
		for _, entry := range catalogChannels {
			name := cleanText(entry.Name)
			if name == "" {
				continue
			}
			aliases := cleanStrings(entry.Aliases)
			mappedCategory, categoryMethod := resolveProviderCategory(category, append([]string{name}, aliases...)...)
			indexed := indexedEntry{name: name, category: mappedCategory, categoryMethod: categoryMethod, aliases: aliases, epgIDs: cleanStrings(entry.EPGIDs), keys: make(map[string]bool)}
			for _, value := range append([]string{name}, indexed.aliases...) {
				if key := identityKey(value); key != "" {
					indexed.keys[key] = true
				}
			}
			entryIndex := len(entries)
			entries = append(entries, indexed)
			for key := range indexed.keys {
				textIndex[key] = append(textIndex[key], entryIndex)
			}
			for _, epgID := range indexed.epgIDs {
				epgIndex[strings.ToLower(epgID)] = append(epgIndex[strings.ToLower(epgID)], entryIndex)
			}
		}
	}
	matched := 0
	ambiguous := 0
	for _, channel := range channels {
		index, ok, wasAmbiguous := bestCatalogMatch(channel, textIndex, epgIndex)
		if wasAmbiguous {
			ambiguous++
		}
		if !ok {
			continue
		}
		entry := entries[index]
		matched++
		channel.matchedSourceSet[sourceID] = true
		if channel.draft.NameSource == "gracenote" {
			channel.draft.Name = entry.name
			channel.draft.NameSource = sourceLabel
			channel.draft.NameMethod = "exact catalog identity"
		}
		if channel.draft.CategorySource == "unresolved" && entry.category != "" {
			channel.draft.Category = entry.category
			channel.draft.CategorySource = sourceLabel
			channel.draft.CategoryMethod = "exact catalog identity; " + entry.categoryMethod
		}
		channel.addAlias(entry.name, sourceID, "exact catalog identity")
		for _, alias := range entry.aliases {
			channel.addAlias(alias, sourceID, "curated catalog alias")
		}
		for _, epgID := range entry.epgIDs {
			channel.addEPGID(epgID, sourceID, "curated catalog EPG ID")
		}
	}
	return matched, ambiguous
}

func bestCatalogMatch(channel *channelWork, textIndex map[string][]int, epgIndex map[string][]int) (int, bool, bool) {
	scores := make(map[int]int)
	addScores(scores, epgIndex[strings.ToLower(channel.input.StationID)], 120)
	for _, value := range append([]string{channel.input.CallSign}, channel.input.EventCallSigns...) {
		addScores(scores, textIndex[identityKey(value)], 100)
	}
	if safeAffiliate(channel.input.Affiliate) {
		addScores(scores, textIndex[identityKey(channel.input.Affiliate)], 80)
	}
	if len(scores) == 0 {
		return 0, false, false
	}
	bestScore := -1
	best := make([]int, 0, 1)
	for index, score := range scores {
		if score > bestScore {
			bestScore = score
			best = []int{index}
		} else if score == bestScore {
			best = append(best, index)
		}
	}
	if len(best) != 1 {
		return 0, false, true
	}
	return best[0], true, false
}

func applyIPTVOrg(channels []*channelWork, sourceEntries []iptvOrgChannel, countryCode, sourceID, sourceLabel string) (int, int) {
	entries := make([]indexedEntry, 0)
	index := make(map[string][]int)
	for _, entry := range sourceEntries {
		if countryCode != "" && !strings.EqualFold(entry.Country, countryCode) {
			continue
		}
		if nonEmptyJSONValue(entry.Closed) || nonEmptyJSONValue(entry.ReplacedBy) {
			continue
		}
		name := cleanText(entry.Name)
		if name == "" {
			continue
		}
		category := ""
		categoryMethod := ""
		for _, rawCategory := range entry.Categories {
			if mapped, method := resolveProviderCategory(rawCategory, append([]string{name}, entry.AltNames...)...); mapped != "" {
				category = mapped
				categoryMethod = method
				break
			}
		}
		indexed := indexedEntry{name: name, category: category, categoryMethod: categoryMethod, aliases: cleanStrings(entry.AltNames), epgIDs: cleanStrings([]string{entry.ID}), keys: make(map[string]bool)}
		for _, value := range append([]string{name}, indexed.aliases...) {
			if key := identityKey(value); key != "" {
				indexed.keys[key] = true
			}
		}
		entryIndex := len(entries)
		entries = append(entries, indexed)
		for key := range indexed.keys {
			index[key] = append(index[key], entryIndex)
		}
	}

	matched := 0
	ambiguous := 0
	for _, channel := range channels {
		candidateScores := make(map[int]int)
		for _, value := range append([]string{channel.draft.Name, channel.input.CallSign}, channel.input.EventCallSigns...) {
			addScores(candidateScores, index[identityKey(value)], 100)
		}
		if safeAffiliate(channel.input.Affiliate) {
			addScores(candidateScores, index[identityKey(channel.input.Affiliate)], 80)
		}
		entryIndex, ok, wasAmbiguous := uniqueBest(candidateScores)
		if wasAmbiguous {
			ambiguous++
		}
		if !ok {
			continue
		}
		entry := entries[entryIndex]
		matched++
		channel.matchedSourceSet[sourceID] = true
		if channel.draft.NameSource == "gracenote" {
			channel.draft.Name = entry.name
			channel.draft.NameSource = sourceLabel
			channel.draft.NameMethod = "exact public database identity"
		}
		if channel.draft.CategorySource == "unresolved" && entry.category != "" {
			channel.draft.Category = entry.category
			channel.draft.CategorySource = sourceLabel
			channel.draft.CategoryMethod = "exact public database identity; " + entry.categoryMethod
		}
		channel.addAlias(entry.name, sourceID, "exact public database identity")
		for _, alias := range entry.aliases {
			channel.addAlias(alias, sourceID, "public database alternate name")
		}
		for _, epgID := range entry.epgIDs {
			channel.addEPGID(epgID, sourceID, "public database channel ID")
		}
	}
	return matched, ambiguous
}

func uniqueBest(scores map[int]int) (int, bool, bool) {
	if len(scores) == 0 {
		return 0, false, false
	}
	bestScore := -1
	best := make([]int, 0, 1)
	for index, score := range scores {
		if score > bestScore {
			bestScore = score
			best = []int{index}
		} else if score == bestScore {
			best = append(best, index)
		}
	}
	if len(best) != 1 {
		return 0, false, true
	}
	return best[0], true, false
}

func addScores(scores map[int]int, candidates []int, score int) {
	for _, candidate := range candidates {
		if score > scores[candidate] {
			scores[candidate] = score
		}
	}
}

func findDuplicateSuggestions(channels []DraftChannel) []DuplicateSuggestion {
	nameGroups := make(map[string][]int)
	for index, channel := range channels {
		if channel.NameSource == "gracenote" {
			continue
		}
		key := identityKey(channel.Name)
		if key != "" {
			nameGroups[key] = append(nameGroups[key], index)
		}
	}
	suggestionByRemoveID := make(map[string]DuplicateSuggestion)
	for _, indexes := range nameGroups {
		if len(indexes) < 2 {
			continue
		}
		bestRank := -1
		keepIndexes := make([]int, 0, 1)
		for _, index := range indexes {
			rank := qualityRank(channels[index])
			if rank > bestRank {
				bestRank = rank
				keepIndexes = []int{index}
			} else if rank == bestRank {
				keepIndexes = append(keepIndexes, index)
			}
		}
		keepIndexes = sameStationKeepPosition(channels, keepIndexes)
		if len(keepIndexes) != 1 || bestRank <= 1 {
			continue
		}
		keep := channels[keepIndexes[0]]
		for _, index := range indexes {
			remove := channels[index]
			if remove.ID == keep.ID || qualityRank(remove) >= bestRank || !sharesAttributableSource(remove, keep) {
				continue
			}
			reason := fmt.Sprintf("Exact source identity maps both positions to %s; %s carries the stronger HD/digital marker", keep.Name, keep.CallSign)
			suggestionByRemoveID[remove.ID] = DuplicateSuggestion{
				RemoveID: remove.ID, RemoveNumber: remove.Number, RemoveName: remove.Name,
				KeepID: keep.ID, KeepNumber: keep.Number, KeepName: keep.Name, Reason: reason,
			}
		}
	}

	suffixGroups := make(map[string][]int)
	groupsWithSuffix := make(map[string]bool)
	for index, channel := range channels {
		base, hasSuffix := qualitySuffixBase(channel.CallSign)
		if base == "" {
			continue
		}
		suffixGroups[base] = append(suffixGroups[base], index)
		groupsWithSuffix[base] = groupsWithSuffix[base] || hasSuffix
	}
	for base, indexes := range suffixGroups {
		if len(indexes) < 2 || !groupsWithSuffix[base] {
			continue
		}
		bestRank := -1
		keepIndexes := make([]int, 0, 1)
		for _, index := range indexes {
			rank := qualityRank(channels[index])
			if rank > bestRank {
				bestRank = rank
				keepIndexes = []int{index}
			} else if rank == bestRank {
				keepIndexes = append(keepIndexes, index)
			}
		}
		keepIndexes = sameStationKeepPosition(channels, keepIndexes)
		if len(keepIndexes) != 1 || bestRank <= 1 {
			continue
		}
		keep := channels[keepIndexes[0]]
		for _, index := range indexes {
			remove := channels[index]
			if remove.ID == keep.ID || qualityRank(remove) >= bestRank {
				continue
			}
			if _, exists := suggestionByRemoveID[remove.ID]; exists {
				continue
			}
			reason := fmt.Sprintf("Callsigns differ only by an HD/SD/DT quality suffix; %s carries the stronger HD/digital marker", keep.CallSign)
			suggestionByRemoveID[remove.ID] = DuplicateSuggestion{
				RemoveID: remove.ID, RemoveNumber: remove.Number, RemoveName: remove.Name,
				KeepID: keep.ID, KeepNumber: keep.Number, KeepName: keep.Name, Reason: reason,
			}
		}
	}

	aliasGroups := make(map[string]*duplicateAliasGroup)
	for index, channel := range channels {
		for _, evidence := range channel.AliasEvidence {
			aliasKey := identityKey(evidence.Value)
			if aliasKey == "" || isDigits(aliasKey) {
				continue
			}
			for _, source := range evidence.Sources {
				sourceKey := strings.ToLower(cleanText(source))
				if sourceKey == "" || sourceKey == "gracenote" {
					continue
				}
				group := aliasGroups[aliasKey]
				if group == nil {
					group = &duplicateAliasGroup{
						alias:                   evidence.Value,
						sources:                 make(map[string]bool),
						sourceMembers:           make(map[string]map[int]bool),
						providerPositionBridges: make(map[int]map[string]bool),
						seen:                    make(map[int]bool),
					}
					aliasGroups[aliasKey] = group
				}
				group.sources[source] = true
				if group.sourceMembers[sourceKey] == nil {
					group.sourceMembers[sourceKey] = make(map[int]bool)
				}
				group.sourceMembers[sourceKey][index] = true
				for _, method := range evidence.Methods {
					if number := providerPositionNumber(method); number != "" {
						if group.providerPositionBridges[index] == nil {
							group.providerPositionBridges[index] = make(map[string]bool)
						}
						group.providerPositionBridges[index][number] = true
					}
				}
				if !group.seen[index] {
					group.indexes = append(group.indexes, index)
					group.seen[index] = true
				}
			}
		}
	}
	aliasCandidates := make(map[string]map[string]DuplicateSuggestion)
	for _, group := range aliasGroups {
		if len(group.indexes) != 2 {
			continue
		}
		left := channels[group.indexes[0]]
		right := channels[group.indexes[1]]
		if looksLikeNumberedDigitalSubchannel(left.CallSign) || looksLikeNumberedDigitalSubchannel(right.CallSign) {
			continue
		}
		leftRank := qualityRank(left)
		rightRank := qualityRank(right)
		if leftRank == rightRank || (!hasExplicitQualityMarker(left) && !hasExplicitQualityMarker(right)) {
			continue
		}
		remove, keep := left, right
		if rightRank < leftRank {
			remove, keep = right, left
		}
		if hasExplicitSDMarker(remove) {
			if !group.hasSharedSource() {
				continue
			}
		} else if !group.hasSharedNonScheduleSource() && !group.hasProviderPositionBridge(group.indexes[0], left, group.indexes[1], right) {
			continue
		}
		sources := strings.Join(mapKeys(group.sources), ", ")
		reason := fmt.Sprintf("Exact attributable alias %s identifies both positions from %s; %s has the unique stronger quality rank", group.alias, sources, keep.CallSign)
		if hasExplicitSDMarker(remove) && !hasExplicitQualityMarker(keep) {
			reason = fmt.Sprintf("Exact attributable alias %s identifies both positions from %s; %s is explicitly SD and %s is the unique non-SD counterpart", group.alias, sources, remove.CallSign, keep.CallSign)
		}
		suggestion := DuplicateSuggestion{
			RemoveID: remove.ID, RemoveNumber: remove.Number, RemoveName: remove.Name,
			KeepID: keep.ID, KeepNumber: keep.Number, KeepName: keep.Name, Reason: reason,
		}
		if aliasCandidates[remove.ID] == nil {
			aliasCandidates[remove.ID] = make(map[string]DuplicateSuggestion)
		}
		aliasCandidates[remove.ID][keep.ID] = suggestion
	}
	for removeID, candidates := range aliasCandidates {
		if _, exists := suggestionByRemoveID[removeID]; exists || len(candidates) != 1 {
			continue
		}
		for _, suggestion := range candidates {
			suggestionByRemoveID[removeID] = suggestion
		}
	}

	// Repeated positions require both the exact station and callsign. Shared
	// generic guide IDs alone must not combine differently named services.
	exactGroups := make(map[string][]int)
	for i, channel := range channels {
		if channel.StationID != "" && identityKey(channel.CallSign) != "" {
			key := channel.StationID + "\x00" + identityKey(channel.CallSign)
			exactGroups[key] = append(exactGroups[key], i)
		}
	}
	for _, indexes := range exactGroups {
		if len(indexes) < 2 {
			continue
		}
		keep := channels[sameStationKeepPosition(channels, indexes)[0]]
		for _, i := range indexes {
			remove := channels[i]
			if remove.ID == keep.ID {
				continue
			}
			if _, exists := suggestionByRemoveID[remove.ID]; exists {
				continue
			}
			suggestionByRemoveID[remove.ID] = DuplicateSuggestion{Exact: true, RemoveID: remove.ID, RemoveNumber: remove.Number, RemoveName: remove.Name, KeepID: keep.ID, KeepNumber: keep.Number, KeepName: keep.Name, Reason: "Exact same Gracenote station and callsign at multiple lineup positions"}
		}
	}
	suggestions := make([]DuplicateSuggestion, 0, len(suggestionByRemoveID))
	for _, suggestion := range suggestionByRemoveID {
		suggestions = append(suggestions, suggestion)
	}
	sort.SliceStable(suggestions, func(i, j int) bool {
		if numberLess(suggestions[i].RemoveNumber, suggestions[j].RemoveNumber) {
			return true
		}
		if numberLess(suggestions[j].RemoveNumber, suggestions[i].RemoveNumber) {
			return false
		}
		return suggestions[i].RemoveID < suggestions[j].RemoveID
	})
	return suggestions
}

type duplicateAliasGroup struct {
	alias                   string
	sources                 map[string]bool
	sourceMembers           map[string]map[int]bool
	providerPositionBridges map[int]map[string]bool
	indexes                 []int
	seen                    map[int]bool
}

func (group *duplicateAliasGroup) hasSharedSource() bool {
	for _, members := range group.sourceMembers {
		if len(members) == 2 {
			return true
		}
	}
	return false
}

func (group *duplicateAliasGroup) hasSharedNonScheduleSource() bool {
	for source, members := range group.sourceMembers {
		if len(members) == 2 && !strings.HasPrefix(source, "gracenote-weekday-epg-") {
			return true
		}
	}
	return false
}

func (group *duplicateAliasGroup) hasProviderPositionBridge(leftIndex int, left DraftChannel, rightIndex int, right DraftChannel) bool {
	return group.providerPositionBridges[leftIndex][cleanText(right.Number)] ||
		group.providerPositionBridges[rightIndex][cleanText(left.Number)]
}

func providerPositionNumber(method string) string {
	lower := strings.ToLower(method)
	const marker = "provider-position:"
	index := strings.Index(lower, marker)
	if index < 0 {
		return ""
	}
	value := method[index+len(marker):]
	if fields := strings.FieldsFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == ')' || r == ';'
	}); len(fields) > 0 {
		value = fields[0]
	}
	separator := strings.LastIndex(value, "|")
	if separator < 0 {
		return ""
	}
	return cleanText(value[separator+1:])
}

func sharesAttributableSource(left, right DraftChannel) bool {
	rightSources := make(map[string]bool, len(right.MatchedSources))
	for _, source := range right.MatchedSources {
		if source != "gracenote" {
			rightSources[source] = true
		}
	}
	for _, source := range left.MatchedSources {
		if source != "gracenote" && rightSources[source] {
			return true
		}
	}
	return false
}

// Multiple positions of the same HD station are not competing identities.
// Prefer an included position, then the lowest numeric channel for review.
func sameStationKeepPosition(channels []DraftChannel, indexes []int) []int {
	if len(indexes) < 2 {
		return indexes
	}
	station := channels[indexes[0]].StationID
	if station == "" {
		return indexes
	}
	best := indexes[0]
	for _, index := range indexes {
		if channels[index].StationID != station {
			return indexes
		}
		candidate, current := channels[index], channels[best]
		cn, ce := strconv.ParseFloat(candidate.Number, 64)
		bn, be := strconv.ParseFloat(current.Number, 64)
		lower := candidate.Number < current.Number
		if ce == nil && be == nil {
			lower = cn < bn
		}
		if (candidate.Included && !current.Included) || (candidate.Included == current.Included && (lower || (candidate.Number == current.Number && candidate.ID < current.ID))) {
			best = index
		}
	}
	return []int{best}
}

func qualitySuffixBase(value string) (string, bool) {
	key := identityKey(value)
	if key == "" {
		return "", false
	}
	for _, suffix := range []string{"hd", "sd", "dt"} {
		if !strings.HasSuffix(key, suffix) {
			continue
		}
		base := strings.TrimSuffix(key, suffix)
		if len(base) < 3 {
			return "", false
		}
		return base, true
	}
	return key, false
}

func qualityRank(channel DraftChannel) int {
	value := strings.ToUpper(channel.CallSign + " " + channel.OriginalName)
	callSign := strings.ToUpper(identityKey(channel.CallSign))
	originalName := strings.ToUpper(identityKey(channel.OriginalName))
	if strings.Contains(value, "4K") || strings.Contains(value, "UHD") {
		return 4
	}
	if strings.Contains(value, " HD") || strings.HasSuffix(callSign, "HD") || strings.HasSuffix(originalName, "HD") || looksLikeDigitalCallSign(callSign) {
		return 3
	}
	if strings.Contains(value, " SD") || strings.HasSuffix(callSign, "SD") || strings.HasSuffix(originalName, "SD") {
		return 0
	}
	return 1
}

func hasExplicitSDMarker(channel DraftChannel) bool {
	value := strings.ToUpper(cleanText(channel.CallSign + " " + channel.OriginalName))
	callSign := strings.ToUpper(identityKey(channel.CallSign))
	originalName := strings.ToUpper(identityKey(channel.OriginalName))
	return strings.Contains(value, " SD") || hasTerminalSDMarker(callSign) || hasTerminalSDMarker(originalName)
}

func hasExplicitQualityMarker(channel DraftChannel) bool {
	value := strings.ToUpper(cleanText(channel.CallSign + " " + channel.OriginalName))
	callSign := strings.ToUpper(identityKey(channel.CallSign))
	originalName := strings.ToUpper(identityKey(channel.OriginalName))
	return strings.Contains(value, "4K") || strings.Contains(value, "UHD") ||
		strings.Contains(value, " HD") || strings.Contains(value, " SD") ||
		hasTerminalQualityMarker(callSign, "HD") || hasTerminalQualityMarker(callSign, "SD") ||
		hasTerminalQualityMarker(originalName, "HD") || hasTerminalQualityMarker(originalName, "SD") ||
		looksLikeDigitalCallSign(callSign)
}

func hasTerminalSDMarker(value string) bool {
	return hasTerminalQualityMarker(value, "SD")
}

func hasTerminalQualityMarker(value, marker string) bool {
	return strings.HasSuffix(value, marker) && len(strings.TrimSuffix(value, marker)) >= 3
}

func looksLikeNumberedDigitalSubchannel(value string) bool {
	key := identityKey(value)
	index := strings.LastIndex(key, "dt")
	return index >= 3 && index+2 < len(key) && isDigits(key[index+2:])
}

func looksLikeDigitalCallSign(value string) bool {
	if !strings.HasSuffix(value, "DT") || len(value) < 5 || len(value) > 9 {
		return false
	}
	first := value[0]
	return first == 'W' || first == 'K'
}

func mapIPTVOrgCategory(category string) string {
	if strings.EqualFold(strings.TrimSpace(category), "xxx") {
		return channelcategory.Other
	}
	if strings.EqualFold(strings.TrimSpace(category), "auto") {
		return channelcategory.Entertainment
	}
	mapped, _ := resolveProviderCategory(category)
	return mapped
}

func resolveProviderCategory(category string, identities ...string) (string, string) {
	if strings.EqualFold(strings.TrimSpace(category), "xxx") {
		return channelcategory.Other, channelcategory.MethodAlias + " (xxx to Adult to Other)"
	}
	if strings.EqualFold(strings.TrimSpace(category), "auto") {
		return channelcategory.Entertainment, channelcategory.MethodAlias + " (auto to Entertainment)"
	}
	match, ok := channelcategory.Resolve(category, identities...)
	if !ok {
		return "", ""
	}
	method := match.Method
	if match.Method == channelcategory.MethodFuzzy {
		method = fmt.Sprintf("%s %.0f%% to %q", match.Method, match.Confidence*100, match.MatchedAlias)
	}
	return match.Category, method
}

func safeAffiliate(value string) bool {
	value = strings.ToUpper(cleanText(value))
	if value == "" || len(value) < 3 {
		return false
	}
	if strings.HasSuffix(value, " TELEVISION NETWORK") || strings.HasSuffix(value, " BROADCASTING NETWORK") || strings.Contains(value, "AFFILIATE") {
		return false
	}
	switch value {
	case "NULL", "INDEPENDENT", "UNKNOWN", "N/A", "NA", "NONE", "UNAVAILABLE", "ABC", "CBS", "NBC", "FOX", "PBS", "CW", "THE CW", "TELEMUNDO NETWORK", "UNIVISION NETWORK":
		return false
	default:
		return true
	}
}

func cleanCategory(value string) string {
	value = cleanText(value)
	if strings.EqualFold(value, "uncategorised") || strings.EqualFold(value, uncategorized) {
		return uncategorized
	}
	match, ok := channelcategory.Resolve(value)
	if !ok {
		return ""
	}
	return match.Category
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func cleanStrings(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = cleanText(value)
		key := strings.ToLower(value)
		if value == "" || strings.EqualFold(value, "null") || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}

func identityKey(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(cleanText(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	key := builder.String()
	if len(key) < 3 {
		return ""
	}
	return key
}

func mapKeys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return strings.ToLower(result[i]) < strings.ToLower(result[j]) })
	return result
}

func aliasLess(left, right string) bool {
	leftNumeric := isDigits(left)
	rightNumeric := isDigits(right)
	if leftNumeric != rightNumeric {
		return !leftNumeric
	}
	return strings.ToLower(left) < strings.ToLower(right)
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func numberLess(left, right string) bool {
	var leftNumber, rightNumber float64
	_, leftErr := fmt.Sscanf(left, "%f", &leftNumber)
	_, rightErr := fmt.Sscanf(right, "%f", &rightNumber)
	if leftErr == nil && rightErr == nil && leftNumber != rightNumber {
		return leftNumber < rightNumber
	}
	return strings.ToLower(left) < strings.ToLower(right)
}

func packageName(lineup LineupContext) string {
	name := cleanText(lineup.ProviderName)
	if name == "" {
		name = "Gracenote lineup"
	}
	if postal := cleanText(lineup.PostalCode); postal != "" {
		return name + " (" + postal + ")"
	}
	return name
}
