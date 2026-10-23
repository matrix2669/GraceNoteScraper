package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

type supplementalGroup struct {
	ChannelDisplayName   string                         `json:"channelDisplayName,omitempty"`
	ChannelCallSign      string                         `json:"channelCallSign,omitempty"`
	RetrievalDescriptors []string                       `json:"retrievalDescriptors,omitempty"`
	Origin               string                         `json:"origin"`
	Key                  string                         `json:"key"`
	ChannelID            string                         `json:"channelId"`
	ChannelNumber        string                         `json:"channelNumber"`
	ChannelName          string                         `json:"channelName"`
	Alias                string                         `json:"alias"`
	StreamNames          []string                       `json:"streamNames"`
	M3UAccountIDs        []int64                        `json:"m3uAccountIds"`
	StreamCount          int                            `json:"streamCount"`
	RetrievalRank        int                            `json:"retrievalRank"`
	Reasons              []string                       `json:"reasons"`
	Constituents         []builder.SupplementalDecision `json:"constituents"`
	Decision             string                         `json:"decision,omitempty"`
	StreamName           string                         `json:"streamName,omitempty"`
	UpdatedAt            time.Time                      `json:"updatedAt,omitempty"`
	NeedsReview          bool                           `json:"needsReview,omitempty"`
	ReviewWarning        string                         `json:"reviewWarning,omitempty"`
}
type supplementalReview struct {
	Status               string              `json:"status"`
	Revision             string              `json:"revision"`
	Complete             bool                `json:"complete"`
	CandidateCount       int                 `json:"candidateCount"`
	CandidateStreamCount int                 `json:"candidateStreamCount"`
	ConfirmedCount       int                 `json:"confirmedCount"`
	DeniedCount          int                 `json:"deniedCount"`
	PreviouslyReviewed   int                 `json:"previouslyReviewed"`
	Candidates           []supplementalGroup `json:"candidates"`
	Decisions            []supplementalGroup `json:"decisions"`
	Message              string              `json:"message"`
}
type supplementalPreparation struct {
	inputs     reviewInputsRevision
	snapshot   string
	candidates []dispatcharr.SupplementalCandidate
	complete   bool
}
type supplementalAdmission struct {
	common reviewAdmission
	groups map[string][]builder.SupplementalDecision
	tokens map[string]string
}

