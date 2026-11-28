package lineupindex

import "sync"

// Every index mutation already holds mu. Advancing on write unlock also fences
// changes in retained timezone and attributed source evidence, not just scans.
type revisionMutex struct {
	sync.RWMutex
	revision uint64
}

func (m *revisionMutex) Unlock() { m.revision++; m.RWMutex.Unlock() }
func (s *Service) EvidenceRevision() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mu.revision
}
func (s *Service) WhileEvidenceRevision(revision uint64, action func() error) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.mu.revision != revision {
		return false, nil
	}
	return true, action()
}
