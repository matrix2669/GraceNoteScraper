package providersource

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

type categoryReadOnlyClients struct{}

func (categoryReadOnlyClients) FindProviders(context.Context, string, string, string) (*web.ProviderResponse, error) {
	panic("unexpected provider request")
}
func (categoryReadOnlyClients) FetchGrid(context.Context, web.Preferences, int64) (*web.GridResponse, error) {
	panic("unexpected grid request")
}

func TestProviderCategoryBucketAdmissionThroughPersistedDraft(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{{"Other", "Uncategorized"}, {" other ", "Uncategorized"}, {"Adult", "Other"}} {
		t.Run(tc.raw, func(t *testing.T) {
			result := matchCatalog(lineupindex.ProviderEvidenceRequest{LineupKey: "L1", Grid: &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: "S1", CallSign: "UNBRANDEDTEST"}}}}, catalogSource{ID: "dish-official-lineup", Label: "DISH", Entries: []catalogEntry{{Name: "UNBRANDEDTEST", Category: tc.raw}}})
			if len(result.CategoryRelations) == 0 {
				t.Fatal("adapter did not retain relationship fixture")
			}
			station := &lineupindex.Station{StationID: "S1", Names: []lineupindex.StationName{{Value: "UNBRANDEDTEST", Normalized: "UNBRANDEDTEST", Kind: lineupindex.NameCallSign}}}
			for _, fact := range result.Facts {
				station.Facts = append(station.Facts, lineupindex.StationFact{Kind: fact.Kind, Value: fact.Value, RawValue: fact.RawValue, SourceID: fact.SourceID, Method: fact.Method, SourceRowID: fact.SourceRowID})
			}
			idx := lineupindex.Index{SchemaVersion: lineupindex.CurrentIndexVersion, Stations: map[string]*lineupindex.Station{"S1": station}, CategoryRelations: result.CategoryRelations}
			data, err := json.Marshal(idx)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "index.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			index, err := lineupindex.NewService(lineupindex.ServiceConfig{Path: path, Providers: categoryReadOnlyClients{}, Grids: categoryReadOnlyClients{}})
			if err != nil {
				t.Fatal(err)
			}
			candidate, found := index.CategoriesForStations([]string{"S1"})["S1"]
			input := builder.InputChannel{Key: "S1", StationID: "S1", CallSign: "UNBRANDEDTEST"}
			if found {
				input.CategoryHint = &builder.AttributedCategory{Value: candidate.Value, Priority: candidate.Priority, Source: "dish-official-lineup", Method: strings.Join(candidate.Methods, "; ")}
			}
			if (tc.want == "Uncategorized" && found) || (tc.want == "Other" && (!found || candidate.Priority != 2)) {
				t.Fatalf("candidate=%+v found=%v", candidate, found)
			}
			store, err := builder.LoadStateStore(filepath.Join(t.TempDir(), "lineuparr.json"))
			if err != nil {
				t.Fatal(err)
			}
			draft, err := builder.NewService(store, builder.ServiceOptions{}).Build(context.Background(), builder.LineupContext{SourceFingerprint: "test"}, []builder.InputChannel{input})
			if err != nil {
				t.Fatal(err)
			}
			if len(draft.Channels) != 1 || draft.Channels[0].Category != tc.want {
				t.Fatalf("draft: %+v", draft)
			}
		})
	}
}

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
