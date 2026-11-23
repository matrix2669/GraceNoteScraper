package lineuparr

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/daniel-widrick/GraceNoteScraper/channelcategory"
)

//go:embed verified_station_catalog.json
var verifiedStationCatalogData []byte

type verifiedStationRow struct {
	ReviewAliases []string `json:"reviewAliases,omitempty"`
	Attestation   string   `json:"attestation,omitempty"`
	Feed          string   `json:"feed,omitempty"`
	CallSigns     []string `json:"callSigns,omitempty"`
	ServiceFamily string   `json:"serviceFamily,omitempty"`
	StationID     string   `json:"stationId"`
	Name          string   `json:"name"`
	Aliases       []string `json:"aliases"`
	Category      string   `json:"category,omitempty"`
	SourceID      string   `json:"sourceId"`
	Revision      string   `json:"revision"`
	RowID         string   `json:"rowId"`
	Path          string   `json:"path"`
}

// ApplyVerifiedStationCatalogs uses audited public registry assertions. It never
// reads provider positions or account inventories. Legacy bundled snapshots stay
// separate and cannot widen this exact-ID path.
func ApplyVerifiedStationCatalogs(inputs []InputChannel) []SourceStatus {
	return applyVerifiedStationCatalogs(inputs, false)
}

func applyVerifiedStationCatalogs(inputs []InputChannel, includeCategories bool) []SourceStatus {
	var catalog struct {
		SchemaVersion int                  `json:"schemaVersion"`
		Channels      []verifiedStationRow `json:"channels"`
	}
	if json.Unmarshal(verifiedStationCatalogData, &catalog) != nil || catalog.SchemaVersion != 1 {
		return []SourceStatus{{ID: "verified-station-catalogs", Label: "Verified station aliases", Status: "error", Message: "Verified station registry could not be decoded"}}
	}
	byID := map[string][]verifiedStationRow{}
	for _, row := range catalog.Channels {
		expected := map[string]string{"prismcast-network-catalog": "0aed952e80ec6a1bd997a7cb16a3abe256bf253c", "pbs-gracenote-station-map": "b651b3e14fb0f8d9b01610cd221b60285b4c2812", "user-attested-renames": "user-attested-2026-10-22-v1", "user-attested-review-identities": "user-attested-review-2026-10-22-v1"}[row.SourceID]
		if expected == "" || row.Revision != expected || row.RowID == "" || row.Path == "" {
			continue
		}
		if row.SourceID == "user-attested-review-identities" {
			for i := range inputs {
				if inputs[i].StationID == row.StationID && stationRowApplies(row, inputs[i]) {
					for _, value := range row.ReviewAliases {
						inputs[i].SupplementalEvidence = append(inputs[i].SupplementalEvidence, ServiceIdentityEvidence{Value: value, BindingKind: "user-attested-review-v1", SourceID: row.SourceID, SourceRowID: row.Path + "#" + row.RowID, SourceRevision: row.Revision, RootStationID: row.StationID, Policy: "review-only-user-attested-v1"})
					}
				}
			}
			continue
		}
		byID[row.StationID] = append(byID[row.StationID], row)
	}
	// Exact-ID attribution is not proof that a shared name uniquely identifies
	// one active target. Preserve ADR-015's active-lineup owner census, counting
	// station IDs rather than positions or equivalent catalog descriptors.
	aliasOwners := map[string]map[string]bool{}
	for _, input := range inputs {
		for _, row := range byID[strings.TrimSpace(input.StationID)] {
			if !stationRowApplies(row, input) {
				continue
			}
			for _, alias := range row.Aliases {
				key := providerNameKey(alias)
				if aliasOwners[key] == nil {
					aliasOwners[key] = map[string]bool{}
				}
				aliasOwners[key][strings.TrimSpace(input.StationID)] = true
			}
		}
	}
	matched, ambiguous := 0, 0
	for i := range inputs {
		rows := []verifiedStationRow{}
		for _, row := range byID[strings.TrimSpace(inputs[i].StationID)] {
			if stationRowApplies(row, inputs[i]) {
				rows = append(rows, row)
			}
		}
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
				key := providerNameKey(alias)
				if key == "" || len(aliasOwners[key]) != 1 {
					ambiguous++
					continue
				}
				proof := &ServiceIdentityEvidence{BindingKind: "station-id-catalog-v1", SourceRevision: row.Revision, Value: alias, SourceID: row.SourceID, SourceRowID: row.Path + "#" + row.RowID, RootStationID: inputs[i].StationID, Policy: "verified-portable-station-alias-v1"}
				method := "verified portable exact station-ID assertion at " + row.Revision + " " + row.Path + "#" + row.RowID
				if row.Attestation == "USER-ATTESTED" {
					proof.BindingKind = "user-attested-station-v1"
					proof.Policy = "user-attested-rename-v1"
					proof.Feed = row.Feed
					proof.ServiceFamily = row.ServiceFamily
					method = "USER-ATTESTED historical rename; exact station and raw callsign; " + row.Revision + " " + row.RowID + "; not independently verified primary proof"
				}
				inputs[i].ExternalAliases = append(inputs[i].ExternalAliases, AttributedAlias{Value: alias, Source: row.SourceID, Method: method, Identity: proof})
			}
			if includeCategories && row.Attestation != "USER-ATTESTED" {
				applyVerifiedCategory(&inputs[i], row)
			}
		}
	}
	categoryStatus := "category enrichment is disabled"
	if includeCategories {
		categoryStatus = "reviewed category enrichment is enabled"
	}
	return []SourceStatus{{ID: "verified-station-catalogs", Label: "Station alias catalogs (PrismCast, PBS and user attestations)", Status: "embedded", Matched: matched, Ambiguous: ambiguous, Message: fmt.Sprintf("%d exact station-ID matches; provider channel numbers are never join keys; shared aliases and ambiguous bindings are withheld; %s", matched, categoryStatus)}}
}

func applyVerifiedCategory(input *InputChannel, row verifiedStationRow) {
	// Use only the reviewed taxonomy attached to the very same admitted source
	// row. It does not depend on an ambiguous shared alias or override a user edit.
	category, ok := channelcategory.Resolve(row.Category, row.Name)
	if row.Category == "" || !ok || input.CategoryConflict {
		return
	}
	candidate := &AttributedCategory{Value: category.Category, Source: row.SourceID,
		Label: "Verified station catalog", Method: "reviewed exact station-ID category at " + row.Revision + " " + row.Path + "#" + row.RowID + "; " + category.Method}
	if old := input.CategoryHint; old != nil && old.Source != "gracenote-schedule" && !strings.EqualFold(strings.TrimSpace(old.Value), candidate.Value) {
		input.CategoryHint = nil
		input.CategoryConflict = true
		return
	}
	input.CategoryHint = candidate
}

// User assertions require both the audited station ID and current raw callsign.
// A provider position, historical station fact or suffix guess cannot admit one.
func stationRowApplies(row verifiedStationRow, input InputChannel) bool {
	if row.SourceID != "user-attested-renames" && row.SourceID != "user-attested-review-identities" {
		return true
	}
	if row.Attestation != "USER-ATTESTED" || (row.SourceID == "user-attested-renames" && (row.ServiceFamily == "" || (row.Feed != "east" && row.Feed != "west"))) {
		return false
	}
	for _, code := range row.CallSigns {
		if code != "" && strings.EqualFold(strings.TrimSpace(input.CallSign), code) {
			return true
		}
	}
	return false
}
