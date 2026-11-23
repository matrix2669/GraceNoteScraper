package providersource

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/daniel-widrick/GraceNoteScraper/channelcategory"
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

const dishURL = "https://webapps.dish.com/api/clu/cludataservice.asmx/getdata?sortby=ranking"

//go:embed official_catalog.json
var officialCatalogData []byte

type Service struct {
	httpClient          *http.Client
	catalog             catalog
	runMu               sync.Mutex
	spectrumRunSource   map[string]providerResult
	spectrumMu          sync.Mutex
	spectrum            spectrumCatalogState
	spectrumCachePath   string
	spectrumForceToken  string
	spectrumCacheWriter func(string, spectrumCatalogFile) error
	now                 func() time.Time
}

type Options struct {
	UseEmbeddedCatalogs  bool
	SpectrumCatalogPath  string
	SpectrumRefreshToken string
	Now                  func() time.Time
}

type catalog struct {
	AsOf    string          `json:"asOf"`
	Sources []catalogSource `json:"sources"`
}

type catalogSource struct {
	ID             string         `json:"id"`
	Label          string         `json:"label"`
	URL            string         `json:"url"`
	Providers      []string       `json:"providers"`
	PostalCodes    []string       `json:"postalCodes,omitempty"`
	Method         string         `json:"method"`
	Status         string         `json:"status,omitempty"`
	Message        string         `json:"message,omitempty"`
	StationBound   bool           `json:"-"`
	SourceRevision string         `json:"-"`
	Entries        []catalogEntry `json:"entries"`
}

