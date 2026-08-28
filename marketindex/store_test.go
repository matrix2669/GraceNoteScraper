package marketindex

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLoadIndexTurnsInterruptedWorkBackIntoPendingWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "market_index.json")
	catalog := testCatalog(MarketSeed{Rank: 1, Name: "New York", Country: "USA", PostalCode: "10001"})
	index := newIndex(catalog, time.Now())
	index.Markets["1"] = &MarketRecord{Rank: 1, Name: "New York", Country: "USA", PostalCode: "10001", Status: StatusRunning}
	index.Lineups["L1"] = &LineupRecord{Key: "L1", LineupID: "L1", Status: StatusRunning}
	if err := writeIndex(path, index); err != nil {
		t.Fatalf("writeIndex() error = %v", err)
	}

	loaded, err := loadIndex(path, catalog, time.Now())
	if err != nil {
		t.Fatalf("loadIndex() error = %v", err)
	}
	if loaded.Markets["1"].Status != StatusPending || loaded.Lineups["L1"].Status != StatusPending {
		t.Fatalf("interrupted states = market %q, lineup %q", loaded.Markets["1"].Status, loaded.Lineups["L1"].Status)
	}
}
