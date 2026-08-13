package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/guide"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

// guideCacheWire extends the on-disk guide cache with the Gracenote source
// that produced it. The in-memory guideCache type stays unchanged so the
// existing scrape/cache flow does not need additional plumbing.
type guideCacheWire struct {
	SavedAt time.Time       `json:"saved_at"`
	Source  web.GuideSource `json:"source"`
	Guide   guide.TVGuide   `json:"guide"`
}

func (c guideCache) MarshalJSON() ([]byte, error) {
	return json.Marshal(guideCacheWire{
		SavedAt: c.SavedAt,
		Source:  web.CurrentGuideSource(),
		Guide:   c.Guide,
	})
}

func (c *guideCache) UnmarshalJSON(data []byte) error {
	var wire guideCacheWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}

	expected := web.CurrentGuideSource()
	if wire.Source == (web.GuideSource{}) {
		return fmt.Errorf("guide cache has no Gracenote source fingerprint")
	}
	if wire.Source != expected {
		return fmt.Errorf(
			"guide cache source mismatch: cached lineup=%s zip=%s, current lineup=%s zip=%s",
			wire.Source.LineupID, wire.Source.ZipCode, expected.LineupID, expected.ZipCode,
		)
	}

	c.SavedAt = wire.SavedAt
	c.Guide = wire.Guide
	return nil
}
