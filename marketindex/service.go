package marketindex

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/daniel-widrick/GraceNoteScraper/web"
)

type ProviderFinder interface {
	FindProviders(ctx context.Context, country, postalCode, language string) (*web.ProviderResponse, error)
}

type GridFetcher interface {
	FetchGrid(ctx context.Context, preferences web.Preferences, at int64) (*web.GridResponse, error)
}

// CurrentStations returns station IDs and the names already used by the active
// lineup. It is used only for reporting candidate aliases relevant to that
// lineup.
type CurrentStations func() map[string][]string

type ServiceConfig struct {
	Path            string
	Catalog         SeedCatalog
	Providers       ProviderFinder
	Grids           GridFetcher
	CurrentStations CurrentStations
	ProviderDelay   time.Duration
	GridDelay       time.Duration
	Now             func() time.Time
}

type Service struct {
	mu              sync.RWMutex
	path            string
	catalog         SeedCatalog
	providers       ProviderFinder
	grids           GridFetcher
	currentStations CurrentStations
	providerDelay   time.Duration
	gridDelay       time.Duration
	now             func() time.Time
	index           Index
	job             JobView
	cancel          context.CancelFunc
}

type WebGridFetcher struct{}

func (WebGridFetcher) FetchGrid(ctx context.Context, preferences web.Preferences, at int64) (*web.GridResponse, error) {
	return web.NewClient(preferences).GetDataByTimeContext(ctx, at)
}

