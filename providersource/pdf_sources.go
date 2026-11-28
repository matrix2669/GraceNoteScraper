package providersource

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/daniel-widrick/GraceNoteScraper/channelcategory"
)

const (
	verizonPDFURL   = "https://www.verizon.com/supportresources/content/dam/verizon/support/consumer/documents/verizon_consumer_fios_tv_channel_lineup.pdf"
	uversePDFURL    = "https://www.att.com/idpassets/pdfs/channel_lineups/Uverse_Channel_Lineup.pdf"
	broadstarPDFURL = "https://www.broadstar.com/wp-content/uploads/2026/04/BDYWN_AmFav.pdf"
	afnGuidePDFURL  = "https://media.myafn.dodmedia.osd.mil/AFNHD/DTH_ATL-North_America_Galaxy_16_AFN7500HD_Settings_%28FINAL%29.pdf"
	verizonGuideURL = "https://www.verizon.com/home/fios-tv/channel-lineup/"
)

var (
	channelNumberPattern    = regexp.MustCompile(`^\d+(?:\s*/\s*\d+)*(?:\s*(?:HD|SD))?$`)
	optimumNumberRange      = regexp.MustCompile(`^(\d{1,4})\s*-\s*(\d{1,4})$`)
	broadstarCellPattern    = regexp.MustCompile(`^\s*(\d+(?:\s*/\s*\d+)?)\s*(.+?)\s*$`)
	parentheticalPattern    = regexp.MustCompile(`\s*\(([^()]*)\)\s*`)
	looseChannelLinePattern = regexp.MustCompile(`(?m)^\s*(\d{1,4}(?:\s*/\s*\d{1,4})?)\s+(.{2,80}?)\s*$`)
)

type pdfPair struct {
	nameStart   float64
	numberStart float64
	end         float64
}

func (s *Service) fetchVerizon(ctx context.Context) providerResult {
	source := catalogSource{
		ID: "verizon-fios-official-lineup", Label: "Verizon FiOS official lineup", URL: verizonGuideURL,
		Method: "exact FiOS channel number from the public national lineup PDF",
	}
	data, err := s.fetchBytes(ctx, verizonPDFURL, "application/pdf", source.Label+" PDF", maxPDFBytes, false)
	if err != nil {
		return sourceFailure(source, err)
	}
	entries, err := parsePairedPDF(data, []pdfPair{{nameStart: 30, numberStart: 250, end: 340}})
	if err != nil {
		return sourceFailure(source, err)
	}
	for index := range entries {
		entries[index].Category = fiosCategory(entries[index].Numbers)
	}
	source.Entries = entries
	return requireEntries(source)
}

func (s *Service) fetchUVerse(ctx context.Context) providerResult {
	source := catalogSource{
		ID: "att-uverse-official-lineup", Label: "AT&T U-verse official lineup", URL: uversePDFURL,
		Method: "exact U-verse channel number from the public channel lineup PDF",
		Status: "limited", Message: "AT&T's current public download URL serves a lineup document marked effective February 2023",
	}
	data, err := s.fetchBytes(ctx, uversePDFURL, "application/pdf", source.Label, maxPDFBytes, false)
	if err != nil {
		return sourceFailure(source, err)
	}
	entries, err := parsePairedPDF(data, []pdfPair{
		{nameStart: 880, numberStart: 1030, end: 1130},
		{nameStart: 1180, numberStart: 1330, end: 1430},
		{nameStart: 1480, numberStart: 1630, end: 1740},
	})
	if err != nil {
		return sourceFailure(source, err)
	}
	source.Entries = entries
	return requireEntries(source)
}

func (s *Service) fetchBroadStar(ctx context.Context) providerResult {
	source := catalogSource{
		ID: "broadstar-official-lineup", Label: "BroadStar official lineup", URL: broadstarPDFURL,
		Method: "exact BroadStar channel number from the public lineup PDF",
	}
	data, err := s.fetchBytes(ctx, broadstarPDFURL, "application/pdf", source.Label, maxPDFBytes, false)
	if err != nil {
		return sourceFailure(source, err)
	}
	entries, err := parseBroadStarPDF(data)
	if err != nil {
		return sourceFailure(source, err)
	}
	source.Entries = entries
	return requireEntries(source)
}

