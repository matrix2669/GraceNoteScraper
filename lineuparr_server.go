package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/geocode"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	lineuparrbuilder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
)

//go:embed lineuparr.html
var lineuparrFS embed.FS

type lineuparrServer struct {
	store           *appconfig.Store
	state           *GuideState
	builder         *lineuparrbuilder.Service
	marketIndex     *lineupindex.Service
	addressSearcher providerAddressSearcher
}

type providerAddressSearcher interface {
	Search(context.Context, string, string, string) ([]geocode.AddressResult, error)
}

type providerAddressConfigResponse struct {
	Required       bool   `json:"required"`
	Enabled        bool   `json:"enabled"`
	ProviderID     string `json:"providerId,omitempty"`
	ProviderLabel  string `json:"providerLabel,omitempty"`
	PostalCode     string `json:"postalCode,omitempty"`
	CountryCode    string `json:"countryCode,omitempty"`
	AttributionURL string `json:"attributionUrl,omitempty"`
	Message        string `json:"message,omitempty"`
}

func (s *lineuparrServer) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/lineuparr" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, configured, _ := s.store.Get(); !configured {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	data, err := lineuparrFS.ReadFile("lineuparr.html")
	if err != nil {
		http.Error(w, "Lineuparr builder page unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		_, _ = w.Write(data)
	}
}

func (s *lineuparrServer) handleProviderAddressConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		http.Error(w, "Choose a provider at /setup first", http.StatusConflict)
		return
	}
	config, configured, _ := s.store.Get()
	if !configured {
		http.Error(w, "Choose a provider at /setup first", http.StatusConflict)
		return
	}
	response := providerAddressConfigResponse{
		PostalCode:  config.Gracenote.PostalCode,
		CountryCode: autocompleteCountryCode(config.Gracenote.Country),
	}
	if source, ok := lineuparrbuilder.ProviderGuideSourceForLineup(config.Gracenote.ProviderName, config.Gracenote.Location, config.Gracenote.PostalCode); ok {
		response.ProviderID = source.ID
		response.ProviderLabel = source.Label
		response.Required = source.LocationMode == "address"
	}
	if response.Required {
		response.Enabled = s.addressSearcher != nil
		response.AttributionURL = "https://www.openstreetmap.org/copyright"
		if response.Enabled {
			response.Message = "Search once, then select a complete OpenStreetMap address that matches the active lineup postal code. The address is sent to the configured geocoder but is not persisted by GraceNoteScraper."
		} else {
			response.Message = "Address search is disabled. Set NOMINATIM_URL to a public, hosted, or self-managed Nominatim endpoint."
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeLineuparrJSON(w, http.StatusOK, response)
}

func (s *lineuparrServer) handleProviderAddressSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}
	if s.addressSearcher == nil {
		http.Error(w, "Address search is disabled", http.StatusServiceUnavailable)
		return
	}
	config, configured, _ := s.store.Get()
	if !configured {
		http.Error(w, "Choose a provider at /setup first", http.StatusConflict)
		return
	}
	source, ok := lineuparrbuilder.ProviderGuideSourceForLineup(config.Gracenote.ProviderName, config.Gracenote.Location, config.Gracenote.PostalCode)
	if !ok || source.LocationMode != "address" {
		http.Error(w, "The active provider source does not require a street address", http.StatusConflict)
		return
	}
	var body struct {
		Query string `json:"query"`
	}
	if !decodeLineuparrRequest(w, r, &body) {
		return
	}
	body.Query = strings.TrimSpace(body.Query)
	if len(body.Query) < 5 {
		http.Error(w, "Enter a complete street address", http.StatusBadRequest)
		return
	}
	searchContext, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	results, err := s.addressSearcher.Search(searchContext, body.Query, config.Gracenote.PostalCode, autocompleteCountryCode(config.Gracenote.Country))
	if err != nil {
		http.Error(w, "Unable to search addresses: "+err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeLineuparrJSON(w, http.StatusOK, map[string]any{"results": results})
}

func autocompleteCountryCode(country string) string {
	switch strings.ToUpper(strings.TrimSpace(country)) {
	case "USA":
		return "us"
	case "CAN":
		return "ca"
	case "GBR":
		return "gb"
	case "AUS":
		return "au"
	default:
		return ""
	}
}

func (s *lineuparrServer) handleDraft(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	draft, _, _, ok := s.buildDraft(w, r)
	if !ok {
		return
	}
	writeLineuparrJSON(w, http.StatusOK, draft)
}

func (s *lineuparrServer) handleChannel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}
	type requestBody struct {
		ChannelID string `json:"channelId"`
		lineuparrbuilder.ChannelUpdate
	}
	var body requestBody
	if !decodeLineuparrRequest(w, r, &body) {
		return
	}
	body.ChannelID = strings.TrimSpace(body.ChannelID)
	config, inputs, ok := s.activeInputs(w)
	if !ok {
		return
	}
	known := false
	for _, input := range inputs {
		if input.Key == body.ChannelID {
			known = true
			break
		}
	}
	if !known {
		http.Error(w, "channel does not belong to the active lineup", http.StatusNotFound)
		return
	}
	current, err := s.store.WhileCurrent(config.Fingerprint(), func() error {
		return s.builder.UpdateChannel(config.Fingerprint(), body.ChannelID, body.ChannelUpdate)
	})
	if err != nil {
		http.Error(w, "Unable to save channel choice: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !current {
		http.Error(w, "The active provider changed; reload the builder before saving", http.StatusConflict)
		return
	}
	writeLineuparrJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

func (s *lineuparrServer) handleRemoveDuplicates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}
	draft, config, _, ok := s.buildDraft(w, r)
	if !ok {
		return
	}
	current, err := s.store.WhileCurrent(config.Fingerprint(), func() error {
		return s.builder.RemoveSuggestedDuplicates(config.Fingerprint(), draft)
	})
	if err != nil {
		http.Error(w, "Unable to remove suggested duplicates: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !current {
		http.Error(w, "The active provider changed; reload the builder before saving", http.StatusConflict)
		return
	}
	writeLineuparrJSON(w, http.StatusOK, map[string]any{"saved": true, "removed": len(draft.DuplicateSuggestions)})
}

