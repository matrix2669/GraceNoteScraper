package lineuparr

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const SupplementalOrigin = "supplemental-v1"

// SupplementalDecision has no score/nameScore. Snapshot and operation identify
// admission, while the key preserves the exact physical and target identity.
type SupplementalDecision struct {
	Origin                 string    `json:"origin"`
	Key                    string    `json:"key"`
	GroupKey               string    `json:"groupKey"`
	Decision               string    `json:"decision"`
	SourceFingerprint      string    `json:"sourceFingerprint"`
	DispatcharrFingerprint string    `json:"dispatcharrFingerprint"`
	ChannelID              string    `json:"channelId"`
	StationID              string    `json:"stationId"`
	PlacementID            string    `json:"placementId"`
	ChannelNumber          string    `json:"channelNumber"`
	ChannelName            string    `json:"channelName"`
	RawCallSign            string    `json:"rawCallsign"`
	StreamID               int64     `json:"streamId"`
	M3UAccountID           int64     `json:"m3uAccountId"`
	StreamName             string    `json:"streamName"`
	StreamFingerprint      string    `json:"streamFingerprint"`
	RetrievalVersion       string    `json:"retrievalVersion"`
	RetrievalRank          int       `json:"retrievalRank"`
	RetrievalReason        string    `json:"retrievalReason"`
	EvidenceRefs           []string  `json:"evidenceRefs"`
	AdmittedSnapshot       string    `json:"admittedSnapshot"`
	OperationID            string    `json:"operationId"`
	UpdatedAt              time.Time `json:"updatedAt"`
}

func SupplementalKey(d SupplementalDecision) string {
	data, _ := json.Marshal([]any{SupplementalOrigin, d.SourceFingerprint, d.DispatcharrFingerprint, d.ChannelID, d.StationID, d.PlacementID, d.RawCallSign, d.M3UAccountID, d.StreamID, d.StreamName, d.StreamFingerprint})
	return "supp:" + fmt.Sprintf("%x", sha256.Sum256(data))
}
func cloneSupplementalDecisions(in map[string]SupplementalDecision) map[string]SupplementalDecision {
	out := make(map[string]SupplementalDecision, len(in))
	for k, d := range in {
		d.EvidenceRefs = append([]string(nil), d.EvidenceRefs...)
		out[k] = d
	}
	return out
}
func (s *Service) SupplementalDecisions(source string) map[string]SupplementalDecision {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	if s.store.state.SourceFingerprint != source {
		return map[string]SupplementalDecision{}
	}
	return cloneSupplementalDecisions(s.store.state.SupplementalDecisions)
}
func literalSupplementalName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

// Saves only admitted immutable evidence, without draft/retrieval/consumer work.
func (s *Service) SaveSupplementalDecisions(source, inputToken string, decisions []SupplementalDecision, retryOnly bool, guard func(func() error) error) error {
	if source == "" || len(decisions) == 0 || guard == nil {
		return errors.New("supplemental decisions and admission required")
	}
	for _, d := range decisions {
		if d.Origin != SupplementalOrigin || d.SourceFingerprint != source || d.Key != SupplementalKey(d) || d.GroupKey == "" || d.StationID == "" || d.StreamName == "" || d.StreamFingerprint == "" || d.DispatcharrFingerprint == "" || d.AdmittedSnapshot == "" || d.OperationID == "" || d.RetrievalVersion == "" || (d.Decision != "confirmed" && d.Decision != "denied") {
			return errors.New("incomplete supplemental provenance")
		}
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if s.store.reviewInputTokenLocked() != inputToken {
		return ErrStaleReview
	}
	if s.store.state.SourceFingerprint != "" && s.store.state.SourceFingerprint != source {
		return ErrStaleReview
	}
	identical := 0
	for _, d := range decisions {
		if inc := s.store.state.Channels[d.ChannelID].Included; inc != nil && !*inc {
			return ErrMatchChannelExcluded
		}
		if old, ok := s.store.state.SupplementalDecisions[d.Key]; ok {
			old.UpdatedAt = time.Time{}
			d.UpdatedAt = time.Time{}
			a, _ := json.Marshal(old)
			b, _ := json.Marshal(d)
			if string(a) != string(b) {
				return ErrStaleReview
			}
			identical++
		}
		for _, old := range s.store.state.MatchDecisions {
			if old.ChannelID == d.ChannelID && (old.StreamFingerprint == d.StreamFingerprint || literalSupplementalName(old.StreamName) == literalSupplementalName(d.StreamName)) {
				return ErrStaleReview
			}
		}
	}
	if identical == len(decisions) {
		return guard(func() error { return nil })
	}
	if retryOnly || identical != 0 {
		return ErrStaleReview
	}
	previous := s.store.state
	s.store.state = cloneReviewState(previous)
	s.store.ensureSourceLocked(source)
	if s.store.state.SupplementalDecisions == nil {
		s.store.state.SupplementalDecisions = map[string]SupplementalDecision{}
	}
	for _, d := range decisions {
		d.UpdatedAt = time.Now().UTC()
		s.store.state.SupplementalDecisions[d.Key] = d
	}
	if err := guard(s.store.saveLocked); err != nil {
		s.store.state = previous
		return err
	}
	return nil
}
func (s *Service) ClearSupplementalDecisions(source, key string) ([]string, error) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if s.store.state.SourceFingerprint != source {
		return nil, ErrStaleReview
	}
	d, ok := s.store.state.SupplementalDecisions[key]
	if !ok {
		for _, v := range s.store.state.SupplementalDecisions {
			if v.GroupKey == key {
				d = v
				ok = true
				break
			}
		}
	}
	if !ok {
		return nil, errors.New("supplemental decision not found")
	}
	previous := s.store.state
	s.store.state = cloneReviewState(previous)
	keys := []string{}
	for k, v := range s.store.state.SupplementalDecisions {
		if v.OperationID == d.OperationID && v.GroupKey == d.GroupKey {
			delete(s.store.state.SupplementalDecisions, k)
			keys = append(keys, k)
		}
	}
	if err := s.store.saveLocked(); err != nil {
		s.store.state = previous
		return nil, err
	}
	return keys, nil
}

