package providersource

import (
	"bufio"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/channelcategory"
)

const (
	spectrumGuideURL        = "https://www.spectrum.com/cable-tv/channel-lineup"
	spectrumCatalogURL      = "https://www.spectrum.com/orchestrator/externalApi/channel-lineup/9999/unknown"
	spectrumCatalogSchema   = 1
	spectrumRefreshInterval = 7 * 24 * time.Hour
	minimumSpectrumEntries  = 300
)

//go:embed spectrum_catalog.tsv
var bundledSpectrumCatalogData []byte

type spectrumCatalogEntry struct {
	StationID string   `json:"stationId"`
	Name      string   `json:"name"`
	Category  string   `json:"category"`
	Packages  []string `json:"packages,omitempty"`
}

type spectrumCatalogFile struct {
	SchemaVersion    int                    `json:"schemaVersion"`
	SourceURL        string                 `json:"sourceUrl"`
	CapturedAt       string                 `json:"capturedAt,omitempty"`
	RefreshedAt      string                 `json:"refreshedAt,omitempty"`
	CheckedAt        string                 `json:"checkedAt,omitempty"`
	ContentHash      string                 `json:"contentHash"`
	RefreshToken     string                 `json:"refreshToken,omitempty"`
	LastRefreshError string                 `json:"lastRefreshError,omitempty"`
	Entries          []spectrumCatalogEntry `json:"entries"`
}

type spectrumCatalogState struct {
	file   spectrumCatalogFile
	origin string
}

type spectrumAPIResponse struct {
	CluInfo struct {
		CLU struct {
			Channels []struct {
				TMSID       json.RawMessage `json:"TMSID"`
				ChannelName string          `json:"ChannelName"`
				Packages    []string        `json:"SPPCategory"`
				Genres      []string        `json:"Genre"`
			} `json:"Channels"`
		} `json:"CLU"`
	} `json:"CluInfo"`
}

func loadSpectrumCatalog(cachePath string) spectrumCatalogState {
	bundled, err := parseBundledSpectrumCatalog(bundledSpectrumCatalogData)
	if err != nil {
		bundled = spectrumCatalogFile{SchemaVersion: spectrumCatalogSchema, SourceURL: spectrumGuideURL}
	}
	state := spectrumCatalogState{file: bundled, origin: "bundled"}
	data, err := os.ReadFile(strings.TrimSpace(cachePath))
	if err != nil {
		return state
	}
	var cached spectrumCatalogFile
	if json.Unmarshal(data, &cached) != nil || validateSpectrumCatalog(cached) != nil {
		return state
	}
	checkedAt := cached.CheckedAt
	refreshToken := cached.RefreshToken
	lastRefreshError := cached.LastRefreshError
	if spectrumCatalogTime(cached).After(spectrumCatalogTime(state.file)) {
		state = spectrumCatalogState{file: cached, origin: "cached"}
	}
	state.file.CheckedAt = checkedAt
	state.file.RefreshToken = refreshToken
	state.file.LastRefreshError = lastRefreshError
	return state
}

func parseBundledSpectrumCatalog(data []byte) (spectrumCatalogFile, error) {
	result := spectrumCatalogFile{SchemaVersion: spectrumCatalogSchema, SourceURL: spectrumGuideURL}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			key, value, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(line, "#")), "=")
			if ok && strings.EqualFold(strings.TrimSpace(key), "capturedAt") {
				result.CapturedAt = strings.TrimSpace(value)
			}
			continue
		}
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) != 3 {
			return spectrumCatalogFile{}, fmt.Errorf("invalid bundled Spectrum catalog row %q", scanner.Text())
		}
		result.Entries = append(result.Entries, spectrumCatalogEntry{
			StationID: strings.TrimSpace(parts[0]), Name: strings.TrimSpace(parts[1]), Category: strings.TrimSpace(parts[2]),
		})
	}
	if err := scanner.Err(); err != nil {
		return spectrumCatalogFile{}, fmt.Errorf("reading bundled Spectrum catalog: %w", err)
	}
	normalizeSpectrumEntries(&result.Entries)
	result.ContentHash = spectrumContentHash(result.Entries)
	if err := validateSpectrumCatalog(result); err != nil {
		return spectrumCatalogFile{}, err
	}
	return result, nil
}