func NewService(config ServiceConfig) (*Service, error) {
	if strings.TrimSpace(config.Path) == "" {
		return nil, errors.New("market index path is required")
	}
	if config.Providers == nil || config.Grids == nil {
		return nil, errors.New("market index provider and grid clients are required")
	}
	if len(config.Catalog.Markets) == 0 {
		return nil, errors.New("market index seed catalog is empty")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	index, err := loadIndex(config.Path, config.Catalog, config.Now())
	if err != nil {
		return nil, err
	}
	return &Service{
		path:            config.Path,
		catalog:         config.Catalog,
		providers:       config.Providers,
		grids:           config.Grids,
		currentStations: config.CurrentStations,
		providerDelay:   config.ProviderDelay,
		gridDelay:       config.GridDelay,
		now:             config.Now,
		index:           index,
	}, nil
}

func (s *Service) Start(request RunRequest) (JobView, error) {
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if action == "" {
		action = "continue"
	}
	if action != "continue" && action != "refresh" && action != "rebuild" {
		return JobView{}, fmt.Errorf("unsupported market-index action %q", action)
	}
	batchSize := request.BatchSize
	if batchSize == 0 {
		batchSize = DefaultBatchSize
	}
	if batchSize < 1 || batchSize > MaxBatchSize {
		return JobView{}, fmt.Errorf("batch size must be between 1 and %d", MaxBatchSize)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job.Running {
		return s.job, ErrAlreadyRunning
	}

	if action == "rebuild" {
		if len(s.index.Markets) > 0 || len(s.index.Lineups) > 0 || len(s.index.Stations) > 0 {
			if err := writeIndex(s.path+".bak", s.index); err != nil {
				return JobView{}, fmt.Errorf("backing up existing market index: %w", err)
			}
		}
		rebuilt := newIndex(s.catalog, s.now())
		if err := writeIndex(s.path, rebuilt); err != nil {
			return JobView{}, err
		}
		s.index = rebuilt
	}

	seeds, err := s.selectSeedsLocked(action, request.Ranks, batchSize)
	if err != nil {
		return JobView{}, err
	}
	startedAt := s.now().UTC().Format(time.RFC3339)
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.job = JobView{
		Running:    true,
		Action:     action,
		StartedAt:  startedAt,
		TotalCount: len(seeds),
	}
	job := s.job
	go s.run(ctx, action, seeds)
	return job, nil
}

func (s *Service) Stop() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.job.Running || s.cancel == nil {
		return false
	}
	s.cancel()
	return true
}

func (s *Service) selectSeedsLocked(action string, ranks []int, batchSize int) ([]MarketSeed, error) {
	if action == "refresh" {
		if len(ranks) == 0 {
			return nil, errors.New("refresh requires at least one market rank")
		}
		if len(ranks) > MaxBatchSize {
			return nil, fmt.Errorf("refresh accepts at most %d market ranks", MaxBatchSize)
		}
		wanted := make(map[int]bool, len(ranks))
		for _, rank := range ranks {
			if rank < 1 || rank > len(s.catalog.Markets) {
				return nil, fmt.Errorf("market rank %d is outside the catalog", rank)
			}
			wanted[rank] = true
		}
		selected := make([]MarketSeed, 0, len(wanted))
		for _, seed := range s.catalog.Markets {
			if wanted[seed.Rank] {
				selected = append(selected, seed)
			}
		}
		return selected, nil
	}

	selected := make([]MarketSeed, 0, batchSize)
	for _, seed := range s.catalog.Markets {
		record := s.index.Markets[strconv.Itoa(seed.Rank)]
		if record != nil && record.Status == StatusComplete && record.PostalCode == seed.PostalCode {
			continue
		}
		selected = append(selected, seed)
		if len(selected) == batchSize {
			break
		}
	}
	if len(selected) == 0 {
		return nil, ErrNoWork
	}
	return selected, nil
}

func (s *Service) run(ctx context.Context, action string, seeds []MarketSeed) {
	report := BatchReport{
		Action:    action,
		StartedAt: s.now().UTC().Format(time.RFC3339),
		FromRank:  seeds[0].Rank,
		ToRank:    seeds[len(seeds)-1].Rank,
	}
	current := normalizeCurrentStations(s.readCurrentStations())
	owners := s.callSignOwners()
	var lastProviderRequest time.Time
	var lastGridRequest time.Time
	var fatalErr error

	for _, seed := range seeds {
		if err := ctx.Err(); err != nil {
			report.Stopped = true
			break
		}
		s.mu.Lock()
		s.job.CurrentRank = seed.Rank
		s.job.CurrentMarket = seed.Name
		s.mu.Unlock()

		if err := waitBetween(ctx, lastProviderRequest, s.providerDelay, s.now); err != nil {
			report.Stopped = true
			break
		}
		lastProviderRequest = s.now()
		force := action == "refresh"
		if err := s.processMarket(ctx, seed, force, current, owners, &lastGridRequest, &report); err != nil {
			if errors.Is(err, context.Canceled) {
				report.Stopped = true
				break
			}
			fatalErr = err
			break
		}
		report.MarketsProcessed++
		s.mu.Lock()
		s.job.CompletedCount = report.MarketsProcessed
		s.mu.Unlock()
	}

	s.mu.Lock()
	report.CompletedAt = s.now().UTC().Format(time.RFC3339)
	summary := s.summaryLocked(current)
	report.CumulativeLineups = summary.Lineups
	report.CumulativeStations = summary.Stations
	report.CumulativeAliases = summary.MeaningfulAliases
	report.CumulativeCurrentAliases = summary.CurrentLineupAliases
	s.index.Batches = append(s.index.Batches, report)
	if len(s.index.Batches) > 100 {
		s.index.Batches = append([]BatchReport(nil), s.index.Batches[len(s.index.Batches)-100:]...)
	}
	s.index.UpdatedAt = report.CompletedAt
	persistErr := writeIndex(s.path, s.index)
	s.job.Running = false
	s.job.CompletedAt = report.CompletedAt
	s.job.CurrentRank = 0
	s.job.CurrentMarket = ""
	if fatalErr != nil {
		s.job.LastError = fatalErr.Error()
	} else if persistErr != nil {
		s.job.LastError = persistErr.Error()
	} else if report.Stopped {
		s.job.LastError = "Scan stopped before the batch completed"
	} else {
		s.job.LastError = ""
	}
	s.cancel = nil
	s.mu.Unlock()
}

func (s *Service) processMarket(ctx context.Context, seed MarketSeed, force bool, current map[string]map[string]bool, owners map[string]map[string]bool, lastGridRequest *time.Time, report *BatchReport) error {
	startedAt := s.now().UTC().Format(time.RFC3339)
	record := &MarketRecord{
		Rank:       seed.Rank,
		Name:       seed.Name,
		Country:    seed.Country,
		PostalCode: seed.PostalCode,
		Status:     StatusRunning,
		StartedAt:  startedAt,
	}
	if err := s.replaceMarketRecord(seed.Rank, record); err != nil {
		return err
	}

	result, err := s.providers.FindProviders(ctx, seed.Country, seed.PostalCode, "en-us")
	report.ProviderLookups++
	if err != nil {
		if ctx.Err() != nil {
			record.Status = StatusPending
			record.LastError = "Scan stopped before this market completed"
			if persistErr := s.replaceMarketRecord(seed.Rank, record); persistErr != nil {
				return persistErr
			}
			return ctx.Err()
		}
		report.Errors++
		record.Status = StatusError
		record.LastError = err.Error()
		record.CompletedAt = s.now().UTC().Format(time.RFC3339)
		return s.replaceMarketRecord(seed.Rank, record)
	}
	if result == nil {
		report.Errors++
		record.Status = StatusError
		record.LastError = "provider lookup returned no response"
		record.CompletedAt = s.now().UTC().Format(time.RFC3339)
		return s.replaceMarketRecord(seed.Rank, record)
	}

	providers := uniqueProviders(result.Providers)
	record.ProviderCount = len(providers)
	report.ProvidersFound += len(providers)
	marketErrors := make([]string, 0)
	for _, provider := range providers {
		if err := ctx.Err(); err != nil {
			record.Status = StatusPending
			record.LastError = "Scan stopped before this market completed"
			if persistErr := s.replaceMarketRecord(seed.Rank, record); persistErr != nil {
				return persistErr
			}
			return err
		}

		lineup, isNew, needsScan, err := s.prepareLineup(seed, provider, force)
		if err != nil {
			return err
		}
		if isNew {
			record.NewLineups++
			report.NewLineups++
		} else if !needsScan {
			record.ReusedLineups++
			report.ReusedLineups++
			continue
		}

		if err := waitBetween(ctx, *lastGridRequest, s.gridDelay, s.now); err != nil {
			record.Status = StatusPending
			record.LastError = "Scan stopped before this market completed"
			if persistErr := s.replaceMarketRecord(seed.Rank, record); persistErr != nil {
				return persistErr
			}
			return err
		}
		*lastGridRequest = s.now()
		at := utcMidnight(s.now()).Unix()
		grid, err := s.grids.FetchGrid(ctx, lineupPreferences(lineup), at)
		record.GridRequests++
		report.GridRequests++
		if err != nil {
			if ctx.Err() != nil {
				record.Status = StatusPending
				record.LastError = "Scan stopped before this market completed"
				if persistErr := s.pendingLineup(lineup.Key); persistErr != nil {
					return persistErr
				}
				if persistErr := s.replaceMarketRecord(seed.Rank, record); persistErr != nil {
					return persistErr
				}
				return ctx.Err()
			}
			report.Errors++
			marketErrors = append(marketErrors, provider.Name+": "+err.Error())
			if updateErr := s.failLineup(lineup.Key, err); updateErr != nil {
				return updateErr
			}
			continue
		}
		if grid == nil {
			err = errors.New("grid lookup returned no response")
			report.Errors++
			marketErrors = append(marketErrors, provider.Name+": "+err.Error())
			if updateErr := s.failLineup(lineup.Key, err); updateErr != nil {
				return updateErr
			}
			continue
		}

		metrics, err := s.ingestGrid(seed.Rank, lineup.Key, grid, current, owners)
		if err != nil {
			return err
		}
		record.NewStations += metrics.newStations
		record.NewAliases += metrics.newCallSignAliases
		record.CurrentAliases += metrics.currentLineupAliases
		record.CosmeticVariants += metrics.cosmeticVariants
		record.Conflicts += metrics.conflicts
		report.NewStations += metrics.newStations
		report.NewNamesOnKnownStations += metrics.newNamesOnKnownStations
		report.NewCallSignAliases += metrics.newCallSignAliases
		report.NewAffiliateNames += metrics.newAffiliateNames
		report.CosmeticVariants += metrics.cosmeticVariants
		report.Conflicts += metrics.conflicts
		report.CurrentLineupAliases += metrics.currentLineupAliases
		if err := s.completeLineup(lineup.Key, len(grid.Channels)); err != nil {
			return err
		}
	}

	record.CompletedAt = s.now().UTC().Format(time.RFC3339)
	if len(marketErrors) > 0 {
		record.Status = StatusError
		record.LastError = strings.Join(marketErrors, "; ")
	} else {
		record.Status = StatusComplete
		record.LastError = ""
	}
	return s.replaceMarketRecord(seed.Rank, record)
}

func uniqueProviders(providers []web.Provider) []web.Provider {
	byLineup := make(map[string]web.Provider)
	for _, provider := range providers {
		provider.LineupID = strings.TrimSpace(provider.LineupID)
		provider.HeadendID = strings.TrimSpace(provider.HeadendID)
		if provider.LineupID == "" || provider.HeadendID == "" {
			continue
		}
		if _, exists := byLineup[provider.LineupID]; !exists {
			byLineup[provider.LineupID] = provider
		}
	}
	result := make([]web.Provider, 0, len(byLineup))
	for _, provider := range byLineup {
		result = append(result, provider)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].LineupID < result[j].LineupID })
	return result
}

