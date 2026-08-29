package geocode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const DefaultNominatimURL = "https://nominatim.openstreetmap.org"

type AddressResult struct {
	ID               string `json:"id"`
	FormattedAddress string `json:"formattedAddress"`
	StreetAddress    string `json:"streetAddress"`
	City             string `json:"city"`
	State            string `json:"state"`
	PostalCode       string `json:"postalCode"`
	CountryCode      string `json:"countryCode"`
}

type NominatimClient struct {
	httpClient *http.Client
	baseURL    string
	userAgent  string

	requestMu   sync.Mutex
	lastRequest time.Time
}

func NewNominatimClient(httpClient *http.Client, baseURL string) *NominatimClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &NominatimClient{
		httpClient: httpClient,
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		userAgent:  "GraceNoteScraper/1.0 (+https://github.com/daniel-widrick/GraceNoteScraper)",
	}
}

func (c *NominatimClient) Search(ctx context.Context, query, postalCode, countryCode string) ([]AddressResult, error) {
	query = strings.TrimSpace(query)
	postalCode = strings.TrimSpace(postalCode)
	countryCode = strings.ToLower(strings.TrimSpace(countryCode))
	if query == "" || postalCode == "" {
		return nil, errors.New("street address and lineup postal code are required")
	}
	if len(query) > 300 || len(postalCode) > 20 || len(countryCode) > 2 {
		return nil, errors.New("address search is too long")
	}
	if c.baseURL == "" {
		return nil, errors.New("address search is disabled")
	}

	endpoint, err := url.JoinPath(c.baseURL, "search")
	if err != nil {
		return nil, fmt.Errorf("building Nominatim search URL: %w", err)
	}
	parameters := url.Values{
		"q":              {query + ", " + postalCode},
		"format":         {"jsonv2"},
		"addressdetails": {"1"},
		"layer":          {"address"},
		"limit":          {"5"},
	}
	if countryCode != "" {
		parameters.Set("countrycodes", countryCode)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+parameters.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("building Nominatim search request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("User-Agent", c.userAgent)

	response, err := c.doRateLimited(req)
	if err != nil {
		return nil, fmt.Errorf("searching addresses: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("address search returned %s", response.Status)
	}

	var raw []struct {
		PlaceID     int64  `json:"place_id"`
		OSMType     string `json:"osm_type"`
		OSMID       int64  `json:"osm_id"`
		DisplayName string `json:"display_name"`
		Address     struct {
			HouseNumber  string `json:"house_number"`
			Road         string `json:"road"`
			Pedestrian   string `json:"pedestrian"`
			City         string `json:"city"`
			Town         string `json:"town"`
			Village      string `json:"village"`
			Hamlet       string `json:"hamlet"`
			Municipality string `json:"municipality"`
			CityDistrict string `json:"city_district"`
			County       string `json:"county"`
			State        string `json:"state"`
			ISOState     string `json:"ISO3166-2-lvl4"`
			PostalCode   string `json:"postcode"`
			CountryCode  string `json:"country_code"`
		} `json:"address"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding address search: %w", err)
	}

	results := make([]AddressResult, 0, len(raw))
	for _, item := range raw {
		road := strings.TrimSpace(item.Address.Road)
		if road == "" {
			road = strings.TrimSpace(item.Address.Pedestrian)
		}
		if strings.TrimSpace(item.Address.HouseNumber) == "" || road == "" || strings.TrimSpace(item.DisplayName) == "" {
			continue
		}
		if !postalCodesMatch(item.Address.PostalCode, postalCode, countryCode) {
			continue
		}
		identifier := fmt.Sprintf("place:%d", item.PlaceID)
		if item.OSMType != "" && item.OSMID != 0 {
			identifier = fmt.Sprintf("%s:%d", item.OSMType, item.OSMID)
		}
		city := firstNonEmpty(
			item.Address.City, item.Address.Town, item.Address.Village, item.Address.Hamlet,
			item.Address.Municipality, item.Address.CityDistrict, item.Address.County,
		)
		state := strings.TrimSpace(item.Address.State)
		if parts := strings.Split(strings.TrimSpace(item.Address.ISOState), "-"); len(parts) == 2 && len(parts[1]) == 2 {
			state = strings.ToUpper(parts[1])
		}
		results = append(results, AddressResult{
			ID: identifier, FormattedAddress: strings.TrimSpace(item.DisplayName),
			StreetAddress: strings.TrimSpace(strings.TrimSpace(item.Address.HouseNumber) + " " + road),
			City:          city, State: state, PostalCode: strings.TrimSpace(item.Address.PostalCode),
			CountryCode: strings.ToUpper(strings.TrimSpace(item.Address.CountryCode)),
		})
	}
	return results, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func (c *NominatimClient) doRateLimited(request *http.Request) (*http.Response, error) {
	c.requestMu.Lock()
	defer c.requestMu.Unlock()
	if wait := time.Second - time.Since(c.lastRequest); !c.lastRequest.IsZero() && wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-request.Context().Done():
			return nil, request.Context().Err()
		case <-timer.C:
		}
	}
	c.lastRequest = time.Now()
	return c.httpClient.Do(request)
}

func postalCodesMatch(selected, active, countryCode string) bool {
	normalize := func(value string) string {
		return strings.Map(func(r rune) rune {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				return r
			}
			return -1
		}, strings.ToUpper(strings.TrimSpace(value)))
	}
	selected = normalize(selected)
	active = normalize(active)
	if selected == "" || active == "" {
		return false
	}
	if selected == active {
		return true
	}
	return countryCode == "us" && len(active) == 5 && strings.HasPrefix(selected, active)
}
