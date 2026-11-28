package lineupindex

// Legacy catalog descriptors are retained only to decode existing evidence.
// This package has no ranked ZIP list, catalog loader, or market scanner.
type SeedCatalog struct {
	SchemaVersion   int          `json:"schemaVersion"`
	Name            string       `json:"name"`
	AsOf            string       `json:"asOf"`
	RankingSource   string       `json:"rankingSource"`
	SelectionMethod string       `json:"selectionMethod"`
	Markets         []MarketSeed `json:"markets"`
	Digest          string       `json:"-"`
}

// MarketSeed is a single provider-discovery starting point. It does not define
// a market boundary.
type MarketSeed struct {
	Rank       int    `json:"rank"`
	Name       string `json:"name"`
	Country    string `json:"country"`
	PostalCode string `json:"postalCode"`
}
