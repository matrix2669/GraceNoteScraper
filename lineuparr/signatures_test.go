package lineuparr

import (
	"encoding/json"
	"testing"
)

func TestExportSignaturesSeparateIncludedCategoryAndAliasChanges(t *testing.T) {
	base := ExportFile{Categories: map[string][]ExportChannel{
		"News":   {{Name: "NEWS", Number: 2, Aliases: []string{"News HD"}, EPGIDs: []string{"epg.news"}}},
		"Movies": {{Name: "MOVIES", Number: 3, Aliases: []string{"Movies East"}}},
	}}
	sign := func(value ExportFile) ExportSignatures {
		t.Helper()
		result, err := SignExport(value)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	original := sign(base)

	category := ExportFile{Categories: map[string][]ExportChannel{
		"Entertainment": {base.Categories["News"][0]},
		"Movies":        {base.Categories["Movies"][0]},
	}}
	categorySignature := sign(category)
	if categorySignature.Included != original.Included || categorySignature.Categories == original.Categories || categorySignature.Aliases != original.Aliases {
		t.Fatalf("category change was not isolated: base=%+v changed=%+v", original, categorySignature)
	}

	alias := base
	alias.Categories = map[string][]ExportChannel{
		"News":   {{Name: "NEWS", Number: 2, Aliases: []string{"News Network"}, EPGIDs: []string{"epg.news"}}},
		"Movies": {base.Categories["Movies"][0]},
	}
	aliasSignature := sign(alias)
	if aliasSignature.Included != original.Included || aliasSignature.Categories != original.Categories || aliasSignature.Aliases == original.Aliases {
		t.Fatalf("alias change was not isolated: base=%+v changed=%+v", original, aliasSignature)
	}

	included := ExportFile{Categories: map[string][]ExportChannel{"News": base.Categories["News"]}}
	includedSignature := sign(included)
	if includedSignature.Included == original.Included {
		t.Fatal("included channel change was not detected")
	}
	newsKey := hashSignature([]string{"2"})
	if includedSignature.Rows[newsKey].Count != 1 || includedSignature.Rows[newsKey].Aliases[0] != original.Rows[newsKey].Aliases[0] || includedSignature.Rows[newsKey].Categories[0] != original.Rows[newsKey].Categories[0] {
		t.Fatalf("common row signatures changed with membership: base=%+v changed=%+v", original.Rows[newsKey], includedSignature.Rows[newsKey])
	}
}

func TestExportSignaturesRoundTripAndIgnorePresentationMetadata(t *testing.T) {
	export := ExportFile{
		Package: "One", Date: "2026-09-07", Description: "First", Source: "Source A",
		Categories: map[string][]ExportChannel{"Sports": {{Name: "SPORTS", Number: "12.1", Aliases: []string{"B", "A"}, EPGIDs: []string{"2", "1"}}}},
	}
	first, err := SignExport(export)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(export)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SignExportJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	export.Package, export.Date, export.Description, export.Source = "Two", "2027-01-01", "Second", "Source B"
	third, err := SignExport(export)
	if err != nil {
		t.Fatal(err)
	}
	if first.Included != second.Included || first.Categories != second.Categories || first.Aliases != second.Aliases || first.Included != third.Included || first.Categories != third.Categories || first.Aliases != third.Aliases {
		t.Fatalf("round trip or metadata changed signatures: first=%+v second=%+v third=%+v", first, second, third)
	}
}

func TestDraftCustomizationSignatureCoversAllPositions(t *testing.T) {
	draft := &Draft{Channels: []DraftChannel{{ID: "one", Included: true, Category: "News"}, {ID: "two", Included: false, Category: "Movies"}}}
	if err := AttachDraftSignatures(draft); err != nil {
		t.Fatal(err)
	}
	base := draft.CustomizationSignature
	draft.Channels[1].Category = "Sports"
	if err := AttachDraftSignatures(draft); err != nil {
		t.Fatal(err)
	}
	if draft.CustomizationSignature == base {
		t.Fatal("excluded-position category change did not invalidate customization")
	}
}
