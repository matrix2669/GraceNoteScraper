package lineupindex

import (
	"sort"
)

func (s *Service) Snapshot() Snapshot {
	current := normalizeCurrentStations(s.readCurrentStations())
	s.mu.RLock()
	defer s.mu.RUnlock()

	return Snapshot{Summary: s.summaryLocked(current), Job: s.job}
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
		safeCallSigns := 0
		for _, name := range station.Names {
			switch name.Kind {
			case NameCallSign, NameEventCallSign:
				if name.Conflict {
					conflictingNames[name.Normalized] = true
					continue
				}
				safeCallSigns++
				if baseline, ok := current[stationID]; ok && !baseline[name.Normalized] {
					summary.CurrentLineupAliases++
				}
			case NameAffiliateName, NameAffiliateCallSign:
				summary.AffiliateNames++
			}
		}
		if safeCallSigns > 1 {
			summary.MeaningfulAliases += safeCallSigns - 1
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
			if !isCallSignKind(name.Kind) || name.Conflict {
				continue
			}
			result[stationID] = append(result[stationID], AliasCandidate{
				StationID: stationID, Value: name.Value, Kind: name.Kind, LineupKeys: append([]string(nil), name.LineupKeys...),
			})
		}
		sort.SliceStable(result[stationID], func(i, j int) bool {
			return result[stationID][i].Value < result[stationID][j].Value
		})
	}
	return result
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
