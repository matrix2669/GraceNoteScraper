package providersource

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

func TestMatchCatalogRetainsAlignedAliasCategoryRelation(t *testing.T) {
	grid := &web.GridResponse{Channels: []web.JSONChannel{
		{ChannelID: "S1", ChannelNo: "1", CallSign: "GRID1"},
		{ChannelID: "S2", ChannelNo: "2", CallSign: "GRID2"},
		{ChannelID: "S3", ChannelNo: "101", CallSign: "NEWSNET"},
	}}
	entries := []catalogEntry{
		{Numbers: []string{"1"}, Name: "GRID1"},
		{Numbers: []string{"2"}, Name: "GRID2"},
		{Numbers: []string{"101"}, Name: "HLN", Category: "News"},
	}
	result := matchCatalog(lineupindex.ProviderEvidenceRequest{AllowChannelNumbers: true, LineupKey: "L1", Grid: grid}, catalogSource{
		ID: "verizon-fios-official-lineup", Label: "Verizon FiOS official lineup", Entries: entries,
	})
	if facts := factsForStation(result.Facts, "S3"); len(facts) != 1 || facts[0].Kind != lineupindex.FactAlias || facts[0].Value != "HLN" {
		t.Fatalf("aligned alias facts = %+v", facts)
	}
	if len(result.CategoryRelations) != 1 {
		t.Fatalf("category relations = %+v", result.CategoryRelations)
	}
	relation := result.CategoryRelations[0]
	if relation.StationID != "S3" || relation.AliasValue != "HLN" || relation.Category != "News & Weather" || relation.SourceID != "verizon-fios-official-lineup" || relation.SourceRowID == "" || !strings.Contains(relation.Method, lineupindex.ProviderSourceAlignmentV1) {
		t.Fatalf("aligned category relation = %+v", relation)
	}
}

func TestFetchProviderEvidenceAggregatesCategoryRelationsAcrossCatalogSources(t *testing.T) {
	now := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	service := newServiceWithOptions(&http.Client{}, Options{Now: func() time.Time { return now }})
	entries := make([]catalogEntry, 0, 10)
	channels := make([]web.JSONChannel, 0, 10)
	for i := 1; i <= 9; i++ {
		name := fmt.Sprintf("GRID%d", i)
		number := strconv.Itoa(i)
		entries = append(entries, catalogEntry{Numbers: []string{number}, Name: name})
		channels = append(channels, web.JSONChannel{ChannelID: "S" + number, ChannelNo: number, CallSign: name})
	}
	entries = append(entries, catalogEntry{Numbers: []string{"101"}, Name: "HLN", Category: "News"})
	channels = append(channels, web.JSONChannel{ChannelID: "S10", ChannelNo: "101", CallSign: "OTHER"})
	service.catalog = catalog{Sources: []catalogSource{{
		ID: "fixture-provider", Label: "Fixture provider", Providers: []string{"fixture"},
		Entries: entries,
	}}}
	// Keep the always-checked national source fresh so this boundary test is
	// deterministic and does not make a network request.
	service.spectrum.file.CheckedAt = now.Add(24 * time.Hour).Format(time.RFC3339)
	result, err := service.FetchProviderEvidence(context.Background(), lineupindex.ProviderEvidenceRequest{
		AllowChannelNumbers: true, LineupKey: "L1", Provider: web.Provider{Name: "Fixture"},
		Grid: &web.GridResponse{Channels: channels},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.CategoryRelations) != 1 {
		t.Fatalf("aggregated category relations = %+v", result.CategoryRelations)
	}
	if result.CategoryRelations[0].SourceID != "fixture-provider" || result.CategoryRelations[0].SourceRevision == "" || result.CategoryRelations[0].SourceRowID == "" {
		t.Fatalf("relation lost source metadata at service boundary: %+v", result.CategoryRelations)
	}
}
