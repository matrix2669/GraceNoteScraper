package lineuparr

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// StabilizePlacementKeys keeps existing keys for unique placements. A provider
// row ID observed at multiple positions is never used to apply an old decision
// to any of those positions, even if later partial data shows just one.
func (s *Service) StabilizePlacementKeys(fingerprint string, inputs []InputChannel) error {
	distinct := make(map[string]map[string]bool)
	identity := func(in InputChannel) string {
		raw, _ := json.Marshal([]string{strings.TrimSpace(in.StationID), strings.TrimSpace(in.Number)})
		return string(raw)
	}
	for _, in := range inputs {
		raw := strings.TrimSpace(in.PlacementID)
		if raw == "" || strings.TrimSpace(in.StationID) == "" || strings.TrimSpace(in.Number) == "" {
			continue
		}
		if distinct[raw] == nil {
			distinct[raw] = make(map[string]bool)
		}
		distinct[raw][identity(in)] = true
	}
	observed := make(map[string]bool)
	for raw, ids := range distinct {
		if len(ids) > 1 {
			observed[raw] = true
		}
	}
	blocked, err := s.store.rememberPlacementCollisions(fingerprint, observed)
	if err != nil {
		return err
	}
	for i := range inputs {
		in := &inputs[i]
		if !blocked[strings.TrimSpace(in.PlacementID)] {
			continue
		}
		raw, _ := json.Marshal([]string{fingerprint, identity(*in)})
		digest := sha256.Sum256(raw)
		in.Key = "position:v1:" + hex.EncodeToString(digest[:])
		in.AmbiguousLegacyPlacement = true
	}
	return nil
}

func (s *StateStore) rememberPlacementCollisions(fingerprint string, observed map[string]bool) (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	collection := s.state.PlacementCollisions
	saved := collection[fingerprint]
	next := make(map[string]bool, len(saved)+len(observed))
	for key, value := range saved {
		next[key] = value
	}
	changed := false
	for key := range observed {
		if !next[key] {
			next[key] = true
			changed = true
		}
	}
	if !changed {
		return next, nil
	}
	update := make(map[string]map[string]bool, len(collection)+1)
	for fp, values := range collection {
		update[fp] = values
	}
	update[fingerprint] = next
	s.state.PlacementCollisions = update
	if err := s.saveLocked(); err != nil {
		s.state.PlacementCollisions = collection
		return nil, err
	}
	return next, nil
}