func (s *lineuparrServer) handleRestoreAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}
	config, _, ok := s.activeInputs(w)
	if !ok {
		return
	}
	current, err := s.store.WhileCurrent(config.Fingerprint(), func() error {
		return s.builder.RestoreAll(config.Fingerprint())
	})
	if err != nil {
		http.Error(w, "Unable to restore channels: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !current {
		http.Error(w, "The active provider changed; reload the builder before saving", http.StatusConflict)
		return
	}
	writeLineuparrJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

func (s *lineuparrServer) handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	draft, _, _, ok := s.buildDraft(w, r)
	if !ok {
		return
	}
	export := lineuparrbuilder.ExportFromDraft(draft)
	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		http.Error(w, "Unable to create Lineuparr JSON", http.StatusInternalServerError)
		return
	}
	data = append(data, '\n')
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+lineuparrbuilder.ExportFilename(draft)+`"`)
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		_, _ = w.Write(data)
	}
}

func (s *lineuparrServer) buildDraft(w http.ResponseWriter, r *http.Request) (*lineuparrbuilder.Draft, appconfig.Config, []lineuparrbuilder.InputChannel, bool) {
	config, inputs, ok := s.activeInputs(w)
	if !ok {
		return nil, appconfig.Config{}, nil, false
	}
	additionalSources := lineuparrbuilder.ApplyProviderGuideAliasesForLineup(config.Gracenote.ProviderName, config.Gracenote.Location, config.Gracenote.PostalCode, inputs)
	additionalSources = append(additionalSources, s.builder.ApplyEmbeddedCatalogs(inputs)...)
	additionalSources = append(additionalSources, s.applyMarketAliases(config.Gracenote.Country, config.Gracenote.PostalCode, inputs)...)
	draft, err := s.builder.Build(r.Context(), lineuparrbuilder.LineupContext{
		SourceFingerprint: config.Fingerprint(),
		Country:           config.Gracenote.Country,
		PostalCode:        config.Gracenote.PostalCode,
		ProviderName:      config.Gracenote.ProviderName,
		LineupID:          config.Gracenote.LineupID,
		AdditionalSources: additionalSources,
	}, inputs)
	if err != nil {
		http.Error(w, "Unable to build Lineuparr draft: "+err.Error(), http.StatusInternalServerError)
		return nil, appconfig.Config{}, nil, false
	}
	current, configured, _ := s.store.Get()
	if !configured || current.Fingerprint() != config.Fingerprint() {
		http.Error(w, "The active provider changed while the draft was building; reload and try again", http.StatusConflict)
		return nil, appconfig.Config{}, nil, false
	}
	return draft, config, inputs, true
}

