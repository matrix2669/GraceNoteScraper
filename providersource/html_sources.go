package providersource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/daniel-widrick/GraceNoteScraper/channelcategory"
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
)

const (
	directvURL         = "https://www.directv.com/channel-lineup/"
	glorystarURL       = "https://www.glorystar.tv/channels/"
	optimumEastURL     = "https://www.optimum.net/pages/channel-lineups.html"
	optimumWestURL     = "https://www.optimum.com/tvlineup"
	optimumLocationURL = "https://static.suddenlink.com/address-ws/rest/AddressService/getServiceabilityDetailByLocationInformation"
	optimumLineupURL   = "https://static.suddenlink.com/live-channel-lineup/services/rest/RestChannelLineupService/getServiceabilityDetailLineup"
	xfinityGuideURL    = "https://www.xfinity.com/support/local-channel-lineup"
	xfinityLineupURL   = "https://api.sc.xfinity.com/channels/lineup"
	spectrumGuideURL   = "https://www.spectrum.com/cable-tv/channel-lineup"
)

var (
	nextDataPattern   = regexp.MustCompile(`(?is)<script[^>]*\bid=["']__NEXT_DATA__["'][^>]*>(.*?)</script>`)
	tableRowPattern   = regexp.MustCompile(`(?is)<tr\b[^>]*>(.*?)</tr>`)
	tableCellPattern  = regexp.MustCompile(`(?is)<td\b[^>]*>(.*?)</td>`)
	tagPattern        = regexp.MustCompile(`(?is)<[^>]+>`)
	optimumPDFPattern = regexp.MustCompile(`(?is)<h3\b[^>]*>\s*<a\b[^>]*href=["']([^"']+\.pdf(?:\?[^"']*)?)["'][^>]*>(.*?)</a>(.*?)</h3>`)
)

type directvChannel struct {
	Name       string          `json:"name"`
	ChannelNo  json.RawMessage `json:"chnlNum"`
	Category   string          `json:"category"`
	Categories []string        `json:"categories"`
}

func (s *Service) fetchDIRECTV(ctx context.Context) providerResult {
	source := catalogSource{
		ID: "directv-official-lineup", Label: "DIRECTV official lineup", URL: directvURL,
		Method: "exact DIRECTV channel number or unique exact identity from the public DIRECTV lineup",
	}
	data, err := s.fetchBytes(ctx, directvURL, "text/html", source.Label, maxHTMLBytes, false)
	if err != nil {
		return sourceFailure(source, err)
	}
	entries, err := parseDIRECTV(data)
	if err != nil {
		return sourceFailure(source, err)
	}
	source.Entries = entries
	return requireEntries(source)
}

func parseDIRECTV(data []byte) ([]catalogEntry, error) {
	match := nextDataPattern.FindSubmatch(data)
	if len(match) != 2 {
		return nil, errors.New("DIRECTV page did not contain its channel data")
	}
	var payload any
	if err := json.Unmarshal([]byte(html.UnescapeString(string(match[1]))), &payload); err != nil {
		return nil, fmt.Errorf("decoding DIRECTV channel data: %w", err)
	}
	channels := collectDIRECTVChannels(payload)
	entries := make([]catalogEntry, 0, len(channels))
	for _, channel := range channels {
		name := cleanText(channel.Name)
		numbers := rawChannelNumbers(channel.ChannelNo)
		if name == "" || len(numbers) == 0 {
			continue
		}
		category := directvCategory(channel.Category, channel.Categories)
		entries = append(entries, catalogEntry{Numbers: numbers, Name: name, Category: category})
	}
	return dedupeEntries(entries), nil
}

func directvCategory(primary string, categories []string) string {
	for _, candidate := range append(append([]string(nil), categories...), primary) {
		candidate = cleanText(candidate)
		trimmed := strings.TrimSpace(strings.TrimSuffix(candidate, " Channels"))
		if _, ok := channelcategory.Resolve(trimmed); ok {
			return trimmed
		}
	}
	return cleanText(primary)
}

func collectDIRECTVChannels(value any) []directvChannel {
	var result []directvChannel
	var visit func(any)
	visit = func(node any) {
		switch typed := node.(type) {
		case map[string]any:
			if name, ok := typed["name"].(string); ok {
				if number, exists := typed["chnlNum"]; exists {
					raw, _ := json.Marshal(number)
					channel := directvChannel{Name: name, ChannelNo: raw}
					channel.Category, _ = typed["category"].(string)
					if categories, ok := typed["categories"].([]any); ok {
						for _, candidate := range categories {
							if text, ok := candidate.(string); ok {
								channel.Categories = append(channel.Categories, text)
							}
						}
					}
					result = append(result, channel)
				}
			}
			for _, child := range typed {
				visit(child)
			}
		case []any:
			for _, child := range typed {
				visit(child)
			}
		}
	}
	visit(value)
	return result
}

