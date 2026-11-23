package lineupindex

import (
	"context"
	"errors"

	"github.com/daniel-widrick/GraceNoteScraper/web"
)

const (
	CurrentIndexVersion = 3
	DefaultBatchSize    = 25
	MaxBatchSize        = 25
)

var (
	ErrAlreadyRunning = errors.New("a market-index scan is already running")
	ErrNoWork         = errors.New("all curated markets have been processed")
)

const (
	StatusPending  = "pending"
	StatusRunning  = "running"
	StatusComplete = "complete"
	StatusError    = "error"
)

const (
	NameCallSign          = "callSign"
	NameEventCallSign     = "eventCallSign"
	NameAffiliateName     = "affiliateName"
	NameAffiliateCallSign = "affiliateCallSign"
)

// Index is the durable, resumable station-name catalog. Programme payloads are
// not persisted; only station identity evidence such as event callsigns is kept.
type Index struct {
	SchemaVersion int                          `json:"schemaVersion"`
	SeedDigest    string                       `json:"seedDigest"`
	SeedAsOf      string                       `json:"seedAsOf"`
	CreatedAt     string                       `json:"createdAt"`
	UpdatedAt     string                       `json:"updatedAt"`
	Markets       map[string]*MarketRecord     `json:"markets"`
	PostalScans   map[string]*PostalScanRecord `json:"postalScans,omitempty"`
	Lineups       map[string]*LineupRecord     `json:"lineups"`
	Stations      map[string]*Station          `json:"stations"`
	Batches       []BatchReport                `json:"batches"`
}

// PostalScanRecord describes an on-demand scan of every unique Gracenote
// lineup returned for the configured postal code. Unlike ranked-market scans,
// this pass also collects attributable aliases and categories from official
// provider sources.
type PostalScanRecord struct {
	Key             string                 `json:"key"`
	Country         string                 `json:"country"`
	PostalCode      string                 `json:"postalCode"`
	Status          string                 `json:"status"`
	ProviderCount   int                    `json:"providerCount"`
	LineupsScanned  int                    `json:"lineupsScanned"`
	GridRequests    int                    `json:"gridRequests"`
	Aliases         int                    `json:"aliases"`
	Categories      int                    `json:"categories"`
	EPGMatches      int                    `json:"epgMatches"`
	EPGQuestionable int                    `json:"epgQuestionable"`
	EPGRejected     int                    `json:"epgRejected"`
	EPGAliases      int                    `json:"epgAliases"`
	EPGCategories   int                    `json:"epgCategories"`
	Sources         []EvidenceSourceRecord `json:"sources,omitempty"`
	StartedAt       string                 `json:"startedAt,omitempty"`
	CompletedAt     string                 `json:"completedAt,omitempty"`
	LastError       string                 `json:"lastError,omitempty"`
}

type MarketRecord struct {
	Rank             int    `json:"rank"`
	Name             string `json:"name"`
	Country          string `json:"country"`
	PostalCode       string `json:"postalCode"`
	Status           string `json:"status"`
	ProviderCount    int    `json:"providerCount"`
	NewLineups       int    `json:"newLineups"`
	ReusedLineups    int    `json:"reusedLineups"`
	GridRequests     int    `json:"gridRequests"`
	NewStations      int    `json:"newStations"`
	NewAliases       int    `json:"newAliases"`
	CurrentAliases   int    `json:"currentLineupAliases"`
	CosmeticVariants int    `json:"cosmeticVariants"`
	Conflicts        int    `json:"conflicts"`
	StartedAt        string `json:"startedAt,omitempty"`
	CompletedAt      string `json:"completedAt,omitempty"`
	LastError        string `json:"lastError,omitempty"`
}

type LineupRecord struct {
	Key          string `json:"key"`
	LineupID     string `json:"lineupId"`
	HeadendID    string `json:"headendId"`
	ProviderName string `json:"providerName"`
	ProviderType string `json:"providerType,omitempty"`
	Device       string `json:"device"`
	Location     string `json:"location,omitempty"`
	Timezone     string `json:"timezone,omitempty"`
	Country      string `json:"country"`
	PostalCode   string `json:"postalCode"`
	Language     string `json:"language"`
	MarketRanks  []int  `json:"marketRanks"`
	Status       string `json:"status"`
	ChannelCount int    `json:"channelCount"`
	ScannedAt    string `json:"scannedAt,omitempty"`
	LastError    string `json:"lastError,omitempty"`
}