func (s *lineuparrServer) applyMarketAliases(country, postalCode string, inputs []lineuparrbuilder.InputChannel) []lineuparrbuilder.SourceStatus {
	if s.marketIndex == nil {
		return nil
	}
	stationIDs := make([]string, 0, len(inputs))
	for _, input := range inputs {
		if strings.TrimSpace(input.StationID) != "" {
			stationIDs = append(stationIDs, input.StationID)
		}
	}
	candidates := s.marketIndex.AliasesForStations(stationIDs)
	categories := s.marketIndex.CategoriesForStations(stationIDs)
	matched := 0
	providerMatched := make(map[string]map[int]bool)
	providerConflicts := make(map[string]int)
	for index := range inputs {
		known := map[string]bool{normalizeAlias(inputs[index].CallSign): true, normalizeAlias(inputs[index].Affiliate): true}
		for _, value := range inputs[index].EventCallSigns {
			known[normalizeAlias(value)] = true
		}
		added := false
		for _, candidate := range candidates[inputs[index].StationID] {
			normalized := normalizeAlias(candidate.Value)
			if normalized == "" || known[normalized] {
				continue
			}
			known[normalized] = true
			method := "same Gracenote station ID across scanned lineups"
			if candidate.Kind == lineupindex.NameEventCallSign {
				method = "event callsign on the same Gracenote station ID"
			} else if candidate.Method != "" {
				method = candidate.Method
			}
			sourceID := candidate.SourceID
			if sourceID == "" {
				sourceID = "gracenote-market-index"
			}
			inputs[index].ExternalAliases = append(inputs[index].ExternalAliases, lineuparrbuilder.AttributedAlias{
				Value: candidate.Value, Source: sourceID, Method: method,
			})
			if providerMatched[sourceID] == nil {
				providerMatched[sourceID] = make(map[int]bool)
			}
			providerMatched[sourceID][index] = true
			added = true
		}
		if category, ok := categories[inputs[index].StationID]; ok {
			sourceID := "same-zip-provider-evidence"
			label := "Same-ZIP official provider evidence"
			if len(category.SourceIDs) > 0 {
				sourceID = category.SourceIDs[0]
			}
			if len(category.SourceLabels) > 0 {
				label = strings.Join(category.SourceLabels, ", ")
			}
			providerCategory := &lineuparrbuilder.AttributedCategory{
				Value: category.Value, Source: sourceID, Label: label,
				Method: strings.Join(category.Methods, "; "),
			}
			if inputs[index].CategoryConflict {
				providerConflicts[sourceID]++
			} else if existing := inputs[index].CategoryHint; existing != nil && existing.Source != "gracenote-schedule" && !strings.EqualFold(strings.TrimSpace(existing.Value), strings.TrimSpace(providerCategory.Value)) {
				inputs[index].CategoryHint = nil
				inputs[index].CategoryConflict = true
				providerConflicts[sourceID]++
			} else {
				inputs[index].CategoryHint = providerCategory
				if providerMatched[sourceID] == nil {
					providerMatched[sourceID] = make(map[int]bool)
				}
				providerMatched[sourceID][index] = true
				added = true
			}
		}
		if added {
			matched++
		}
	}
	snapshot := s.marketIndex.SnapshotForPostal(country, postalCode)
	statuses := []lineuparrbuilder.SourceStatus{{
		ID: "gracenote-market-index", Label: "Gracenote lineup alias index", Status: "local", Matched: matched,
		Message: fmt.Sprintf("%d unique lineups indexed; exact station-ID aliases plus confirmed pair-level weekday EPG evidence", snapshot.Summary.Lineups),
	}}
	if snapshot.PostalScan != nil {
		for _, source := range snapshot.PostalScan.Sources {
			statuses = append(statuses, lineuparrbuilder.SourceStatus{
				ID: source.ID, Label: source.Label, URL: source.URL, Status: source.Status,
				Matched: len(providerMatched[source.ID]), Ambiguous: providerConflicts[source.ID], Message: fmt.Sprintf("%d provider joins, %d aliases and %d categories captured for ZIP %s. %s", source.Matched, source.Aliases, source.Categories, postalCode, source.Message),
			})
		}
	}
	return statuses
}

