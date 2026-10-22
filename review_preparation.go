package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/providersource"
	"net/http"
	"time"
)

type reviewInputsRevision struct {
	config      appconfig.Config
	guide       *guide.TVGuide
	index, tmdb uint64
	choices     string
}
type preparedReviewInputs struct {
	key     reviewInputsRevision
	inputs  []builder.InputChannel
	sources []builder.SourceStatus
}
type cachedReviewDraft struct {
	key       string
	inputsKey reviewInputsRevision
	data      []byte
	lease     *builder.AutomaticMatchLease
	inputs    []builder.InputChannel
	builtAt   time.Time
}

func (s *lineuparrServer) reviewInputsRevision() reviewInputsRevision {
	config, _, _ := s.store.Get()
	key := reviewInputsRevision{config: config, guide: s.state.GetForSource(config.Fingerprint()), choices: s.builder.ReviewInputToken()}
	if s.marketIndex != nil {
		key.index = s.marketIndex.EvidenceRevision()
	}
	if s.tmdbEvidenceRevision != nil {
		key.tmdb = s.tmdbEvidenceRevision()
	}
	return key
}
func (s *lineuparrServer) reusableEvidence() bool {
	config, _, _ := s.store.Get()
	return s.builder.LocalReviewSources(config.Gracenote.Country, config.Gracenote.ProviderName) && (s.tmdbCachedEvidence == nil || s.tmdbEvidenceRevision != nil && s.tmdbWhileEvidence != nil)
}

// JSON copies preserve the complete exported input model, including typed
// evidence. No caller may mutate an immutable preparation kept for later reads.
func copyReviewInputs(inputs []builder.InputChannel) []builder.InputChannel {
	data, _ := json.Marshal(inputs)
	var copy []builder.InputChannel
	_ = json.Unmarshal(data, &copy)
	return copy
}
func (s *lineuparrServer) preparedInputs(w http.ResponseWriter) (appconfig.Config, []builder.InputChannel, []builder.SourceStatus, bool) {
	s.preparationMu.Lock()
	defer s.preparationMu.Unlock()
	key := s.reviewInputsRevision()
	if s.reusableEvidence() && key.guide != nil && key == s.preparation.key {
		return key.config, copyReviewInputs(s.preparation.inputs), copyReviewSources(s.preparation.sources), true
	}
	config, inputs, ok := s.activeInputs(w)
	if !ok {
		return config, nil, nil, false
	}
	sources := builder.ApplyProviderGuideAliasesForLineup(config.Gracenote.ProviderName, config.Gracenote.Location, config.Gracenote.PostalCode, inputs)
	sources = append(sources, s.builder.ApplyEmbeddedCatalogs(inputs)...)
	sources = append(sources, s.applyMarketAliases(config.Gracenote.Country, config.Gracenote.PostalCode, providersource.OfficialSourceID(config.Gracenote.ProviderName), inputs, lineupindex.EvidenceContext{Country: config.Gracenote.Country, PostalCode: config.Gracenote.PostalCode, LineupID: config.Gracenote.LineupID, Device: config.Gracenote.Device, Headend: config.Gracenote.HeadendID, Language: config.Gracenote.Language, Fingerprint: config.Fingerprint()})...)
	if key != s.reviewInputsRevision() {
		http.Error(w, "Review evidence changed; reload the draft", 409)
		return config, nil, nil, false
	}
	if s.reusableEvidence() {
		s.preparation = preparedReviewInputs{key, copyReviewInputs(inputs), copyReviewSources(sources)}
	}
	return config, inputs, sources, true
}

// Coalesce on-demand readers. Saves never take this mutex or wait for a draft.
// Revisions are checked again before publication; a superseded computation is
// discarded. Optional remote catalogs retain their existing uncached read path.
func (s *lineuparrServer) buildDraft(w http.ResponseWriter, r *http.Request) (*builder.Draft, appconfig.Config, []builder.InputChannel, bool) {
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	inputsKey := s.reviewInputsRevision()
	revision := s.builder.ReviewStateToken()
	if s.derivedRevision != nil {
		revision += s.derivedRevision()
	}

	reusable := s.reusableEvidence() && (s.automaticProjection == nil || s.derivedRevision != nil)
	if reusable && s.draftCache.key == revision && s.draftCache.inputsKey == inputsKey && s.draftCache.data != nil && time.Since(s.draftCache.builtAt) < 5*time.Second {
		var draft builder.Draft
		_ = json.Unmarshal(s.draftCache.data, &draft)
		if s.draftCache.lease != nil {
			lease := *s.draftCache.lease
			draft.AutomaticLease = &lease
		}
		if inputsKey != s.reviewInputsRevision() || revision != s.draftRevision() {
			http.Error(w, "Review changed while reading; reload the draft", 409)
			return nil, inputsKey.config, nil, false
		}
		return &draft, inputsKey.config, copyReviewInputs(s.draftCache.inputs), true
	}
	draft, config, inputs, ok := s.buildDraftUncached(w, r)
	if !ok {
		return draft, config, inputs, false
	}
	if inputsKey != s.reviewInputsRevision() {
		http.Error(w, "Review evidence changed; reload the draft", 409)
		return nil, config, nil, false
	}
	current := s.builder.ReviewStateToken()
	if s.derivedRevision != nil {
		current += s.derivedRevision()
	}
	if current != revision {
		http.Error(w, "Review changed while building; reload the draft", 409)
		return nil, config, nil, false
	}
	if reusable {
		data, err := json.Marshal(draft)
		if err == nil {
			s.draftCache = cachedReviewDraft{revision, inputsKey, data, draft.AutomaticLease, copyReviewInputs(inputs), time.Now()}
		}
	}
	return draft, config, inputs, true
}

func configurationToken(config appconfig.Config) string {
	data, _ := json.Marshal(config)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
func (s *lineuparrServer) draftRevision() string {
	revision := s.builder.ReviewStateToken()
	if s.derivedRevision != nil {
		revision += s.derivedRevision()
	}
	return revision
}
func (s *lineuparrServer) whileEvidence(index, tmdb uint64, action func() error) error {
	retained := func() error {
		if s.tmdbWhileEvidence == nil {
			return action()
		}
		current, err := s.tmdbWhileEvidence(tmdb, action)
		if err != nil {
			return err
		}
		if !current {
			return builder.ErrStaleReview
		}
		return nil
	}
	if s.marketIndex == nil {
		return retained()
	}
	current, err := s.marketIndex.WhileEvidenceRevision(index, retained)
	if err != nil {
		return err
	}
	if !current {
		return builder.ErrStaleReview
	}
	return nil
}

func copyReviewSources(sources []builder.SourceStatus) []builder.SourceStatus {
	data, _ := json.Marshal(sources)
	var result []builder.SourceStatus
	_ = json.Unmarshal(data, &result)
	return result
}