func rawChannelNumbers(raw json.RawMessage) []string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return splitChannelNumbers(text)
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return splitChannelNumbers(number.String())
	}
	return nil
}

func (s *Service) fetchGlorystar(ctx context.Context) providerResult {
	source := catalogSource{
		ID: "glorystar-official-lineup", Label: "Glorystar official lineup", URL: glorystarURL,
		Method: "exact Glorystar channel number from the public channel guide",
	}
	data, err := s.fetchBytes(ctx, glorystarURL, "text/html", source.Label, maxHTMLBytes, false)
	if err != nil {
		return sourceFailure(source, err)
	}
	source.Entries = parseGlorystar(data)
	return requireEntries(source)
}

func parseGlorystar(data []byte) []catalogEntry {
	var entries []catalogEntry
	for _, row := range tableRowPattern.FindAllSubmatch(data, -1) {
		cells := tableCellPattern.FindAllSubmatch(row[1], -1)
		if len(cells) < 3 {
			continue
		}
		number := cleanHTML(cells[0][1])
		name := cleanHTML(cells[2][1])
		if !isSingleChannelNumber(number) || name == "" || strings.EqualFold(name, "Available!") || strings.Contains(strings.ToLower(name), "channel name") {
			continue
		}
		entries = append(entries, catalogEntry{Numbers: []string{number}, Name: name, Category: channelcategory.Faith})
	}
	return dedupeEntries(entries)
}

func (s *Service) fetchXfinity(ctx context.Context, request lineupindex.ProviderEvidenceRequest) providerResult {
	source := catalogSource{
		ID: "xfinity-official-lineup", Label: "Xfinity official lineup", URL: xfinityGuideURL,
		Method: "exact Xfinity channel number from the public address-qualified lineup service",
	}
	address := strings.TrimSpace(request.ServiceAddress.FormattedAddress)
	if address == "" {
		source.Status = "address-required"
		source.Message = "Select a service address for the active Xfinity lineup before scanning this ZIP"
		return providerResult{source: source}
	}
	endpoint := xfinityLineupURL + "?address=" + url.QueryEscape(address)
	data, err := s.fetchBytes(ctx, endpoint, "application/json", source.Label, maxJSONBytes, true)
	if err != nil {
		return sourceFailure(source, err)
	}
	entries, err := parseXfinity(data)
	if err != nil {
		return sourceFailure(source, err)
	}
	source.Entries = entries
	return requireEntries(source)
}

func parseXfinity(data []byte) ([]catalogEntry, error) {
	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decoding Xfinity channel data: %w", err)
	}
	var entries []catalogEntry
	var visit func(any)
	visit = func(node any) {
		switch typed := node.(type) {
		case map[string]any:
			name := firstString(typed, "channelName", "name", "displayName")
			number := firstString(typed, "channelNumber", "channelNo")
			if name != "" && number != "" {
				entries = append(entries, catalogEntry{Numbers: splitChannelNumbers(number), Name: cleanText(name), Category: firstString(typed, "category", "genre")})
			}
			for _, child := range typed {
				visit(child)
			}
		case []any:
			for _, child := range typed {
				visit(child)
			}
		}
	}
	visit(payload)
	return dedupeEntries(entries), nil
}

func (s *Service) fetchSpectrum(ctx context.Context) providerResult {
	source := catalogSource{
		ID: "spectrum-official-lineup", Label: "Spectrum official lineup", URL: spectrumGuideURL,
		Method: "exact Spectrum channel number from public channel data embedded in the official page",
	}
	data, err := s.fetchBytes(ctx, spectrumGuideURL, "text/html", source.Label, maxHTMLBytes, false)
	if err != nil {
		return sourceFailure(source, err)
	}
	entries := parseEmbeddedChannelJSON(data)
	if len(entries) == 0 {
		source.Status = "login-required"
		source.Message = "Spectrum does not expose a stable no-login residential lineup in this response; account automation is intentionally disabled"
		return providerResult{source: source}
	}
	source.Entries = entries
	return requireEntries(source)
}

func parseEmbeddedChannelJSON(data []byte) []catalogEntry {
	var entries []catalogEntry
	for _, match := range regexp.MustCompile(`(?is)<script[^>]*type=["']application/(?:ld\+)?json["'][^>]*>(.*?)</script>`).FindAllSubmatch(data, -1) {
		var payload any
		if json.Unmarshal([]byte(html.UnescapeString(string(match[1]))), &payload) != nil {
			continue
		}
		parsed, _ := parseXfinityValue(payload)
		entries = append(entries, parsed...)
	}
	return dedupeEntries(entries)
}