func normalizeAlias(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToUpper(r)
		}
		return -1
	}, strings.TrimSpace(value))
}

func (s *lineuparrServer) handleAliasIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.marketIndex == nil {
		http.Error(w, "Alias discovery is unavailable; check the application log", http.StatusServiceUnavailable)
		return
	}
	if s.store == nil {
		writeLineuparrJSON(w, http.StatusOK, s.marketIndex.Snapshot())
		return
	}
	config, configured, _ := s.store.Get()
	if !configured {
		http.Error(w, "Choose a provider at /setup first", http.StatusConflict)
		return
	}
	writeLineuparrJSON(w, http.StatusOK, s.marketIndex.SnapshotForPostal(config.Gracenote.Country, config.Gracenote.PostalCode))
}

func (s *lineuparrServer) handleAliasIndexRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.marketIndex == nil {
		http.Error(w, "Alias discovery is unavailable; check the application log", http.StatusServiceUnavailable)
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}
	var body struct {
		lineupindex.RunRequest
		ProviderAddress lineupindex.ProviderAddress `json:"providerAddress,omitempty"`
	}
	if !decodeLineuparrRequest(w, r, &body) {
		return
	}
	request := body.RunRequest
	if strings.EqualFold(strings.TrimSpace(request.Action), "postal") {
		if s.store == nil {
			http.Error(w, "Choose a provider at /setup first", http.StatusConflict)
			return
		}
		config, configured, _ := s.store.Get()
		if !configured {
			http.Error(w, "Choose a provider at /setup first", http.StatusConflict)
			return
		}
		request.Country = config.Gracenote.Country
		request.PostalCode = config.Gracenote.PostalCode
		request.Language = config.Gracenote.Language
		request.AddressProvider = config.Gracenote.ProviderName
		providerAddress, err := validateEphemeralProviderAddress(body.ProviderAddress, config.Gracenote.PostalCode)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		request.ProviderAddress = providerAddress
	}
	job, err := s.marketIndex.Start(request)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, lineupindex.ErrAlreadyRunning) || errors.Is(err, lineupindex.ErrNoWork) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeLineuparrJSON(w, http.StatusAccepted, map[string]any{"started": true, "job": job})
}