func (s *Service) fetchAFN(ctx context.Context) providerResult {
	source := catalogSource{
		ID: "afn-official-guide", Label: "AFN official guide", URL: "https://myafn.dodmedia.osd.mil/",
		Method: "exact AFN channel number from the public AFN guide PDF",
	}
	data, err := s.fetchBytes(ctx, afnGuidePDFURL, "application/pdf", source.Label, maxPDFBytes, false)
	if err != nil {
		return sourceFailure(source, err)
	}
	entries, err := parseAFNPDF(data)
	if err != nil {
		return sourceFailure(source, err)
	}
	source.Entries = entries
	return requireEntries(source)
}

func parseOptimumPDF(data []byte) ([]catalogEntry, error) {
	flatLines, err := extractPDFLines(data)
	if err != nil {
		return nil, err
	}
	layouts := [][]pdfPair{
		{
			{nameStart: 45, numberStart: 235, end: 285},
			{nameStart: 285, numberStart: 475, end: 535},
			{nameStart: 545, numberStart: 735, end: 790},
			{nameStart: 790, numberStart: 975, end: 1045},
			{nameStart: 1050, numberStart: 1235, end: 1300},
		},
		{
			{nameStart: 70, numberStart: 35, end: 290},
			{nameStart: 340, numberStart: 305, end: 570},
		},
	}
	var best []catalogEntry
	for _, layout := range layouts {
		entries := parsePairedLines(flatLines, layout)
		if len(entries) > len(best) {
			best = entries
		}
	}
	if categorizedLines, categoryErr := extractPDFLinesWithXObjects(data); categoryErr == nil {
		categoryLayout := []pdfPair{
			{nameStart: 45, numberStart: 175, end: 218},
			{nameStart: 285, numberStart: 415, end: 452},
			{nameStart: 545, numberStart: 670, end: 721},
			{nameStart: 790, numberStart: 915, end: 1018},
			{nameStart: 1050, numberStart: 1175, end: 1215},
		}
		categorized := parseOptimumLines(optimumCategorizedPages(categorizedLines, categoryLayout), categoryLayout)
		best = mergeOptimumCategoryEvidence(best, categorized)
	}
	if len(best) == 0 {
		return nil, errors.New("Optimum PDF contained no recognizable channel rows")
	}
	return best, nil
}

func optimumCategorizedPages(lines []pdfLine, pairs []pdfPair) []pdfLine {
	pageSections := make(map[int]map[string]bool)
	for _, line := range lines {
		for _, pair := range pairs {
			if section, ok := optimumPDFSection(textInRange(line, pair.nameStart, pair.numberStart)); ok {
				if pageSections[line.Page] == nil {
					pageSections[line.Page] = make(map[string]bool)
				}
				pageSections[line.Page][section] = true
			}
		}
	}
	var result []pdfLine
	for _, line := range lines {
		if len(pageSections[line.Page]) >= 3 {
			result = append(result, line)
		}
	}
	return result
}

func parseOptimumLines(lines []pdfLine, pairs []pdfPair) []catalogEntry {
	pageOrder := make([]int, 0)
	pageLines := make(map[int][]pdfLine)
	for _, line := range lines {
		if _, exists := pageLines[line.Page]; !exists {
			pageOrder = append(pageOrder, line.Page)
		}
		pageLines[line.Page] = append(pageLines[line.Page], line)
	}

	var entries []catalogEntry
	for _, page := range pageOrder {
		category := ""
		for _, pair := range pairs {
			for _, line := range pageLines[page] {
				nameStart := pair.nameStart
				numberStart := pair.numberStart
				var name, number string
				if nameStart < numberStart {
					name = textInRange(line, nameStart, numberStart)
					number = textInRange(line, numberStart, pair.end)
				} else {
					number = textInRange(line, numberStart, nameStart)
					name = textInRange(line, nameStart, pair.end)
				}
				if heading, ok := optimumPDFSection(name); ok && !isOptimumChannelNumberValue(number) {
					category = heading
					continue
				}
				if !isOptimumChannelNumberValue(number) {
					continue
				}
				var aliases, callSigns []string
				if plausibleChannelName(name) {
					name, aliases, callSigns = deriveNameEvidence(name)
				} else {
					name = ""
				}
				if name == "" && category == "" {
					continue
				}
				categoryMethod := ""
				if category != "" {
					categoryMethod = "Optimum PDF section heading"
				}
				for _, channelNumber := range splitOptimumChannelNumbers(number) {
					entries = append(entries, catalogEntry{
						Numbers: []string{channelNumber}, Name: name, Aliases: aliases, CallSigns: callSigns,
						Category: category, CategoryMethod: categoryMethod,
					})
				}
			}
		}
	}
	return dedupeOptimumRows(entries)
}

