package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

// All automatic results are immutable. Projection and the immediately following
// review read share the same decision overlay; changing any evidence or saved
// choice replaces it. No pending computation can publish under a newer key.
type cachedAutomaticView struct {
	key    string
	inputs reviewInputsRevision
	view   automaticMatchView
	result dispatcharr.AutomaticResult
}

func (s *dispatcharrServer) automaticView(draft *builder.Draft, config appconfig.Config, snapshot dispatcharrMatchSnapshot) (automaticMatchView, dispatcharr.AutomaticResult) {
	s.automaticMu.Lock()
	defer s.automaticMu.Unlock()
	inputs := s.lineup.reviewInputsRevision()
	key := snapshotIdentity(snapshot) + s.inventoryObservationToken() + s.lineup.builder.ReviewStateToken() + automaticDraftEvidenceToken(draft)
	if s.lineup.reusableEvidence() && s.automaticCache.key == key && s.automaticCache.inputs == inputs {
		return s.automaticCache.view, s.automaticCache.result
	}
	view, result := s.automaticViewUncached(draft, config, snapshot)
	if s.lineup.reusableEvidence() && inputs == s.lineup.reviewInputsRevision() {
		current := snapshotIdentity(snapshot) + s.inventoryObservationToken() + s.lineup.builder.ReviewStateToken() + automaticDraftEvidenceToken(draft)
		if current == key {
			s.automaticCache = cachedAutomaticView{key, inputs, view, result}
		}
	}
	return view, result
}

// Catalog eligibility depends on the complete attributed station facts, not
// merely the flattened aliases used by the matcher. Exclusion projection does
// not modify these fields, so its immediately following review can reuse them.
func automaticDraftEvidenceToken(draft *builder.Draft) string {
	facts := make([]any, 0, len(draft.Channels))
	for _, c := range draft.Channels {
		facts = append(facts, []any{c.ID, c.StationID, c.CallSign, c.Name, c.Number, c.Included, c.AliasEvidence, c.ServiceEvidence})
	}
	data, _ := json.Marshal(facts)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