func validateEphemeralProviderAddress(value lineupindex.ProviderAddress, postalCode string) (lineupindex.ProviderAddress, error) {
	value.FormattedAddress = strings.Join(strings.Fields(strings.TrimSpace(value.FormattedAddress)), " ")
	value.StreetAddress = strings.Join(strings.Fields(strings.TrimSpace(value.StreetAddress)), " ")
	value.City = strings.Join(strings.Fields(strings.TrimSpace(value.City)), " ")
	value.State = strings.Join(strings.Fields(strings.TrimSpace(value.State)), " ")
	value.PostalCode = strings.Join(strings.Fields(strings.TrimSpace(value.PostalCode)), " ")
	value.CountryCode = strings.ToUpper(strings.TrimSpace(value.CountryCode))
	if value.FormattedAddress == "" {
		return lineupindex.ProviderAddress{}, nil
	}
	if len(value.FormattedAddress) > 300 || len(value.StreetAddress) > 180 || len(value.City) > 100 || len(value.State) > 100 || len(value.PostalCode) > 20 || len(value.CountryCode) > 2 {
		return lineupindex.ProviderAddress{}, errors.New("provider address is too long")
	}
	if postalCode = strings.TrimSpace(postalCode); postalCode != "" {
		selected := compactLocationValue(value.PostalCode)
		active := compactLocationValue(postalCode)
		matches := selected == active
		if value.CountryCode == "US" && len(active) == 5 && len(selected) == 9 {
			matches = strings.HasPrefix(selected, active)
		}
		if !matches {
			return lineupindex.ProviderAddress{}, errors.New("provider address must match the active lineup postal code")
		}
	}
	return value, nil
}

func compactLocationValue(value string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			return unicode.ToUpper(character)
		}
		return -1
	}, value)
}

func (s *lineuparrServer) handleAliasIndexStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.marketIndex == nil {
		http.Error(w, "Alias discovery is unavailable; check the application log", http.StatusServiceUnavailable)
		return
	}
	writeLineuparrJSON(w, http.StatusOK, map[string]bool{"stopping": s.marketIndex.Stop()})
}

func (s *lineuparrServer) activeInputs(w http.ResponseWriter) (appconfig.Config, []lineuparrbuilder.InputChannel, bool) {
	config, configured, _ := s.store.Get()
	if !configured {
		http.Error(w, "Choose a provider at /setup first", http.StatusConflict)
		return appconfig.Config{}, nil, false
	}
	g := s.state.GetForSource(config.Fingerprint())
	if g == nil {
		w.Header().Set("Retry-After", "30")
		http.Error(w, "The active lineup is still being generated", http.StatusServiceUnavailable)
		return appconfig.Config{}, nil, false
	}
	channels := g.LineupChannels
	if len(channels) == 0 {
		channels = g.Channels
	}
	categoryHints := scheduleCategoryHints(g)
	inputs := make([]lineuparrbuilder.InputChannel, 0, len(channels))
	seenKeys := make(map[string]int)
	for _, channel := range channels {
		input := lineupInput(channel)
		input.CategoryHint = categoryHints[channel.ID]
		baseKey := strings.TrimSpace(input.Key)
		if count := seenKeys[baseKey]; count > 0 {
			input.Key = fmt.Sprintf("%s-%d", baseKey, count+1)
		}
		seenKeys[baseKey]++
		inputs = append(inputs, input)
	}
	return config, inputs, true
}

const (
	scheduleCategoryMinimumPrograms = 8
	scheduleCategoryMinimumMinutes  = 360
	scheduleCategoryMinimumShare    = 70
)

type scheduleCategoryProfile struct {
	programs int
	minutes  int
	byFilter map[string]int
}

