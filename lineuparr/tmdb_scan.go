package lineuparr

import "time"

type TMDBCategoryScan struct {
	Revision              string                        `json:"revision"`
	Timezone              string                        `json:"timezone,omitempty"`
	ScannedAt             time.Time                     `json:"scannedAt"`
	Categories            map[string]AttributedCategory `json:"categories"`
	IndependentCategories map[string][]string           `json:"independentCategories,omitempty"`
}

func (s *Service) TMDBCategoryScan(fingerprint string) TMDBCategoryScan {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	if s.store.state.SourceFingerprint != fingerprint {
		return TMDBCategoryScan{}
	}
	result := s.store.state.TMDBCategoryScan
	result.Categories = map[string]AttributedCategory{}
	for k, v := range s.store.state.TMDBCategoryScan.Categories {
		result.Categories[k] = v
	}
	result.IndependentCategories = map[string][]string{}
	for k, values := range s.store.state.TMDBCategoryScan.IndependentCategories {
		result.IndependentCategories[k] = append([]string(nil), values...)
	}
	return result
}

func (s *Service) SaveTMDBCategoryScan(fingerprint string, scan TMDBCategoryScan) error {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	previous := s.store.state
	s.store.ensureSourceLocked(fingerprint)
	copy := scan
	copy.Categories = map[string]AttributedCategory{}
	for k, v := range scan.Categories {
		copy.Categories[k] = v
	}
	copy.IndependentCategories = map[string][]string{}
	for k, values := range scan.IndependentCategories {
		copy.IndependentCategories[k] = append([]string(nil), values...)
	}
	s.store.state.TMDBCategoryScan = copy
	if err := s.store.saveLocked(); err != nil {
		s.store.state = previous
		return err
	}
	return nil
}