func lineupPreferences(lineup *LineupRecord) web.Preferences {
	return web.Preferences{
		Country:  lineup.Country,
		ZipCode:  lineup.PostalCode,
		Headend:  lineup.HeadendID,
		LineupId: lineup.LineupID,
		Device:   lineup.Device,
		Language: lineup.Language,
	}
}

func utcMidnight(now time.Time) time.Time {
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func waitBetween(ctx context.Context, previous time.Time, delay time.Duration, now func() time.Time) error {
	if previous.IsZero() || delay <= 0 {
		return ctx.Err()
	}
	remaining := delay - now().Sub(previous)
	if remaining <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *Service) replaceMarketRecord(rank int, record *MarketRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *record
	s.index.Markets[strconv.Itoa(rank)] = &copy
	s.index.UpdatedAt = s.now().UTC().Format(time.RFC3339)
	return writeIndex(s.path, s.index)
}

func (s *Service) prepareLineup(seed MarketSeed, provider web.Provider, force bool) (*LineupRecord, bool, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := lineupStorageKey(seed, provider)
	lineup, exists := s.index.Lineups[key]
	if !exists {
		lineup = &LineupRecord{Key: key, LineupID: provider.LineupID, Status: StatusPending}
		s.index.Lineups[key] = lineup
	}
	needsScan := force || lineup.Status != StatusComplete
	if !exists || needsScan {
		lineup.HeadendID = provider.HeadendID
		lineup.ProviderName = strings.TrimSpace(provider.Name)
		lineup.ProviderType = strings.ToUpper(strings.TrimSpace(provider.Type))
		lineup.Device = strings.TrimSpace(provider.Device)
		lineup.Location = strings.TrimSpace(provider.Location)
		lineup.Country = seed.Country
		lineup.PostalCode = seed.PostalCode
		lineup.Language = "en-us"
	}
	lineup.MarketRanks = appendUniqueInt(lineup.MarketRanks, seed.Rank)
	sort.Ints(lineup.MarketRanks)
	if needsScan {
		lineup.Status = StatusRunning
		lineup.LastError = ""
	}
	s.index.UpdatedAt = s.now().UTC().Format(time.RFC3339)
	if err := writeIndex(s.path, s.index); err != nil {
		return nil, false, false, err
	}
	copy := *lineup
	return &copy, !exists, needsScan, nil
}

func lineupStorageKey(seed MarketSeed, provider web.Provider) string {
	lineupID := strings.TrimSpace(provider.LineupID)
	headendID := strings.TrimSpace(provider.HeadendID)
	if strings.EqualFold(headendID, "lineupId") || strings.EqualFold(lineupID, "USA-lineupId-DEFAULT") {
		return lineupID + "@" + seed.PostalCode
	}
	return lineupID
}

func (s *Service) failLineup(lineupKey string, scanErr error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	lineup := s.index.Lineups[lineupKey]
	lineup.Status = StatusError
	lineup.LastError = scanErr.Error()
	s.index.UpdatedAt = s.now().UTC().Format(time.RFC3339)
	return writeIndex(s.path, s.index)
}

func (s *Service) pendingLineup(lineupKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	lineup := s.index.Lineups[lineupKey]
	lineup.Status = StatusPending
	lineup.LastError = "Interrupted before completion"
	s.index.UpdatedAt = s.now().UTC().Format(time.RFC3339)
	return writeIndex(s.path, s.index)
}

func (s *Service) completeLineup(lineupKey string, channelCount int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	lineup := s.index.Lineups[lineupKey]
	lineup.Status = StatusComplete
	lineup.ChannelCount = channelCount
	lineup.ScannedAt = s.now().UTC().Format(time.RFC3339)
	lineup.LastError = ""
	s.index.UpdatedAt = lineup.ScannedAt
	return writeIndex(s.path, s.index)
}

type ingestMetrics struct {
	newStations             int
	newNamesOnKnownStations int
	newCallSignAliases      int
	newAffiliateNames       int
	cosmeticVariants        int
	conflicts               int
	currentLineupAliases    int
}

func (s *Service) ingestGrid(marketRank int, lineupKey string, grid *web.GridResponse, current map[string]map[string]bool, owners map[string]map[string]bool) (ingestMetrics, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	metrics := ingestMetrics{}
	for _, channel := range grid.Channels {
		stationID := strings.TrimSpace(channel.ChannelID)
		if stationID == "" {
			continue
		}
		station, knownStation := s.index.Stations[stationID]
		if !knownStation {
			station = &Station{StationID: stationID, Names: []StationName{}, Observations: []StationObservation{}}
			s.index.Stations[stationID] = station
			metrics.newStations++
		}
		addStationObservation(station, lineupKey, strings.TrimSpace(channel.ChannelNo))

		values := []struct {
			kind  string
			value string
		}{
			{kind: NameCallSign, value: channel.CallSign},
			{kind: NameAffiliateName, value: channel.AffiliateName},
			{kind: NameAffiliateCallSign, value: channel.AffiliateCallSign},
		}
		for _, candidate := range values {
			previousCallSigns := countNames(station, NameCallSign)
			added, cosmetic, conflict := addStationName(s.index.Stations, station, owners, candidate.kind, candidate.value, lineupKey, marketRank)
			if cosmetic {
				metrics.cosmeticVariants++
			}
			if !added {
				continue
			}
			if knownStation {
				metrics.newNamesOnKnownStations++
			}
			if candidate.kind != NameCallSign {
				metrics.newAffiliateNames++
				continue
			}
			normalized := normalizeName(candidate.value)
			if conflict {
				metrics.conflicts++
				continue
			}
			baseline, currentStation := current[stationID]
			differsFromCurrent := currentStation && len(baseline) > 0 && !baseline[normalized]
			if previousCallSigns > 0 {
				metrics.newCallSignAliases++
			}
			if differsFromCurrent {
				metrics.currentLineupAliases++
			}
		}
	}
	s.index.UpdatedAt = s.now().UTC().Format(time.RFC3339)
	if err := writeIndex(s.path, s.index); err != nil {
		return ingestMetrics{}, err
	}
	return metrics, nil
}

func addStationName(stations map[string]*Station, station *Station, owners map[string]map[string]bool, kind, value, lineupKey string, marketRank int) (added bool, cosmetic bool, conflict bool) {
	value = strings.TrimSpace(value)
	normalized := normalizeName(value)
	if ignoredName(normalized) {
		return false, false, false
	}
	for i := range station.Names {
		name := &station.Names[i]
		if name.Kind != kind || name.Normalized != normalized {
			continue
		}
		name.LineupKeys = appendUniqueString(name.LineupKeys, lineupKey)
		sort.Strings(name.LineupKeys)
		if value != name.Value && !containsString(name.Variants, value) {
			name.Variants = append(name.Variants, value)
			sort.Strings(name.Variants)
			return false, true, name.Conflict
		}
		return false, false, name.Conflict
	}

	name := StationName{
		Value:           value,
		Normalized:      normalized,
		Kind:            kind,
		LineupKeys:      []string{lineupKey},
		FirstMarketRank: marketRank,
	}
	station.Names = append(station.Names, name)
	if kind == NameCallSign {
		if owners[normalized] == nil {
			owners[normalized] = make(map[string]bool)
		}
		owners[normalized][station.StationID] = true
		if len(owners[normalized]) > 1 {
			markCallSignConflict(stations, normalized)
			return true, false, true
		}
	}
	return true, false, false
}

func markCallSignConflict(stations map[string]*Station, normalized string) {
	for _, station := range stations {
		for i := range station.Names {
			if station.Names[i].Kind == NameCallSign && station.Names[i].Normalized == normalized {
				station.Names[i].Conflict = true
			}
		}
	}
}

func (s *Service) callSignOwners() map[string]map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	owners := make(map[string]map[string]bool)
	for stationID, station := range s.index.Stations {
		for _, name := range station.Names {
			if name.Kind != NameCallSign {
				continue
			}
			if owners[name.Normalized] == nil {
				owners[name.Normalized] = make(map[string]bool)
			}
			owners[name.Normalized][stationID] = true
		}
	}
	return owners
}