func scheduleCategoryHints(g *guide.TVGuide) map[string]*lineuparrbuilder.AttributedCategory {
	profiles := make(map[string]*scheduleCategoryProfile)
	for _, program := range g.Programs {
		channelID := strings.TrimSpace(program.Channel)
		minutes, err := strconv.Atoi(strings.TrimSpace(program.Length))
		if channelID == "" || err != nil || minutes <= 0 {
			continue
		}
		profile := profiles[channelID]
		if profile == nil {
			profile = &scheduleCategoryProfile{byFilter: make(map[string]int)}
			profiles[channelID] = profile
		}
		profile.programs++
		profile.minutes += minutes
		seenFilters := make(map[string]bool)
		for _, category := range program.Categories {
			filter := strings.ToLower(strings.TrimSpace(category.Name))
			switch filter {
			case "sports", "news", "movie", "family":
				if !seenFilters[filter] {
					profile.byFilter[filter] += minutes
					seenFilters[filter] = true
				}
			}
		}
	}

	channels := make(map[string]guide.Channel)
	for _, channel := range g.Channels {
		channels[channel.ID] = channel
	}
	for _, channel := range g.LineupChannels {
		channels[channel.ID] = channel
	}

	hints := make(map[string]*lineuparrbuilder.AttributedCategory)
	for channelID, profile := range profiles {
		if profile.programs < scheduleCategoryMinimumPrograms || profile.minutes < scheduleCategoryMinimumMinutes {
			continue
		}
		filter, filterMinutes := dominantScheduleFilter(profile.byFilter)
		share := filterMinutes * 100 / profile.minutes
		if filter == "" || share < scheduleCategoryMinimumShare {
			continue
		}
		category := mapScheduleFilter(filter, channels[channelID])
		if category == "" {
			continue
		}
		hints[channelID] = &lineuparrbuilder.AttributedCategory{
			Value:  category,
			Source: "gracenote-schedule",
			Label:  "Gracenote schedule profile",
			Method: fmt.Sprintf("%d%% of scheduled minutes use Gracenote %s filter", share, filter),
		}
	}
	return hints
}

func dominantScheduleFilter(minutesByFilter map[string]int) (string, int) {
	bestFilter := ""
	bestMinutes := 0
	for _, filter := range []string{"sports", "news", "movie", "family"} {
		if minutesByFilter[filter] > bestMinutes {
			bestFilter = filter
			bestMinutes = minutesByFilter[filter]
		}
	}
	return bestFilter, bestMinutes
}

func mapScheduleFilter(filter string, channel guide.Channel) string {
	switch filter {
	case "sports":
		return "Sports"
	case "news":
		return "News"
	case "movie":
		return "Movies"
	case "family":
		if looksLikeKidsChannel(channel) {
			return "Kids"
		}
	}
	return ""
}

func looksLikeKidsChannel(channel guide.Channel) bool {
	parts := []string{channel.CallSign, channel.Affiliate}
	for _, displayName := range channel.DisplayNames {
		parts = append(parts, displayName.Name)
	}
	compact := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, strings.Join(parts, " "))
	for _, marker := range []string{
		"babyfirst", "boomerang", "cartoonnetwork", "disney", "nickelodeon", "nickjr", "nicktoons", "niktoon", "pbskids", "sprout", "teennick", "universalkids",
	} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}

func lineupInput(channel guide.Channel) lineuparrbuilder.InputChannel {
	key := strings.TrimSpace(channel.PlacementID)
	if key == "" {
		key = strings.Join([]string{channel.ID, channel.ChannelNo, channel.CallSign}, "|")
	}
	callSign := html.UnescapeString(channel.CallSign)
	if callSign == "" && len(channel.DisplayNames) >= 3 {
		callSign = html.UnescapeString(channel.DisplayNames[2].Name)
	}
	number := html.UnescapeString(channel.ChannelNo)
	if number == "" && len(channel.DisplayNames) >= 2 {
		number = html.UnescapeString(channel.DisplayNames[1].Name)
	}
	return lineuparrbuilder.InputChannel{
		Key:            key,
		StationID:      channel.ID,
		PlacementID:    channel.PlacementID,
		Number:         number,
		CallSign:       callSign,
		Affiliate:      html.UnescapeString(channel.Affiliate),
		EventCallSigns: append([]string(nil), channel.EventCallSigns...),
	}
}

func requireJSONContentType(w http.ResponseWriter, r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return false
	}
	return true
}

func decodeLineuparrRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		http.Error(w, "Invalid request: request must contain one JSON object", http.StatusBadRequest)
		return false
	}
	return true
}

func writeLineuparrJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(true)
	_ = encoder.Encode(value)
}
