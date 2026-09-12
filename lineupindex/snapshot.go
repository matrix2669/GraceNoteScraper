package lineupindex

import (
	"fmt"
	"sort"
	"strings"

	"github.com/daniel-widrick/GraceNoteScraper/channelcategory"
)

func (s *Service) Snapshot() Snapshot {
	return s.SnapshotForPostal("", "")
}

func (s *Service) SnapshotForPostal(country, postalCode string) Snapshot {
	current := normalizeCurrentStations(s.readCurrentStations())
	s.mu.RLock()
	defer s.mu.RUnlock()

	snapshot := Snapshot{Summary: s.summaryLocked(current), Job: s.job, CategoryRelationsRefreshRequired: s.index.CategoryRelationsRefreshRequired}
	if record := s.index.PostalScans[postalScanKey(country, postalCode)]; record != nil {
		copy := *record
		copy.Sources = nil
		for _, source := range record.Sources {
			if !excludedEnrichmentSource(source.ID) {
				copy.Sources = append(copy.Sources, source)
			}
		}
		snapshot.PostalScan = &copy
	}
	return snapshot
}

func (s *Service) summaryLocked(current map[string]map[string]bool) IndexSummary {
	summary := IndexSummary{
		UpdatedAt:             s.index.UpdatedAt,
		Lineups:               len(s.index.Lineups),
		Stations:              len(s.index.Stations),
		CurrentLineupStations: len(current),
	}

	conflictingNames := make(map[string]bool)
	for stationID, station := range s.index.Stations {
		safeAliases := map[string]bool{}
		for _, name := range station.Names {
			if !s.allowedEnrichmentOrigins(name.LineupKeys) {
				continue
			}
			switch name.Kind {
			case NameCallSign, NameEventCallSign:
				if name.Conflict {
					conflictingNames[name.Normalized] = true
					continue
				}
				safeAliases[name.Normalized] = true
			case NameAffiliateName, NameAffiliateCallSign:
				summary.AffiliateNames++
			}
		}
		for _, fact := range station.Facts {
			key := normalizeName(fact.Value)
			if fact.Kind == FactAlias && usableFact(fact) && s.allowedEnrichmentOrigins(fact.LineupKeys) && !ignoredName(key) {
				safeAliases[key] = true
			}
		}
		if len(safeAliases) > 1 {
			summary.MeaningfulAliases += len(safeAliases) - 1
		}
		if baseline, ok := current[stationID]; ok {
			for key := range safeAliases {
				if !baseline[key] {
					summary.CurrentLineupAliases++
				}
			}
		}
	}
	summary.Conflicts = len(conflictingNames)
	return summary
}

func (s *Service) AliasesForStations(stationIDs []string) map[string][]AliasCandidate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string][]AliasCandidate)
	seen := make(map[string]bool)
	for _, stationID := range stationIDs {
		if seen[stationID] {
			continue
		}
		seen[stationID] = true
		station := s.index.Stations[stationID]
		if station == nil {
			continue
		}
		for _, name := range station.Names {
			if !isCallSignKind(name.Kind) || name.Conflict || !s.allowedEnrichmentOrigins(name.LineupKeys) {
				continue
			}
			result[stationID] = append(result[stationID], AliasCandidate{
				StationID: stationID, Value: name.Value, Kind: name.Kind, LineupKeys: append([]string(nil), name.LineupKeys...),
			})
		}
		for _, fact := range station.Facts {
			if fact.Kind != FactAlias || !usableFact(fact) || !s.allowedEnrichmentOrigins(fact.LineupKeys) {
				continue
			}
			result[stationID] = append(result[stationID], AliasCandidate{
				StationID: stationID, Value: fact.Value, Kind: FactAlias,
				LineupKeys: append([]string(nil), fact.LineupKeys...), SourceID: fact.SourceID,
				SourceLabel: fact.SourceLabel, SourceURL: fact.SourceURL, Method: fact.Method,
			})
		}
		sort.SliceStable(result[stationID], func(i, j int) bool {
			return result[stationID][i].Value < result[stationID][j].Value
		})
	}
	return result
}

