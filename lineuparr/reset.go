package lineuparr

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

func (s *Service) ResetGeneration() uint64 {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	return s.store.state.ResetGeneration
}

func (s *Service) ResetRecoveryMatchesSource(fingerprint string) bool {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	return s.store.state.SourceFingerprint == fingerprint
}

var resetBackupName = regexp.MustCompile(`^builder-reset-[a-f0-9]{64}\.json$`)

// ResetBuilder changes only its own explicit choices. The full previous state
// is recoverable; shared guide/TMDB/source/connection stores are never opened.
func (s *Service) ResetBuilder(fingerprint, token, restore string) (string, error) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if token != s.store.reviewTokenLocked() {
		return "", ErrStaleReview
	}
	previous := s.store.state
	next := previous
	if restore != "" {
		if !resetBackupName.MatchString(restore) {
			return "", errors.New("invalid builder backup")
		}
		data, err := os.ReadFile(filepath.Join(filepath.Dir(s.store.path), "builder-reset-backups", restore))
		if err != nil {
			return "", errors.New("builder backup unavailable")
		}
		next = State{}
		if json.Unmarshal(data, &next) != nil || next.Version != CurrentStateVersion || next.SourceFingerprint != fingerprint {
			return "", errors.New("backup does not belong to this lineup")
		}
		// Category results belong to their source. Shared TMDB lookup data
		// is separate and never opened here.
		next.TMDBCategoryScan = TMDBCategoryScan{}
		next.Workflow.TMDBDisposition = ""
		next.Workflow.TMDBCompletedAt = time.Time{}
		if previous.SourceFingerprint == fingerprint {
			next.TMDBCategoryScan = previous.TMDBCategoryScan
			next.Workflow.TMDBDisposition = previous.Workflow.TMDBDisposition
			next.Workflow.TMDBCompletedAt = previous.Workflow.TMDBCompletedAt
		}
	} else {
		if previous.SourceFingerprint != fingerprint {
			// Old lineup-derived category results are audit-only in the backup.
			// Shared downloaded guide and TMDB lookup files are untouched.
			next.TMDBCategoryScan = TMDBCategoryScan{}
			next.Workflow = WorkflowProgress{}
		}
		next.Channels = map[string]ChannelOverride{}
		next.MatchDecisions = map[string]MatchDecision{}
		next.Workflow.CustomizationSignature = ""
		next.Workflow.CustomizationCompletedAt = time.Time{}
	}
	next.SourceFingerprint = fingerprint
	next.ResetGeneration = previous.ResetGeneration + 1
	backup := "builder-reset-" + token + ".json"
	dir := filepath.Join(filepath.Dir(s.store.path), "builder-reset-backups")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create builder backup: %w", err)
	}
	data, err := json.MarshalIndent(previous, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	path := filepath.Join(dir, backup)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		old, e := os.ReadFile(path)
		if e != nil || string(old) != string(data) {
			return "", errors.New("builder backup conflict")
		}
	} else if err != nil {
		return "", fmt.Errorf("create builder backup: %w", err)
	} else {
		_, err = file.Write(data)
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			_ = os.Remove(path)
			return "", fmt.Errorf("save builder backup: %w", err)
		}
	}
	s.store.state = next
	if err := s.store.saveLocked(); err != nil {
		s.store.state = previous
		return "", err
	}
	return backup, nil
}

// ResetRecovery exposes only an opaque identifier and time, never state contents,
// filesystem paths or credentials. Recovery remains discoverable after restart.
type ResetRecovery struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
}

func (s *Service) LatestResetRecovery(fingerprint string) *ResetRecovery {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	dir := filepath.Join(filepath.Dir(s.store.path), "builder-reset-backups")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var found []ResetRecovery
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !resetBackupName.MatchString(entry.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var state State
		if json.Unmarshal(data, &state) != nil || state.Version != CurrentStateVersion || state.SourceFingerprint != fingerprint {
			continue
		}
		info, err := entry.Info()
		if err == nil {
			found = append(found, ResetRecovery{entry.Name(), info.ModTime()})
		}
	}
	if len(found) == 0 {
		return nil
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].CreatedAt.Equal(found[j].CreatedAt) {
			return found[i].ID < found[j].ID
		}
		return found[i].CreatedAt.After(found[j].CreatedAt)
	})
	return &found[0]
}
