package lineupindex

import "testing"

func TestSummaryIncludesUniqueProviderAndEPGAliases(t *testing.T) {
	s := &Service{index: Index{Stations: map[string]*Station{"11207": {Names: []StationName{{Value: "USA", Normalized: "USA", Kind: NameCallSign}}, Facts: []StationFact{
		{Kind: FactAlias, Value: "USA Network", SourceID: "one"},
		{Kind: FactAlias, Value: "USA Network", SourceID: "two"},
		{Kind: FactAlias, Value: "USAHD", SourceID: "epg"},
		{Kind: FactAlias, Value: "Investigation Discovery", Method: "official PDF; exact provider channel number"},
	}}}}}
	got := s.summaryLocked(map[string]map[string]bool{"11207": {"USA": true}})
	if got.MeaningfulAliases != 2 || got.CurrentLineupAliases != 2 {
		t.Fatalf("summary=%+v", got)
	}
	if len(s.AliasesForStations([]string{"11207"})["11207"]) != 4 {
		t.Fatal("unsafe legacy alias survived")
	}
	if usableFact(StationFact{Kind: FactCategory, Method: "EPG; category carried from BroadStar"}) {
		t.Fatal("unverified derived category survived")
	}
	if !usableFact(StationFact{Kind: FactCategory, Method: "EPG; category carried from BroadStar; identity-policy-v2"}) {
		t.Fatal("fresh verified category suppressed")
	}
}
