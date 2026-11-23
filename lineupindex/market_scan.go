package lineupindex

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

//go:embed market_zips.json
var marketSeeds []byte

type MarketProviderAudit struct {
	GridStatus     string      `json:"gridStatus"`
	Provider       string      `json:"provider"`
	Family         string      `json:"family"`
	LineupKey      string      `json:"lineupKey"`
	Access         string      `json:"access"`
	RepeatedFamily bool        `json:"repeatedFamily"`
	Yield          MarketYield `json:"yield"`
}
type MarketYield struct {
	Aliases         int `json:"aliases"`
	Categories      int `json:"categories"`
	CurrentStations int `json:"currentStations"`
}
type MarketScanView struct {
	Catalog SeedCatalog         `json:"catalog"`
	Scans   []*PostalScanRecord `json:"scans"`
	Next    *MarketSeed         `json:"next,omitempty"`
	Job     JobView             `json:"job"`
}

func scanRequestKey(request RunRequest) string {
	key := postalScanKey(strings.ToUpper(strings.TrimSpace(request.Country)), strings.ToUpper(strings.TrimSpace(request.PostalCode)))
	if request.marketRank > 0 {
		return fmt.Sprintf("market:%d:%s", request.marketRank, key)
	}
	return key
}
func marketCatalog() SeedCatalog {
	var catalog SeedCatalog
	_ = json.Unmarshal(marketSeeds, &catalog)
	return catalog
}
func (s *Service) MarketView() MarketScanView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	view := MarketScanView{Catalog: marketCatalog(), Job: s.job, Scans: []*PostalScanRecord{}}
	for _, seed := range view.Catalog.Markets {
		key := scanRequestKey(RunRequest{Country: seed.Country, PostalCode: seed.PostalCode, marketRank: seed.Rank})
		record := s.index.PostalScans[key]
		if record != nil {
			data, _ := json.Marshal(record)
			var copy PostalScanRecord
			_ = json.Unmarshal(data, &copy)
			view.Scans = append(view.Scans, &copy)
		}
		if view.Next == nil && (record == nil || record.Status != StatusComplete) {
			copy := seed
			view.Next = &copy
		}
	}
	return view
}

// StartNextMarket deliberately accepts no address or arbitrary ZIP. Exactly one
// curated market is run; legacy 25-market records do not mark it completed.
func (s *Service) StartNextMarket(comparison *LineupRecord) (JobView, error) {
	view := s.MarketView()
	if view.Job.Running {
		return view.Job, ErrAlreadyRunning
	}
	if view.Next == nil {
		return JobView{}, ErrNoWork
	}
	return s.StartMarket(view.Next.Rank, comparison)
}

// StartMarket explicitly runs one catalog market, including a completed market.
// The ZIP always comes from the curated catalog, never caller-supplied data.
func (s *Service) StartMarket(rank int, comparison *LineupRecord) (JobView, error) {
	var selected *MarketSeed
	for _, seed := range marketCatalog().Markets {
		if seed.Rank == rank {
			copy := seed
			selected = &copy
			break
		}
	}
	if selected == nil {
		return JobView{}, errors.New("unknown market rank")
	}
	if s.providerAccess == nil {
		return JobView{}, errors.New("market provider access classifier is required")
	}
	request := RunRequest{Action: "postal", Country: selected.Country, PostalCode: selected.PostalCode, Language: "en-us", marketRank: selected.Rank, priorFamilies: map[string]bool{}, priorFacts: map[string]bool{}}
	if comparison != nil {
		copy := *comparison
		request.comparison = &copy
	}
	s.mu.RLock()
	for key, lineup := range s.index.Lineups {
		if lineup.Status == StatusComplete && !strings.HasPrefix(key, scanRequestKey(request)+"|") {
			request.priorFamilies[providerFamilyKey(lineup.ProviderName)] = true
		}
	}
	for id, station := range s.index.Stations {
		for _, name := range station.Names {
			request.priorFacts[marketFactKey(id, FactAlias, name.Value)] = true
		}
		for _, fact := range station.Facts {
			if usableFact(fact) {
				request.priorFacts[marketFactKey(id, fact.Kind, fact.Value)] = true
			}
		}
	}
	s.mu.RUnlock()
	return s.startPostal(request)
}
func marketFactKey(id, kind, value string) string {
	return id + "\x00" + kind + "\x00" + strings.ToLower(strings.Join(strings.Fields(value), " "))
}
func marketFactYield(facts []ProviderFact, before map[string]bool, current map[string]map[string]bool) MarketYield {
	seen := map[string]bool{}
	stations := map[string]bool{}
	yield := MarketYield{}
	for _, fact := range facts {
		if !usableFact(StationFact{SourceID: fact.SourceID, Method: fact.Method}) {
			continue
		}
		key := marketFactKey(fact.StationID, fact.Kind, fact.Value)
		if fact.Kind == FactAlias && current[fact.StationID][normalizeName(fact.Value)] {
			continue
		}
		if seen[key] || before[key] {
			continue
		}
		seen[key] = true
		switch fact.Kind {
		case FactAlias:
			yield.Aliases++
		case FactCategory:
			yield.Categories++
		}
		if _, ok := current[fact.StationID]; ok {
			stations[fact.StationID] = true
		}
	}
	yield.CurrentStations = len(stations)
	return yield
}

// Re-evaluate the already downloaded blocks without fetching anything. Removing
// repeat-provider scans also removes bridges that depended on their schedules.
func (s *Service) auditMarketYield(key string, request RunRequest, scans []*postalLineupScan, blocks []epgBlock, current map[string]map[string]bool) {
	evaluate := func(includeRepeated bool) MarketYield {
		kept := []*postalLineupScan{}
		facts := []ProviderFact{}
		for _, scan := range scans {
			if !scan.Comparison && !includeRepeated && request.priorFamilies[providerFamilyKey(scan.Provider.Name)] {
				continue
			}
			kept = append(kept, scan)
			facts = append(facts, scan.Facts...)
		}
		if len(blocks) > 0 {
			stations, pairs := buildEPGCandidates(kept, blocks[0].ID)
			results := evaluateEPGPairs(kept, pairs, blocks)
			for _, fact := range buildEPGDerivedFacts(stations, results, "market-audit", "") {
				facts = append(facts, fact.ProviderFact)
			}
		}
		return marketFactYield(facts, request.priorFacts, current)
	}
	all, reduced := evaluate(true), evaluate(false)
	s.updatePostalJob(key, func(record *PostalScanRecord) {
		record.AllProviderYield = all
		record.NewFamilyYield = reduced
		sort.SliceStable(record.ProviderAudit, func(i, j int) bool { return record.ProviderAudit[i].Provider < record.ProviderAudit[j].Provider })
	})
}
