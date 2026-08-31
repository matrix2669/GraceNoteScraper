package providersource

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/daniel-widrick/GraceNoteScraper/channelcategory"
	"github.com/daniel-widrick/GraceNoteScraper/marketindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

const dishURL = "https://webapps.dish.com/api/clu/cludataservice.asmx/getdata?sortby=ranking"

//go:embed official_catalog.json
var officialCatalogData []byte

type Service struct {
	httpClient *http.Client
	catalog    catalog
}

type Options struct {
	UseEmbeddedCatalogs bool
}

type catalog struct {
	AsOf    string          `json:"asOf"`
	Sources []catalogSource `json:"sources"`
}

type catalogSource struct {
	ID          string         `json:"id"`
	Label       string         `json:"label"`
	URL         string         `json:"url"`
	Providers   []string       `json:"providers"`
	PostalCodes []string       `json:"postalCodes,omitempty"`
	Method      string         `json:"method"`
	Status      string         `json:"status,omitempty"`
	Message     string         `json:"message,omitempty"`
	Entries     []catalogEntry `json:"entries"`
}

type catalogEntry struct {
	Numbers        []string `json:"numbers,omitempty"`
	Name           string   `json:"name"`
	Aliases        []string `json:"aliases,omitempty"`
	CallSigns      []string `json:"callSigns,omitempty"`
	Category       string   `json:"category,omitempty"`
	CategoryMethod string   `json:"-"`
}

type dishResponse struct {
	Channels []dishChannel `json:"Channels"`
}

type dishChannel struct {
	Name      string `json:"name"`
	Category  string `json:"catg"`
	CallSign  string `json:"calltr"`
	ChannelNo string `json:"ChannelNo"`
}

func NewService(options ...Options) *Service {
	selected := Options{}
	if len(options) > 0 {
		selected = options[0]
	}
	return newServiceWithOptions(&http.Client{Timeout: 20 * time.Second}, selected)
}

func newService(client *http.Client) *Service {
	return newServiceWithOptions(client, Options{})
}

func newServiceWithOptions(client *http.Client, options Options) *Service {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	var sources catalog
	if options.UseEmbeddedCatalogs {
		_ = json.Unmarshal(officialCatalogData, &sources)
		filtered := sources.Sources[:0]
		for _, source := range sources.Sources {
			if len(source.Entries) > 0 {
				filtered = append(filtered, source)
			}
		}
		sources.Sources = filtered
	}
	return &Service{httpClient: client, catalog: sources}
}

// OfficialSourceID returns the runtime official-source identifier used for a
// provider. It lets selected-lineup consumers distinguish that provider's own
// exact evidence from category facts copied through competing lineups.
func OfficialSourceID(providerName string) string {
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	switch {
	case strings.Contains(providerName, "broadstar") || strings.Contains(providerName, "broadstream"):
		return "broadstar-official-lineup"
	case strings.Contains(providerName, "verizon") || strings.Contains(providerName, "fios"):
		return "verizon-fios-official-lineup"
	case strings.Contains(providerName, "optimum") || strings.Contains(providerName, "cablevision"):
		return "optimum-official-lineup"
	case strings.Contains(providerName, "directv"):
		return "directv-official-lineup"
	case strings.Contains(providerName, "dish"):
		return "dish-official-lineup"
	case strings.Contains(providerName, "armed forces") || strings.Contains(providerName, "afn"):
		return "afn-official-guide"
	case strings.Contains(providerName, "glorystar"):
		return "glorystar-official-lineup"
	case strings.Contains(providerName, "u-verse") || strings.Contains(providerName, "uverse") || strings.Contains(providerName, "at&t"):
		return "att-uverse-official-lineup"
	case strings.Contains(providerName, "xfinity") || strings.Contains(providerName, "comcast"):
		return "xfinity-official-lineup"
	case strings.Contains(providerName, "spectrum") || strings.Contains(providerName, "charter") || strings.Contains(providerName, "time warner"):
		return "spectrum-official-lineup"
	default:
		return ""
	}
}

