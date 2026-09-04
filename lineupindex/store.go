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
	initializeIndexMaps(&index)
	if index.SchemaVersion == 1 {
		if err := writeIndex(path+".schema-1.bak", index); err != nil {
			return Index{}, fmt.Errorf("backing up market index before evidence migration: %w", err)
		}
		migrateIndexV1(&index, now)
		if err := writeIndex(path, index); err != nil {
			return Index{}, fmt.Errorf("saving migrated market index: %w", err)
		}
	} else if index.SchemaVersion == 2 {
		if err := writeIndex(path+".schema-2.bak", index); err != nil {
			return Index{}, fmt.Errorf("backing up market index before provider-evidence migration: %w", err)
		}
		migrateIndexV2(&index, now)
		if err := writeIndex(path, index); err != nil {
			return Index{}, fmt.Errorf("saving migrated market index: %w", err)
		}
	} else if index.SchemaVersion != CurrentIndexVersion {
		return Index{}, fmt.Errorf("unsupported market index schema %d", index.SchemaVersion)
	}
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
	for _, postal := range index.PostalScans {
		if postal.Status == StatusRunning {
			postal.Status = StatusPending
			postal.LastError = "Interrupted before completion"
		}
	}
	// Preserve legacy catalog metadata when loading without a ranked catalog.
	if catalog.Digest != "" {
		index.SeedDigest = catalog.Digest
	}
	if catalog.AsOf != "" {
		index.SeedAsOf = catalog.AsOf
	}
	return index, nil
}

func migrateIndexV1(index *Index, now time.Time) {
	index.SchemaVersion = 2
	index.UpdatedAt = now.UTC().Format(time.RFC3339)
	for _, market := range index.Markets {
		market.Status = StatusPending
		market.LastError = "Refresh required for event-callsign alias evidence"
	}
	for _, lineup := range index.Lineups {
		lineup.Status = StatusPending
		lineup.LastError = "Refresh required for event-callsign alias evidence"
	}
	for _, station := range index.Stations {
		for nameIndex := range station.Names {
			if len(station.Names[nameIndex].ObservedAs) == 0 {
				station.Names[nameIndex].ObservedAs = []string{station.Names[nameIndex].Kind}
			}
		}
	}
	index.Batches = []BatchReport{}
	migrateIndexV2(index, now)
}

func migrateIndexV2(index *Index, now time.Time) {
	index.SchemaVersion = CurrentIndexVersion
	index.UpdatedAt = now.UTC().Format(time.RFC3339)
	if index.PostalScans == nil {
		index.PostalScans = make(map[string]*PostalScanRecord)
	}
	for _, station := range index.Stations {
		if station.Facts == nil {
			station.Facts = []StationFact{}
		}
	}
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
		PostalScans:   make(map[string]*PostalScanRecord),
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
	if index.PostalScans == nil {
		index.PostalScans = make(map[string]*PostalScanRecord)
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