type Station struct {
	StationID    string               `json:"stationId"`
	Names        []StationName        `json:"names"`
	Observations []StationObservation `json:"observations"`
	Facts        []StationFact        `json:"facts,omitempty"`
}

const (
	FactAlias    = "alias"
	FactCategory = "category"
)

// StationFact is official evidence joined to a provider's Gracenote grid and
// therefore to a provider-independent Gracenote station ID.
type StationFact struct {
	Kind            string   `json:"kind"`
	Value           string   `json:"value"`
	Normalized      string   `json:"normalized"`
	RawValue        string   `json:"rawValue,omitempty"`
	MatchMethod     string   `json:"matchMethod,omitempty"`
	MatchConfidence float64  `json:"matchConfidence,omitempty"`
	SourceID        string   `json:"sourceId"`
	SourceLabel     string   `json:"sourceLabel"`
	SourceURL       string   `json:"sourceUrl,omitempty"`
	Method          string   `json:"method"`
	LineupKeys      []string `json:"lineupKeys"`
}

// ProviderEvidenceFetcher converts an official provider listing and its
// matching Gracenote grid into exact station-ID facts.
type ProviderEvidenceFetcher interface {
	FetchProviderEvidence(context.Context, ProviderEvidenceRequest) (ProviderEvidenceResult, error)
}

type ProviderEvidenceRequest struct {
	Provider   web.Provider
	LineupKey  string
	Country    string
	PostalCode string
	// ServiceAddress is an ephemeral, active-provider-only input. It must not
	// be persisted in the index, snapshots, logs, source URLs, or API views.
	ServiceAddress ProviderAddress `json:"-"`
	Grid           *web.GridResponse
}

// ProviderAddress contains a user-selected geocoder result for one in-memory
// scan. The containing request fields are excluded from every serialized view.
type ProviderAddress struct {
	FormattedAddress string `json:"formattedAddress,omitempty"`
	StreetAddress    string `json:"streetAddress,omitempty"`
	City             string `json:"city,omitempty"`
	State            string `json:"state,omitempty"`
	PostalCode       string `json:"postalCode,omitempty"`
	CountryCode      string `json:"countryCode,omitempty"`
}

type ProviderEvidenceResult struct {
	Facts   []ProviderFact
	Sources []EvidenceSourceRecord
}

type ProviderFact struct {
	StationID       string
	Kind            string
	Value           string
	RawValue        string
	MatchMethod     string
	MatchConfidence float64
	SourceID        string
	SourceLabel     string
	SourceURL       string
	Method          string
}

type EvidenceSourceRecord struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	URL        string `json:"url,omitempty"`
	Status     string `json:"status"`
	Matched    int    `json:"matched"`
	Aliases    int    `json:"aliases"`
	Categories int    `json:"categories"`
	Message    string `json:"message,omitempty"`
}

type StationObservation struct {
	LineupKey string `json:"lineupKey"`
	ChannelNo string `json:"channelNo,omitempty"`
}

type StationName struct {
	Value           string   `json:"value"`
	Normalized      string   `json:"normalized"`
	Kind            string   `json:"kind"`
	ObservedAs      []string `json:"observedAs,omitempty"`
	Variants        []string `json:"variants,omitempty"`
	LineupKeys      []string `json:"lineupKeys"`
	FirstMarketRank int      `json:"firstMarketRank"`
	Conflict        bool     `json:"conflict,omitempty"`
}

type AliasCandidate struct {
	StationID   string   `json:"stationId"`
	Value       string   `json:"value"`
	Kind        string   `json:"kind"`
	LineupKeys  []string `json:"lineupKeys"`
	SourceID    string   `json:"sourceId,omitempty"`
	SourceLabel string   `json:"sourceLabel,omitempty"`
	SourceURL   string   `json:"sourceUrl,omitempty"`
	Method      string   `json:"method,omitempty"`
}

type CategoryCandidate struct {
	StationID    string   `json:"stationId"`
	Value        string   `json:"value"`
	SourceIDs    []string `json:"sourceIds"`
	SourceLabels []string `json:"sourceLabels"`
	Methods      []string `json:"methods"`
}

