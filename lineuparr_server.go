package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	lineuparrbuilder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

//go:embed lineuparr.html
var lineuparrFS embed.FS

type lineuparrServer struct {
	store     *appconfig.Store
	state     *GuideState
	builder   *lineuparrbuilder.Service
	exportDir string
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

func (s *lineuparrServer) handleCategories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}
	var body struct {
		ChannelIDs []string `json:"channelIds"`
		Category   string   `json:"category"`
	}
	if !decodeLineuparrRequest(w, r, &body) {
		return
	}
	if len(body.ChannelIDs) == 0 {
		http.Error(w, "select at least one channel", http.StatusBadRequest)
		return
	}
	if len(body.ChannelIDs) > 1000 {
		http.Error(w, "no more than 1000 channels can be changed at once", http.StatusBadRequest)
		return
	}
	config, inputs, ok := s.activeInputs(w)
	if !ok {
		return
	}
	known := make(map[string]bool, len(inputs))
	for _, input := range inputs {
		known[input.Key] = true
	}
	channelIDs := make([]string, 0, len(body.ChannelIDs))
	seen := make(map[string]bool, len(body.ChannelIDs))
	for _, channelID := range body.ChannelIDs {
		channelID = strings.TrimSpace(channelID)
		if channelID == "" || seen[channelID] {
			continue
		}
		if !known[channelID] {
			http.Error(w, "channel does not belong to the active lineup", http.StatusNotFound)
			return
		}
		seen[channelID] = true
		channelIDs = append(channelIDs, channelID)
	}
	if len(channelIDs) == 0 {
		http.Error(w, "select at least one channel", http.StatusBadRequest)
		return
	}
	current, err := s.store.WhileCurrent(config.Fingerprint(), func() error {
		return s.builder.UpdateChannelsCategory(config.Fingerprint(), channelIDs, body.Category)
	})
	if err != nil {
		http.Error(w, "Unable to save channel categories: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !current {
		http.Error(w, "The active provider changed; reload the builder before saving", http.StatusConflict)
		return
	}
	writeLineuparrJSON(w, http.StatusOK, map[string]any{"saved": true, "updated": len(channelIDs)})
}

type lineuparrChannelProgramsResponse struct {
	ChannelID string       `json:"channelId"`
	StationID string       `json:"stationId"`
	Number    string       `json:"number"`
	Name      string       `json:"name"`
	Programs  []APIProgram `json:"programs"`
}

func (s *lineuparrServer) handleChannelPrograms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	channelID := strings.TrimSpace(r.URL.Query().Get("channelId"))
	if channelID == "" {
		http.Error(w, "channelId is required", http.StatusBadRequest)
		return
	}
	config, inputs, ok := s.activeInputs(w)
	if !ok {
		return
	}
	var selected lineuparrbuilder.InputChannel
	found := false
	for _, input := range inputs {
		if input.Key == channelID {
			selected = input
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "channel does not belong to the active lineup", http.StatusNotFound)
		return
	}
	g := s.state.GetForSource(config.Fingerprint())
	if g == nil {
		http.Error(w, "The active lineup is still being generated", http.StatusServiceUnavailable)
		return
	}
	programs := make([]APIProgram, 0, 24)
	now := time.Now().UTC()
	for _, program := range g.Programs {
		if program.Channel != selected.StationID {
			continue
		}
		item := apiProgram(program)
		if end, err := time.Parse(time.RFC3339, item.End); err == nil && !end.After(now) {
			continue
		}
		programs = append(programs, item)
	}
	sort.Slice(programs, func(i, j int) bool { return programs[i].Start < programs[j].Start })
	if len(programs) > 24 {
		programs = programs[:24]
	}
	name := selected.CallSign
	if name == "" {
		name = selected.Affiliate
	} else if selected.Affiliate != "" && !strings.EqualFold(selected.Affiliate, selected.CallSign) {
		name += " · " + selected.Affiliate
	}
	writeLineuparrJSON(w, http.StatusOK, lineuparrChannelProgramsResponse{
		ChannelID: channelID, StationID: selected.StationID, Number: selected.Number, Name: name, Programs: programs,
	})
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
	var body struct {
		ChannelIDs *[]string `json:"channelIds"`
	}
	if !decodeLineuparrRequest(w, r, &body) {
		return
	}
	draft, config, _, ok := s.buildDraft(w, r)
	if !ok {
		return
	}
	removed := len(draft.DuplicateSuggestions)
	current, err := s.store.WhileCurrent(config.Fingerprint(), func() error {
		if body.ChannelIDs != nil {
			requested := make([]string, 0, len(*body.ChannelIDs))
			seen := make(map[string]bool, len(*body.ChannelIDs))
			for _, id := range *body.ChannelIDs {
				id = strings.TrimSpace(id)
				if id != "" && !seen[id] {
					seen[id] = true
					requested = append(requested, id)
				}
			}
			removed = len(requested)
			return s.builder.RemoveSuggestedDuplicateIDs(config.Fingerprint(), draft, requested)
		}
		return s.builder.RemoveSuggestedDuplicates(config.Fingerprint(), draft)
	})
	if err != nil {
		http.Error(w, "Unable to remove suggested duplicates: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !current {
		http.Error(w, "The active provider changed; reload the builder before saving", http.StatusConflict)
		return
	}
	writeLineuparrJSON(w, http.StatusOK, map[string]any{"saved": true, "removed": removed})
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
	draft, err := s.builder.Build(r.Context(), lineuparrbuilder.LineupContext{
		SourceFingerprint: config.Fingerprint(),
		Country:           config.Gracenote.Country,
		PostalCode:        config.Gracenote.PostalCode,
		ProviderName:      config.Gracenote.ProviderName,
		LineupID:          config.Gracenote.LineupID,
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
	draft.SourceFingerprint = config.Fingerprint()
	return draft, config, inputs, true
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
