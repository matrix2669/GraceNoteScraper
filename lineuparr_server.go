package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode"

	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	lineuparrbuilder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"github.com/daniel-widrick/GraceNoteScraper/marketindex"
)

//go:embed lineuparr.html
var lineuparrFS embed.FS

type lineuparrServer struct {
	store       *appconfig.Store
	state       *GuideState
	builder     *lineuparrbuilder.Service
	marketIndex *marketindex.Service
	aliasQueue  *aliasJobQueue
}

type aliasIndexResponse struct {
	marketindex.Snapshot
	Queue aliasQueueView `json:"queue"`
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
	additionalSources := lineuparrbuilder.ApplyProviderGuideAliases(config.Gracenote.ProviderName, inputs)
	additionalSources = append(additionalSources, s.applyMarketAliases(inputs)...)
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

func (s *lineuparrServer) applyMarketAliases(inputs []lineuparrbuilder.InputChannel) []lineuparrbuilder.SourceStatus {
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
	matched := 0
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
			if candidate.Kind == marketindex.NameEventCallSign {
				method = "event callsign on the same Gracenote station ID"
			}
			inputs[index].ExternalAliases = append(inputs[index].ExternalAliases, lineuparrbuilder.AttributedAlias{
				Value: candidate.Value, Source: "gracenote-market-index", Method: method,
			})
			added = true
		}
		if added {
			matched++
		}
	}
	snapshot := s.marketIndex.Snapshot()
	return []lineuparrbuilder.SourceStatus{{
		ID: "gracenote-market-index", Label: "Gracenote market alias index", Status: "local", Matched: matched,
		Message: fmt.Sprintf("%d markets and %d unique lineups scanned; exact station-ID aliases only", snapshot.Summary.CompletedMarkets, snapshot.Summary.Lineups),
	}}
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
	response := aliasIndexResponse{Snapshot: s.marketIndex.Snapshot()}
	if s.aliasQueue != nil {
		s.aliasQueue.TryStart()
		response.Snapshot = s.marketIndex.Snapshot()
		response.Queue = s.aliasQueue.View()
	}
	writeLineuparrJSON(w, http.StatusOK, response)
}

func (s *lineuparrServer) handleAliasIndexRun(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		cancelled := false
		if s.aliasQueue != nil {
			cancelled = s.aliasQueue.Cancel()
		}
		writeLineuparrJSON(w, http.StatusOK, map[string]bool{"cancelled": cancelled})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, DELETE")
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
	var request marketindex.RunRequest
	if !decodeLineuparrRequest(w, r, &request) {
		return
	}
	if s.aliasQueue != nil {
		queueView := s.aliasQueue.View()
		if queueView.Queued {
			http.Error(w, errAliasJobAlreadyQueued.Error(), http.StatusConflict)
			return
		}
		if queueView.GuideBusy {
			queued, err := s.aliasQueue.Queue(request)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeLineuparrJSON(w, http.StatusAccepted, map[string]any{"queued": true, "queue": queued})
			return
		}
		s.aliasQueue.ClearError()
	}
	job, err := s.marketIndex.Start(request)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, marketindex.ErrAlreadyRunning) || errors.Is(err, marketindex.ErrNoWork) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeLineuparrJSON(w, http.StatusAccepted, map[string]any{"started": true, "job": job})
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
	inputs := make([]lineuparrbuilder.InputChannel, 0, len(channels))
	seenKeys := make(map[string]int)
	for _, channel := range channels {
		input := lineupInput(channel)
		baseKey := strings.TrimSpace(input.Key)
		if count := seenKeys[baseKey]; count > 0 {
			input.Key = fmt.Sprintf("%s-%d", baseKey, count+1)
		}
		seenKeys[baseKey]++
		inputs = append(inputs, input)
	}
	return config, inputs, true
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