func parseXfinityValue(payload any) ([]catalogEntry, error) {
	var entries []catalogEntry
	var visit func(any)
	visit = func(node any) {
		switch typed := node.(type) {
		case map[string]any:
			name := firstString(typed, "channelName", "name", "displayName", "title")
			number := firstString(typed, "channelNumber", "channelNo")
			if name != "" && number != "" {
				entries = append(entries, catalogEntry{Numbers: splitChannelNumbers(number), Name: cleanText(name), Category: firstString(typed, "category", "genre")})
			}
			for _, child := range typed {
				visit(child)
			}
		case []any:
			for _, child := range typed {
				visit(child)
			}
		}
	}
	visit(payload)
	return dedupeEntries(entries), nil
}

func (s *Service) fetchOptimum(ctx context.Context, request lineupindex.ProviderEvidenceRequest) providerResult {
	providerText := strings.Join([]string{request.Provider.Name, request.Provider.Location}, " ")
	if optimumEasternMarket(request.PostalCode, providerText) {
		return s.fetchOptimumEast(ctx, providerText)
	}
	return s.fetchOptimumWest(ctx, request.ServiceAddress)
}

func (s *Service) fetchOptimumEast(ctx context.Context, providerText string) providerResult {
	source := catalogSource{
		ID: "optimum-official-lineup", Label: "Optimum official lineup", URL: optimumEastURL,
		Method: "exact Optimum channel number from the public market lineup PDF",
	}
	page, err := s.fetchBytes(ctx, optimumEastURL, "text/html", source.Label+" market list", maxHTMLBytes, false)
	if err != nil {
		return sourceFailure(source, err)
	}
	link, label := selectOptimumPDF(page, providerText)
	if link == "" {
		return sourceFailure(source, errors.New("Optimum market list did not identify a unique lineup PDF for "+cleanText(providerText)))
	}
	data, err := s.fetchBytes(ctx, link, "application/pdf", source.Label+" PDF", maxPDFBytes, false)
	if err != nil {
		return sourceFailure(source, err)
	}
	entries, err := parseOptimumPDF(data)
	if err != nil {
		return sourceFailure(source, err)
	}
	source.URL = link
	source.Label += " (" + label + ")"
	source.Entries = entries
	return requireEntries(source)
}

func (s *Service) fetchOptimumWest(ctx context.Context, address lineupindex.ProviderAddress) providerResult {
	source := catalogSource{
		ID: "optimum-official-lineup", Label: "Optimum official lineup", URL: optimumWestURL,
		Method: "exact Optimum channel number from the public address-qualified lineup service",
	}
	if strings.TrimSpace(address.FormattedAddress) == "" {
		source.Status = "address-required"
		source.Message = "Select a service address for the active Optimum lineup before scanning this ZIP"
		return providerResult{source: source}
	}
	if address.StreetAddress == "" || address.City == "" || address.State == "" || address.PostalCode == "" {
		return sourceFailure(source, errors.New("selected Optimum service address is missing street, city, state, or postal-code details"))
	}
	locationData, err := s.postJSON(ctx, optimumLocationURL, source.Label+" address lookup", map[string]any{
		"serviceabilityByLocationRequest": map[string]string{
			"streetAddressLine1": address.StreetAddress, "streetAddressLine2": "", "city": address.City,
			"state": address.State, "zipcode": address.PostalCode,
		},
	}, maxJSONBytes, true)
	if err != nil {
		return sourceFailure(source, err)
	}
	var location struct {
		Response struct {
			Detail json.RawMessage `json:"serviceabilityDetail"`
		} `json:"serviceabilityByLocationResponse"`
	}
	if err := json.Unmarshal(locationData, &location); err != nil || len(location.Response.Detail) == 0 || string(location.Response.Detail) == "null" {
		return sourceFailure(source, errors.New("Optimum address lookup returned no serviceability detail"))
	}
	lineupData, err := s.postJSON(ctx, optimumLineupURL, source.Label+" channel lookup", map[string]any{
		"serviceabilityDetailLineupRequest": map[string]json.RawMessage{"serviceabilityDetail": location.Response.Detail},
	}, maxJSONBytes, true)
	if err != nil {
		return sourceFailure(source, err)
	}
	entries, err := parseOptimumLineup(lineupData)
	if err != nil {
		return sourceFailure(source, err)
	}
	source.Entries = entries
	return requireEntries(source)
}

