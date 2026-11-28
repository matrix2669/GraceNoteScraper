package lineuparr

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type StateStore struct {
	mu    sync.RWMutex
	path  string
	state State
}

func LoadStateStore(path string) (*StateStore, error) {
	store := &StateStore{
		path: path,
		state: State{
			Version:        CurrentStateVersion,
			Channels:       make(map[string]ChannelOverride),
			MatchDecisions: make(map[string]MatchDecision),
		},
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return store, nil
		}
		return store, fmt.Errorf("reading Lineuparr builder state: %w", err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return store, fmt.Errorf("decoding Lineuparr builder state: %w", err)
	}
	if state.Version != 1 && state.Version != CurrentStateVersion {
		return store, fmt.Errorf("unsupported Lineuparr builder state version %d", state.Version)
	}
	state.Version = CurrentStateVersion
	if state.Channels == nil {
		state.Channels = make(map[string]ChannelOverride)
	}
	if state.MatchDecisions == nil {
		state.MatchDecisions = make(map[string]MatchDecision)
	}
	store.state = state
	return store, nil
}

func (s *StateStore) Snapshot(fingerprint string) map[string]ChannelOverride {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state.SourceFingerprint != fingerprint {
		return map[string]ChannelOverride{}
	}
	result := make(map[string]ChannelOverride, len(s.state.Channels))
	for id, override := range s.state.Channels {
		if override.Included != nil {
			included := *override.Included
			override.Included = &included
		}
		override.SuppressedAliases = append([]string(nil), override.SuppressedAliases...)
		result[id] = override
	}
	return result
}

func (s *StateStore) MatchDecisionSnapshot(fingerprint string) map[string]MatchDecision {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.state.SourceFingerprint != fingerprint {
		return map[string]MatchDecision{}
	}
	result := make(map[string]MatchDecision, len(s.state.MatchDecisions))
	for key, decision := range s.state.MatchDecisions {
		result[key] = decision
	}
	return result
}

func (s *StateStore) Update(fingerprint, channelID string, update ChannelUpdate) error {
	if fingerprint == "" || channelID == "" {
		return errors.New("source fingerprint and channel ID are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureSourceLocked(fingerprint)
	override := s.state.Channels[channelID]
	if update.Included != nil {
		included := *update.Included
		override.Included = &included
	}
	if update.Category != nil {
		override.Category = *update.Category
	}
	s.state.Channels[channelID] = override
	return s.saveLocked()
}

func (s *StateStore) SetIncluded(fingerprint string, channelIDs []string, included bool) error {
	if fingerprint == "" {
		return errors.New("source fingerprint is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureSourceLocked(fingerprint)
	for _, channelID := range channelIDs {
		if channelID == "" {
			continue
		}
		override := s.state.Channels[channelID]
		value := included
		override.Included = &value
		s.state.Channels[channelID] = override
	}
	return s.saveLocked()
}

func (s *StateStore) SetCategory(fingerprint string, channelIDs []string, category string) error {
	if fingerprint == "" {
		return errors.New("source fingerprint is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureSourceLocked(fingerprint)
	for _, channelID := range channelIDs {
		if channelID == "" {
			continue
		}
		override := s.state.Channels[channelID]
		override.Category = category
		s.state.Channels[channelID] = override
	}
	return s.saveLocked()
}

func (s *StateStore) RestoreAll(fingerprint string) error {
	if fingerprint == "" {
		return errors.New("source fingerprint is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureSourceLocked(fingerprint)
	for channelID, override := range s.state.Channels {
		override.Included = nil
		if override.Category == "" && len(override.SuppressedAliases) == 0 {
			delete(s.state.Channels, channelID)
			continue
		}
		s.state.Channels[channelID] = override
	}
	return s.saveLocked()
}

func (s *StateStore) SetAliasSuppressed(fingerprint, channelID, alias string, suppressed bool) error {
	if fingerprint == "" || channelID == "" || alias == "" {
		return errors.New("source fingerprint, channel ID, and alias are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureSourceLocked(fingerprint)
	override := s.state.Channels[channelID]
	key := strings.ToLower(alias)
	filtered := make([]string, 0, len(override.SuppressedAliases)+1)
	found := false
	for _, existing := range override.SuppressedAliases {
		if strings.ToLower(existing) == key {
			found = true
			if !suppressed {
				continue
			}
		}
		filtered = append(filtered, existing)
	}
	if suppressed && !found {
		filtered = append(filtered, alias)
	}
	override.SuppressedAliases = filtered
	if override.Included == nil && override.Category == "" && len(override.SuppressedAliases) == 0 {
		delete(s.state.Channels, channelID)
	} else {
		s.state.Channels[channelID] = override
	}
	return s.saveLocked()
}

func (s *StateStore) SetMatchDecision(fingerprint string, decision MatchDecision) error {
	return s.SetMatchDecisions(fingerprint, []MatchDecision{decision})
}

func (s *StateStore) SetMatchDecisions(fingerprint string, decisions []MatchDecision) error {
	if fingerprint == "" || len(decisions) == 0 {
		return errors.New("source fingerprint and match decisions are required")
	}
	for _, decision := range decisions {
		if decision.Key == "" {
			return errors.New("match decision key is required")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureSourceLocked(fingerprint)
	for _, decision := range decisions {
		s.state.MatchDecisions[decision.Key] = decision
		if decision.Decision == "confirmed" && decision.ChannelID != "" && decision.StreamName != "" {
			override := s.state.Channels[decision.ChannelID]
			filtered := override.SuppressedAliases[:0]
			for _, alias := range override.SuppressedAliases {
				if !strings.EqualFold(alias, decision.StreamName) {
					filtered = append(filtered, alias)
				}
			}
			override.SuppressedAliases = filtered
			if override.Included != nil || override.Category != "" || len(override.SuppressedAliases) > 0 {
				s.state.Channels[decision.ChannelID] = override
			} else {
				delete(s.state.Channels, decision.ChannelID)
			}
		}
	}
	return s.saveLocked()
}

func (s *StateStore) ClearMatchDecision(fingerprint, key string) error {
	return s.ClearMatchDecisions(fingerprint, []string{key})
}

func (s *StateStore) ClearMatchDecisions(fingerprint string, keys []string) error {
	if fingerprint == "" || len(keys) == 0 {
		return errors.New("source fingerprint and match decision keys are required")
	}
	for _, key := range keys {
		if key == "" {
			return errors.New("match decision key is required")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureSourceLocked(fingerprint)
	for _, key := range keys {
		delete(s.state.MatchDecisions, key)
	}
	return s.saveLocked()
}

func (s *StateStore) ensureSourceLocked(fingerprint string) {
	if s.state.SourceFingerprint == fingerprint {
		return
	}
	s.state = State{
		Version:           CurrentStateVersion,
		SourceFingerprint: fingerprint,
		Channels:          make(map[string]ChannelOverride),
		MatchDecisions:    make(map[string]MatchDecision),
	}
}

func (s *StateStore) saveLocked() error {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding Lineuparr builder state: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating Lineuparr state directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".lineuparr-state-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temporary Lineuparr state: %w", err)
	}
	tmpName := tmp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("securing temporary Lineuparr state: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing Lineuparr state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("syncing Lineuparr state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temporary Lineuparr state: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replacing Lineuparr state: %w", err)
	}
	removeTemp = false
	if err := os.Chmod(s.path, 0600); err != nil {
		return fmt.Errorf("securing Lineuparr state: %w", err)
	}
	return nil
}