type BatchReport struct {
	Action                   string `json:"action"`
	StartedAt                string `json:"startedAt"`
	CompletedAt              string `json:"completedAt,omitempty"`
	FromRank                 int    `json:"fromRank"`
	ToRank                   int    `json:"toRank"`
	MarketsProcessed         int    `json:"marketsProcessed"`
	ProviderLookups          int    `json:"providerLookups"`
	ProvidersFound           int    `json:"providersFound"`
	NewLineups               int    `json:"newLineups"`
	ReusedLineups            int    `json:"reusedLineups"`
	GridRequests             int    `json:"gridRequests"`
	NewStations              int    `json:"newStations"`
	NewNamesOnKnownStations  int    `json:"newNamesOnKnownStations"`
	NewCallSignAliases       int    `json:"newCallSignAliases"`
	NewAffiliateNames        int    `json:"newAffiliateNames"`
	CosmeticVariants         int    `json:"cosmeticVariants"`
	Conflicts                int    `json:"conflicts"`
	CurrentLineupAliases     int    `json:"currentLineupAliases"`
	Errors                   int    `json:"errors"`
	CumulativeLineups        int    `json:"cumulativeLineups"`
	CumulativeStations       int    `json:"cumulativeStations"`
	CumulativeAliases        int    `json:"cumulativeAliases"`
	CumulativeCurrentAliases int    `json:"cumulativeCurrentLineupAliases"`
	Stopped                  bool   `json:"stopped,omitempty"`
}

type RunRequest struct {
	Action     string `json:"action"`
	BatchSize  int    `json:"batchSize,omitempty"`
	Ranks      []int  `json:"ranks,omitempty"`
	Country    string `json:"country,omitempty"`
	PostalCode string `json:"postalCode,omitempty"`
	Language   string `json:"language,omitempty"`
	// ProviderAddress and AddressProvider are populated at the HTTP boundary
	// for one postal scan and intentionally excluded from serialization.
	ProviderAddress  ProviderAddress `json:"-"`
	AddressProvider  string          `json:"-"`
	AddressProviders []string        `json:"-"`
}

type JobView struct {
	Running         bool   `json:"running"`
	Action          string `json:"action,omitempty"`
	StartedAt       string `json:"startedAt,omitempty"`
	CompletedAt     string `json:"completedAt,omitempty"`
	CurrentRank     int    `json:"currentRank,omitempty"`
	CurrentMarket   string `json:"currentMarket,omitempty"`
	CurrentProvider string `json:"currentProvider,omitempty"`
	CompletedCount  int    `json:"completedCount"`
	TotalCount      int    `json:"totalCount"`
	LastError       string `json:"lastError,omitempty"`
}

type CatalogView struct {
	Name            string `json:"name"`
	AsOf            string `json:"asOf"`
	RankingSource   string `json:"rankingSource"`
	SelectionMethod string `json:"selectionMethod"`
	Digest          string `json:"digest"`
	MarketCount     int    `json:"marketCount"`
}

type IndexSummary struct {
	UpdatedAt             string `json:"updatedAt,omitempty"`
	CompletedMarkets      int    `json:"completedMarkets"`
	ErrorMarkets          int    `json:"errorMarkets"`
	PendingMarkets        int    `json:"pendingMarkets"`
	Lineups               int    `json:"lineups"`
	Stations              int    `json:"stations"`
	MeaningfulAliases     int    `json:"meaningfulAliases"`
	AffiliateNames        int    `json:"affiliateNames"`
	Conflicts             int    `json:"conflicts"`
	CurrentLineupAliases  int    `json:"currentLineupAliases"`
	CurrentLineupStations int    `json:"currentLineupStations"`
	NextRank              int    `json:"nextRank,omitempty"`
}

type MarketView struct {
	Rank       int           `json:"rank"`
	Name       string        `json:"name"`
	Country    string        `json:"country"`
	PostalCode string        `json:"postalCode"`
	Status     string        `json:"status"`
	Record     *MarketRecord `json:"result,omitempty"`
}

type Snapshot struct {
	Catalog    CatalogView       `json:"catalog"`
	Summary    IndexSummary      `json:"summary"`
	Job        JobView           `json:"job"`
	Markets    []MarketView      `json:"markets"`
	Batches    []BatchReport     `json:"batches"`
	PostalScan *PostalScanRecord `json:"postalScan,omitempty"`
}