func supplementalLiteral(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}
func supplementalHash(value any) string {
	data, _ := json.Marshal(value)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
func (s *dispatcharrServer) prepareSupplemental(draft *builder.Draft, config appconfig.Config, snapshot dispatcharrMatchSnapshot, inputs reviewInputsRevision) ([]dispatcharr.SupplementalCandidate, bool) {
	key := snapshotIdentity(snapshot)
	s.supplementalMu.Lock()
	defer s.supplementalMu.Unlock()
	old := s.supplementalPreparation
	if old.snapshot == key && old.inputs == inputs {
		return old.candidates, old.complete
	}
	feeds, _ := stationFeedCatalog(draft, config)
	targets := []dispatcharr.SupplementalTarget{}
	for _, c := range draft.Channels {
		if !c.Included {
			continue
		}
		labels := []dispatcharr.SupplementalLabel{}
		suppressed := map[string]bool{}
		for _, a := range c.SuppressedAliasEvidence {
			suppressed[supplementalLiteral(a.Value)] = true
		}
		for _, a := range c.AliasEvidence {
			if suppressed[supplementalLiteral(a.Value)] {
				continue
			}
			for _, source := range a.Sources {
				if !strings.HasPrefix(source, "dispatcharr-") {
					labels = append(labels, dispatcharr.SupplementalLabel{Value: a.Value, Evidence: source + ": " + strings.Join(a.Methods, "; ")})
					break
				}
			}
		}
		for _, e := range c.SupplementalEvidence {
			if e.RootStationID == c.StationID && !suppressed[supplementalLiteral(e.Value)] {
				labels = append(labels, dispatcharr.SupplementalLabel{Value: e.Value, Evidence: "USER-ATTESTED review descriptor: " + e.SourceID + "/" + e.SourceRowID + "@" + e.SourceRevision})
			}
		}
		targets = append(targets, dispatcharr.SupplementalTarget{ID: c.ID, StationID: c.StationID, PlacementID: c.PlacementID, Number: c.Number, Name: c.Name, CallSign: c.CallSign, Country: affiliateCountry(config.Gracenote.Country), Labels: labels, Feed: feeds[c.ID]})
	}
	candidates, complete := dispatcharr.RetrieveSupplemental(targets, snapshot.identityStreams, snapshot.candidates)
	s.supplementalPreparation = supplementalPreparation{inputs, key, candidates, complete}
	return candidates, complete
}
func supplementalDecision(c dispatcharr.SupplementalCandidate, source, connection, snapshot string) builder.SupplementalDecision {
	d := builder.SupplementalDecision{Origin: builder.SupplementalOrigin, SourceFingerprint: source, DispatcharrFingerprint: connection, ChannelID: c.Target.ID, StationID: c.Target.StationID, PlacementID: c.Target.PlacementID, ChannelName: c.Target.Name, ChannelNumber: c.Target.Number, RawCallSign: c.Target.CallSign, StreamID: c.Stream.ID, M3UAccountID: c.Stream.AccountID, StreamName: c.Stream.Name, StreamFingerprint: c.Stream.Fingerprint, RetrievalVersion: dispatcharr.SupplementalRevision, RetrievalRank: c.Rank, RetrievalReason: c.Reason, EvidenceRefs: []string{"Retrieval descriptor: " + c.Descriptor, c.Evidence}, AdmittedSnapshot: snapshot}
	d.Key = builder.SupplementalKey(d)
	return d
}
func supplementalGroups(rows []builder.SupplementalDecision, history bool) []supplementalGroup {
	byKey := map[string]*supplementalGroup{}
	for _, d := range rows {
		key := d.ChannelID + "\x00" + supplementalLiteral(d.StreamName)
		if history {
			key = d.GroupKey + "\x00" + d.OperationID
		}
		g := byKey[key]
		if g == nil {
			g = &supplementalGroup{Origin: builder.SupplementalOrigin, ChannelID: d.ChannelID, ChannelNumber: d.ChannelNumber, ChannelName: d.ChannelName, Alias: d.StreamName, StreamName: d.StreamName, RetrievalRank: d.RetrievalRank, Decision: d.Decision, UpdatedAt: d.UpdatedAt}
			byKey[key] = g
		}
		g.StreamCount++
		g.StreamNames = appendUniqueReviewString(g.StreamNames, d.StreamName)
		g.M3UAccountIDs = appendUniqueReviewInt64(g.M3UAccountIDs, d.M3UAccountID)
		g.Reasons = appendUniqueReviewString(g.Reasons, d.RetrievalReason)
		g.Constituents = append(g.Constituents, d)
		if d.UpdatedAt.After(g.UpdatedAt) {
			g.UpdatedAt = d.UpdatedAt
		}
	}
	result := []supplementalGroup{}
	for _, g := range byKey {
		sort.Slice(g.Constituents, func(i, j int) bool { return g.Constituents[i].Key < g.Constituents[j].Key })
		keys := []string{}
		for _, d := range g.Constituents {
			keys = append(keys, d.Key)
		}
		g.Key = "supp:g:" + supplementalHash(keys)
		if history {
			g.Key = g.Constituents[0].GroupKey
		}
		for i := range g.Constituents {
			g.Constituents[i].GroupKey = g.Key
		}
		result = append(result, *g)
	}
	sort.SliceStable(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if history && !a.UpdatedAt.Equal(b.UpdatedAt) {
			return a.UpdatedAt.After(b.UpdatedAt)
		}
		if a.RetrievalRank != b.RetrievalRank {
			return a.RetrievalRank < b.RetrievalRank
		}
		if a.ChannelNumber != b.ChannelNumber {
			return a.ChannelNumber < b.ChannelNumber
		}
		return a.Key < b.Key
	})
	return result
}
func (s *dispatcharrServer) buildSupplementalReview(draft *builder.Draft, config appconfig.Config, snapshot dispatcharrMatchSnapshot, inputs reviewInputsRevision, valid bool, limit int) (supplementalReview, []supplementalGroup) {
	response := supplementalReview{Status: "refresh-required", Revision: dispatcharr.SupplementalRevision, Candidates: []supplementalGroup{}, Decisions: []supplementalGroup{}, Message: "Supplemental retrieval has no LineupARR score. Review raw/account constituents, then explicitly check export impact. Full names can expand to extra streams; identical names share all-account export outcomes. Future inventory requires another check."}
	stored := s.lineup.builder.SupplementalDecisions(config.Fingerprint())
	primary := s.lineup.builder.MatchDecisions(config.Fingerprint())
	history := []builder.SupplementalDecision{}
	for _, d := range stored {
		history = append(history, d)
	}
	response.Decisions = supplementalGroups(history, true)
	connection, _ := s.config.Get()
	current := map[string]builder.DraftChannel{}
	for _, c := range draft.Channels {
		current[c.ID] = c
	}
	for i := range response.Decisions {
		g := &response.Decisions[i]
		if g.Decision == "confirmed" {
			response.ConfirmedCount++
		} else {
			response.DeniedCount++
		}
		d := g.Constituents[0]
		c := current[d.ChannelID]
		g.ChannelDisplayName, g.ChannelCallSign = reviewDisplayName(c), c.CallSign
		for _, old := range primary {
			if old.ChannelID == d.ChannelID && (old.StreamFingerprint == d.StreamFingerprint || supplementalLiteral(old.StreamName) == supplementalLiteral(d.StreamName)) {
				g.NeedsReview = true
				g.ReviewWarning = "A saved primary decision now covers this literal name. Primary policy takes precedence; this supplemental selector is inactive. Undo remains available."
				break
			}
		}
		if d.DispatcharrFingerprint != connection.Fingerprint() || c.StationID != d.StationID || c.PlacementID != d.PlacementID || c.CallSign != d.RawCallSign {
			g.NeedsReview = true
			g.ReviewWarning = "Saved history belongs to an earlier connection or target identity; its aliases are inactive. Undo remains available."
		}
	}
	if !valid || snapshot.warning != "" || !supplementalEvidenceCurrent(snapshot, inputs) {
		return response, nil
	}
	candidates, complete := s.prepareSupplemental(draft, config, snapshot, inputs)
	candidates = append(append([]dispatcharr.SupplementalCandidate(nil), candidates...), s.impactExtraCandidates(draft, snapshot, inputs)...)
	response.Complete = complete
	response.Status = "current"
	if !complete {
		response.Status = "incomplete"
		response.Message += " Retrieval reached its bound; more inventory remains unreviewed."
	}
	rows := []builder.SupplementalDecision{}
	descriptors := map[string]string{}
	seen := map[string]bool{}
	for _, c := range candidates {
		d := supplementalDecision(c, config.Fingerprint(), snapshot.dispatcharrFingerprint, snapshotIdentity(snapshot))
		if seen[d.Key] {
			continue
		}
		seen[d.Key] = true
		descriptors[d.Key] = c.Descriptor
		if _, settled := stored[d.Key]; settled {
			continue
		}
		reviewed := false
		for _, p := range primary {
			if p.ChannelID == d.ChannelID && (p.StreamFingerprint == d.StreamFingerprint || supplementalLiteral(p.StreamName) == supplementalLiteral(d.StreamName)) {
				reviewed = true
				break
			}
		}
		if reviewed {
			response.PreviouslyReviewed++
			continue
		}
		rows = append(rows, d)
	}
	groups := supplementalGroups(rows, false)
	for i := range groups {
		g := &groups[i]
		c := current[g.ChannelID]
		g.ChannelDisplayName, g.ChannelCallSign = reviewDisplayName(c), c.CallSign
		for _, d := range g.Constituents {
			g.RetrievalDescriptors = appendUniqueReviewString(g.RetrievalDescriptors, descriptors[d.Key])
		}
	}
	response.CandidateCount = len(groups)
	response.CandidateStreamCount = len(rows)
	response.Candidates = groups
	if len(groups) > limit {
		response.Candidates = groups[:limit]
	}
	return response, groups
}