func dedupeOptimumRows(entries []catalogEntry) []catalogEntry {
	result := make([]catalogEntry, 0, len(entries))
	seen := make(map[string]bool)
	for _, entry := range entries {
		if len(entry.Numbers) == 0 || (entry.Name == "" && entry.Category == "") {
			continue
		}
		key := strings.Join(entry.Numbers, ",") + "\x00" + identityKey(entry.Name) + "\x00" + identityKey(entry.Category)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, entry)
	}
	return result
}

func mergeOptimumCategoryEvidence(flat, categorized []catalogEntry) []catalogEntry {
	type categoryEvidence struct {
		category  string
		method    string
		ambiguous bool
	}

	byNumber := make(map[string]categoryEvidence)
	for _, entry := range categorized {
		if strings.TrimSpace(entry.Category) == "" {
			continue
		}
		for _, number := range entry.Numbers {
			key := normalizeNumber(number)
			if key == "" {
				continue
			}
			current, exists := byNumber[key]
			switch {
			case !exists:
				byNumber[key] = categoryEvidence{category: entry.Category, method: entry.CategoryMethod}
			case current.category != entry.Category:
				current.ambiguous = true
				byNumber[key] = current
			}
		}
	}

	result := make([]catalogEntry, 0, len(flat))
	flatNumbers := make(map[string]bool)
	for _, entry := range flat {
		for _, number := range entry.Numbers {
			key := normalizeNumber(number)
			if key == "" {
				continue
			}
			flatNumbers[key] = true
			copy := entry
			copy.Numbers = []string{number}
			if evidence, ok := byNumber[key]; ok && !evidence.ambiguous {
				copy.Category = evidence.category
				copy.CategoryMethod = evidence.method
			}
			result = append(result, copy)
		}
	}

	// The alphabetical index can omit compact range rows such as Stingray
	// Music 850-899. Retain categorized-page rows only when their exact channel
	// number is absent from the canonical index.
	for _, entry := range categorized {
		if strings.TrimSpace(entry.Category) == "" || strings.TrimSpace(entry.Name) == "" || len(entry.Numbers) != 1 {
			continue
		}
		key := normalizeNumber(entry.Numbers[0])
		if key == "" || flatNumbers[key] {
			continue
		}
		flatNumbers[key] = true
		result = append(result, entry)
	}
	return dedupeEntries(result)
}

func optimumPDFSection(value string) (string, bool) {
	sections := map[string]string{
		"NETWORKS":      "Networks",
		"KIDS":          "Kids",
		"SPORTS":        "Sports",
		"PREMIUMS":      "Premiums",
		"ONDEMANDPPV":   "On Demand & PPV",
		"INTERACTIVE":   "Interactive",
		"MUSIC":         "Music",
		"INTERNATIONAL": "International",
	}
	section, ok := sections[identityKey(value)]
	return section, ok
}

func isOptimumChannelNumberValue(value string) bool {
	return isChannelNumberValue(value) || optimumNumberRange.MatchString(strings.TrimSpace(value))
}

func splitOptimumChannelNumbers(value string) []string {
	match := optimumNumberRange.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 3 {
		return splitChannelNumbers(value)
	}
	start, startErr := strconv.Atoi(match[1])
	end, endErr := strconv.Atoi(match[2])
	if startErr != nil || endErr != nil || end < start || end-start > 200 {
		return nil
	}
	result := make([]string, 0, end-start+1)
	for number := start; number <= end; number++ {
		result = append(result, strconv.Itoa(number))
	}
	return result
}

