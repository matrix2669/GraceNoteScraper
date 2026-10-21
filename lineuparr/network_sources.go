package lineuparr

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/daniel-widrick/GraceNoteScraper/channelcategory"
)

//go:embed network_catalog.json
var networkCatalogData []byte

//go:embed pbs_catalog.json
var pbsCatalogData []byte

type networkCatalog struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Source        networkCatalogSource `json:"source"`
	Channels      []networkChannel     `json:"channels"`
}

type networkCatalogSource struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	URL     string `json:"url"`
	License string `json:"license"`
	Commit  string `json:"commit"`
	Method  string `json:"method"`
}

type networkChannel struct {
	StationID string   `json:"stationId"`
	Name      string   `json:"name"`
	Aliases   []string `json:"aliases"`
	Category  string   `json:"category"`
	Tags      []string `json:"tags"`
	URL       string   `json:"url,omitempty"`
}

func ApplyNetworkCatalog(inputs []InputChannel) []SourceStatus {
	return applyExactStationCatalog(networkCatalogData, "prismcast-network-catalog", "PrismCast network catalog", inputs)
}

func ApplyPBSCatalog(inputs []InputChannel) []SourceStatus {
	return applyExactStationCatalog(pbsCatalogData, "pbs-gracenote-station-map", "PBS station map", inputs)
}

// ApplyEmbeddedCatalogs supplies only portable assertions from the audited
// station registry. Legacy provider-position snapshots do not enter this path.
func (s *Service) ApplyEmbeddedCatalogs(inputs []InputChannel) []SourceStatus {
	if s == nil {
		return nil
	}
	// Verified portable assertions are independent of the historical all-or-none
	// compatibility switch. Unsafe old snapshots remain available for audit only.
	return ApplyVerifiedStationCatalogs(inputs)
}

func applyExactStationCatalog(data []byte, fallbackID, fallbackLabel string, inputs []InputChannel) []SourceStatus {
	var catalog networkCatalog
	if err := json.Unmarshal(data, &catalog); err != nil || catalog.SchemaVersion != 1 {
		return []SourceStatus{{
			ID: fallbackID, Label: fallbackLabel, Status: "error",
			Message: "The embedded network catalog could not be loaded",
		}}
	}
	byStationID := make(map[string]networkChannel, len(catalog.Channels))
	for _, channel := range catalog.Channels {
		byStationID[strings.TrimSpace(channel.StationID)] = channel
	}
	aliasOwners := make(map[string]map[string]bool)
	for _, input := range inputs {
		channel, ok := byStationID[strings.TrimSpace(input.StationID)]
		if !ok {
			continue
		}
		for _, alias := range append([]string{channel.Name}, channel.Aliases...) {
			key := providerNameKey(alias)
			if key == "" {
				continue
			}
			if aliasOwners[key] == nil {
				aliasOwners[key] = make(map[string]bool)
			}
			aliasOwners[key][input.StationID] = true
		}
	}
	matched := 0
	ambiguous := 0
	for index := range inputs {
		channel, ok := byStationID[strings.TrimSpace(inputs[index].StationID)]
		if !ok {
			continue
		}
		matched++
		for _, alias := range append([]string{channel.Name}, channel.Aliases...) {
			if strings.TrimSpace(alias) == "" || len(aliasOwners[providerNameKey(alias)]) != 1 {
				continue
			}
			inputs[index].ExternalAliases = append(inputs[index].ExternalAliases, AttributedAlias{
				Value: alias, Source: catalog.Source.ID, Method: catalog.Source.Method,
			})
		}
		category, categoryOK := channelcategory.Resolve(channel.Category, append([]string{channel.Name}, channel.Aliases...)...)
		if categoryOK && !inputs[index].CategoryConflict {
			categoryMethod := category.Method
			if category.Method == channelcategory.MethodFuzzy {
				categoryMethod = fmt.Sprintf("%s %.0f%% to %q", category.Method, category.Confidence*100, category.MatchedAlias)
			}
			candidate := &AttributedCategory{
				Value: category.Category, Source: catalog.Source.ID, Label: catalog.Source.Label,
				Method: catalog.Source.Method + "; category derived from the reviewed source taxonomy; " + categoryMethod,
			}
			existing := inputs[index].CategoryHint
			if existing != nil && existing.Source != "gracenote-schedule" && !strings.EqualFold(strings.TrimSpace(existing.Value), strings.TrimSpace(candidate.Value)) {
				inputs[index].CategoryHint = nil
				inputs[index].CategoryConflict = true
				ambiguous++
			} else {
				inputs[index].CategoryHint = candidate
			}
		}
	}
	return []SourceStatus{{
		ID: catalog.Source.ID, Label: catalog.Source.Label, URL: catalog.Source.URL,
		Status: "embedded", Matched: matched, Ambiguous: ambiguous,
		Message: fmt.Sprintf("%d exact Gracenote station-ID matches; %s snapshot at %s", matched, catalog.Source.License, shortCommit(catalog.Source.Commit)),
	}}
}

func shortCommit(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 12 {
		return value[:12]
	}
	return value
}