func parseOptimumLineup(data []byte) ([]catalogEntry, error) {
	var payload struct {
		Response struct {
			Networks []struct {
				ChannelNumber json.RawMessage `json:"channelNumber"`
				Name          string          `json:"name"`
			} `json:"networks"`
		} `json:"serviceabilityDetailLineupResponse"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decoding Optimum lineup: %w", err)
	}
	entries := make([]catalogEntry, 0, len(payload.Response.Networks))
	for _, channel := range payload.Response.Networks {
		name := cleanText(channel.Name)
		numbers := rawChannelNumbers(channel.ChannelNumber)
		if name == "" || len(numbers) == 0 || strings.Contains(strings.ToUpper(name), "OTT") {
			continue
		}
		name, aliases, callSigns := deriveNameEvidence(name)
		entries = append(entries, catalogEntry{Numbers: numbers, Name: name, Aliases: aliases, CallSigns: callSigns})
	}
	entries = dedupeEntries(entries)
	if len(entries) == 0 {
		return nil, errors.New("Optimum lineup returned no usable channels")
	}
	return entries, nil
}

func optimumEasternMarket(postalCode, providerText string) bool {
	digits := ""
	for _, r := range postalCode {
		if unicode.IsDigit(r) {
			digits += string(r)
			if len(digits) == 3 {
				break
			}
		}
	}
	if len(digits) == 3 && ((digits >= "060" && digits <= "069") ||
		(digits >= "070" && digits <= "089") ||
		(digits >= "100" && digits <= "149") ||
		(digits >= "150" && digits <= "196")) {
		return true
	}
	text := strings.ToLower(providerText)
	return strings.Contains(text, "hendersonville") || strings.Contains(text, "west jefferson")
}

func selectOptimumPDF(data []byte, providerText string) (string, string) {
	wanted := tokenSet(providerText)
	type candidate struct {
		url, label string
		score      int
	}
	var candidates []candidate
	for _, match := range optimumPDFPattern.FindAllSubmatch(data, -1) {
		label := cleanHTML(append(append([]byte(nil), match[2]...), match[3]...))
		score := overlapScore(wanted, tokenSet(label))
		link, ok := approvedOptimumPDFURL(html.UnescapeString(string(match[1])))
		if score > 0 && ok {
			candidates = append(candidates, candidate{url: link, label: cleanHTML(match[2]), score: score})
		}
	}
	if len(candidates) == 0 {
		return "", ""
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	if len(candidates) > 1 && candidates[0].score == candidates[1].score {
		return "", ""
	}
	return candidates[0].url, candidates[0].label
}

func approvedOptimumPDFURL(raw string) (string, bool) {
	base, err := url.Parse(optimumEastURL)
	if err != nil {
		return "", false
	}
	candidate, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	candidate = base.ResolveReference(candidate)
	host := strings.ToLower(candidate.Hostname())
	if candidate.Scheme != "https" || (host != "optimum.net" && !strings.HasSuffix(host, ".optimum.net")) {
		return "", false
	}
	candidate.Fragment = ""
	return candidate.String(), true
}

func firstPDFLink(data []byte, contains string) string {
	pattern := regexp.MustCompile(`(?is)href=["']([^"']+\.pdf(?:\?[^"']*)?)["']`)
	for _, match := range pattern.FindAllSubmatch(data, -1) {
		link := html.UnescapeString(string(match[1]))
		if contains == "" || strings.Contains(strings.ToLower(link), strings.ToLower(contains)) {
			return link
		}
	}
	return ""
}

func cleanHTML(value []byte) string {
	text := tagPattern.ReplaceAllString(string(value), " ")
	return cleanText(html.UnescapeString(text))
}

func cleanText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func tokenSet(value string) map[string]bool {
	stop := map[string]bool{"optimum": true, "of": true, "digital": true, "rebuild": true, "the": true, "most": true, "parts": true, "township": true, "townships": true, "county": true}
	result := make(map[string]bool)
	for _, token := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len(token) > 2 && !stop[token] {
			result[token] = true
		}
	}
	return result
}

func overlapScore(left, right map[string]bool) int {
	score := 0
	for token := range left {
		if right[token] {
			score++
		}
	}
	return score
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		switch value := values[key].(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				return value
			}
		case float64:
			return fmt.Sprintf("%.0f", value)
		case json.Number:
			return value.String()
		}
	}
	return ""
}

func dedupeEntries(entries []catalogEntry) []catalogEntry {
	result := make([]catalogEntry, 0, len(entries))
	seen := make(map[string]bool)
	for _, entry := range entries {
		if entry.Name == "" || len(entry.Numbers) == 0 {
			continue
		}
		key := strings.Join(entry.Numbers, ",") + "\x00" + identityKey(entry.Name)
		if key == "\x00" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, entry)
	}
	return result
}
