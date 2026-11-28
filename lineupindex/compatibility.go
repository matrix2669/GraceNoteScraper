package lineupindex

// Catalog descriptors decode the versioned, fixed ranked-market discovery list.
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
