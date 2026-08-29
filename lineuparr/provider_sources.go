package lineuparr

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
	"unicode"
)

//go:embed provider_sources.json
var providerSourcesData []byte

type providerSourceCatalog struct {
	AsOf      string                `json:"asOf"`
	Providers []ProviderGuideSource `json:"providers"`
	Renames   []providerRename      `json:"renames"`
}

type ProviderGuideSource struct {
	ID           string               `json:"id"`
	Names        []string             `json:"names"`
	Label        string               `json:"label"`
	URL          string               `json:"url"`
	Access       string               `json:"access"`
	LocationMode string               `json:"locationMode,omitempty"`
	Routes       []ProviderGuideRoute `json:"routes,omitempty"`
}

type ProviderGuideRoute struct {
	URL                string                      `json:"url"`
	Access             string                      `json:"access"`
	LocationMode       string                      `json:"locationMode,omitempty"`
	PostalPrefixRanges []ProviderPostalPrefixRange `json:"postalPrefixRanges,omitempty"`
	Locations          []string                    `json:"locations,omitempty"`
}

type ProviderPostalPrefixRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type providerRename struct {
	Current  []string `json:"current"`
	Aliases  []string `json:"aliases"`
	SourceID string   `json:"sourceId"`
	Method   string   `json:"method"`
}

func loadProviderSourceCatalog() providerSourceCatalog {
	var catalog providerSourceCatalog
	_ = json.Unmarshal(providerSourcesData, &catalog)
	return catalog
}

func ProviderGuideSources() []ProviderGuideSource {
	catalog := loadProviderSourceCatalog()
	return append([]ProviderGuideSource(nil), catalog.Providers...)
}

func ProviderGuideSourceFor(providerName string) (ProviderGuideSource, bool) {
	return ProviderGuideSourceForLineup(providerName, "", "")
}

// ProviderGuideSourceForLineup resolves providers whose official guide entry
// point varies by service area. The location is Gracenote's non-secret lineup
// label, not a subscriber address.
func ProviderGuideSourceForLineup(providerName, location, postalCode string) (ProviderGuideSource, bool) {
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	if providerName == "" {
		return ProviderGuideSource{}, false
	}
	for _, source := range loadProviderSourceCatalog().Providers {
		for _, name := range source.Names {
			if strings.Contains(providerName, strings.ToLower(name)) {
				for _, route := range source.Routes {
					if !providerGuideRouteMatches(route, providerName, location, postalCode) {
						continue
					}
					source.URL = route.URL
					source.Access = route.Access
					source.LocationMode = route.LocationMode
					break
				}
				source.Routes = nil
				return source, true
			}
		}
	}
	return ProviderGuideSource{}, false
}

func ApplyProviderGuideAliases(providerName string, inputs []InputChannel) []SourceStatus {
	return ApplyProviderGuideAliasesForLineup(providerName, "", "", inputs)
}

func ApplyProviderGuideAliasesForLineup(providerName, location, postalCode string, inputs []InputChannel) []SourceStatus {
	catalog := loadProviderSourceCatalog()
	matchedSource, matchedProvider := ProviderGuideSourceForLineup(providerName, location, postalCode)
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	statusByID := make(map[string]*SourceStatus)
	if matchedProvider {
		statusByID[matchedSource.ID] = &SourceStatus{
			ID: "provider-guide-" + matchedSource.ID, Label: matchedSource.Label + " official lineup", URL: matchedSource.URL,
			Status: "registered", Access: matchedSource.Access, LocationMode: matchedSource.LocationMode,
			Message: "Maintained provider guide source (" + matchedSource.Access + "); aliases require attributable exact evidence",
		}
	}

	sources := make(map[string]ProviderGuideSource, len(catalog.Providers))
	for _, source := range catalog.Providers {
		sources[source.ID] = source
	}
	for index := range inputs {
		identities := make(map[string]bool)
		for _, value := range append([]string{inputs[index].CallSign, inputs[index].Affiliate}, inputs[index].EventCallSigns...) {
			identities[providerNameKey(value)] = true
		}
		for _, rule := range catalog.Renames {
			matched := false
			for _, identity := range append(append([]string(nil), rule.Current...), rule.Aliases...) {
				if identities[providerNameKey(identity)] {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			source, ok := sources[rule.SourceID]
			if !ok {
				continue
			}
			status := statusByID[source.ID]
			if status == nil {
				status = &SourceStatus{
					ID: "provider-guide-" + source.ID, Label: source.Label + " official lineup", URL: source.URL,
					Status: "maintained", Access: source.Access, LocationMode: source.LocationMode,
					Message: "Exact renamed-network evidence from an official provider guide",
				}
				statusByID[source.ID] = status
			}
			applied := false
			if len(rule.Current) > 0 {
				inputs[index].PreferredName = &AttributedAlias{Value: rule.Current[0], Source: status.ID, Method: rule.Method}
				applied = true
			}
			for _, alias := range rule.Aliases {
				key := providerNameKey(alias)
				if key == "" || identities[key] {
					continue
				}
				identities[key] = true
				inputs[index].ExternalAliases = append(inputs[index].ExternalAliases, AttributedAlias{
					Value: alias, Source: status.ID, Method: rule.Method,
				})
				applied = true
			}
			if applied {
				status.Matched++
			}
		}
	}

	statuses := make([]SourceStatus, 0, len(statusByID))
	for _, status := range statusByID {
		statuses = append(statuses, *status)
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Label < statuses[j].Label })
	return statuses
}

func providerGuideRouteMatches(route ProviderGuideRoute, providerName, location, postalCode string) bool {
	prefix := postalPrefix(postalCode)
	for _, candidate := range route.PostalPrefixRanges {
		if prefix != "" && prefix >= candidate.Start && prefix <= candidate.End {
			return true
		}
	}
	haystack := strings.ToLower(strings.Join([]string{providerName, location}, " "))
	for _, candidate := range route.Locations {
		if strings.Contains(haystack, strings.ToLower(candidate)) {
			return true
		}
	}
	return false
}

func postalPrefix(postalCode string) string {
	var digits []rune
	for _, character := range strings.TrimSpace(postalCode) {
		if !unicode.IsDigit(character) {
			if len(digits) > 0 {
				break
			}
			continue
		}
		digits = append(digits, character)
		if len(digits) == 3 {
			break
		}
	}
	if len(digits) != 3 {
		return ""
	}
	return string(digits)
}

func providerNameKey(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToUpper(r)
		}
		return -1
	}, strings.TrimSpace(value))
}