func parseSpectrumAPI(data []byte) (spectrumCatalogFile, error) {
	var payload spectrumAPIResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		return spectrumCatalogFile{}, fmt.Errorf("decoding Spectrum national catalog: %w", err)
	}
	result := spectrumCatalogFile{SchemaVersion: spectrumCatalogSchema, SourceURL: spectrumGuideURL}
	for _, channel := range payload.CluInfo.CLU.Channels {
		stationID := rawSpectrumStationID(channel.TMSID)
		name := cleanText(channel.ChannelName)
		category := firstSpectrumGenre(channel.Genres)
		if stationID == "" || name == "" || category == "" {
			continue
		}
		result.Entries = append(result.Entries, spectrumCatalogEntry{
			StationID: stationID, Name: name, Category: category, Packages: cleanSpectrumValues(channel.Packages),
		})
	}
	normalizeSpectrumEntries(&result.Entries)
	result.ContentHash = spectrumContentHash(result.Entries)
	if err := validateSpectrumCatalog(result); err != nil {
		return spectrumCatalogFile{}, err
	}
	return result, nil
}

func rawSpectrumStationID(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return ""
}

func firstSpectrumGenre(values []string) string {
	var selected string
	var selectedMaster string
	for _, value := range cleanSpectrumValues(values) {
		master, ok := mapSpectrumCategory(value)
		if !ok {
			continue
		}
		if selectedMaster != "" && master != selectedMaster {
			return ""
		}
		selected = value
		selectedMaster = master
	}
	return selected
}

func mapSpectrumCategory(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "broadcasters":
		return channelcategory.LocalPublic, true
	case "entertainment", "shopping":
		return channelcategory.Entertainment, true
	case "inspiration":
		return channelcategory.Faith, true
	case "international", "latino":
		return channelcategory.International, true
	case "kids", "kids & family":
		return channelcategory.KidsFamily, true
	case "movies", "premiums":
		return channelcategory.Movies, true
	case "music":
		return channelcategory.Music, true
	case "news & info":
		return channelcategory.NewsWeather, true
	case "sports":
		return channelcategory.Sports, true
	default:
		return "", false
	}
}

func spectrumCatalogEntries(records []spectrumCatalogEntry) []catalogEntry {
	entries := make([]catalogEntry, 0, len(records))
	for _, record := range records {
		category, ok := mapSpectrumCategory(record.Category)
		if !ok {
			continue
		}
		entries = append(entries, catalogEntry{
			StationIDs: []string{record.StationID}, Name: record.Name, Category: category, RawCategory: record.Category,
			CategoryMethod: "maintained Spectrum national genre mapping",
		})
	}
	return entries
}

func normalizeSpectrumEntries(entries *[]spectrumCatalogEntry) {
	byID := make(map[string]spectrumCatalogEntry)
	conflicts := make(map[string]bool)
	for _, entry := range *entries {
		entry.StationID = strings.TrimSpace(entry.StationID)
		entry.Name = cleanText(entry.Name)
		entry.Category = strings.TrimSpace(entry.Category)
		entry.Packages = cleanSpectrumValues(entry.Packages)
		if entry.StationID == "" || entry.Name == "" || entry.Category == "" {
			continue
		}
		if current, ok := byID[entry.StationID]; ok && (current.Name != entry.Name || current.Category != entry.Category) {
			conflicts[entry.StationID] = true
			continue
		}
		byID[entry.StationID] = entry
	}
	result := make([]spectrumCatalogEntry, 0, len(byID))
	for stationID, entry := range byID {
		if !conflicts[stationID] {
			result = append(result, entry)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StationID < result[j].StationID })
	*entries = result
}