func parsePairedPDF(data []byte, pairs []pdfPair) ([]catalogEntry, error) {
	lines, err := extractPDFLines(data)
	if err != nil {
		return nil, err
	}
	entries := parsePairedLines(lines, pairs)
	if len(entries) == 0 {
		return nil, errors.New("PDF contained no recognizable channel rows")
	}
	return entries, nil
}

func parsePairedLines(lines []pdfLine, pairs []pdfPair) []catalogEntry {
	var entries []catalogEntry
	for _, line := range lines {
		for _, pair := range pairs {
			nameStart := pair.nameStart
			numberStart := pair.numberStart
			var name, number string
			if nameStart < numberStart {
				name = textInRange(line, nameStart, numberStart)
				number = textInRange(line, numberStart, pair.end)
			} else {
				number = textInRange(line, numberStart, nameStart)
				name = textInRange(line, nameStart, pair.end)
			}
			if !isChannelNumberValue(number) || !plausibleChannelName(name) {
				continue
			}
			name, aliases, callSigns := deriveNameEvidence(name)
			if name == "" {
				continue
			}
			entries = append(entries, catalogEntry{Numbers: splitChannelNumbers(number), Name: name, Aliases: aliases, CallSigns: callSigns})
		}
	}
	return dedupeEntries(entries)
}

func parseBroadStarPDF(data []byte) ([]catalogEntry, error) {
	lines, err := extractPDFLines(data)
	if err != nil {
		return nil, err
	}
	columns := [][2]float64{{20, 150}, {150, 315}, {315, 450}, {450, 620}}
	var entries []catalogEntry
	for _, line := range lines {
		for _, column := range columns {
			cell := textInRange(line, column[0], column[1])
			match := broadstarCellPattern.FindStringSubmatch(cell)
			if len(match) != 3 || !plausibleChannelName(match[2]) {
				continue
			}
			name, aliases, callSigns := deriveNameEvidence(match[2])
			numbers := splitChannelNumbers(match[1])
			entries = append(entries, catalogEntry{
				Numbers: numbers, Name: name, Aliases: aliases, CallSigns: callSigns,
				Category: broadStarCategory(numbers, name),
			})
		}
	}
	entries = dedupeEntries(entries)
	if len(entries) == 0 {
		return nil, errors.New("BroadStar PDF contained no recognizable channel rows")
	}
	return entries, nil
}

func parseAFNPDF(data []byte) ([]catalogEntry, error) {
	text, err := extractPDFText(data)
	if err != nil {
		return nil, err
	}
	var entries []catalogEntry
	for _, match := range looseChannelLinePattern.FindAllStringSubmatch(text, -1) {
		name := cleanText(match[2])
		if !strings.Contains(strings.ToUpper(name), "AFN") || !plausibleChannelName(name) {
			continue
		}
		entries = append(entries, catalogEntry{Numbers: splitChannelNumbers(match[1]), Name: name, Category: afnCategory(name)})
	}
	entries = dedupeEntries(entries)
	if len(entries) == 0 {
		return nil, errors.New("AFN guide contained no recognizable channel rows")
	}
	return entries, nil
}

func textInRange(line pdfLine, start, end float64) string {
	var builder strings.Builder
	previousEnd := 0.0
	for _, word := range line.Words {
		if word.Size > 0 && word.Size < 6 {
			continue
		}
		if word.X < start || word.X >= end {
			continue
		}
		if builder.Len() > 0 && strings.TrimSpace(word.Text) != "" && word.X-previousEnd > 1.2 {
			builder.WriteByte(' ')
		}
		builder.WriteString(word.Text)
		if word.X+word.W > previousEnd {
			previousEnd = word.X + word.W
		}
	}
	return cleanText(builder.String())
}

