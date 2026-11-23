package dispatcharr

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSupplementalImpactActualJSONLiteralMasksAndFutureExpansion(t *testing.T) {
	makeJSON := func(aliases, exclusions []string) string {
		b, _ := json.Marshal(map[string]any{"categories": map[string]any{"US| Entertainment": []any{map[string]any{"name": "Fixture Service", "number": 25001, "aliases": aliases, "excluded_aliases": exclusions}}}})
		return string(b)
	}
	names := []string{"US| Fixture Service HD", "US| Fixture Service West HD", "US| Fixture Service * literal", `US| Fixture Service \ path`, "Completely Different Service"}
	request := ImpactRequest{Country: "US", Baseline: makeJSON(nil, nil), Proposed: makeJSON([]string{"US| Fixture Service HD"}, []string{`US| Fixture Service \* literal`, `US| Fixture Service \\ path`})}
	for _, name := range names {
		request.Streams = append(request.Streams, ImpactStream{Name: name, GroupName: "US| Entertainment"})
	}
	r, err := CheckSupplementalImpact(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Proposed) != 1 {
		t.Fatal(r)
	}
	got := strings.Join(r.Proposed[0].Names, "\n")
	if !strings.Contains(got, names[0]) || strings.Contains(got, names[2]) || strings.Contains(got, names[3]) || strings.Contains(got, names[4]) {
		t.Fatalf("literal boundary: %q", got)
	}
	// Future same-service names can match a saved positive selector. The earlier
	// captured-inventory check must never be represented as future protection.
	request.Streams = append(request.Streams, ImpactStream{Name: "US: Fixture Service HD", GroupName: "US| Entertainment"})
	r, err = CheckSupplementalImpact(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(r.Proposed[0].Names, "\n"), "US: Fixture Service HD") {
		t.Fatal("fixture no longer demonstrates future name expansion", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = CheckSupplementalImpact(ctx, request); err == nil {
		t.Fatal("cancelled process succeeded")
	}
}
