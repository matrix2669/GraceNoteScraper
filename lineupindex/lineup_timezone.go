package lineupindex

import (
	"github.com/daniel-widrick/GraceNoteScraper/web"
	"time"
)

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
