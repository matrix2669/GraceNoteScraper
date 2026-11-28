package lineupindex

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/web"
)

// providersWithResponseTimezone copies the postal-code timezone onto each
// provider when Gracenote returns it only on the response envelope. The public
// provider service currently leaves Provider.Timezone empty while supplying
// standard and daylight offsets at the top level.
func providersWithResponseTimezone(response *web.ProviderResponse) []web.Provider {
	if response == nil {
		return nil
	}
	providers := append([]web.Provider(nil), response.Providers...)
	_, timezone, err := providerLocation(providers, response)
	if err != nil || timezone == "" {
		return providers
	}
	for i := range providers {
		if strings.TrimSpace(providers[i].Timezone) == "" {
			providers[i].Timezone = timezone
		}
	}
	return providers
}

func (s *Service) LineupTimezone(country, postal, lineup, device string) *time.Location {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.index.Lineups {
		if r.Country == country && r.PostalCode == postal && r.LineupID == lineup && r.Device == device && r.Timezone != "" {
			loc, _, err := providerLocation([]web.Provider{{Timezone: r.Timezone}}, &web.ProviderResponse{})
			if err == nil {
				return loc
			}
		}
	}
	return nil
}

// ResolveLineupTimezone returns a retained lineup timezone when possible. For
// indexes created before response-level timezones were persisted, it performs
// one lightweight provider-discovery lookup and repairs matching retained
// lineup records. It never downloads a guide grid.
func (s *Service) ResolveLineupTimezone(ctx context.Context, country, postal, lineup, device, language string) (*time.Location, error) {
	if location := s.LineupTimezone(country, postal, lineup, device); location != nil {
		return location, nil
	}
	response, err := s.providers.FindProviders(ctx, country, postal, language)
	if err != nil {
		return nil, fmt.Errorf("refreshing lineup timezone: %w", err)
	}
	providers := providersWithResponseTimezone(response)
	var matched *web.Provider
	for i := range providers {
		provider := &providers[i]
		if strings.EqualFold(strings.TrimSpace(provider.LineupID), strings.TrimSpace(lineup)) &&
			strings.EqualFold(strings.TrimSpace(provider.Device), strings.TrimSpace(device)) {
			matched = provider
			break
		}
	}
	if matched == nil {
		return nil, fmt.Errorf("selected lineup was not returned for postal code %s", postal)
	}
	location, timezone, err := providerLocation([]web.Provider{*matched}, response)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for _, record := range s.index.Lineups {
		if strings.EqualFold(strings.TrimSpace(record.Country), strings.TrimSpace(country)) &&
			strings.EqualFold(strings.TrimSpace(record.PostalCode), strings.TrimSpace(postal)) &&
			strings.EqualFold(strings.TrimSpace(record.LineupID), strings.TrimSpace(lineup)) &&
			strings.EqualFold(strings.TrimSpace(record.Device), strings.TrimSpace(device)) &&
			record.Timezone != timezone {
			record.Timezone = timezone
			changed = true
		}
	}
	if changed {
		s.index.UpdatedAt = s.now().UTC().Format(time.RFC3339)
		if err := writeIndex(s.path, s.index); err != nil {
			return nil, fmt.Errorf("saving lineup timezone: %w", err)
		}
	}
	return location, nil
}
