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