// Caller holds reviewMu after primary admission publishes the common scope.
func (s *dispatcharrServer) publishSupplementalLocked(inputs reviewInputsRevision, snapshot, inventory, scope string, groups []supplementalGroup) map[string]string {
	previous := s.supplementalAdmission
	next := supplementalAdmission{common: reviewAdmission{inputs: inputs, snapshot: snapshot, inventory: inventory}, groups: map[string][]builder.SupplementalDecision{}, tokens: map[string]string{}}
	tokens := map[string]string{}
	if previous.common.inputs == inputs && previous.common.snapshot == snapshot && previous.common.inventory == inventory {
		for k, g := range previous.groups {
			next.groups[k] = g
		}
		for k, t := range previous.tokens {
			next.tokens[k] = t
		}
	}
	for _, g := range groups {
		token := next.tokens[g.Key]
		op, ok := s.operations[token]
		if !ok || op.State != "offered" || op.Scope != scope {
			token = rand.Text()
			s.operations[token] = reviewOperation{Key: g.Key, State: "offered", Scope: scope}
		}
		next.groups[g.Key] = g.Constituents
		next.tokens[g.Key] = token
		tokens[g.Key] = token
	}
	s.supplementalAdmission = next
	return tokens
}
func (s *dispatcharrServer) handleSupplementalDecision(w http.ResponseWriter, r *http.Request, key, operation, choice string) {
	connection, connected := s.config.Get()
	config, configured, _ := s.lineup.store.Get()
	if !connected || !configured {
		http.Error(w, "Choose the current source and connection first", 409)
		return
	}
	if r.Method == http.MethodDelete {
		var keys []string
		if !s.saveWhileCurrent(connection, config, func() error {
			var err error
			keys, err = s.lineup.builder.ClearSupplementalDecisions(config.Fingerprint(), key)
			return err
		}, w) {
			return
		}
		s.retireDecisionOperations(keys)
		writeLineuparrJSON(w, 200, map[string]bool{"saved": true})
		return
	}
	if choice != "confirmed" && choice != "denied" {
		http.Error(w, "decision must be confirmed or denied", 400)
		return
	}
	inputs := s.lineup.reviewInputsRevision()
	inventory := s.inventoryObservationToken()
	s.reviewMu.RLock()
	a := s.supplementalAdmission
	op := s.operations[operation]
	rows := append([]builder.SupplementalDecision(nil), a.groups[key]...)
	valid := operation != "" && a.tokens[key] == operation && a.common.inputs == inputs && a.common.inventory == inventory && a.common.snapshot == snapshotIdentity(s.snapshot) && (op.State == "offered" || op.State == "saved")
	s.reviewMu.RUnlock()
	if !valid || len(rows) == 0 {
		http.Error(w, "Supplemental proposal is no longer current; reload review", 409)
		return
	}
	for i := range rows {
		if rows[i].DispatcharrFingerprint != connection.Fingerprint() || rows[i].SourceFingerprint != config.Fingerprint() {
			http.Error(w, "Source or connection changed; refresh before choosing", 409)
			return
		}
		rows[i].Decision = choice
		rows[i].OperationID = operation
	}
	if !s.saveWhileCurrent(connection, config, func() error {
		return s.commitAdmittedDecision(a.common, func() error {
			return s.lineup.builder.SaveSupplementalDecisions(config.Fingerprint(), a.common.inputs.choices, rows, op.State == "saved", func(write func() error) error {
				return s.whileAdmissionSnapshot(a.common, func() error {
					if err := r.Context().Err(); err != nil {
						return err
					}
					return write()
				})
			})
		})
	}, w) {
		return
	}
	s.reviewMu.Lock()
	op = s.operations[operation]
	op.State = "saved"
	op.Decision = choice
	op.Members = nil
	for _, d := range rows {
		op.Members = append(op.Members, d.Key)
	}
	s.operations[operation] = op
	s.reviewMu.Unlock()
	writeLineuparrJSON(w, 200, map[string]bool{"saved": true})
}