// CategoriesForStations returns only categories that are unambiguous across
// official sources. Conflicting provider classifications remain visible in the
// persisted evidence but are not applied automatically.
func (s *Service) CategoriesForStations(stationIDs []string) map[string]CategoryCandidate {
	return s.categoriesForStations(stationIDs, "")
}

// CategoriesForStationsWithPreferredSource is a compatibility wrapper for
// callers that still pass the old selected-provider argument. Resolution is
// always independent of that argument.
func (s *Service) CategoriesForStationsWithPreferredSource(stationIDs []string, preferredSourceID string) map[string]CategoryCandidate {
	// Kept as a source-compatible shim for callers built against the earlier
	// selected-provider contract. Category resolution is intentionally
	// independent of the active provider; a preferred source must not override
	// corroborating or conflicting provider evidence.
	return s.categoriesForStations(stationIDs, "")
}

func (s *Service) categoriesForStations(stationIDs []string, preferredSourceID string) map[string]CategoryCandidate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]CategoryCandidate)
	seenStations := make(map[string]bool)
	for _, stationID := range stationIDs {
		if seenStations[stationID] {
			continue
		}
		seenStations[stationID] = true
		station := s.index.Stations[stationID]
		if station == nil {
			continue
		}
		// A provider contributes at most one vote at an ordinal tier. The same
		// source is commonly present for several markets, devices, aliases, and
		// EPG copies; those are evidence copies, not independent votes.
		type providerVote struct {
			category string
			priority int
			facts    []StationFact
		}
		votes := make(map[string]map[int]map[string]*providerVote)
		identities := make([]string, 0, len(station.Names))
		for _, name := range station.Names {
			if s.allowedEnrichmentOrigins(name.LineupKeys) {
				identities = append(identities, name.Value)
			}
		}
		for _, fact := range station.Facts {
			if fact.Kind != FactCategory || !usableFact(fact) || !s.allowedEnrichmentOrigins(fact.LineupKeys) {
				continue
			}
			categoryValue := fact.Value
			// A miscellaneous provider bucket is not evidence of a service channel.
			if fact.Value == channelcategory.Other && strings.EqualFold(strings.TrimSpace(fact.RawValue), "Other") {
				continue
			}
			if strings.TrimSpace(fact.RawValue) != "" {
				if remapped, ok := channelcategory.Resolve(fact.RawValue, identities...); ok {
					categoryValue = remapped.Category
				} else if !strings.EqualFold(strings.TrimSpace(fact.RawValue), strings.TrimSpace(fact.Value)) {
					// The raw provider label no longer maps to the canonical value.
					// This primarily protects existing indexes from broad headings
					// such as Optimum's "Networks" that were mapped too eagerly.
					continue
				}
			}
			match, ok := channelcategory.Resolve(categoryValue)
			if !ok {
				continue
			}
			fact.Value = match.Category
			priority := categoryFactPriority(fact)
			if fact.SourceID == "xfinity-official-lineup" || strings.Contains(fact.Method, "priority-4") || strings.Contains(fact.Method, channelcategory.MethodFuzzy) {
				priority = 4
			}
			fact.Normalized = normalizeName(match.Category)
			if match.Method != channelcategory.MethodCanonical {
				fact.Method = appendMethod(fact.Method, "master taxonomy: "+match.Method)
			}
			for _, provider := range factProviderRoots(s, fact) {
				if votes[provider] == nil {
					votes[provider] = make(map[int]map[string]*providerVote)
				}
				if votes[provider][priority] == nil {
					votes[provider][priority] = make(map[string]*providerVote)
				}
				categoryKey := match.Category
				if votes[provider][priority][categoryKey] == nil {
					votes[provider][priority][categoryKey] = &providerVote{category: categoryKey, priority: priority}
				}
				pv := votes[provider][priority][categoryKey]
				if !sameStationFact(pv.facts, fact) {
					pv.facts = append(pv.facts, fact)
				}
			}
		}
		// Provider-number joins are retained separately from accepted facts so
		// they cannot masquerade as identity evidence. Once persisted against a
		// station, however, their attributable category is eligible for this
		// independent provider vote.
		for _, relation := range s.index.CategoryRelations {
			if strings.TrimSpace(relation.StationID) != stationID || !relationEligible(s, relation) {
				continue
			}
			match, ok := channelcategory.Resolve(relation.Category)
			if !ok {
				continue
			}
			fact := StationFact{Kind: FactCategory, Value: match.Category, RawValue: relation.RawCategory, SourceID: relation.SourceID, SourceLabel: relation.SourceLabel, SourceURL: relation.SourceURL, Method: relation.Method + "; provider-category-relation", LineupKeys: append([]string(nil), relation.LineupKeys...), SourceRowID: relation.SourceRowID, RootSourceID: relation.SourceID, RootSourceLabel: relation.SourceLabel, RootSourceURL: relation.SourceURL}
			priority := categoryFactPriority(fact)
			if strings.Contains(strings.ToLower(fact.Method), "priority-4") || strings.Contains(strings.ToLower(fact.Method), channelcategory.MethodFuzzy) {
				priority = 4
			}
			for _, provider := range factProviderRoots(s, fact) {
				if votes[provider] == nil {
					votes[provider] = make(map[int]map[string]*providerVote)
				}
				if votes[provider][priority] == nil {
					votes[provider][priority] = make(map[string]*providerVote)
				}
				if votes[provider][priority][match.Category] == nil {
					votes[provider][priority][match.Category] = &providerVote{category: match.Category, priority: priority}
				}
				vote := votes[provider][priority][match.Category]
				if !sameStationFact(vote.facts, fact) {
					vote.facts = append(vote.facts, fact)
				}
			}
		}
		if len(votes) == 0 {
			continue
		}
		bestPriority := 5
		for _, byPriority := range votes {
			for priority := range byPriority {
				if priority < bestPriority {
					bestPriority = priority
				}
			}
		}
		// A provider that disagrees with itself at the active tier abstains at
		// that tier. Its conflict is retained in the reason so the UI does not
		// mistake the resulting majority for unanimous evidence.
		categoryProviders := make(map[string]map[int]map[string]bool)
		categoryFacts := make(map[string][]StationFact)
		providerConflicts := make(map[string]map[int][]string)
		for provider, byPriority := range votes {
			for priority, byCategory := range byPriority {
				if len(byCategory) == 0 {
					continue
				}
				if len(byCategory) > 1 {
					if providerConflicts[provider] == nil {
						providerConflicts[provider] = make(map[int][]string)
					}
					for category := range byCategory {
						providerConflicts[provider][priority] = append(providerConflicts[provider][priority], category)
					}
					continue
				}
				for category, vote := range byCategory {
					if categoryProviders[category] == nil {
						categoryProviders[category] = make(map[int]map[string]bool)
					}
					if categoryProviders[category][priority] == nil {
						categoryProviders[category][priority] = make(map[string]bool)
					}
					categoryProviders[category][priority][provider] = true
					categoryFacts[category] = append(categoryFacts[category], vote.facts...)
				}
			}
		}
		if len(categoryProviders) == 0 {
			// Keep a supported provisional value visible when the only source is
			// self-conflicting. It is explicitly marked for review below, and the
			// provider contributes no vote to the choice.
			for _, byPriority := range votes {
				for category, vote := range byPriority[bestPriority] {
					if categoryProviders[category] == nil {
						categoryProviders[category] = make(map[int]map[string]bool)
					}
					if categoryProviders[category][bestPriority] == nil {
						categoryProviders[category][bestPriority] = make(map[string]bool)
					}
					categoryFacts[category] = append(categoryFacts[category], vote.facts...)
				}
			}
		}
		categories := make([]string, 0, len(categoryProviders))
		for category := range categoryProviders {
			categories = append(categories, category)
		}
		sort.Slice(categories, func(i, j int) bool { return categoryOrder(categories[i]) < categoryOrder(categories[j]) })
		bestCategory := categories[0]
		for _, category := range categories[1:] {
			if categoryVectorGreater(categoryProviders[category], categoryProviders[bestCategory]) {
				bestCategory = category
			}
		}
		candidate := CategoryCandidate{StationID: stationID, Value: bestCategory, Priority: bestPriority}
		for priority := 1; priority <= 4; priority++ {
			if len(categoryProviders[bestCategory][priority]) > 0 {
				candidate.Priority = priority
				break
			}
		}
		relationOnly := len(categoryFacts[bestCategory]) > 0
		for _, fact := range categoryFacts[bestCategory] {
			if !strings.Contains(fact.Method, "provider-category-relation") {
				relationOnly = false
			}
			candidate.SourceIDs = appendUniqueString(candidate.SourceIDs, fact.SourceID)
			candidate.SourceLabels = appendUniqueString(candidate.SourceLabels, fact.SourceLabel)
			candidate.Methods = appendUniqueString(candidate.Methods, fact.Method)
		}
		if relationOnly {
			candidate.Methods = appendUniqueString(candidate.Methods, "category review: provider-category relation is provisional and requires review")
		}
		// Include all eligible source metadata, including losing categories and
		// lower tiers, so callers can explain a provisional choice without
		// reopening the index. The marker is deliberately plain text because the
		// existing API shape predates structured category-choice fields.
		allProviders := make(map[string]bool)
		for _, category := range categories {
			vector := make([]string, 0, 4)
			for priority := 1; priority <= 4; priority++ {
				providers := categoryProviders[category][priority]
				if len(providers) > 0 {
					for provider := range providers {
						allProviders[provider] = true
					}
				}
				vector = append(vector, fmt.Sprintf("%d", len(providers)))
			}
			candidate.Methods = appendUniqueString(candidate.Methods, fmt.Sprintf("category choice: %s = [%s] provider vote vector (priority 1..4)", category, strings.Join(vector, ", ")))
			for priority := 1; priority <= 4; priority++ {
				if providers := categoryProviders[category][priority]; len(providers) > 0 {
					candidate.Methods = appendUniqueString(candidate.Methods, fmt.Sprintf("category choice detail: %s priority %d = %d provider(s) [%s]", category, priority, len(providers), strings.Join(sortedKeys(providers), ", ")))
				}
			}
		}
		for provider, byPriority := range votes {
			for priority, byCategory := range byPriority {
				if priority == bestPriority {
					continue
				}
				for category := range byCategory {
					candidate.Methods = appendUniqueString(candidate.Methods, fmt.Sprintf("category evidence priority %d: %s from provider %s", priority, category, provider))
				}
			}
		}
		for provider, conflicts := range providerConflicts {
			for priority, conflict := range conflicts {
				sort.Strings(conflict)
				candidate.Methods = appendUniqueString(candidate.Methods, fmt.Sprintf("category review: provider %s abstained at priority %d after conflicting choices [%s]", provider, priority, strings.Join(conflict, ", ")))
			}
		}
		if len(categories) > 1 || len(providerConflicts) > 0 {
			candidate.Methods = appendUniqueString(candidate.Methods, "category review: provider disagreement remains until user or independent schedule confirmation")
		}
		if len(allProviders) == 0 && len(providerConflicts) == 0 {
			continue
		}
		sort.Strings(candidate.SourceIDs)
		sort.Strings(candidate.SourceLabels)
		sort.Strings(candidate.Methods)
		result[stationID] = candidate
	}
	return result
}

