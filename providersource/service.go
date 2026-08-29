package providersource

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

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
	Numbers   []string `json:"numbers,omitempty"`
	Name      string   `json:"name"`
	Aliases   []string `json:"aliases,omitempty"`
	CallSigns []string `json:"callSigns,omitempty"`
	Category  string   `json:"category,omitempty"`
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

func NewService() *Service {
	return newService(&http.Client{Timeout: 20 * time.Second})
}

func newService(client *http.Client) *Service {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	var sources catalog
	_ = json.Unmarshal(officialCatalogData, &sources)
	return &Service{httpClient: client, catalog: sources}
}

func (s *Service) FetchProviderEvidence(ctx context.Context, request marketindex.ProviderEvidenceRequest) (marketindex.ProviderEvidenceResult, error) {
	providerName := strings.ToLower(strings.TrimSpace(request.Provider.Name))
	if providerName == "" || request.Grid == nil {
		return marketindex.ProviderEvidenceResult{}, nil
	}
	if strings.Contains(providerName, "dish") {
		entries, err := s.fetchDISH(ctx)
		if err != nil {
			return marketindex.ProviderEvidenceResult{Sources: []marketindex.EvidenceSourceRecord{{
				ID: "dish-official-lineup", Label: "DISH official lineup", URL: dishURL,
				Status: marketindex.StatusError, Message: err.Error(),
			}}}, err
		}
		return matchCatalog(request, catalogSource{
			ID: "dish-official-lineup", Label: "DISH official lineup", URL: dishURL,
			Method: "exact DISH channel number or unique exact callsign from the public DISH lineup service", Entries: entries,
		}), nil
	}

	result := marketindex.ProviderEvidenceResult{}
	for _, source := range s.catalog.Sources {
		if !sourceMatches(source, providerName, request.PostalCode) {
			continue
		}
		matched := matchCatalog(request, source)
		result.Facts = append(result.Facts, matched.Facts...)
		result.Sources = append(result.Sources, matched.Sources...)
	}
	return result, nil
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dishURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "GraceNoteScraper provider enrichment")
	response, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DISH lineup request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("DISH lineup returned %s", response.Status)
	}
	var payload dishResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decoding DISH lineup: %w", err)
	}
	if len(payload.Channels) == 0 {
		return nil, errors.New("DISH lineup returned no channels")
	}
	entries := make([]catalogEntry, 0, len(payload.Channels))
	for _, channel := range payload.Channels {
		name := cleanDISHName(channel.Name)
		entry := catalogEntry{
			Numbers: []string{strings.TrimSpace(channel.ChannelNo)}, Name: name,
			CallSigns: []string{strings.TrimSpace(channel.CallSign)},
			Category:  dishCategory(channel.Category, name, channel.CallSign),
		}
		if name == "" || entry.Numbers[0] == "" {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
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

func dishCategory(raw, name, callSign string) string {
	identity := identityKey(name + " " + callSign)
	for _, sports := range []string{"ESPN", "SPORT", "NFL", "NHL", "NBA", "MLB", "GOLF", "TENNIS", "SEC", "BTN", "PAC12", "FANDUEL", "RACING"} {
		if strings.Contains(identity, sports) {
			return "Sports"
		}
	}
	for _, category := range strings.Split(raw, "|") {
		switch strings.ToLower(strings.TrimSpace(category)) {
		case "sports":
			return "Sports"
		case "news & info":
			return "News"
		case "kids & family":
			return "Kids"
		case "movies":
			return "Movies"
		case "reality & game shows":
			return "Reality & Lifestyle"
		case "latino":
			return "Spanish"
		case "music":
			return "Music"
		case "international":
			return "International"
		case "entertainment":
			return "Entertainment"
		}
	}
	return ""
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
		channel, method, ok := matchEntry(entry, byNumber, byIdentity)
		if !ok || strings.TrimSpace(channel.ChannelID) == "" {
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
			result.Facts = append(result.Facts, marketindex.ProviderFact{
				StationID: channel.ChannelID, Kind: marketindex.FactAlias, Value: strings.TrimSpace(alias),
				SourceID: source.ID, SourceLabel: source.Label, SourceURL: source.URL, Method: factMethod,
			})
		}
		if strings.TrimSpace(entry.Category) != "" {
			result.Facts = append(result.Facts, marketindex.ProviderFact{
				StationID: channel.ChannelID, Kind: marketindex.FactCategory, Value: entry.Category,
				SourceID: source.ID, SourceLabel: source.Label, SourceURL: source.URL, Method: factMethod,
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
	status := "complete"
	message := fmt.Sprintf("%d exact station-ID joins from the official provider source", len(matchedStations))
	if len(matchedStations) == 0 {
		status = "no-matches"
		message = "No exact provider channel-number or unique identity joins were found"
	}
	result.Sources = []marketindex.EvidenceSourceRecord{{
		ID: source.ID, Label: source.Label, URL: source.URL, Status: status,
		Matched: len(matchedStations), Aliases: aliases, Categories: categories, Message: message,
	}}
	return result
}

func matchEntry(entry catalogEntry, byNumber map[string][]web.JSONChannel, byIdentity map[string][]web.JSONChannel) (web.JSONChannel, string, bool) {
	for _, number := range entry.Numbers {
		matches := byNumber[normalizeNumber(number)]
		if len(matches) == 1 {
			return matches[0], "exact provider channel number", true
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
			return channel, "unique exact provider callsign or name", true
		}
	}
	return web.JSONChannel{}, "", false
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
