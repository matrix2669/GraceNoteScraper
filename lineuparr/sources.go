package lineuparr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultIPTVOrgURL = "https://iptv-org.github.io/api/channels.json"
	catalogBaseURL    = "https://raw.githubusercontent.com/matrix2669/Dispatcharr-Lineuparr-Plugin/main/Lineuparr/"
	maxSourceBytes    = 32 << 20
)

type ServiceOptions struct {
	CacheDir           string
	HTTPClient         *http.Client
	CatalogURLs        []string
	UseDefaultCatalogs bool
	IPTVOrgURL         string
}

type remoteSource struct {
	ID      string
	Kind    string
	URL     string
	Label   string
	Data    []byte
	Status  string
	Message string
	Err     error
}

type catalogFile struct {
	Package    string                      `json:"package"`
	Source     string                      `json:"source"`
	Categories map[string][]catalogChannel `json:"categories"`
}

type catalogChannel struct {
	Name    string     `json:"name"`
	Number  any        `json:"number"`
	Aliases stringList `json:"aliases"`
	EPGIDs  stringList `json:"epg_ids"`
}

type iptvOrgChannel struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	AltNames   stringList `json:"alt_names"`
	Country    string     `json:"country"`
	Categories stringList `json:"categories"`
	Closed     any        `json:"closed"`
	ReplacedBy any        `json:"replaced_by"`
}

type stringList []string

func (s *stringList) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*s = nil
		return nil
	}
	var values []any
	if err := json.Unmarshal(data, &values); err == nil {
		result := make([]string, 0, len(values))
		for _, value := range values {
			switch typed := value.(type) {
			case string:
				if strings.TrimSpace(typed) != "" {
					result = append(result, typed)
				}
			case float64:
				result = append(result, fmt.Sprintf("%v", typed))
			}
		}
		*s = result
		return nil
	}
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		if strings.TrimSpace(single) == "" {
			*s = nil
		} else {
			*s = []string{single}
		}
		return nil
	}
	return errors.New("expected a string or array of strings")
}

func DefaultCatalogURLs(country, providerName string) []string {
	provider := strings.ToLower(providerName)
	var names []string
	switch strings.ToUpper(strings.TrimSpace(country)) {
	case "USA", "US":
		switch {
		case strings.Contains(provider, "verizon") || strings.Contains(provider, "fios"):
			names = append(names, "US_Verizon-FIOS-All-11743_lineup.json")
		case strings.Contains(provider, "directv"):
			names = append(names, "US_DirecTV-Premier_lineup.json")
		case strings.Contains(provider, "dish"):
			names = append(names, "US_DISH-Top250_lineup.json")
		}
		names = append(names, "US_Combined_lineup.json")
	case "GBR", "GB", "UK":
		names = append(names, "UK_Combined_lineup.json")
	case "CAN", "CA":
		names = append(names, "CA_Telus-Optik_lineup.json")
	case "AUS", "AU":
		names = append(names, "AU_Foxtel_lineup.json")
	case "ESP", "ES":
		names = append(names, "ES_Movistar_lineup.json")
	case "FRA", "FR":
		names = append(names, "FR_CanalPlus_lineup.json")
	case "NLD", "NL":
		names = append(names, "NL_ODIDO_lineup.json")
	}
	urls := make([]string, 0, len(names))
	for _, name := range names {
		urls = append(urls, catalogBaseURL+name)
	}
	return urls
}

func countryAlpha2(country string) string {
	switch strings.ToUpper(strings.TrimSpace(country)) {
	case "USA", "US":
		return "US"
	case "GBR", "GB", "UK":
		return "GB"
	case "CAN", "CA":
		return "CA"
	case "AUS", "AU":
		return "AU"
	case "ESP", "ES":
		return "ES"
	case "FRA", "FR":
		return "FR"
	case "NLD", "NL":
		return "NL"
	default:
		country = strings.ToUpper(strings.TrimSpace(country))
		if len(country) == 2 {
			return country
		}
		return ""
	}
}