func (s *Service) FetchProviderEvidence(ctx context.Context, request marketindex.ProviderEvidenceRequest) (marketindex.ProviderEvidenceResult, error) {
	providerName := strings.ToLower(strings.TrimSpace(request.Provider.Name))
	if providerName == "" || request.Grid == nil {
		return marketindex.ProviderEvidenceResult{}, nil
	}
	var live providerResult
	hasLiveSource := true
	switch {
	case strings.Contains(providerName, "broadstar") || strings.Contains(providerName, "broadstream"):
		live = s.fetchBroadStar(ctx)
	case strings.Contains(providerName, "verizon") || strings.Contains(providerName, "fios"):
		live = s.fetchVerizon(ctx)
	case strings.Contains(providerName, "optimum") || strings.Contains(providerName, "cablevision"):
		live = s.fetchOptimum(ctx, request)
	case strings.Contains(providerName, "directv"):
		live = s.fetchDIRECTV(ctx)
	case strings.Contains(providerName, "dish"):
		entries, err := s.fetchDISH(ctx)
		live.source = catalogSource{
			ID: "dish-official-lineup", Label: "DISH official lineup", URL: dishURL,
			Method: "exact DISH channel number or unique exact callsign from the public DISH lineup service", Entries: entries,
		}
		if err != nil {
			live = sourceFailure(live.source, err)
		}
	case strings.Contains(providerName, "armed forces") || strings.Contains(providerName, "afn"):
		live = s.fetchAFN(ctx)
	case strings.Contains(providerName, "glorystar"):
		live = s.fetchGlorystar(ctx)
	case strings.Contains(providerName, "u-verse") || strings.Contains(providerName, "uverse") || strings.Contains(providerName, "at&t"):
		live = s.fetchUVerse(ctx)
	case strings.Contains(providerName, "xfinity") || strings.Contains(providerName, "comcast"):
		live = s.fetchXfinity(ctx, request)
	case strings.Contains(providerName, "spectrum") || strings.Contains(providerName, "charter") || strings.Contains(providerName, "time warner"):
		live = s.fetchSpectrum(ctx)
	default:
		hasLiveSource = false
	}

	result := marketindex.ProviderEvidenceResult{}
	if hasLiveSource {
		matched := matchCatalog(request, live.source)
		result.Facts = append(result.Facts, matched.Facts...)
		result.Sources = append(result.Sources, matched.Sources...)
	}
	for _, source := range s.catalog.Sources {
		if !sourceMatches(source, providerName, request.PostalCode) {
			continue
		}
		matched := matchCatalog(request, source)
		result.Facts = append(result.Facts, matched.Facts...)
		result.Sources = append(result.Sources, matched.Sources...)
	}
	return result, live.err
}

func sourceMatches(source catalogSource, providerName, postalCode string) bool {
	providerMatches := false
	for _, candidate := range source.Providers {
		if strings.Contains(providerName, strings.ToLower(strings.TrimSpace(candidate))) {
			providerMatches = true
			break
		}
	}
	if !providerMatches {
		return false
	}
	if len(source.PostalCodes) == 0 {
		return true
	}
	for _, candidate := range source.PostalCodes {
		if strings.EqualFold(strings.TrimSpace(candidate), strings.TrimSpace(postalCode)) {
			return true
		}
	}
	return false
}

