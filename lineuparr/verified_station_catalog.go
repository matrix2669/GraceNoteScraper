package lineuparr

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed verified_station_catalog.json
var verifiedStationCatalogData []byte

type verifiedStationRow struct {
	StationID string   `json:"stationId"`
	Name      string   `json:"name"`
	Aliases   []string `json:"aliases"`
	SourceID  string   `json:"sourceId"`
	Revision  string   `json:"revision"`
	RowID     string   `json:"rowId"`
	Path      string   `json:"path"`
}

// ApplyVerifiedStationCatalogs uses audited public registry assertions. It never
// reads provider positions or account inventories. Legacy bundled snapshots stay
// separate and cannot widen this exact-ID path.
func ApplyVerifiedStationCatalogs(inputs []InputChannel) []SourceStatus {
	var catalog struct {
		SchemaVersion int                  `json:"schemaVersion"`
		Channels      []verifiedStationRow `json:"channels"`
	}
	if json.Unmarshal(verifiedStationCatalogData, &catalog) != nil || catalog.SchemaVersion != 1 {
		return []SourceStatus{{ID: "verified-station-catalogs", Label: "Verified station aliases", Status: "error", Message: "Verified station registry could not be decoded"}}
	}
	byID := map[string][]verifiedStationRow{}
	for _, row := range catalog.Channels {
		expected := map[string]string{"prismcast-network-catalog": "0aed952e80ec6a1bd997a7cb16a3abe256bf253c", "pbs-gracenote-station-map": "b651b3e14fb0f8d9b01610cd221b60285b4c2812"}[row.SourceID]
		if expected == "" || row.Revision != expected || row.RowID == "" || row.Path == "" {
			continue
		}
		byID[row.StationID] = append(byID[row.StationID], row)
	}
	matched := 0
	for i := range inputs {
		rows := byID[strings.TrimSpace(inputs[i].StationID)]
		if len(rows) == 0 {
			continue
		}
		names := map[string]bool{}
		for _, r := range rows {
			names[strings.ToLower(r.Name)] = true
		}
		if len(names) != 1 {
			continue
		}
		matched++
		for _, row := range rows {
			for _, alias := range row.Aliases {
				proof := &ServiceIdentityEvidence{BindingKind: "station-id-catalog-v1", SourceRevision: row.Revision, Value: alias, SourceID: row.SourceID, SourceRowID: row.Path + "#" + row.RowID, RootStationID: inputs[i].StationID, Policy: "verified-portable-station-alias-v1"}
				inputs[i].ExternalAliases = append(inputs[i].ExternalAliases, AttributedAlias{Value: alias, Source: row.SourceID, Method: "verified portable exact station-ID assertion at " + row.Revision + " " + row.Path + "#" + row.RowID, Identity: proof})
			}
		}
	}
	return []SourceStatus{{ID: "verified-station-catalogs", Label: "Verified station aliases (PrismCast and PBS)", Status: "embedded", Matched: matched, Message: fmt.Sprintf("%d exact station-ID matches; provider channel numbers are never join keys; ambiguous registry bindings are inactive", matched)}}
}
