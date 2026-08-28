package marketindex

import (
	"sort"
	"strconv"
)

func (s *Service) Snapshot() Snapshot {
	current := normalizeCurrentStations(s.readCurrentStations())
	s.mu.RLock()
	defer s.mu.RUnlock()

	markets := make([]MarketView, 0, len(s.catalog.Markets))
	for _, seed := range s.catalog.Markets {
		view := MarketView{
			Rank:       seed.Rank,
			Name:       seed.Name,
			Country:    seed.Country,
			PostalCode: seed.PostalCode,
			Status:     StatusPending,
		}
		if record := s.index.Markets[strconv.Itoa(seed.Rank)]; record != nil {
			copy := *record
			view.Record = &copy
			view.Status = record.Status
			if record.Status == StatusComplete && record.PostalCode != seed.PostalCode {
				view.Status = StatusPending
			}
		}
		markets = append(markets, view)
	}
	batches := append([]BatchReport(nil), s.index.Batches...)
	return Snapshot{
		Catalog: CatalogView{
			Name:            s.catalog.Name,
			AsOf:            s.catalog.AsOf,
			RankingSource:   s.catalog.RankingSource,
			SelectionMethod: s.catalog.SelectionMethod,
			Digest:          s.catalog.Digest,
			MarketCount:     len(s.catalog.Markets),
		},
		Summary: s.summaryLocked(current),
		Job:     s.job,
		Markets: markets,
		Batches: batches,
	}
}

func (s *Service) summaryLocked(current map[string]map[string]bool) IndexSummary {
	summary := IndexSummary{
		UpdatedAt:             s.index.UpdatedAt,
		Lineups:               len(s.index.Lineups),
		Stations:              len(s.index.Stations),
		CurrentLineupStations: len(current),
	}
	for _, seed := range s.catalog.Markets {
		record := s.index.Markets[strconv.Itoa(seed.Rank)]
		if record == nil || record.PostalCode != seed.PostalCode {
			summary.PendingMarkets++
			if summary.NextRank == 0 {
				summary.NextRank = seed.Rank
			}
			continue
		}
		switch record.Status {
		case StatusComplete:
			summary.CompletedMarkets++
		case StatusError:
			summary.ErrorMarkets++
			if summary.NextRank == 0 {
				summary.NextRank = seed.Rank
			}
		default:
			summary.PendingMarkets++
			if summary.NextRank == 0 {
				summary.NextRank = seed.Rank
			}
		}
	}

	conflictingNames := make(map[string]bool)
	for stationID, station := range s.index.Stations {
		safeCallSigns := 0
		for _, name := range station.Names {
			switch name.Kind {
			case NameCallSign:
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
