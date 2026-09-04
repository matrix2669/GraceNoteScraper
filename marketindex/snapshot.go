package marketindex

import (
	"sort"
	"strconv"
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
	snapshot := Snapshot{
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
	if record := s.index.PostalScans[postalScanKey(country, postalCode)]; record != nil {
		copy := *record
		copy.Sources = append([]EvidenceSourceRecord(nil), record.Sources...)
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
		for _, fact := range station.Facts {
			if fact.Kind != FactAlias {
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
		preferredCategories := make(map[string][]StationFact)
		identities := make([]string, 0, len(station.Names))
		for _, name := range station.Names {
			identities = append(identities, name.Value)
		}
		for _, fact := range station.Facts {
			if fact.Kind != FactCategory {
				continue
			}
			categoryValue := fact.Value
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
			candidate := CategoryCandidate{StationID: stationID, Value: facts[0].Value}
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