// Evidence captured by refresh is immutable. Decisions and exclusions can evolve
// in a batch, but a new catalog/guide/source generation requires another refresh.
func supplementalEvidenceCurrent(snapshot dispatcharrMatchSnapshot, inputs reviewInputsRevision) bool {
	old := snapshot.supplementalInputs
	return snapshot.supplementalBound && old.config == inputs.config && old.guide == inputs.guide && old.index == inputs.index && old.tmdb == inputs.tmdb
}

// Impact extras are proposals, never denials. Keep them available while the user
// reviews that batch even after its impact becomes stale from saved choices.
func (s *dispatcharrServer) impactExtraCandidates(draft *builder.Draft, snapshot dispatcharrMatchSnapshot, inputs reviewInputsRevision) []dispatcharr.SupplementalCandidate {
	s.impactMu.Lock()
	job := s.impact
	if job == nil || job.response.State != "complete" || job.snapshot != snapshotIdentity(snapshot) || job.connection != snapshot.dispatcharrFingerprint || job.inputs != inputs {
		s.impactMu.Unlock()
		return nil
	}
	rows := append([]supplementalImpactRow(nil), job.response.Rows...)
	fingerprint := job.response.Fingerprint
	s.impactMu.Unlock()
	targets := map[string]builder.DraftChannel{}
	for _, c := range draft.Channels {
		if c.Included {
			targets[c.ID] = c
		}
	}
	out := []dispatcharr.SupplementalCandidate{}
	for _, row := range rows {
		c, ok := targets[row.ChannelID]
		if !ok {
			continue
		}
		for _, extra := range row.Extra {
			for _, stream := range snapshot.identityStreams {
				if stream.ID == extra.StreamID && stream.AccountID == extra.AccountID && stream.Name == extra.Name {
					out = append(out, dispatcharr.SupplementalCandidate{Target: dispatcharr.SupplementalTarget{ID: c.ID, StationID: c.StationID, PlacementID: c.PlacementID, Number: c.Number, Name: c.Name, CallSign: c.CallSign}, Stream: stream, Rank: 4, Reason: "Extra result from explicit prior Exact95 impact; review its literal name before choosing", Evidence: "explicit-impact:" + fingerprint})
				}
			}
		}
	}
	return out
}