func isSingleChannelNumber(value string) bool {
	return channelNumberPattern.MatchString(strings.TrimSpace(value)) && !strings.Contains(value, "/")
}
func isChannelNumberValue(value string) bool {
	return channelNumberPattern.MatchString(strings.TrimSpace(value))
}

func plausibleChannelName(value string) bool {
	value = cleanText(value)
	if len(value) < 2 || len(value) > 100 {
		return false
	}
	upper := strings.ToUpper(value)
	for _, rejected := range []string{"CHANNEL", "LINEUP", "PACKAGE", "EFFECTIVE DATE", "AVAILABLE", "FOLD", "CUSTOMERS"} {
		if upper == rejected || strings.HasPrefix(upper, rejected+" ") {
			return false
		}
	}
	return strings.IndexFunc(value, unicode.IsLetter) >= 0
}

func deriveNameEvidence(value string) (string, []string, []string) {
	value = cleanText(value)
	var aliases, callSigns []string
	current := value
	for _, match := range parentheticalPattern.FindAllStringSubmatch(value, -1) {
		inside := cleanText(match[1])
		key := identityKey(inside)
		switch {
		case key == "HDONLY" || key == "SAP" || key == "WEST" || key == "EAST":
			current = strings.Replace(current, match[0], " ", 1)
		case key == "BIOGRAPHY" || key == "BIOGRAPHYCHANNEL" || key == "H2" || key == "TVG" || key == "TVG2":
			aliases = append(aliases, inside)
			current = strings.Replace(current, match[0], " ", 1)
		case isCallSign(inside):
			callSigns = append(callSigns, inside)
		}
	}
	return cleanText(current), aliases, callSigns
}

func isCallSign(value string) bool {
	if len(value) < 2 || len(value) > 8 || strings.ToUpper(value) != value {
		return false
	}
	for _, r := range value {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' {
			return false
		}
	}
	return true
}

func fiosCategory(numbers []string) string {
	for _, raw := range numbers {
		number, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		switch {
		case number >= 1 && number <= 49, number >= 501 && number <= 549:
			return "Local channels"
		case number >= 50 && number <= 69, number >= 550 && number <= 569:
			return "Entertainment"
		case number >= 70 && number <= 99, number >= 300 && number <= 339, number >= 570 && number <= 599, number >= 800 && number <= 839:
			return "Sports"
		case number >= 100 && number <= 119, number >= 600 && number <= 619:
			return "News"
		case number >= 120 && number <= 139, number >= 620 && number <= 639:
			return "Information and education"
		case number >= 150 && number <= 159, number >= 650 && number <= 659:
			return "Marketplace"
		case number >= 210 && number <= 229, number >= 710 && number <= 729, number >= 1800 && number <= 1850:
			return "Music"
		case number >= 230 && number <= 249, number >= 340 && number <= 449, number >= 730 && number <= 749, number >= 840 && number <= 949:
			return "Movies"
		case number >= 250 && number <= 269, number >= 780 && number <= 789:
			return "Kids"
		case number >= 790 && number <= 799:
			return "Religion"
		case number >= 1000 && number <= 1499:
			return "PPV and subscription events"
		}
	}
	return ""
}

func broadStarCategory(numbers []string, name string) string {
	for _, raw := range numbers {
		number, err := strconv.Atoi(raw)
		if err != nil {
			continue
		}
		switch {
		case number >= 200 && number < 300:
			return "Sports"
		case number >= 300 && number < 400:
			return "Premium movies"
		case number >= 400 && number < 500:
			return "Music"
		}
	}
	if strings.Contains(strings.ToLower(name), "ambiance") {
		return "Other"
	}
	return ""
}

func afnCategory(name string) string {
	upper := strings.ToUpper(name)
	switch {
	case strings.Contains(upper, "SPORT"):
		return channelcategory.Sports
	case strings.Contains(upper, "NEWS"):
		return channelcategory.NewsWeather
	case strings.Contains(upper, "FAMILY"):
		return channelcategory.KidsFamily
	case strings.Contains(upper, "MOVIE"):
		return channelcategory.Movies
	case strings.Contains(upper, "MUSIC") || strings.Contains(upper, "RADIO"):
		return channelcategory.Music
	default:
		return ""
	}
}