func (s *Service) fetchDISH(ctx context.Context) ([]catalogEntry, error) {
	data, err := s.fetchBytes(ctx, dishURL, "application/json", "DISH official lineup", maxJSONBytes, false)
	if err != nil {
		return nil, err
	}
	var payload dishResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decoding DISH lineup: %w", err)
	}
	if len(payload.Channels) == 0 {
		return nil, errors.New("DISH lineup returned no channels")
	}
	entries := make([]catalogEntry, 0, len(payload.Channels))
	for _, channel := range payload.Channels {
		name := cleanDISHName(channel.Name)
		category, categoryMethod := dishCategory(channel.Category, name, channel.CallSign)
		entry := catalogEntry{
			Numbers: []string{strings.TrimSpace(channel.ChannelNo)}, Name: name,
			CallSigns: []string{strings.TrimSpace(channel.CallSign)},
			Category:  category, CategoryMethod: categoryMethod,
		}
		if name == "" || entry.Numbers[0] == "" {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func dishCategory(raw, name, callSign string) (string, string) {
	identity := identityKey(name + " " + callSign)
	for _, sports := range []string{"ESPN", "SPORT", "NFL", "NHL", "NBA", "MLB", "GOLF", "TENNIS", "SEC", "BTN", "PAC12", "FANDUEL", "RACING"} {
		if strings.Contains(identity, sports) {
			return channelcategory.Sports, "DISH exact sports-network identity override"
		}
	}
	return strings.TrimSpace(raw), ""
}

var dishSuffix = regexp.MustCompile(`(?i)\s*(?:\([^)]*\)\s*)?(?:HD|SD)?\s*$`)

func cleanDISHName(value string) string {
	value = strings.TrimSpace(value)
	for {
		cleaned := strings.TrimSpace(dishSuffix.ReplaceAllString(value, ""))
		if cleaned == value {
			return cleaned
		}
		value = cleaned
	}
}

func matchCatalog(request marketindex.ProviderEvidenceRequest, source catalogSource) marketindex.ProviderEvidenceResult {
	if len(source.Entries) == 0 {
		status := source.Status
		if status == "" {
			status = "registered"
		}
		return marketindex.ProviderEvidenceResult{Sources: []marketindex.EvidenceSourceRecord{{
			ID: source.ID, Label: source.Label, URL: source.URL, Status: status, Message: source.Message,
		}}}
	}
	byNumber := make(map[string][]web.JSONChannel)
	byIdentity := make(map[string][]web.JSONChannel)
	for _, channel := range request.Grid.Channels {
		if number := normalizeNumber(channel.ChannelNo); number != "" {
			byNumber[number] = append(byNumber[number], channel)
		}
		for _, value := range channelIdentityValues(channel) {
			if key := identityKey(value); key != "" {
				byIdentity[key] = append(byIdentity[key], channel)
			}
		}
	}

	result := marketindex.ProviderEvidenceResult{}
	matchedStations := make(map[string]bool)
	type matchedEntry struct {
		channel web.JSONChannel
		entry   catalogEntry
		method  string
	}
	matches := make([]matchedEntry, 0, len(source.Entries))
	aliasOwners := make(map[string]map[string]bool)
	for _, entry := range source.Entries {
		channels, method, ok := matchEntry(entry, byNumber, byIdentity)
		if !ok {
			continue
		}
		for _, channel := range channels {
			if strings.TrimSpace(channel.ChannelID) == "" {
				continue
			}
			matchedStations[channel.ChannelID] = true
			matches = append(matches, matchedEntry{channel: channel, entry: entry, method: method})
			aliases := append([]string{entry.Name}, entry.Aliases...)
			aliases = append(aliases, entry.CallSigns...)
			for _, alias := range aliases {
				key := identityKey(alias)
				if key == "" {
					continue
				}
				if aliasOwners[key] == nil {
					aliasOwners[key] = make(map[string]bool)
				}
				aliasOwners[key][channel.ChannelID] = true
			}
		}
	}
	seenFacts := make(map[string]bool)
	for _, match := range matches {
		entry := match.entry
		channel := match.channel
		factMethod := source.Method
		if factMethod == "" {
			factMethod = match.method
		} else {
			factMethod += "; " + match.method
		}
		aliases := append([]string{entry.Name}, entry.Aliases...)
		aliases = append(aliases, entry.CallSigns...)
		seenAliases := make(map[string]bool)
		for _, alias := range aliases {
			key := identityKey(alias)
			if key == "" || seenAliases[key] || len(aliasOwners[key]) != 1 {
				continue
			}
			seenAliases[key] = true
			factKey := channel.ChannelID + "\x00" + marketindex.FactAlias + "\x00" + key
			if seenFacts[factKey] {
				continue
			}
			seenFacts[factKey] = true
			result.Facts = append(result.Facts, marketindex.ProviderFact{
				StationID: channel.ChannelID, Kind: marketindex.FactAlias, Value: strings.TrimSpace(alias),
				SourceID: source.ID, SourceLabel: source.Label, SourceURL: source.URL, Method: factMethod,
			})
		}
		categoryIdentities := append([]string{entry.Name}, entry.Aliases...)
		categoryIdentities = append(categoryIdentities, entry.CallSigns...)
		categoryIdentities = append(categoryIdentities, channelIdentityValues(channel)...)
		if category, ok := channelcategory.Resolve(entry.Category, categoryIdentities...); ok {
			categoryMethod := category.Method
			if category.Method == channelcategory.MethodFuzzy {
				categoryMethod = fmt.Sprintf("%s %.0f%% to %q", category.Method, category.Confidence*100, category.MatchedAlias)
			}
			if entry.CategoryMethod != "" {
				categoryMethod = entry.CategoryMethod + "; " + categoryMethod
			}
			factKey := channel.ChannelID + "\x00" + marketindex.FactCategory + "\x00" + identityKey(category.Category)
			if seenFacts[factKey] {
				continue
			}
			seenFacts[factKey] = true
			result.Facts = append(result.Facts, marketindex.ProviderFact{
				StationID: channel.ChannelID, Kind: marketindex.FactCategory, Value: category.Category,
				RawValue: strings.TrimSpace(entry.Category), MatchMethod: category.Method, MatchConfidence: category.Confidence,
				SourceID: source.ID, SourceLabel: source.Label, SourceURL: source.URL,
				Method: factMethod + "; provider category " + strconv.Quote(strings.TrimSpace(entry.Category)) + " mapped by " + categoryMethod,
			})
		}
	}
	aliases := 0
	categories := 0
	for _, fact := range result.Facts {
		if fact.Kind == marketindex.FactCategory {
			categories++
		} else {
			aliases++
		}
	}
	status := source.Status
	if status == "" {
		status = "complete"
	}
	message := source.Message
	if message == "" {
		message = fmt.Sprintf("%d exact station-ID joins from the official provider source", len(matchedStations))
	}
	if len(matchedStations) == 0 {
		if source.Status == "" {
			status = "no-matches"
		}
		if source.Message == "" {
			message = "No exact provider channel-number or unique identity joins were found"
		}
	}
	result.Sources = []marketindex.EvidenceSourceRecord{{
		ID: source.ID, Label: source.Label, URL: source.URL, Status: status,
		Matched: len(matchedStations), Aliases: aliases, Categories: categories, Message: message,
	}}
	return result
}

func matchEntry(entry catalogEntry, byNumber map[string][]web.JSONChannel, byIdentity map[string][]web.JSONChannel) ([]web.JSONChannel, string, bool) {
	for _, number := range entry.Numbers {
		matches := byNumber[normalizeNumber(number)]
		if len(matches) == 1 {
			return matches, "exact provider channel number", true
		}
		if len(matches) > 1 {
			entryIdentities := make(map[string]bool)
			for _, value := range append(append([]string{entry.Name}, entry.Aliases...), entry.CallSigns...) {
				if key := identityKey(value); key != "" {
					entryIdentities[key] = true
				}
			}
			exact := make(map[string]web.JSONChannel)
			for _, channel := range matches {
				for _, value := range channelIdentityValues(channel) {
					if entryIdentities[identityKey(value)] {
						exact[channel.ChannelID] = channel
						break
					}
				}
			}
			if len(exact) > 0 {
				result := make([]web.JSONChannel, 0, len(exact))
				for _, channel := range exact {
					result = append(result, channel)
				}
				sort.Slice(result, func(i, j int) bool { return result[i].ChannelID < result[j].ChannelID })
				return result, "exact provider channel number plus exact identity across same-number variants", true
			}
		}
	}
	identities := append([]string{entry.Name}, entry.Aliases...)
	identities = append(identities, entry.CallSigns...)
	unique := make(map[string]web.JSONChannel)
	for _, identity := range identities {
		matches := byIdentity[identityKey(identity)]
		if len(matches) == 1 {
			unique[matches[0].ChannelID] = matches[0]
		}
	}
	if len(unique) == 1 {
		for _, channel := range unique {
			return []web.JSONChannel{channel}, "unique exact provider callsign or name", true
		}
	}
	return nil, "", false
}

func channelIdentityValues(channel web.JSONChannel) []string {
	values := []string{channel.CallSign, channel.AffiliateName, channel.AffiliateCallSign}
	for _, event := range channel.Events {
		values = append(values, event.CallSign)
	}
	return values
}

func normalizeNumber(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func identityKey(value string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			return unicode.ToUpper(character)
		}
		return -1
	}, strings.TrimSpace(value))
}
