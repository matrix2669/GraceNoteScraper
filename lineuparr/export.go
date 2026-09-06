package lineuparr

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

func ExportFromDraft(draft *Draft) ExportFile {
	categories := make(map[string][]ExportChannel)
	for _, channel := range draft.Channels {
		if !channel.Included {
			continue
		}
		category := cleanCategory(channel.Category)
		if category == "" {
			category = uncategorized
		}
		categories[category] = append(categories[category], ExportChannel{
			Name:    channel.Name,
			Number:  exportNumber(channel.Number),
			Aliases: append([]string(nil), channel.Aliases...),
			EPGIDs:  append([]string(nil), channel.EPGIDs...),
		})
	}
	for category := range categories {
		sort.SliceStable(categories[category], func(i, j int) bool {
			left := fmt.Sprint(categories[category][i].Number)
			right := fmt.Sprint(categories[category][j].Number)
			return numberLess(left, right)
		})
	}

	sourceNames := make([]string, 0, len(draft.Sources))
	for _, source := range draft.Sources {
		if source.ID == "gracenote" || source.Matched > 0 {
			sourceNames = append(sourceNames, source.Label)
		}
	}
	return ExportFile{
		Package:     draft.Package,
		Date:        time.Now().UTC().Format("2006-01-02"),
		Description: fmt.Sprintf("Lineuparr-compatible export for the active %s Gracenote lineup.", draft.ProviderName),
		Source:      strings.Join(sourceNames, "; "),
		Notes:       "All channels were retained by default. Exclusions and duplicate-SD removals in this file reflect explicit builder choices. Uncategorized channels remain visible for review.",
		Categories:  categories,
	}
}

func ExportFilename(draft *Draft) string {
	country := strings.ToUpper(strings.TrimSpace(draft.CountryCode))
	if country == "" {
		country = "XX"
	}
	provider := filenameSlug(draft.ProviderName)
	if provider == "" {
		provider = "Gracenote"
	}
	postal := filenameSlug(draft.PostalCode)
	if postal != "" {
		provider += "-" + postal
	}
	return country + "_" + provider + "_lineup.json"
}

func ExportFilenameForSource(country, provider, postal string) string {
	return ExportFilename(&Draft{CountryCode: countryAlpha2(country), ProviderName: provider, PostalCode: postal})
}

func exportNumber(value string) any {
	value = strings.TrimSpace(value)
	if integer, err := strconv.ParseInt(value, 10, 64); err == nil {
		return integer
	}
	if decimal, err := strconv.ParseFloat(value, 64); err == nil {
		return decimal
	}
	return value
}

func filenameSlug(value string) string {
	var builder strings.Builder
	lastDash := false
	for _, r := range strings.TrimSpace(value) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			builder.WriteRune(r)
			lastDash = false
		case !lastDash:
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}
