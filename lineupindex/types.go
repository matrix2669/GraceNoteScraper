package lineupindex

import "errors"

const (
	CurrentIndexVersion = 2
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
	SchemaVersion int                      `json:"schemaVersion"`
	SeedDigest    string                   `json:"seedDigest"`
	SeedAsOf      string                   `json:"seedAsOf"`
	CreatedAt     string                   `json:"createdAt"`
	UpdatedAt     string                   `json:"updatedAt"`
	Markets       map[string]*MarketRecord `json:"markets"`
	Lineups       map[string]*LineupRecord `json:"lineups"`
	Stations      map[string]*Station      `json:"stations"`
	Batches       []BatchReport            `json:"batches"`
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
	StationID  string   `json:"stationId"`
	Value      string   `json:"value"`
	Kind       string   `json:"kind"`
	LineupKeys []string `json:"lineupKeys"`
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
	Country    string `json:"country,omitempty"`
	PostalCode string `json:"postalCode,omitempty"`
	Language   string `json:"language,omitempty"`
	Action     string `json:"action"`
	BatchSize  int    `json:"batchSize,omitempty"`
	Ranks      []int  `json:"ranks,omitempty"`
}

type JobView struct {
	Running        bool   `json:"running"`
	Action         string `json:"action,omitempty"`
	StartedAt      string `json:"startedAt,omitempty"`
	CompletedAt    string `json:"completedAt,omitempty"`
	CurrentRank    int    `json:"currentRank,omitempty"`
	CurrentMarket  string `json:"currentMarket,omitempty"`
	CompletedCount int    `json:"completedCount"`
	TotalCount     int    `json:"totalCount"`
	LastError      string `json:"lastError,omitempty"`
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
	Catalog CatalogView   `json:"catalog"`
	Summary IndexSummary  `json:"summary"`
	Job     JobView       `json:"job"`
	Markets []MarketView  `json:"markets"`
	Batches []BatchReport `json:"batches"`
}
