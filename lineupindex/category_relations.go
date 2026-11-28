package lineupindex

import "strings"

// EligibleProviderCategoryFacts returns reviewable category facts from source
// rows whose alias is already bound to the requested station by independent,
// accepted identity evidence. A relation never establishes that binding; a
// confirmed two-block EPG bridge is represented by the normal EPG-derived
// StationFact path and therefore remains subject to the same historical guards.
//
// Results are keyed by station and deliberately retain source metadata so the
// snapshot resolver can apply its existing source-preference and conflict
// rules. Duplicate aliases/markets from one provider do not create votes.
func (s *Service) EligibleProviderCategoryFacts(stationIDs []string, preferredSourceID string) map[string][]ProviderFact {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.eligibleProviderCategoryFactsLocked(stationIDs, preferredSourceID)
}

// eligibleProviderCategoryFactsLocked is the lock-free implementation for
// snapshot resolution, whose caller already holds the service read lock.
func (s *Service) eligibleProviderCategoryFactsLocked(stationIDs []string, preferredSourceID string) map[string][]ProviderFact {
	result := make(map[string][]ProviderFact)
	seenStations := make(map[string]bool)
	seen := make(map[string]bool)
	for _, stationID := range stationIDs {
		stationID = strings.TrimSpace(stationID)
		if stationID == "" || seenStations[stationID] {
			continue
		}
		seenStations[stationID] = true
		station := s.index.Stations[stationID]
		if station == nil {
			continue
		}
		for _, relation := range s.index.CategoryRelations {
			sourceStationID := strings.TrimSpace(relation.StationID)
			if sourceStationID == "" || relation.StationBound && !s.allowedEnrichmentOrigins(relation.LineupKeys) {
				continue
			}
			if !s.allowedEnrichmentOrigins(relation.LineupKeys) {
				continue
			}
			sourceStation := s.index.Stations[sourceStationID]
			if sourceStation == nil || !relationAliasIsBound(sourceStation, relation.AliasNormalized) {
				continue
			}
			if sourceStationID != stationID && !relationHasConfirmedEPGBinding(station, relation) {
				continue
			}
			category := strings.TrimSpace(relation.Category)
			if category == "" {
				continue
			}
			key := stationID + "\x00" + normalizeName(category) + "\x00" + strings.TrimSpace(relation.SourceID)
			if seen[key] {
				continue
			}
			seen[key] = true
			result[stationID] = append(result[stationID], ProviderFact{
				StationID: stationID, Kind: FactCategory, Value: category, RawValue: relation.RawCategory,
				SourceID: relation.SourceID, SourceLabel: relation.SourceLabel, SourceURL: relation.SourceURL,
				Method:         strings.TrimSpace(relation.Method) + "; provider-category-relation; eligible via accepted alias binding",
				SourceRevision: relation.SourceRevision, SourceRowID: relation.SourceRowID,
				RootSourceID: relation.SourceID, RootSourceLabel: relation.SourceLabel, RootSourceURL: relation.SourceURL,
				StationBound: relation.StationBound,
			})
		}
	}
	return result
}

func relationAliasIsBound(station *Station, normalized string) bool {
	normalized = normalizeName(normalized)
	if normalized == "" {
		return false
	}
	for _, name := range station.Names {
		if isCallSignKind(name.Kind) && !name.Conflict && name.Normalized == normalized {
			return true
		}
	}
	for _, fact := range station.Facts {
		if fact.Kind == FactAlias && usableFact(fact) && fact.Normalized == normalized {
			return true
		}
	}
	return false
}

func relationHasConfirmedEPGBinding(station *Station, relation ProviderCategoryRelation) bool {
	for _, fact := range station.Facts {
		if fact.Kind != FactAlias || !usableFact(fact) || fact.Normalized != relation.AliasNormalized {
			continue
		}
		if strings.TrimSpace(fact.RootSourceID) == strings.TrimSpace(relation.SourceID) && strings.TrimSpace(fact.SourceRowID) == strings.TrimSpace(relation.SourceRowID) && strings.Contains(fact.Method, "pair-level identity (") {
			return true
		}
	}
	return false
}