func (s *Service) sourceURLs(lineup LineupContext) []remoteSource {
	catalogURLs := append([]string(nil), s.options.CatalogURLs...)
	if s.options.UseDefaultCatalogs {
		catalogURLs = append(catalogURLs, DefaultCatalogURLs(lineup.Country, lineup.ProviderName)...)
	}
	seen := make(map[string]bool)
	result := make([]remoteSource, 0, len(catalogURLs)+1)
	for _, rawURL := range catalogURLs {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" || seen[rawURL] {
			continue
		}
		seen[rawURL] = true
		result = append(result, remoteSource{
			ID:    "catalog-" + shortHash(rawURL),
			Kind:  "catalog",
			URL:   rawURL,
			Label: "Lineuparr catalog",
		})
	}
	if rawURL := strings.TrimSpace(s.options.IPTVOrgURL); rawURL != "" {
		result = append(result, remoteSource{
			ID:    "iptv-org",
			Kind:  "iptv-org",
			URL:   rawURL,
			Label: "iptv-org channel database",
		})
	}
	return result
}

func (s *Service) fetchSources(ctx context.Context, lineup LineupContext) []remoteSource {
	sources := s.sourceURLs(lineup)
	if len(sources) == 0 {
		return nil
	}
	type indexedSource struct {
		index  int
		source remoteSource
	}
	results := make(chan indexedSource, len(sources))
	for index, source := range sources {
		go func(index int, source remoteSource) {
			data, status, message, err := s.fetchSource(ctx, source.URL)
			source.Data = data
			source.Status = status
			source.Message = message
			source.Err = err
			results <- indexedSource{index: index, source: source}
		}(index, source)
	}
	ordered := make([]remoteSource, len(sources))
	for range sources {
		result := <-results
		ordered[result.index] = result.source
	}
	return ordered
}

func (s *Service) fetchSource(ctx context.Context, rawURL string) ([]byte, string, string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return nil, "error", "Source URL must use HTTP or HTTPS", errors.New("invalid source URL")
	}
	cachePath := filepath.Join(s.options.CacheDir, shortHash(rawURL)+".json")
	cached, cacheErr := os.ReadFile(cachePath)
	if cacheErr == nil && !json.Valid(cached) {
		cacheErr = errors.New("cached source is not valid JSON")
		cached = nil
	}
	if cacheErr == nil {
		if info, err := os.Stat(cachePath); err == nil && time.Since(info.ModTime()) < 24*time.Hour {
			return cached, "cached", "Using a source copy cached within the last 24 hours", nil
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err == nil {
		req.Header.Set("User-Agent", "GraceNoteScraper-Lineuparr-Builder/1")
		resp, requestErr := s.options.HTTPClient.Do(req)
		if requestErr == nil {
			defer resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				limited := io.LimitReader(resp.Body, maxSourceBytes+1)
				data, readErr := io.ReadAll(limited)
				if readErr == nil && len(data) <= maxSourceBytes && json.Valid(data) {
					if err := writeSourceCache(cachePath, data); err != nil {
						return data, "live", "Loaded live; local cache could not be updated", nil
					}
					return data, "live", "Loaded from the source", nil
				}
				if readErr != nil {
					err = readErr
				} else if !json.Valid(data) {
					err = errors.New("source did not return valid JSON")
				} else {
					err = fmt.Errorf("source exceeds %d bytes", maxSourceBytes)
				}
			} else {
				err = fmt.Errorf("source returned HTTP %d", resp.StatusCode)
			}
		} else {
			err = errors.New("source request failed")
		}
	}
	if cacheErr == nil {
		return cached, "stale-cache", "Live refresh failed; using the last cached copy", nil
	}
	if err == nil {
		err = errors.New("source fetch failed")
	}
	return nil, "error", "The source could not be loaded", err
}

func publicSourceURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	publicURL := parsed.String()
	if !strings.HasPrefix(publicURL, catalogBaseURL) && publicURL != DefaultIPTVOrgURL {
		parsed.Path = ""
		parsed.RawPath = ""
		return parsed.String()
	}
	return publicURL
}

func writeSourceCache(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".lineuparr-source-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	return nil
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:12])
}

func nonEmptyJSONValue(value any) bool {
	if value == nil {
		return false
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) != ""
	case bool:
		return typed
	default:
		return true
	}
}