// Projection is scoped to the current connection and target identity. Raw names
// remain raw until the ordinary export boundary; canonical names never change.
func (s *Service) ProjectSupplementalDecisions(draft *Draft, source, connection string) {
	decisions := s.SupplementalDecisions(source)
	primary := s.MatchDecisions(source)
	overrides := s.store.Snapshot(source)
	keys := make([]string, 0, len(decisions))
	for k := range decisions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i := range draft.Channels {
		c := &draft.Channels[i]
		for _, k := range keys {
			d := decisions[k]
			if d.Origin != SupplementalOrigin || d.DispatcharrFingerprint != connection || d.ChannelID != c.ID || d.StationID != c.StationID || d.PlacementID != c.PlacementID || d.RawCallSign != c.CallSign {
				continue
			}
			protected := false
			for _, old := range primary {
				if old.ChannelID == d.ChannelID && (old.StreamFingerprint == d.StreamFingerprint || literalSupplementalName(old.StreamName) == literalSupplementalName(d.StreamName)) {
					protected = true
					break
				}
			}
			if protected {
				continue
			}
			draft.SupplementalDecisionCount++
			if d.Decision == "confirmed" {
				evidence := AliasEvidence{Value: d.StreamName, Sources: []string{"dispatcharr-supplemental"}, Methods: []string{"user-confirmed full raw name; supplemental retrieval, no primary score; downstream name matching may expand"}}
				// Build applied base-alias suppression before this projection. A
				// supplemental-only alias must consult the same persisted choices.
				// Positive suppression never removes a reviewed negative exclusion.
				suppressed, recorded := false, false
				for _, a := range c.SuppressedAliasEvidence {
					if literalSupplementalName(a.Value) == literalSupplementalName(d.StreamName) {
						suppressed, recorded = true, true
					}
				}
				for _, a := range overrides[c.ID].SuppressedAliases {
					if literalSupplementalName(a) == literalSupplementalName(d.StreamName) {
						suppressed = true
					}
				}
				if suppressed {
					if !recorded {
						c.SuppressedAliasEvidence = append(c.SuppressedAliasEvidence, evidence)
					}
					continue
				}
				if !containsExact(c.Aliases, d.StreamName) {
					c.Aliases = append(c.Aliases, d.StreamName)
					c.SupplementalAliases = append(c.SupplementalAliases, d.StreamName)
				}
				c.AliasEvidence = append(c.AliasEvidence, evidence)
			} else if d.Decision == "denied" {
				exists := false
				for _, a := range c.ExcludedAliases {
					if literalSupplementalName(a) == literalSupplementalName(d.StreamName) {
						exists = true
					}
				}
				if !exists {
					c.ExcludedAliases = append(c.ExcludedAliases, d.StreamName)
					c.SupplementalExcludedAliases = append(c.SupplementalExcludedAliases, d.StreamName)
				}
			}
		}
	}
	if len(decisions) > 0 {
		draft.SupplementalMessage = "Supplemental choices are saved separately without primary scores. Full raw aliases still match fuzzily; identical names cannot have different account outcomes. Use Check supplemental export impact after a batch. Unchecked, stale or unresolved impact does not block explicit generation and does not protect future inventory."
	}
}
func containsExact(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
