package lineupindex

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/daniel-widrick/GraceNoteScraper/channelcategory"
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
	SnapshotDir     string
	Catalog         SeedCatalog
	Providers       ProviderFinder
	Grids           GridFetcher
	Evidence        ProviderEvidenceFetcher
	CurrentStations CurrentStations
	ProviderDelay   time.Duration
	GridDelay       time.Duration
	Now             func() time.Time
	ProviderAccess  func(web.Provider, string) string
}

type Service struct {
	mu              sync.RWMutex
	path            string
	snapshotDir     string
	catalog         SeedCatalog
	providers       ProviderFinder
	grids           GridFetcher
	evidence        ProviderEvidenceFetcher
	currentStations CurrentStations
	providerDelay   time.Duration
	gridDelay       time.Duration
	now             func() time.Time
	index           Index
	providerAccess  func(web.Provider, string) string
	job             JobView
	cancel          context.CancelFunc
}

type WebGridFetcher struct{}

func (WebGridFetcher) FetchGrid(ctx context.Context, preferences web.Preferences, at int64) (*web.GridResponse, error) {
	return web.NewClient(preferences).GetDataByTimeContext(ctx, at)
}

func NewService(config ServiceConfig) (*Service, error) {
	if strings.TrimSpace(config.Path) == "" {
		return nil, errors.New("lineup index path is required")
	}
	if config.Providers == nil || config.Grids == nil {
		return nil, errors.New("lineup index provider and grid clients are required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	index, err := loadIndex(config.Path, config.Catalog, config.Now())
	if err != nil {
		return nil, err
	}
	snapshotDir := strings.TrimSpace(config.SnapshotDir)
	if snapshotDir == "" {
		snapshotDir = filepath.Join(filepath.Dir(config.Path), "lineup_snapshots")
	}
	return &Service{
		providerAccess:  config.ProviderAccess,
		path:            config.Path,
		snapshotDir:     snapshotDir,
		catalog:         config.Catalog,
		providers:       config.Providers,
		grids:           config.Grids,
		evidence:        config.Evidence,
		currentStations: config.CurrentStations,
		providerDelay:   config.ProviderDelay,
		gridDelay:       config.GridDelay,
		now:             config.Now,
		index:           index,
	}, nil
}

func (s *Service) Start(request RunRequest) (JobView, error) {
	if request.ValidateSource != nil {
		if err := request.ValidateSource(); err != nil {
			return JobView{}, err
		}
	}
	if strings.ToLower(strings.TrimSpace(request.Action)) != "postal" || request.BatchSize != 0 || len(request.Ranks) != 0 {
		return JobView{}, errors.New("only configured-postal scans are supported; ranked-market scanning is not enabled")
	}
	if strings.TrimSpace(request.Country) == "" || strings.TrimSpace(request.PostalCode) == "" {
		return JobView{}, errors.New("postal scan requires country and postal code")
	}
	return s.startPostal(request)
}

// ProviderLineups resolves the same variants used by a postal scan, without
// starting a job or retaining address information.
func (s *Service) ProviderLineups(ctx context.Context, country, postalCode, language string) ([]web.Provider, error) {
	response, err := s.providers.FindProviders(ctx, country, postalCode, language)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errors.New("provider lookup returned no response")
	}
	return uniquePostalProviders(providersWithResponseTimezone(response)), nil
}

func (s *Service) startPostal(request RunRequest) (JobView, error) {
	country := strings.ToUpper(strings.TrimSpace(request.Country))
	postalCode := strings.ToUpper(strings.TrimSpace(request.PostalCode))
	language := strings.ToLower(strings.TrimSpace(request.Language))
	if country == "" || postalCode == "" {
		return JobView{}, errors.New("postal scan requires country and postal code")
	}
	if language == "" {
		language = "en-us"
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job.Running {
		return s.job, ErrAlreadyRunning
	}
	startedAt := s.now().UTC().Format(time.RFC3339)
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.job = JobView{
		Running:       true,
		Action:        "postal",
		StartedAt:     startedAt,
		CurrentMarket: postalCode,
	}
	if request.marketRank > 0 {
		s.job.Action = "market"
		s.job.CurrentRank = request.marketRank
	}
	record := &PostalScanRecord{
		Key: scanRequestKey(request), Country: country, PostalCode: postalCode, MarketRank: request.marketRank,
		Status: StatusRunning, StartedAt: startedAt,
	}
	s.index.PostalScans[record.Key] = record
	s.index.UpdatedAt = startedAt
	if err := writeIndex(s.path, s.index); err != nil {
		s.job = JobView{}
		s.cancel = nil
		cancel()
		return JobView{}, err
	}
	job := s.job
	request.Country = country
	request.PostalCode = postalCode
	request.Language = language
	go s.runPostal(ctx, request)
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

func (s *Service) runPostal(ctx context.Context, request RunRequest) {
	country := request.Country
	postalCode := request.PostalCode
	language := request.Language
	key := scanRequestKey(request)
	current := normalizeCurrentStations(s.readCurrentStations())
	owners := s.callSignOwners()
	var lastGridRequest time.Time
	var runErr error
	gridErrors := make([]string, 0)

	result, err := s.providers.FindProviders(ctx, country, postalCode, language)
	if err != nil {
		runErr = err
	} else if result == nil {
		runErr = errors.New("provider lookup returned no response")
	}
	providers := []web.Provider{}
	if runErr == nil {
		providers = uniquePostalProviders(providersWithResponseTimezone(result))
		families := map[string]bool{}
		for _, provider := range providers {
			families[providerFamilyKey(provider.Name)] = true
		}
		s.updatePostalJob(key, func(record *PostalScanRecord) {
			record.DiscoveredCount = len(result.Providers)
			record.ProviderFamilies = len(families)
			record.ProviderCount = len(providers)
		})
		s.mu.Lock()
		s.job.TotalCount = len(providers)
		s.mu.Unlock()
	}
	if runErr == nil && len(providers) == 0 {
		runErr = errors.New("Gracenote returned no lineups for this postal code")
	}
	blocks, epgTimezone, epgBlockErr := weekdayEPGBlocks(s.now(), providers, result)
	primaryGridTime := utcMidnight(s.now()).Unix()
	if epgBlockErr == nil {
		primaryGridTime = blocks[0].Start.Unix()
	}
	postalScans := make([]*postalLineupScan, 0, len(providers))

	seed := MarketSeed{Name: "Configured postal code " + postalCode, Country: country, PostalCode: postalCode}
	seenLineupIDs := make(map[string]bool)
	for _, provider := range providers {
		if err := ctx.Err(); err != nil {
			runErr = err
			break
		}
		s.mu.Lock()
		s.job.CurrentProvider = strings.TrimSpace(provider.Name)
		s.mu.Unlock()
		access := "public"
		if request.marketRank > 0 && s.providerAccess != nil {
			access = s.providerAccess(provider, postalCode)
		}

		lineupKey := lineupStorageKey(seed, provider)
		lineupIDKey := strings.ToUpper(strings.TrimSpace(provider.LineupID))
		if seenLineupIDs[lineupIDKey] {
			lineupKey = lineupVariantStorageKey(seed, provider)
		}
		seenLineupIDs[lineupIDKey] = true
		if request.marketRank > 0 {
			lineupKey = key + "|" + lineupVariantStorageKey(seed, provider)
		}
		lineup, _, _, err := s.prepareLineupWithKey(seed, provider, true, lineupKey)
		if err != nil {
			runErr = err
			break
		}
		if err := waitBetween(ctx, lastGridRequest, s.gridDelay, s.now); err != nil {
			runErr = err
			break
		}
		lastGridRequest = s.now()
		grid, err := s.grids.FetchGrid(ctx, lineupPreferences(lineup), primaryGridTime)
		s.updatePostalJob(key, func(record *PostalScanRecord) { record.GridRequests++ })
		if err != nil || grid == nil {
			if err == nil {
				err = errors.New("grid lookup returned no response")
			}
			_ = s.failLineup(lineup.Key, err)
			s.updatePostalJob(key, func(record *PostalScanRecord) {
				record.Sources = append(record.Sources, EvidenceSourceRecord{
					ID: "gracenote-grid-" + lineup.Key, Label: provider.Name + " Gracenote lineup",
					Status: StatusError, Message: err.Error(),
				})
			})
			gridErrors = append(gridErrors, provider.Name+": "+err.Error())
			if request.marketRank > 0 {
				s.updatePostalJob(key, func(record *PostalScanRecord) {
					record.ProviderAudit = append(record.ProviderAudit, MarketProviderAudit{Provider: provider.Name, Family: providerFamilyKey(provider.Name), LineupKey: lineup.Key, Access: access, GridStatus: StatusError, RepeatedFamily: request.priorFamilies[providerFamilyKey(provider.Name)]})
				})
			}
			continue
		}

		if _, err := s.ingestGrid(0, lineup.Key, grid, current, owners); err != nil {
			runErr = err
			break
		}
		evidence := ProviderEvidenceResult{}
		if s.evidence != nil && access == "public" {
			var evidenceErr error
			serviceAddress := ProviderAddress{}
			addressAllowed := sameProviderFamily(provider.Name, request.AddressProvider)
			for _, approved := range request.AddressProviders {
				addressAllowed = addressAllowed || sameProviderFamily(provider.Name, approved)
			}
			if addressAllowed {
				serviceAddress = request.ProviderAddress
			}
			evidence, evidenceErr = s.evidence.FetchProviderEvidence(ctx, ProviderEvidenceRequest{
				// This is the scanned provider's own grid, never the selected
				// comparison lineup. The adapter still requires matching identity.
				AllowChannelNumbers: true,
				Provider:            provider, LineupKey: lineup.Key, Country: country, PostalCode: postalCode,
				ServiceAddress: serviceAddress, Grid: grid,
			})
			if evidenceErr != nil {
				if len(evidence.Sources) == 0 {
					evidence.Sources = append(evidence.Sources, EvidenceSourceRecord{
						ID: "provider-evidence-" + lineup.Key, Label: provider.Name + " official source",
						Status: StatusError, Message: evidenceErr.Error(),
					})
				}
				// Official enrichment is deliberately best-effort. The source record
				// preserves the attributable failure without invalidating Gracenote
				// lineup data or successful evidence from other providers.
			}
			_, _, ingestErr := s.ingestProviderFacts(lineup.Key, evidence.Facts)
			if ingestErr != nil {
				runErr = ingestErr
				break
			}
			s.updatePostalJob(key, func(record *PostalScanRecord) {
				for _, fact := range evidence.Facts {
					if fact.Kind == FactAlias {
						record.Aliases++
					} else if fact.Kind == FactCategory {
						record.Categories++
					}
				}
				record.Sources = mergeEvidenceSources(record.Sources, evidence.Sources)
			})
		}
		if request.marketRank > 0 {
			audit := MarketProviderAudit{Provider: provider.Name, Family: providerFamilyKey(provider.Name), LineupKey: lineup.Key, Access: access, GridStatus: StatusComplete, RepeatedFamily: request.priorFamilies[providerFamilyKey(provider.Name)]}
			if access == "public" {
				audit.Access = "empty"
				if len(evidence.Facts) > 0 {
					audit.Access = "enriched"
				}
				for _, source := range evidence.Sources {
					if source.Status == StatusError {
						audit.Access = "error"
					}
				}
			}
			audit.Yield = marketFactYield(evidence.Facts, request.priorFacts, current)
			s.updatePostalJob(key, func(record *PostalScanRecord) { record.ProviderAudit = append(record.ProviderAudit, audit) })
		}
		if err := s.writeLineupSnapshot(*lineup, grid, evidence); err != nil {
			runErr = err
			break
		}
		if err := s.completeLineup(lineup.Key, len(grid.Channels)); err != nil {
			runErr = err
			break
		}
		s.updatePostalJob(key, func(record *PostalScanRecord) { record.LineupsScanned++ })
		gridID := "identity"
		if epgBlockErr == nil {
			gridID = blocks[0].ID
		}
		postalScans = append(postalScans, &postalLineupScan{
			Lineup: lineup, Provider: provider, Grids: map[string]*web.GridResponse{gridID: grid},
			Facts: append([]ProviderFact(nil), evidence.Facts...), Sources: append([]EvidenceSourceRecord(nil), evidence.Sources...),
		})
		s.mu.Lock()
		s.job.CompletedCount++
		s.mu.Unlock()
	}
	if runErr == nil && len(gridErrors) > 0 {
		runErr = errors.New(strings.Join(gridErrors, "; "))
	}
	if runErr == nil && request.marketRank > 0 && request.comparison != nil && epgBlockErr == nil {
		anchor := *request.comparison
		provider := web.Provider{Name: anchor.ProviderName, LineupID: anchor.LineupID, HeadendID: anchor.HeadendID, Device: anchor.Device, Location: anchor.Location}
		anchorSeed := MarketSeed{Country: anchor.Country, PostalCode: anchor.PostalCode}
		lineup, _, _, err := s.prepareLineupWithKey(anchorSeed, provider, true, key+"|comparison")
		if err == nil {
			err = waitBetween(ctx, lastGridRequest, s.gridDelay, s.now)
		}
		if err == nil {
			lastGridRequest = s.now()
			grid, gridErr := s.grids.FetchGrid(ctx, lineupPreferences(lineup), primaryGridTime)
			s.updatePostalJob(key, func(record *PostalScanRecord) { record.GridRequests++ })
			if gridErr != nil {
				err = gridErr
			} else if grid == nil {
				err = errors.New("comparison grid returned no data")
			} else {
				_, err = s.ingestGrid(0, lineup.Key, grid, current, owners)
				postalScans = append(postalScans, &postalLineupScan{Comparison: true, Lineup: lineup, Provider: provider, Grids: map[string]*web.GridResponse{blocks[0].ID: grid}})
			}
		}
		if err != nil {
			runErr = fmt.Errorf("selected-lineup EPG comparison: %w", err)
		}
	}
	if runErr == nil {
		if epgBlockErr != nil {
			if request.marketRank > 0 {
				runErr = epgBlockErr
			}
			s.updatePostalJob(key, func(record *PostalScanRecord) {
				record.Sources = mergeEvidenceSources(record.Sources, []EvidenceSourceRecord{{
					ID: weekdayEPGSourceID(country, postalCode), Label: "Gracenote weekday EPG confirmation",
					Status: StatusError, Message: epgBlockErr.Error(),
				}})
			})
		} else {
			epgResult, epgErr := s.runPostalEPG(ctx, key, country, postalCode, epgTimezone, postalScans, blocks, &lastGridRequest)
			if request.marketRank > 0 && epgErr != nil {
				runErr = epgErr
			}
			if errors.Is(epgErr, context.Canceled) {
				runErr = epgErr
			} else {
				s.updatePostalJob(key, func(record *PostalScanRecord) {
					record.EPGMatches = epgResult.ConfirmedPairs
					record.EPGQuestionable = epgResult.QuestionablePairs
					record.EPGRejected = epgResult.RejectedPairs
					record.EPGAliases = epgResult.Aliases
					record.EPGCategories = epgResult.Categories
					record.Aliases += epgResult.Aliases
					record.Categories += epgResult.Categories
					record.Sources = mergeEvidenceSources(record.Sources, []EvidenceSourceRecord{epgResult.Source})
				})
			}
		}
	}

	if request.marketRank > 0 {
		s.auditMarketYield(key, request, postalScans, blocks, current)
	}
	completedAt := s.now().UTC().Format(time.RFC3339)
	s.mu.Lock()
	record := s.index.PostalScans[key]
	record.CompletedAt = completedAt
	if runErr == nil {
		record.Status = StatusComplete
		record.LastError = ""
	} else if errors.Is(runErr, context.Canceled) {
		record.Status = StatusPending
		record.LastError = "Scan stopped before all lineups completed"
	} else {
		record.Status = StatusError
		record.LastError = runErr.Error()
	}
	s.index.UpdatedAt = completedAt
	persistErr := writeIndex(s.path, s.index)
	s.job.Running = false
	s.job.CompletedAt = completedAt
	s.job.CurrentMarket = ""
	s.job.CurrentProvider = ""
	if persistErr != nil {
		s.job.LastError = persistErr.Error()
	} else {
		s.job.LastError = record.LastError
	}
	s.cancel = nil
	s.mu.Unlock()
}

func sameProviderFamily(left, right string) bool {
	left = providerFamilyKey(left)
	right = providerFamilyKey(right)
	return left != "" && right != "" && (left == right || strings.Contains(left, right) || strings.Contains(right, left))
}

func providerFamilyKey(value string) string {
	value = strings.ToLower(value)
	for canonical, aliases := range map[string][]string{
		"xfinity":   {"xfinity", "comcast"},
		"optimum":   {"optimum", "cablevision"},
		"spectrum":  {"spectrum", "charter", "time warner"},
		"fios":      {"verizon", "fios"},
		"uverse":    {"u-verse", "uverse"},
		"directv":   {"directv"},
		"dish":      {"dish"},
		"afn":       {"afn"},
		"glorystar": {"glorystar"},
		"broadstar": {"broadstar"},
	} {
		for _, alias := range aliases {
			if strings.Contains(value, alias) {
				return canonical
			}
		}
	}
	for _, ignored := range []string{"digital rebuild", "satellite", "television", "tv", "of", "the", "-"} {
		value = strings.ReplaceAll(value, ignored, " ")
	}
	return strings.Join(strings.Fields(value), "")
}

func postalScanKey(country, postalCode string) string {
	return strings.ToUpper(strings.TrimSpace(country)) + ":" + strings.ToUpper(strings.TrimSpace(postalCode))
}

func (s *Service) updatePostalJob(key string, update func(*PostalScanRecord)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := s.index.PostalScans[key]
	if record == nil {
		return
	}
	update(record)
	s.index.UpdatedAt = s.now().UTC().Format(time.RFC3339)
	_ = writeIndex(s.path, s.index)
}

func mergeEvidenceSources(existing, incoming []EvidenceSourceRecord) []EvidenceSourceRecord {
	byID := make(map[string]int, len(existing))
	for index := range existing {
		byID[existing[index].ID] = index
	}
	for _, source := range incoming {
		if index, ok := byID[source.ID]; ok {
			existing[index].Matched += source.Matched
			existing[index].Aliases += source.Aliases
			existing[index].Categories += source.Categories
			if source.Status == StatusError || existing[index].Status == "" {
				existing[index].Status = source.Status
			}
			if source.Message != "" {
				existing[index].Message = source.Message
			}
			continue
		}
		byID[source.ID] = len(existing)
		existing = append(existing, source)
	}
	sort.SliceStable(existing, func(i, j int) bool { return existing[i].Label < existing[j].Label })
	return existing
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

// uniquePostalProviders retains Gracenote device variants as distinct
// configured-ZIP lineups. Gracenote commonly returns basic cable, digital,
// digital-rebuild, and generic digital grids with one shared lineup ID; each
// grid can contain different station IDs and provider positions.
func uniquePostalProviders(providers []web.Provider) []web.Provider {
	byVariant := make(map[string]web.Provider)
	for _, provider := range providers {
		if ExcludedEnrichmentProvider(provider.Name) {
			continue
		}
		provider.LineupID = strings.TrimSpace(provider.LineupID)
		provider.HeadendID = strings.TrimSpace(provider.HeadendID)
		provider.Device = strings.TrimSpace(provider.Device)
		if provider.LineupID == "" || provider.HeadendID == "" {
			continue
		}
		key := strings.ToUpper(provider.LineupID) + "\x00" + strings.ToUpper(provider.Device)
		if _, exists := byVariant[key]; !exists {
			byVariant[key] = provider
		}
	}
	result := make([]web.Provider, 0, len(byVariant))
	for _, provider := range byVariant {
		result = append(result, provider)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].LineupID != result[j].LineupID {
			return result[i].LineupID < result[j].LineupID
		}
		if result[i].Device != result[j].Device {
			return result[i].Device < result[j].Device
		}
		return result[i].Name < result[j].Name
	})
	return result
}

// ExcludedEnrichmentProvider affects enrichment only, never setup discovery or
// the selected provider's XMLTV guide.
func ExcludedEnrichmentProvider(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return strings.Contains(name, "glorystar") || strings.Contains(name, "armed forces") || name == "afn" || strings.HasPrefix(name, "afn ")
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

func (s *Service) prepareLineup(seed MarketSeed, provider web.Provider, force bool) (*LineupRecord, bool, bool, error) {
	return s.prepareLineupWithKey(seed, provider, force, lineupStorageKey(seed, provider))
}

func (s *Service) prepareLineupWithKey(seed MarketSeed, provider web.Provider, force bool, key string) (*LineupRecord, bool, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
		lineup.Timezone = strings.TrimSpace(provider.Timezone)
		lineup.Country = seed.Country
		lineup.PostalCode = seed.PostalCode
		lineup.Language = "en-us"
	}
	if seed.Rank > 0 {
		lineup.MarketRanks = appendUniqueInt(lineup.MarketRanks, seed.Rank)
		sort.Ints(lineup.MarketRanks)
	}
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

func lineupVariantStorageKey(seed MarketSeed, provider web.Provider) string {
	device := strings.ToUpper(strings.TrimSpace(provider.Device))
	if device == "" {
		device = "NONE"
	}
	device = strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '-' {
			return character
		}
		return -1
	}, device)
	if device == "" {
		device = "NONE"
	}
	return lineupStorageKey(seed, provider) + "@device=" + device
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
		for _, event := range channel.Events {
			values = append(values, struct {
				kind  string
				value string
			}{kind: NameEventCallSign, value: event.CallSign})
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
			if !isCallSignKind(candidate.kind) {
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

func (s *Service) ingestProviderFacts(lineupKey string, facts []ProviderFact) (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	aliases := 0
	categories := 0
	for _, fact := range facts {
		fact.Method = appendMethod(fact.Method, "identity-policy-v2")
		stationID := strings.TrimSpace(fact.StationID)
		value := strings.TrimSpace(fact.Value)
		kind := strings.TrimSpace(fact.Kind)
		if stationID == "" || value == "" || (kind != FactAlias && kind != FactCategory) {
			continue
		}
		if kind == FactCategory {
			match, ok := channelcategory.Resolve(value)
			if !ok {
				continue
			}
			value = match.Category
			if match.Method != channelcategory.MethodCanonical {
				fact.Method = appendMethod(fact.Method, "master taxonomy: "+match.Method)
			}
		}
		station := s.index.Stations[stationID]
		if station == nil {
			continue
		}
		normalized := normalizeName(value)
		if ignoredName(normalized) {
			continue
		}
		found := false
		for index := range station.Facts {
			current := &station.Facts[index]
			if current.Kind != kind || current.Normalized != normalized || current.SourceID != fact.SourceID {
				continue
			}
			if !usableFact(*current) {
				current.LineupKeys = nil
			}
			current.LineupKeys = appendUniqueString(current.LineupKeys, lineupKey)
			current.Method = fact.Method
			current.RawValue = fact.RawValue
			current.MatchMethod = fact.MatchMethod
			current.MatchConfidence = fact.MatchConfidence
			sort.Strings(current.LineupKeys)
			found = true
			break
		}
		if found {
			continue
		}
		station.Facts = append(station.Facts, StationFact{
			Kind: kind, Value: value, Normalized: normalized, RawValue: strings.TrimSpace(fact.RawValue),
			MatchMethod: strings.TrimSpace(fact.MatchMethod), MatchConfidence: fact.MatchConfidence,
			SourceID:    strings.TrimSpace(fact.SourceID),
			SourceLabel: strings.TrimSpace(fact.SourceLabel), SourceURL: strings.TrimSpace(fact.SourceURL),
			Method: strings.TrimSpace(fact.Method), LineupKeys: []string{lineupKey},
		})
		if kind == FactAlias {
			aliases++
		} else {
			categories++
		}
	}
	s.index.UpdatedAt = s.now().UTC().Format(time.RFC3339)
	if err := writeIndex(s.path, s.index); err != nil {
		return 0, 0, err
	}
	return aliases, categories, nil
}

func appendMethod(existing, addition string) string {
	existing = strings.TrimSpace(existing)
	addition = strings.TrimSpace(addition)
	if existing == "" {
		return addition
	}
	if addition == "" || strings.Contains(existing, addition) {
		return existing
	}
	return existing + "; " + addition
}

func addStationName(stations map[string]*Station, station *Station, owners map[string]map[string]bool, kind, value, lineupKey string, marketRank int) (added bool, cosmetic bool, conflict bool) {
	value = strings.TrimSpace(value)
	normalized := normalizeName(value)
	if ignoredName(normalized) {
		return false, false, false
	}
	for i := range station.Names {
		name := &station.Names[i]
		if name.Normalized != normalized || (isCallSignKind(name.Kind) != isCallSignKind(kind)) || (!isCallSignKind(kind) && name.Kind != kind) {
			continue
		}
		name.ObservedAs = appendUniqueString(name.ObservedAs, kind)
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
		ObservedAs:      []string{kind},
		LineupKeys:      []string{lineupKey},
		FirstMarketRank: marketRank,
	}
	station.Names = append(station.Names, name)
	if isCallSignKind(kind) {
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
			if isCallSignKind(station.Names[i].Kind) && station.Names[i].Normalized == normalized {
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
			if !isCallSignKind(name.Kind) {
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
		if name.Kind == kind || (isCallSignKind(kind) && isCallSignKind(name.Kind)) {
			count++
		}
	}
	return count
}

func isCallSignKind(kind string) bool {
	return kind == NameCallSign || kind == NameEventCallSign
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