func categoryFactPriority(fact StationFact) int {
	method := strings.ToLower(fact.Method)
	if strings.Contains(method, "priority-1") || strings.Contains(method, "maintained") || strings.Contains(method, "manual") {
		return 1
	}
	return 2
}

func categoryOrder(value string) int {
	for index, category := range channelcategory.Categories() {
		if category == value {
			return index
		}
	}
	return len(channelcategory.Categories())
}

func categoryVectorGreater(left, right map[int]map[string]bool) bool {
	for priority := 1; priority <= 4; priority++ {
		leftCount, rightCount := len(left[priority]), len(right[priority])
		if leftCount != rightCount {
			return leftCount > rightCount
		}
	}
	return false
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sameStationFact(existing []StationFact, candidate StationFact) bool {
	for _, fact := range existing {
		if fact.SourceID == candidate.SourceID && fact.SourceRowID == candidate.SourceRowID && fact.SourceLabel == candidate.SourceLabel && fact.RawValue == candidate.RawValue && fact.Method == candidate.Method && strings.Join(fact.LineupKeys, "\x00") == strings.Join(candidate.LineupKeys, "\x00") {
			return true
		}
	}
	return false
}

func relationEligible(s *Service, relation ProviderCategoryRelation) bool {
	// number-policy-provider-alias-v3 is an alias-admission marker. It is
	// valid on the separately persisted category relation after alignment, but
	// usableFact quite correctly rejects that marker on a StationFact category.
	method := strings.ReplaceAll(relation.Method, "number-policy-provider-alias-v3", "")
	if !usableFact(StationFact{Kind: FactCategory, SourceID: relation.SourceID, Method: method, LineupKeys: relation.LineupKeys}) || !s.allowedEnrichmentOrigins(relation.LineupKeys) {
		return false
	}
	// A relation is useful only when its provider alias is actually attached to
	// this station. The station ID alone is not enough to turn an arbitrary row
	// into category evidence after a legacy migration.
	station := s.index.Stations[relation.StationID]
	if station == nil {
		return false
	}
	want := normalizeName(relation.AliasValue)
	for _, name := range station.Names {
		if name.Normalized == want && s.allowedEnrichmentOrigins(name.LineupKeys) {
			return true
		}
	}
	for _, fact := range station.Facts {
		if fact.Kind == FactAlias && usableFact(fact) && fact.Normalized == want && s.allowedEnrichmentOrigins(fact.LineupKeys) {
			return true
		}
	}
	return false
}

func factProviderRoots(s *Service, fact StationFact) []string {
	roots := make(map[string]bool)
	// LineupKeys identify where a fact was observed, not who authored it.
	// In particular, a relation reused through an EPG bridge must not inherit
	// the active/scanned lineup's provider. Resolve roots from the attributable
	// source identity only.
	source := strings.TrimSpace(fact.RootSourceID)
	if source == "" {
		source = strings.TrimSpace(fact.SourceID)
	}
	if fact.RootSourceLabel != "" {
		source += " " + strings.TrimSpace(fact.RootSourceLabel)
	}
	if strings.HasPrefix(strings.ToLower(source), "provider-category-relation:") {
		source = strings.TrimSpace(source[len("provider-category-relation:"):])
	}
	root := providerFamilyKey(source + " " + fact.SourceLabel)
	if root != "" {
		roots[root] = true
	}
	return sortedKeys(roots)
}

// Old number-only joins may have been made against a different headend PDF.
// Preserve their evidence on disk, but quarantine it from current drafts. Old
// EPG-carried categories also need a fresh scan through the corrected adapters.
func usableFact(fact StationFact) bool {
	if strings.Contains(fact.Method, "number-policy-provider-alias-v3") {
		if fact.Kind != FactAlias || !strings.Contains(fact.Method, ProviderSourceAlignmentV1) {
			return false
		}
	}
	if strings.Contains(fact.Method, "exact provider channel number plus") && !strings.Contains(fact.Method, "number-policy-local-v1") && !strings.Contains(fact.Method, "number-policy-provider-v2") {
		return false
	}
	// Old Xfinity labels and their EPG-carried copies must not outlive the
	// source-policy correction. Names/aliases remain independent evidence.
	if fact.Kind == FactCategory && (fact.SourceID == "xfinity-official-lineup" || strings.Contains(fact.Method, "Xfinity")) && !strings.Contains(fact.Method, "category-quality-v1") {
		return false
	}
	if excludedEnrichmentSource(fact.SourceID) {
		return false
	}
	for _, part := range strings.Split(fact.Method, ";") {
		if strings.TrimSpace(part) == "exact provider channel number" {
			return false
		}
	}
	if (strings.Contains(fact.Method, "pair-level identity (") || strings.Contains(fact.Method, "category carried from")) && !strings.Contains(fact.Method, "identity-policy-v2") {
		return false
	}
	return true
}

func excludedEnrichmentSource(id string) bool {
	return id == "afn-official-guide" || id == "glorystar-official-lineup"
}

// Historical observations remain on disk but excluded-provider-only names
// no longer enrich drafts. Unknown legacy origins are preserved.
func (s *Service) allowedEnrichmentOrigins(keys []string) bool {
	if len(keys) == 0 {
		return true
	}
	for _, key := range keys {
		record := s.index.Lineups[key]
		if record == nil || !ExcludedEnrichmentProvider(record.ProviderName) {
			return true
		}
	}
	return false
}

// SortedStationIDs is useful to downstream consumers that need deterministic
// traversal of the persisted index.
func SortedStationIDs(index Index) []string {
	ids := make([]string, 0, len(index.Stations))
	for id := range index.Stations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