func cleanSpectrumValues(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = cleanText(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func validateSpectrumCatalog(file spectrumCatalogFile) error {
	if file.SchemaVersion != spectrumCatalogSchema {
		return fmt.Errorf("unsupported Spectrum catalog schema %d", file.SchemaVersion)
	}
	if file.SourceURL != spectrumGuideURL {
		return errors.New("Spectrum catalog source URL is not the official channel lineup")
	}
	if len(file.Entries) < minimumSpectrumEntries {
		return fmt.Errorf("Spectrum catalog has only %d usable channels", len(file.Entries))
	}
	for _, entry := range file.Entries {
		if entry.StationID == "" || entry.Name == "" {
			return errors.New("Spectrum catalog contains an incomplete channel")
		}
		for _, character := range entry.StationID {
			if character < '0' || character > '9' {
				return fmt.Errorf("Spectrum catalog contains invalid station ID %q", entry.StationID)
			}
		}
		if _, ok := mapSpectrumCategory(entry.Category); !ok {
			return fmt.Errorf("Spectrum catalog contains unsupported category %q", entry.Category)
		}
	}
	if file.ContentHash != spectrumContentHash(file.Entries) {
		return errors.New("Spectrum catalog content hash does not match")
	}
	return nil
}

func spectrumContentHash(entries []spectrumCatalogEntry) string {
	data, _ := json.Marshal(entries)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func spectrumCatalogTime(file spectrumCatalogFile) time.Time {
	for _, value := range []string{file.RefreshedAt, file.CapturedAt} {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func (s *Service) fetchSpectrumCatalog(ctx context.Context) providerResult {
	s.spectrumMu.Lock()
	defer s.spectrumMu.Unlock()

	now := s.now().UTC()
	state := s.spectrum
	force := s.spectrumForceToken != "" && s.spectrumForceToken != state.file.RefreshToken
	checkedAt, _ := time.Parse(time.RFC3339, state.file.CheckedAt)
	if checkedAt.IsZero() {
		checkedAt = spectrumCatalogTime(state.file)
	}
	refreshDue := force || checkedAt.IsZero() || !now.Before(checkedAt.Add(spectrumRefreshInterval))
	if refreshDue {
		refreshed, err := s.retrieveSpectrumCatalog(ctx)
		state.file.CheckedAt = now.Format(time.RFC3339)
		state.file.RefreshToken = s.spectrumForceToken
		if err == nil {
			minimumSafeEntries := len(state.file.Entries) * 4 / 5
			if len(refreshed.Entries) < minimumSafeEntries {
				err = fmt.Errorf("Spectrum national catalog shrank from %d to %d channels; retaining the last-known-good catalog", len(state.file.Entries), len(refreshed.Entries))
			}
		}
		if err == nil {
			refreshed.CapturedAt = now.Format(time.RFC3339)
			refreshed.RefreshedAt = now.Format(time.RFC3339)
			refreshed.CheckedAt = now.Format(time.RFC3339)
			refreshed.RefreshToken = s.spectrumForceToken
			state = spectrumCatalogState{file: refreshed, origin: "refreshed"}
		} else {
			state.file.LastRefreshError = err.Error()
		}
		if saveErr := writeSpectrumCatalog(s.spectrumCachePath, state.file); saveErr != nil {
			if state.file.LastRefreshError == "" {
				state.file.LastRefreshError = saveErr.Error()
			} else {
				state.file.LastRefreshError += "; " + saveErr.Error()
			}
		}
		s.spectrum = state
	}

	source := catalogSource{
		ID: "spectrum-official-lineup", Label: "Spectrum national channel catalog", URL: spectrumGuideURL,
		Method: "exact Gracenote station ID from Spectrum TMSID; national catalog does not assert local availability or channel number",
		Status: "complete", Entries: spectrumCatalogEntries(state.file.Entries),
	}
	message := fmt.Sprintf("Using %s Spectrum catalog with %d exact station-ID records", state.origin, len(source.Entries))
	if state.file.LastRefreshError != "" {
		message += "; live refresh was unavailable, so the last-known-good catalog was retained"
	}
	source.Message = message
	if len(source.Entries) == 0 {
		return sourceFailure(source, errors.New("no valid bundled or cached Spectrum catalog is available"))
	}
	return providerResult{source: source}
}

func (s *Service) retrieveSpectrumCatalog(ctx context.Context) (spectrumCatalogFile, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, spectrumCatalogURL, nil)
	if err != nil {
		return spectrumCatalogFile{}, fmt.Errorf("building Spectrum national catalog request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Referer", spectrumGuideURL)
	request.Header.Set("User-Agent", "Mozilla/5.0 (compatible; GraceNoteScraper provider enrichment)")
	response, err := s.httpClient.Do(request)
	if err != nil {
		return spectrumCatalogFile{}, fmt.Errorf("Spectrum national catalog request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return spectrumCatalogFile{}, fmt.Errorf("Spectrum national catalog returned %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxJSONBytes+1))
	if err != nil {
		return spectrumCatalogFile{}, fmt.Errorf("reading Spectrum national catalog: %w", err)
	}
	if len(data) > maxJSONBytes {
		return spectrumCatalogFile{}, errors.New("Spectrum national catalog exceeds the response limit")
	}
	return parseSpectrumAPI(data)
}

func writeSpectrumCatalog(path string, file spectrumCatalogFile) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	file.SchemaVersion = spectrumCatalogSchema
	file.SourceURL = spectrumGuideURL
	file.ContentHash = spectrumContentHash(file.Entries)
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding Spectrum catalog cache: %w", err)
	}
	data = append(data, '\n')
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return fmt.Errorf("creating Spectrum catalog cache directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".spectrum-catalog-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temporary Spectrum catalog cache: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("setting Spectrum catalog cache permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("writing Spectrum catalog cache: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("syncing Spectrum catalog cache: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("closing Spectrum catalog cache: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replacing Spectrum catalog cache: %w", err)
	}
	removeTemporary = false
	return nil
}
