package lineupindex

import (
	"github.com/daniel-widrick/GraceNoteScraper/web"
	"testing"
)

func TestExcludedEnrichmentProviders(t *testing.T) {
	providers := []web.Provider{{Name: "AFN Satellite", LineupID: "A", HeadendID: "A"}, {Name: "GLORYSTAR", LineupID: "G", HeadendID: "G"}, {Name: "Xfinity", LineupID: "X", HeadendID: "X"}}
	got := uniquePostalProviders(providers)
	if len(got) != 1 || got[0].Name != "Xfinity" {
		t.Fatalf("providers: %+v", got)
	}
	if len(providers) != 3 {
		t.Fatal("modified setup inventory")
	}
	for _, id := range []string{"afn-official-guide", "glorystar-official-lineup"} {
		if usableFact(StationFact{SourceID: id}) {
			t.Fatalf("legacy source still usable: %s", id)
		}
	}
	s := &Service{index: Index{Lineups: map[string]*LineupRecord{"a": {ProviderName: "AFN Satellite"}, "x": {ProviderName: "Xfinity"}}}}
	if s.allowedEnrichmentOrigins([]string{"a"}) || !s.allowedEnrichmentOrigins([]string{"a", "x"}) {
		t.Fatal("wrong historical origin filtering")
	}
}