type catalogEntry struct {
	Numbers        []string `json:"numbers,omitempty"`
	Name           string   `json:"name"`
	Aliases        []string `json:"aliases,omitempty"`
	CallSigns      []string `json:"callSigns,omitempty"`
	Category       string   `json:"category,omitempty"`
	CategoryMethod string   `json:"-"`
	RawCategory    string   `json:"-"`
	StationIDs     []string `json:"-"`
	EventFeed      bool     `json:"-"`
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
	service := &Service{httpClient: client, catalog: sources, spectrumRunSource: make(map[string]providerResult), spectrumCachePath: strings.TrimSpace(options.SpectrumCatalogPath), spectrumForceToken: strings.TrimSpace(options.SpectrumRefreshToken), spectrumCacheWriter: writeSpectrumCatalog, now: options.Now}
	if service.now == nil {
		service.now = time.Now
	}
	service.spectrum = loadSpectrumCatalog(service.spectrumCachePath)
	return service
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

func (s *Service) FetchProviderEvidence(ctx context.Context, request lineupindex.ProviderEvidenceRequest) (lineupindex.ProviderEvidenceResult, error) {
	providerName := strings.ToLower(strings.TrimSpace(request.Provider.Name))
	if providerName == "" || request.Grid == nil || lineupindex.ExcludedEnrichmentProvider(providerName) {
		return lineupindex.ProviderEvidenceResult{}, nil
	}
	if request.NationalOnly {
		spectrum := s.fetchSpectrumForRun(ctx, request.EvidenceRunID)
		return spectrumEvidenceResult(request, spectrum), spectrum.err
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
		hasLiveSource = false
	default:
		hasLiveSource = false
	}

	result := lineupindex.ProviderEvidenceResult{}
	if hasLiveSource {
		matched := matchCatalog(request, live.source)
		result.Facts = append(result.Facts, matched.Facts...)
		result.CategoryRelations = append(result.CategoryRelations, matched.CategoryRelations...)
		result.IdentityFacts = append(result.IdentityFacts, matched.IdentityFacts...)
		result.Sources = append(result.Sources, matched.Sources...)
	}
	spectrum := s.fetchSpectrumForRun(ctx, request.EvidenceRunID)
	spectrumMatched := spectrumEvidenceResult(request, spectrum)
	result.Facts = append(result.Facts, spectrumMatched.Facts...)
	result.CategoryRelations = append(result.CategoryRelations, spectrumMatched.CategoryRelations...)
	if spectrumMatched.SnapshotComplete {
		result.SnapshotComplete, result.SnapshotSourceID, result.SnapshotRevision = true, spectrumMatched.SnapshotSourceID, spectrumMatched.SnapshotRevision
		result.SnapshotStationIDs, result.SnapshotFacts = spectrumMatched.SnapshotStationIDs, spectrumMatched.SnapshotFacts
	}
	isSpectrumProvider := strings.Contains(providerName, "spectrum") || strings.Contains(providerName, "charter") || strings.Contains(providerName, "time warner")
	if isSpectrumProvider || (len(spectrumMatched.Sources) > 0 && spectrumMatched.Sources[0].Matched > 0) {
		result.Sources = append(result.Sources, spectrumMatched.Sources...)
	}
	for _, source := range s.catalog.Sources {
		if !sourceMatches(source, providerName, request.PostalCode) {
			continue
		}
		matched := matchCatalog(request, source)
		result.Facts = append(result.Facts, matched.Facts...)
		result.CategoryRelations = append(result.CategoryRelations, matched.CategoryRelations...)
		result.IdentityFacts = append(result.IdentityFacts, matched.IdentityFacts...)
		result.Sources = append(result.Sources, matched.Sources...)
	}
	if live.err != nil {
		return result, live.err
	}
	return result, spectrum.err
}

func (s *Service) EndProviderEvidenceRun(runID string) {
	if strings.TrimSpace(runID) != "" {
		s.runMu.Lock()
		delete(s.spectrumRunSource, runID)
		s.runMu.Unlock()
	}
}

func (s *Service) fetchSpectrumForRun(ctx context.Context, runID string) providerResult {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return s.fetchSpectrum(ctx)
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if result, ok := s.spectrumRunSource[runID]; ok {
		return result
	}
	result := s.fetchSpectrum(ctx)
	if s.spectrumRunSource == nil {
		s.spectrumRunSource = make(map[string]providerResult)
	}
	s.spectrumRunSource[runID] = result
	return result
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

type entryMatchKind int

const (
	entryMatchNone entryMatchKind = iota
	entryMatchIdentity
	entryMatchAliasOnly
	entryMatchEPGCandidate
)

const providerNumberAlignmentPercent = 10

type providerNumberAlignment struct {
	overlappingPositions int
	exactPositions       int
	requiredPositions    int
}

func (alignment providerNumberAlignment) allowsAliasRecovery() bool {
	return alignment.overlappingPositions > 0 && alignment.exactPositions >= alignment.requiredPositions
}

func matchCatalog(request lineupindex.ProviderEvidenceRequest, source catalogSource) lineupindex.ProviderEvidenceResult {
	if strings.TrimSpace(source.SourceRevision) == "" {
		source.SourceRevision = sourceRevision(source)
	}
	byStationID := make(map[string][]web.JSONChannel)
	if len(source.Entries) == 0 {
		status := source.Status
		if status == "" {
			status = "registered"
		}
		return lineupindex.ProviderEvidenceResult{Sources: []lineupindex.EvidenceSourceRecord{{
			ID: source.ID, Label: source.Label, URL: source.URL, Status: status, Message: source.Message,
		}}}
	}
	byNumber := make(map[string][]web.JSONChannel)
	byIdentity := make(map[string][]web.JSONChannel)
	for _, channel := range request.Grid.Channels {
		if stationID := strings.TrimSpace(channel.ChannelID); stationID != "" {
			byStationID[stationID] = append(byStationID[stationID], channel)
		}
		if number := normalizeNumber(channel.ChannelNo); request.AllowChannelNumbers && number != "" {
			byNumber[number] = append(byNumber[number], channel)
		}
		for _, value := range channelIdentityValues(channel) {
			if key := identityKey(value); key != "" {
				byIdentity[key] = append(byIdentity[key], channel)
			}
		}
	}

	result := lineupindex.ProviderEvidenceResult{}
	matchedStations := make(map[string]bool)
	ambiguousNumbers := ambiguousCatalogNumbers(source.Entries)
	alignment := catalogNumberAlignment(source.Entries, byNumber, ambiguousNumbers)
	allowNumberAliases := request.AllowChannelNumbers && alignment.allowsAliasRecovery()
	type matchedEntry struct {
		channel web.JSONChannel
		entry   catalogEntry
		method  string
		kind    entryMatchKind
	}
	matches := make([]matchedEntry, 0, len(source.Entries))
	aliasOwners := make(map[string]map[string]bool)
	epgAliasOwners := make(map[string]map[string]bool)
	for _, entry := range source.Entries {
		channels, method, kind := matchEntry(entry, byStationID, byNumber, byIdentity, ambiguousNumbers, allowNumberAliases)
		if kind == entryMatchNone {
			continue
		}
		for _, channel := range channels {
			if strings.TrimSpace(channel.ChannelID) == "" {
				continue
			}
			matchedStations[channel.ChannelID] = true
			matches = append(matches, matchedEntry{channel: channel, entry: entry, method: method, kind: kind})
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
				if kind == entryMatchIdentity || kind == entryMatchAliasOnly || kind == entryMatchEPGCandidate {
					if epgAliasOwners[key] == nil {
						epgAliasOwners[key] = make(map[string]bool)
					}
					epgAliasOwners[key][channel.ChannelID] = true
				}
			}
		}
	}
	seenFacts := make(map[string]bool)
	seenIdentityFacts := make(map[string]bool)
	for _, match := range matches {
		entry := match.entry
		channel := match.channel
		factMethod := source.Method
		if factMethod == "" {
			factMethod = match.method
		} else {
			factMethod += "; " + match.method
		}
		if match.kind == entryMatchIdentity {
			factMethod += "; identity-policy-v2"
			if request.AllowChannelNumbers {
				factMethod += "; number-policy-provider-v2"
			}
		} else if match.kind == entryMatchAliasOnly || match.kind == entryMatchEPGCandidate {
			factMethod += "; " + lineupindex.ProviderSourceAlignmentV1
		}
		aliases := append([]string{entry.Name}, entry.Aliases...)
		aliases = append(aliases, entry.CallSigns...)
		seenAliases := make(map[string]bool)
		for _, alias := range aliases {
			key := identityKey(alias)
			if key == "" || seenAliases[key] {
				continue
			}
			seenAliases[key] = true
			if len(aliasOwners[key]) != 1 {
				// A shared alias is not safe to apply directly. When every owner
				// was reached through exact identity or one unambiguous official
				// provider row, retain it transiently so the weekday EPG matcher
				// can confirm the cross-GNID pair.
				if len(epgAliasOwners[key]) > 1 && len(epgAliasOwners[key]) == len(aliasOwners[key]) {
					identityFactKey := channel.ChannelID + "\x00" + lineupindex.FactAlias + "\x00" + key
					if !seenIdentityFacts[identityFactKey] {
						seenIdentityFacts[identityFactKey] = true
						result.IdentityFacts = append(result.IdentityFacts, lineupindex.ProviderFact{
							StationID: channel.ChannelID, Kind: lineupindex.FactAlias, Value: strings.TrimSpace(alias),
							SourceID: source.ID, SourceLabel: source.Label, SourceURL: source.URL,
							Method:         factMethod + "; shared exact official provider identity; EPG confirmation required",
							SourceRevision: source.SourceRevision, SourceRowID: sourceRowIdentity(source, entry),
							RootSourceLineupKey: request.LineupKey, RootSourceStationID: channel.ChannelID,
						})
					}
				}
				continue
			}
			if match.kind == entryMatchEPGCandidate {
				continue
			}
			factKey := channel.ChannelID + "\x00" + lineupindex.FactAlias + "\x00" + key
			if seenFacts[factKey] {
				continue
			}
			seenFacts[factKey] = true
			result.Facts = append(result.Facts, lineupindex.ProviderFact{
				StationID: channel.ChannelID, Kind: lineupindex.FactAlias, Value: strings.TrimSpace(alias),
				SourceID: source.ID, SourceLabel: source.Label, SourceURL: source.URL, Method: factMethod, StationBound: source.StationBound, SourceRevision: source.SourceRevision,
				SourceRowID: sourceRowIdentity(source, entry), RootSourceLineupKey: request.LineupKey, RootSourceStationID: channel.ChannelID,
			})
		}
		categoryIdentities := append([]string{entry.Name}, entry.Aliases...)
		categoryIdentities = append(categoryIdentities, entry.CallSigns...)
		categoryIdentities = append(categoryIdentities, channelIdentityValues(channel)...)
		// A unique provider-local number is sufficient to recover descriptive
		// aliases, but category transfer still requires corroborating identity.
		if category, ok := channelcategory.Resolve(entry.Category, categoryIdentities...); ok {
			categoryMethod := category.Method
			if category.Method == channelcategory.MethodFuzzy {
				categoryMethod = fmt.Sprintf("%s %.0f%% to %q", category.Method, category.Confidence*100, category.MatchedAlias)
			}
			if entry.CategoryMethod != "" {
				categoryMethod = entry.CategoryMethod + "; " + categoryMethod
			}
			rawCategory := strings.TrimSpace(entry.RawCategory)
			if rawCategory == "" {
				rawCategory = strings.TrimSpace(entry.Category)
			}
			categoryKey := channel.ChannelID + "\x00" + lineupindex.FactCategory + "\x00" + identityKey(category.Category)
			categoryMethodText := factMethod + "; provider category " + strconv.Quote(rawCategory) + " mapped by " + categoryMethod
			if match.kind == entryMatchIdentity && !seenFacts[categoryKey] {
				seenFacts[categoryKey] = true
				result.Facts = append(result.Facts, lineupindex.ProviderFact{
					StationID: channel.ChannelID, Kind: lineupindex.FactCategory, Value: category.Category,
					RawValue: rawCategory, MatchMethod: category.Method, MatchConfidence: category.Confidence,
					SourceID: source.ID, SourceLabel: source.Label, SourceURL: source.URL,
					Method: categoryMethodText, StationBound: source.StationBound, SourceRevision: source.SourceRevision,
					SourceRowID: sourceRowIdentity(source, entry), RootSourceLineupKey: request.LineupKey, RootSourceStationID: channel.ChannelID,
				})
			}
			// A relation survives an aligned number-only join for review, but does
			// not itself become an accepted category or a new identity proof.
			for _, alias := range aliases {
				alias = strings.TrimSpace(alias)
				aliasKey := identityKey(alias)
				if alias == "" || aliasKey == "" {
					continue
				}
				result.CategoryRelations = append(result.CategoryRelations, lineupindex.ProviderCategoryRelation{
					StationID: channel.ChannelID, AliasValue: alias, AliasNormalized: aliasKey,
					Category: category.Category, RawCategory: rawCategory,
					SourceID: source.ID, SourceLabel: source.Label, SourceURL: source.URL,
					SourceRevision: source.SourceRevision, SourceRowID: sourceRowIdentity(source, entry),
					LineupKeys: []string{request.LineupKey}, SourceLineupKey: request.LineupKey, Method: categoryMethodText,
					StationBound: source.StationBound,
				})
			}
		}
	}
	aliases := 0
	categories := 0
	for _, fact := range result.Facts {
		if fact.Kind == lineupindex.FactCategory {
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
	if request.AllowChannelNumbers && alignment.overlappingPositions > 0 && !alignment.allowsAliasRecovery() {
		alignmentMessage := fmt.Sprintf("%d provider-grid joins from the official provider source; skipped provider-number-only alias recovery because %d of %d overlapping positions had exact same-number identity anchors (minimum %d)", len(matchedStations), alignment.exactPositions, alignment.overlappingPositions, alignment.requiredPositions)
		if message == "" {
			message = alignmentMessage
		} else {
			message += "; " + alignmentMessage
		}
	} else if message == "" {
		if len(matchedStations) == 0 {
			message = "No usable provider-local channel-number or unique identity joins were found"
		} else {
			message = fmt.Sprintf("%d provider-grid joins from the official provider source", len(matchedStations))
		}
	}
	if len(matchedStations) == 0 {
		if source.Status == "" {
			status = "no-matches"
		}
	}
	result.Sources = []lineupindex.EvidenceSourceRecord{{
		ID: source.ID, Label: source.Label, URL: source.URL, Status: status,
		Matched: len(matchedStations), Aliases: aliases, Categories: categories, Message: message,
	}}
	return result
}

func matchEntry(entry catalogEntry, byStationID map[string][]web.JSONChannel, byNumber map[string][]web.JSONChannel, byIdentity map[string][]web.JSONChannel, ambiguousNumbers map[string]bool, allowNumberAliases bool) ([]web.JSONChannel, string, entryMatchKind) {
	if len(entry.StationIDs) > 0 {
		matches := map[string]web.JSONChannel{}
		for _, stationID := range entry.StationIDs {
			for _, channel := range byStationID[strings.TrimSpace(stationID)] {
				if strings.TrimSpace(channel.ChannelID) != "" {
					matches[channel.ChannelID] = channel
				}
			}
		}
		if len(matches) == 0 {
			return nil, "", entryMatchNone
		}
		result := make([]web.JSONChannel, 0, len(matches))
		for _, channel := range matches {
			result = append(result, channel)
		}
		sort.Slice(result, func(i, j int) bool { return result[i].ChannelID < result[j].ChannelID })
		return result, "exact Gracenote station ID from official provider source", entryMatchIdentity
	}
	if entry.EventFeed {
		if channel, ok := uniqueIdentityMatch(entry, byIdentity); ok {
			return []web.JSONChannel{channel}, "unique exact event-feed identity", entryMatchIdentity
		}
		return nil, "", entryMatchNone
	}
	for _, number := range entry.Numbers {
		normalizedNumber := normalizeNumber(number)
		matches := byNumber[normalizedNumber]
		if len(matches) > 0 && !ambiguousNumbers[normalizedNumber] {
			exact := exactSameNumberMatches(entry, matches)
			if len(exact) > 0 {
				result := make([]web.JSONChannel, 0, len(exact))
				for _, channel := range exact {
					result = append(result, channel)
				}
				sort.Slice(result, func(i, j int) bool { return result[i].ChannelID < result[j].ChannelID })
				return result, "exact provider channel number plus exact identity across same-number variants", entryMatchIdentity
			}
			if allowNumberAliases && len(matches) == 1 {
				return matches, "unique provider-local channel number; number-policy-provider-alias-v3", entryMatchAliasOnly
			}
			if allowNumberAliases && len(matches) > 1 {
				return matches, "one official provider row shared by same-position station variants; number-policy-provider-alias-v3; EPG confirmation required", entryMatchEPGCandidate
			}
		}
	}
	if channel, ok := uniqueIdentityMatch(entry, byIdentity); ok {
		return []web.JSONChannel{channel}, "unique exact provider callsign or name", entryMatchIdentity
	}
	return nil, "", entryMatchNone
}

func catalogNumberAlignment(entries []catalogEntry, byNumber map[string][]web.JSONChannel, ambiguousNumbers map[string]bool) providerNumberAlignment {
	overlapping := make(map[string]bool)
	exact := make(map[string]bool)
	for _, entry := range entries {
		if entry.EventFeed {
			continue
		}
		for _, value := range entry.Numbers {
			number := normalizeNumber(value)
			matches := byNumber[number]
			if number == "" || len(matches) == 0 || ambiguousNumbers[number] {
				continue
			}
			overlapping[number] = true
			if len(exactSameNumberMatches(entry, matches)) > 0 {
				exact[number] = true
			}
		}
	}
	alignment := providerNumberAlignment{
		overlappingPositions: len(overlapping),
		exactPositions:       len(exact),
	}
	if alignment.overlappingPositions > 0 {
		alignment.requiredPositions = (alignment.overlappingPositions*providerNumberAlignmentPercent + 99) / 100
	}
	return alignment
}

func exactSameNumberMatches(entry catalogEntry, matches []web.JSONChannel) map[string]web.JSONChannel {
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
	return exact
}

func ambiguousCatalogNumbers(entries []catalogEntry) map[string]bool {
	identities := make(map[string]map[string]bool)
	for _, entry := range entries {
		if entry.EventFeed {
			continue
		}
		identity := identityKey(entry.Name)
		if identity == "" {
			continue
		}
		for _, number := range entry.Numbers {
			number = normalizeNumber(number)
			if number == "" {
				continue
			}
			if identities[number] == nil {
				identities[number] = make(map[string]bool)
			}
			identities[number][identity] = true
		}
	}
	result := make(map[string]bool)
	for number, owners := range identities {
		result[number] = len(owners) > 1
	}
	return result
}

func uniqueIdentityMatch(entry catalogEntry, byIdentity map[string][]web.JSONChannel) (web.JSONChannel, bool) {
	identities := append([]string{entry.Name}, entry.Aliases...)
	identities = append(identities, entry.CallSigns...)
	unique := make(map[string]web.JSONChannel)
	for _, identity := range identities {
		matches := byIdentity[identityKey(identity)]
		if len(matches) == 1 {
			unique[matches[0].ChannelID] = matches[0]
		}
	}
	if len(unique) != 1 {
		return web.JSONChannel{}, false
	}
	for _, channel := range unique {
		return channel, true
	}
	return web.JSONChannel{}, false
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
	key := strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			return unicode.ToUpper(character)
		}
		return -1
	}, strings.TrimSpace(value))
	if strings.HasSuffix(key, "HD") && len(key) > 4 {
		key = strings.TrimSuffix(key, "HD")
	}
	if (strings.HasPrefix(key, "W") || strings.HasPrefix(key, "K")) && (len(key) == 5 || len(key) == 6) && strings.HasSuffix(key, "DT") {
		key = strings.TrimSuffix(key, "DT")
	}
	return key
}