func normalizeName(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToUpper(r)
		}
		return -1
	}, strings.TrimSpace(value))
}

func ignoredName(normalized string) bool {
	switch normalized {
	case "", "NA", "NONE", "NULL", "TBA", "UNKNOWN", "INDEPENDENT":
		return true
	default:
		return false
	}
}

func countNames(station *Station, kind string) int {
	count := 0
	for _, name := range station.Names {
		if name.Kind == kind {
			count++
		}
	}
	return count
}

func appendUniqueString(values []string, value string) []string {
	if value == "" || containsString(values, value) {
		return values
	}
	return append(values, value)
}

func addStationObservation(station *Station, lineupKey, channelNo string) {
	for i := range station.Observations {
		if station.Observations[i].LineupKey != lineupKey {
			continue
		}
		if channelNo != "" {
			station.Observations[i].ChannelNo = channelNo
		}
		return
	}
	station.Observations = append(station.Observations, StationObservation{LineupKey: lineupKey, ChannelNo: channelNo})
	sort.Slice(station.Observations, func(i, j int) bool { return station.Observations[i].LineupKey < station.Observations[j].LineupKey })
}

func containsString(values []string, value string) bool {
	for _, current := range values {
		if current == value {
			return true
		}
	}
	return false
}

func appendUniqueInt(values []int, value int) []int {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func (s *Service) readCurrentStations() map[string][]string {
	if s.currentStations == nil {
		return nil
	}
	return s.currentStations()
}

func normalizeCurrentStations(stations map[string][]string) map[string]map[string]bool {
	result := make(map[string]map[string]bool, len(stations))
	for stationID, names := range stations {
		normalizedNames := make(map[string]bool)
		for _, name := range names {
			normalized := normalizeName(name)
			if !ignoredName(normalized) {
				normalizedNames[normalized] = true
			}
		}
		result[stationID] = normalizedNames
	}
	return result
}
