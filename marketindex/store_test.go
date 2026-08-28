package marketindex

import (
	"os"
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

func TestLoadIndexMigratesV1AndQueuesEvidenceRefresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "market_index.json")
	catalog := testCatalog(MarketSeed{Rank: 1, Name: "New York", Country: "USA", PostalCode: "10001"})
	index := newIndex(catalog, time.Now())
	index.SchemaVersion = 1
	index.Markets["1"] = &MarketRecord{Rank: 1, Name: "New York", Country: "USA", PostalCode: "10001", Status: StatusComplete}
	index.Lineups["L1"] = &LineupRecord{Key: "L1", LineupID: "L1", Status: StatusComplete}
	index.Stations["S1"] = &Station{StationID: "S1", Names: []StationName{{Value: "ONE", Normalized: "ONE", Kind: NameCallSign}}}
	index.Batches = []BatchReport{{Action: "continue"}}
	if err := writeIndex(path, index); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadIndex(path, catalog, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SchemaVersion != CurrentIndexVersion || loaded.Markets["1"].Status != StatusPending || loaded.Lineups["L1"].Status != StatusPending {
		t.Fatalf("migrated index = schema %d market %q lineup %q", loaded.SchemaVersion, loaded.Markets["1"].Status, loaded.Lineups["L1"].Status)
	}
	if len(loaded.Stations["S1"].Names[0].ObservedAs) != 1 || loaded.Stations["S1"].Names[0].ObservedAs[0] != NameCallSign {
		t.Fatalf("observed kinds = %+v", loaded.Stations["S1"].Names[0].ObservedAs)
	}
	if len(loaded.Batches) != 0 {
		t.Fatalf("stale batch reports were retained: %+v", loaded.Batches)
	}
	if _, err := os.Stat(path + ".schema-1.bak"); err != nil {
		t.Fatalf("migration backup: %v", err)
	}
}
