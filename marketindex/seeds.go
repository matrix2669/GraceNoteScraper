package marketindex

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
)

//go:embed market_zips.json
var seedFS embed.FS

var postalCodePattern = regexp.MustCompile(`^[0-9]{5}$`)

// SeedCatalog is the ranked collection of representative market ZIP codes.
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

// LoadSeeds reads an optional operator-supplied catalog, or the embedded
// catalog when path is empty.
func LoadSeeds(path string) (SeedCatalog, error) {
	var (
		data []byte
		err  error
	)
	if strings.TrimSpace(path) == "" {
		data, err = seedFS.ReadFile("market_zips.json")
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return SeedCatalog{}, fmt.Errorf("reading market ZIP catalog: %w", err)
	}
	if len(data) > 1<<20 {
		return SeedCatalog{}, errors.New("market ZIP catalog exceeds 1 MiB")
	}

	var catalog SeedCatalog
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&catalog); err != nil {
		return SeedCatalog{}, fmt.Errorf("decoding market ZIP catalog: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return SeedCatalog{}, errors.New("decoding market ZIP catalog: expected one JSON object")
	}
	if err := validateSeeds(catalog); err != nil {
		return SeedCatalog{}, err
	}
	sort.Slice(catalog.Markets, func(i, j int) bool { return catalog.Markets[i].Rank < catalog.Markets[j].Rank })
	sum := sha256.Sum256(data)
	catalog.Digest = hex.EncodeToString(sum[:])
	return catalog, nil
}

func validateSeeds(catalog SeedCatalog) error {
	if catalog.SchemaVersion != 1 {
		return fmt.Errorf("unsupported market ZIP catalog schema %d", catalog.SchemaVersion)
	}
	if strings.TrimSpace(catalog.Name) == "" || strings.TrimSpace(catalog.AsOf) == "" || strings.TrimSpace(catalog.RankingSource) == "" || strings.TrimSpace(catalog.SelectionMethod) == "" {
		return errors.New("market ZIP catalog name, asOf, rankingSource, and selectionMethod are required")
	}
	if len(catalog.Markets) == 0 {
		return errors.New("market ZIP catalog contains no markets")
	}
	ranks := make(map[int]bool, len(catalog.Markets))
	postalCodes := make(map[string]bool, len(catalog.Markets))
	for _, market := range catalog.Markets {
		if market.Rank < 1 || market.Rank > len(catalog.Markets) {
			return fmt.Errorf("market %q has invalid rank %d", market.Name, market.Rank)
		}
		if ranks[market.Rank] {
			return fmt.Errorf("market ZIP catalog repeats rank %d", market.Rank)
		}
		ranks[market.Rank] = true
		if strings.TrimSpace(market.Name) == "" {
			return fmt.Errorf("market at rank %d has no name", market.Rank)
		}
		if strings.ToUpper(strings.TrimSpace(market.Country)) != "USA" {
			return fmt.Errorf("market %q must use country USA", market.Name)
		}
		if !postalCodePattern.MatchString(strings.TrimSpace(market.PostalCode)) {
			return fmt.Errorf("market %q has invalid ZIP code %q", market.Name, market.PostalCode)
		}
		if postalCodes[market.PostalCode] {
			return fmt.Errorf("market ZIP catalog repeats ZIP code %s", market.PostalCode)
		}
		postalCodes[market.PostalCode] = true
	}
	for rank := 1; rank <= len(catalog.Markets); rank++ {
		if !ranks[rank] {
			return fmt.Errorf("market ZIP catalog is missing rank %d", rank)
		}
	}
	return nil
}
