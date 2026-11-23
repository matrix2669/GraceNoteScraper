package lineupindex

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func loadIndex(path string, catalog SeedCatalog, now time.Time) (Index, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return newIndex(catalog, now), nil
		}
		return Index{}, fmt.Errorf("reading market index: %w", err)
	}

	var index Index
	if err := json.Unmarshal(data, &index); err != nil {
		return Index{}, fmt.Errorf("decoding market index: %w", err)
	}
	if index.SchemaVersion != CurrentIndexVersion {
		return Index{}, fmt.Errorf("unsupported market index schema %d", index.SchemaVersion)
	}
	initializeIndexMaps(&index)
	for _, market := range index.Markets {
		if market.Status == StatusRunning {
			market.Status = StatusPending
			market.LastError = "Interrupted before completion"
		}
	}
	for _, lineup := range index.Lineups {
		if lineup.Status == StatusRunning {
			lineup.Status = StatusPending
			lineup.LastError = "Interrupted before completion"
		}
	}
	index.SeedDigest = catalog.Digest
	index.SeedAsOf = catalog.AsOf
	return index, nil
}

func newIndex(catalog SeedCatalog, now time.Time) Index {
	timestamp := now.UTC().Format(time.RFC3339)
	return Index{
		SchemaVersion: CurrentIndexVersion,
		SeedDigest:    catalog.Digest,
		SeedAsOf:      catalog.AsOf,
		CreatedAt:     timestamp,
		UpdatedAt:     timestamp,
		Markets:       make(map[string]*MarketRecord),
		Lineups:       make(map[string]*LineupRecord),
		Stations:      make(map[string]*Station),
		Batches:       []BatchReport{},
	}
}

func initializeIndexMaps(index *Index) {
	if index.Markets == nil {
		index.Markets = make(map[string]*MarketRecord)
	}
	if index.Lineups == nil {
		index.Lineups = make(map[string]*LineupRecord)
	}
	if index.Stations == nil {
		index.Stations = make(map[string]*Station)
	}
	if index.Batches == nil {
		index.Batches = []BatchReport{}
	}
}

func writeIndex(path string, index Index) error {
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding market index: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating market index directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".gracenote-market-index-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temporary market index: %w", err)
	}
	tmpName := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(0644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("setting temporary market index permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing market index: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("syncing market index: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing market index: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replacing market index: %w", err)
	}
	removeTemp = false
	return nil
}