// sourceRowIdentity is stable across scans of the same provider source row;
// source revisions intentionally remain separate metadata so a revised row
// replaces the prior relation during source-scope reconciliation.
func sourceRowIdentity(source catalogSource, entry catalogEntry) string {
	values := []string{source.ID, identityKey(entry.Name), identityKey(entry.Category)}
	stationIDs := append([]string(nil), entry.StationIDs...)
	sort.Strings(stationIDs)
	for _, stationID := range stationIDs {
		values = append(values, strings.TrimSpace(stationID))
	}
	for _, group := range [][]string{entry.Numbers, entry.Aliases, entry.CallSigns} {
		cleaned := make([]string, 0, len(group))
		for _, value := range group {
			cleaned = append(cleaned, identityKey(value))
		}
		values = append(values, strings.Join(cleaned, ","))
	}
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return "row-" + hex.EncodeToString(digest[:12])
}

func sourceRevision(source catalogSource) string {
	rows := make([]string, 0, len(source.Entries))
	for _, entry := range source.Entries {
		rows = append(rows, sourceRowIdentity(source, entry))
	}
	sort.Strings(rows)
	digest := sha256.Sum256([]byte(source.ID + "\x00" + strings.Join(rows, "\x00")))
	return "catalog-" + hex.EncodeToString(digest[:12])
}
