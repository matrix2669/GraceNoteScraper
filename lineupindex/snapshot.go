package lineupindex

import (
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

	snapshot := Snapshot{Summary: s.summaryLocked(current), Job: s.job}
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

// CategoriesForStationsWithPreferredSource prefers one unambiguous category
// from the selected provider's own official source. If that source has no
// category evidence for a station, all other official sources must still agree.
func (s *Service) CategoriesForStationsWithPreferredSource(stationIDs []string, preferredSourceID string) map[string]CategoryCandidate {
	return s.categoriesForStations(stationIDs, strings.TrimSpace(preferredSourceID))
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
		allCategories := make(map[string][]StationFact)
		bestPriority := 5
		preferredCategories := make(map[string][]StationFact)
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
			priority := 2
			if fact.SourceID == "xfinity-official-lineup" || strings.Contains(fact.Method, "priority-4") || strings.Contains(fact.Method, channelcategory.MethodFuzzy) || strings.EqualFold(strings.TrimSpace(fact.RawValue), "Adult") {
				priority = 4
			}
			if priority < bestPriority {
				bestPriority = priority
				allCategories = make(map[string][]StationFact)
				preferredCategories = make(map[string][]StationFact)
			}
			if priority > bestPriority {
				continue
			}
			fact.Normalized = normalizeName(match.Category)
			if match.Method != channelcategory.MethodCanonical {
				fact.Method = appendMethod(fact.Method, "master taxonomy: "+match.Method)
			}
			allCategories[fact.Normalized] = append(allCategories[fact.Normalized], fact)
			if preferredSourceID != "" && fact.SourceID == preferredSourceID {
				preferredCategories[fact.Normalized] = append(preferredCategories[fact.Normalized], fact)
			}
		}
		byCategory := allCategories
		if len(preferredCategories) > 0 {
			byCategory = preferredCategories
		}
		if len(byCategory) != 1 {
			continue
		}
		for _, facts := range byCategory {
			candidate := CategoryCandidate{StationID: stationID, Value: facts[0].Value, Priority: bestPriority}
			for _, fact := range facts {
				candidate.SourceIDs = appendUniqueString(candidate.SourceIDs, fact.SourceID)
				candidate.SourceLabels = appendUniqueString(candidate.SourceLabels, fact.SourceLabel)
				candidate.Methods = appendUniqueString(candidate.Methods, fact.Method)
			}
			sort.Strings(candidate.SourceIDs)
			sort.Strings(candidate.SourceLabels)
			sort.Strings(candidate.Methods)
			result[stationID] = candidate
		}
	}
	return result
}

// Old number-only joins may have been made against a different headend PDF.
// Preserve their evidence on disk, but quarantine it from current drafts. Old
// EPG-carried categories also need a fresh scan through the corrected adapters.
func usableFact(fact StationFact) bool {
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
