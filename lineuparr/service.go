package lineuparr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/daniel-widrick/GraceNoteScraper/channelcategory"
)

const uncategorized = "Uncategorized"

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

	resultChannels := make([]DraftChannel, 0, len(channels))
	for _, channel := range channels {
		finalizeChannel(channel)
		if override, ok := overrides[channel.draft.ID]; ok {
			if override.Included != nil {
				channel.draft.Included = *override.Included
			}
			if category := cleanCategory(override.Category); category != "" {
				channel.draft.Category = category
				channel.draft.CategorySource = "user"
				channel.draft.CategoryMethod = "user edit"
			}
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
		Sources:              statuses,
		Categories:           append(channelcategory.Categories(), uncategorized),
		Total:                len(resultChannels),
	}
	for _, channel := range resultChannels {
		if channel.Included {
			draft.Included++
		} else {
			draft.Excluded++
		}
		draft.AliasCount += len(channel.Aliases)
		if channel.Category == uncategorized {
			draft.Uncategorized++
		} else {
			draft.Categorized++
		}
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

func (s *Service) RemoveSuggestedDuplicates(fingerprint string, draft *Draft) error {
	ids := make([]string, 0, len(draft.DuplicateSuggestions))
	for _, suggestion := range draft.DuplicateSuggestions {
		ids = append(ids, suggestion.RemoveID)
	}
	return s.store.SetIncluded(fingerprint, ids, false)
}

func (s *Service) RestoreAll(fingerprint string) error {
	return s.store.RestoreAll(fingerprint)
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
	groups := make(map[string][]int)
	for index, channel := range channels {
		if channel.NameSource == "gracenote" {
			continue
		}
		key := identityKey(channel.Name)
		if key != "" {
			groups[key] = append(groups[key], index)
		}
	}
	var suggestions []DuplicateSuggestion
	for _, indexes := range groups {
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
			suggestions = append(suggestions, DuplicateSuggestion{
				RemoveID: remove.ID, RemoveNumber: remove.Number, RemoveName: remove.Name,
				KeepID: keep.ID, KeepNumber: keep.Number, KeepName: keep.Name, Reason: reason,
			})
		}
	}
	sort.SliceStable(suggestions, func(i, j int) bool {
		return numberLess(suggestions[i].RemoveNumber, suggestions[j].RemoveNumber)
	})
	return suggestions
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
