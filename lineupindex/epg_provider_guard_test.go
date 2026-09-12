package lineupindex

import (
	"github.com/daniel-widrick/GraceNoteScraper/web"
	"testing"
)

func TestEPGRejectsUnverifiedNumberOnlyProviderEvidence(t *testing.T) {
	stations, _ := buildEPGCandidates([]*postalLineupScan{{
		Lineup: &LineupRecord{Key: "L1"}, Provider: web.Provider{Name: "BroadStar"},
		Grids: map[string]*web.GridResponse{"primary": {Channels: []web.JSONChannel{{ChannelID: "USA", ChannelNo: "104", CallSign: "USA"}}}},
		Facts: []ProviderFact{
			{StationID: "USA", Kind: FactAlias, Value: "Investigation Discovery", Method: "PDF; exact provider channel number"},
			{StationID: "USA", Kind: FactCategory, Value: "Entertainment", Method: "PDF; exact provider channel number"},
			{StationID: "USA", Kind: FactAlias, Value: "USA Network", Method: "unique exact provider callsign or name; identity-policy-v2"},
		},
	}}, "primary")
	station := stations["USA"]
	if len(station.Categories) != 0 || len(station.ProviderNames) != 1 {
		t.Fatalf("unverified facts reached EPG comparison: %+v", station)
	}
}

func TestEPGStationBoundFactsCannotBridgeGNIDs(t *testing.T) {
	blocks := testEPGBlocks()
	scans := []*postalLineupScan{
		testEPGScan("L1", "Spectrum", map[string]*web.GridResponse{blocks[0].ID: {Channels: []web.JSONChannel{{ChannelID: "A", CallSign: "LOCAL-A"}}}}),
		testEPGScan("L2", "Xfinity", map[string]*web.GridResponse{blocks[0].ID: {Channels: []web.JSONChannel{{ChannelID: "B", CallSign: "LOCAL-B"}}}}),
	}
	scans[0].Facts = []ProviderFact{
		{StationID: "A", Kind: FactAlias, Value: "SHARED NETWORK", StationBound: true},
		{StationID: "A", Kind: FactCategory, Value: "Sports", StationBound: true},
	}
	scans[1].Facts = []ProviderFact{{StationID: "B", Kind: FactAlias, Value: "SHARED NETWORK"}}
	stations, pairs := buildEPGCandidates(scans, blocks[0].ID)
	if len(stations["A"].ProviderNames) != 0 || len(stations["A"].Categories) != 0 {
		t.Fatalf("station-bound facts entered EPG identity state: %+v", stations["A"])
	}
	if len(pairs) != 0 {
		t.Fatalf("station-bound facts bridged GNIDs: %+v", pairs)
	}
}

func TestEPGStationBoundCategoryRelationsCannotBridgeGNIDs(t *testing.T) {
	blocks := testEPGBlocks()
	scans := []*postalLineupScan{
		testEPGScan("L1", "Spectrum", map[string]*web.GridResponse{blocks[0].ID: {Channels: []web.JSONChannel{{ChannelID: "A", CallSign: "LOCAL-A"}}}}),
		testEPGScan("L2", "Xfinity", map[string]*web.GridResponse{blocks[0].ID: {Channels: []web.JSONChannel{{ChannelID: "B", CallSign: "LOCAL-B"}}}}),
	}
	scans[0].Relations = []ProviderCategoryRelation{{StationID: "A", AliasValue: "SHARED NETWORK", AliasNormalized: "SHAREDNETWORK", Category: "Sports", SourceID: "spectrum-official-lineup", SourceRowID: "row-1", StationBound: true}}
	scans[1].Relations = []ProviderCategoryRelation{{StationID: "B", AliasValue: "SHARED NETWORK", AliasNormalized: "SHAREDNETWORK", Category: "Sports", SourceID: "spectrum-official-lineup", SourceRowID: "row-1"}}
	stations, pairs := buildEPGCandidates(scans, blocks[0].ID)
	if len(stations["A"].Categories) != 0 || len(pairs) != 0 {
		t.Fatalf("station-bound category relation bridged GNIDs: stations=%+v pairs=%+v", stations["A"], pairs)
	}
}
