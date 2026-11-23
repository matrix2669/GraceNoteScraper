package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	matcher "github.com/daniel-widrick/GraceNoteScraper/lineuparr_matcher"
)

type supplementalImpactPhysical struct {
	Name      string `json:"name"`
	AccountID int64  `json:"accountId"`
	StreamID  int64  `json:"streamId"`
}
type supplementalImpactRow struct {
	ChannelID          string                       `json:"channelId"`
	ChannelName        string                       `json:"channelName"`
	ChannelNumber      string                       `json:"channelNumber"`
	Selected           []supplementalImpactPhysical `json:"selected"`
	Matched            []supplementalImpactPhysical `json:"matched"`
	Extra              []supplementalImpactPhysical `json:"extra"`
	Missed             []supplementalImpactPhysical `json:"missed"`
	ProtectedConflicts []string                     `json:"protectedConflicts"`
	SameNameConflicts  []string                     `json:"sameNameConflicts"`
}
type supplementalImpactResponse struct {
	FindingsStale    bool                    `json:"findingsStale,omitempty"`
	Unresolved       bool                    `json:"unresolved"`
	ID               string                  `json:"id"`
	Fingerprint      string                  `json:"fingerprint"`
	JSONHash         string                  `json:"jsonHash"`
	InventoryHash    string                  `json:"inventoryHash"`
	ConsumerRevision string                  `json:"consumerRevision"`
	Profile          string                  `json:"profile"`
	State            string                  `json:"state"`
	Completed        int                     `json:"completed"`
	Total            int                     `json:"total"`
	CheckedAt        time.Time               `json:"checkedAt,omitempty"`
	Rows             []supplementalImpactRow `json:"rows"`
	Message          string                  `json:"message"`
}
type supplementalImpactJob struct {
	response                               supplementalImpactResponse
	inputs                                 reviewInputsRevision
	snapshot, inventory, state, connection string
	cancel                                 context.CancelFunc
	done                                   chan struct{}
	reset                                  uint64
}

func (s *dispatcharrServer) impactCurrent(job *supplementalImpactJob) bool {
	if job == nil || job.reset != s.lineup.builder.ResetGeneration() || job.inputs != s.lineup.reviewInputsRevision() || job.state != s.lineup.builder.ReviewStateToken() || job.inventory != s.inventoryObservationToken() {
		return false
	}
	conn, connected := s.config.Get()
	if !connected || conn.Fingerprint() != job.connection {
		return false
	}
	s.reviewMu.RLock()
	snapshot := snapshotIdentity(s.snapshot)
	s.reviewMu.RUnlock()
	return snapshot == job.snapshot
}
func (s *dispatcharrServer) impactResponse() supplementalImpactResponse {
	s.impactMu.Lock()
	job := s.impact
	lifecycle := s.impactLifecycle
	var response supplementalImpactResponse
	if job != nil {
		response = job.response
	}
	if (lifecycle != "" || response.State == "running") && s.impactCompleted != nil && job != s.impactCompleted {
		response.Rows = append([]supplementalImpactRow(nil), s.impactCompleted.response.Rows...)
		response.FindingsStale = true
	}
	s.impactMu.Unlock()
	if lifecycle != "" {
		response.State = lifecycle
		response.Message = "Validating saved choices in the background. You can continue reviewing or generate the lineup."
		return response
	}
	if job == nil {
		return supplementalImpactResponse{State: "idle", Profile: dispatcharr.ImpactProfile, Message: "No saved name effects require validation. Generation remains available."}
	}
	if response.State != "idle" && response.State != "refresh-required" && !(response.State == "failed" && response.ID == "") && !s.impactCurrent(job) {
		response.State = "stale"
		response.Message = "Inputs or decisions changed. This prior impact is stale; validation requires current inputs. Reload review to recover a failed check, or Refresh streams if the inventory changed. Future inventory is not protected."
	}
	return response
}
func (s *dispatcharrServer) handleSupplementalImpact(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		writeLineuparrJSON(w, 200, s.impactResponse())
		return
	}
	if r.Method == http.MethodDelete {
		var body struct {
			ID string `json:"id"`
		}
		if !requireJSONContentType(w, r) || !decodeLineuparrRequest(w, r, &body) {
			return
		}
		s.impactMu.Lock()
		job := s.impact
		if job == nil || job.response.ID != body.ID {
			s.impactMu.Unlock()
			http.Error(w, "Impact job was superseded", 409)
			return
		}
		if job.response.State == "running" {
			job.cancel()
			job.response.State = "cancelled"
			job.response.Message = "Impact cancelled; no decisions or export changed. Check again when ready."
		}
		response := job.response
		s.impactMu.Unlock()
		writeLineuparrJSON(w, 200, response)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "method not allowed", 405)
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}
	s.impactMu.Lock()
	automatic := s.impactSignal != nil
	s.impactMu.Unlock()
	if automatic {
		s.recoverImpact()
		writeLineuparrJSON(w, 202, s.impactResponse())
		return
	}
	s.startSupplementalImpact(w, r, false)
}

// Both automatic validation and explicit API recovery share this exact consumer
// preparation. Ordinary reads never call it.
func (s *dispatcharrServer) startSupplementalImpact(w http.ResponseWriter, r *http.Request, automatic bool) {
	s.impactLaunchMu.Lock()
	defer s.impactLaunchMu.Unlock()
	s.impactMu.Lock()
	refreshing := s.impactRefreshes > 0
	s.impactMu.Unlock()
	if refreshing {
		http.Error(w, "Primary refresh has priority", 409)
		return
	}
	inputs := s.lineup.reviewInputsRevision()
	state := s.lineup.builder.ReviewStateToken()
	config, configured, _ := s.lineup.store.Get()
	conn, connected := s.config.Get()
	s.reviewMu.RLock()
	snapshot := s.snapshot
	s.reviewMu.RUnlock()
	var draft *builder.Draft
	var ok bool
	if automatic {
		if configured && len(s.lineup.builder.SupplementalDecisions(config.Fingerprint())) == 0 {
			writeLineuparrJSON(w, 200, supplementalImpactResponse{State: "idle", Rows: []supplementalImpactRow{}, Message: "No saved name effects require validation."})
			return
		}
		// Reject missing/stale inventory before any draft work. Automatic validation
		// never enters the ordinary builder's optional-source acquisition path.
		if !configured || !connected || !s.automaticImpactSnapshotCurrent(snapshot, inputs, config, conn) || snapshot.reviewSources == nil || snapshot.reviewInputs == nil {
			http.Error(w, "Refresh streams before validating saved names against current captured inputs", 409)
			return
		}
		draft, ok = s.buildCapturedImpactDraft(w, r, snapshot, config, inputs, state)
	} else {
		draft, config, _, ok = s.lineup.buildDraft(w, r)
	}
	if !ok {
		return
	}
	channels, _ := dispatcharrSnapshotChannels(draft)
	if !connected || !s.automaticImpactSnapshotCurrent(snapshot, inputs, config, conn) || !dispatcharrInputsSubset(channels, snapshot.channels) {
		http.Error(w, "Refresh streams before validating saved names against a current successful inventory", 409)
		return
	}

	// Actual JSON serialization is the consumer boundary. Remove only additions
	// attributed to this origin for the protected baseline; keep prior primary data.
	bytes, _ := json.Marshal(draft)
	var baseline builder.Draft
	_ = json.Unmarshal(bytes, &baseline)
	for i := range baseline.Channels {
		c := &baseline.Channels[i]
		remove := func(values, added []string) []string {
			set := map[string]bool{}
			for _, v := range added {
				set[v] = true
			}
			out := []string{}
			for _, v := range values {
				if !set[v] {
					out = append(out, v)
				}
			}
			return out
		}
		c.Aliases = remove(c.Aliases, c.SupplementalAliases)
		c.ExcludedAliases = remove(c.ExcludedAliases, c.SupplementalExcludedAliases)
	}
	proposedExport, beforeExport := builder.ExportFromDraft(draft), builder.ExportFromDraft(&baseline)
	// Capture serialization date once, outside the key's scheduling identity.
	proposedExport.Date = snapshot.fetchedAt.UTC().Format("2006-01-02")
	beforeExport.Date = proposedExport.Date
	proposed, _ := json.Marshal(proposedExport)
	before, _ := json.Marshal(beforeExport)
	request := dispatcharr.ImpactRequest{Country: draft.CountryCode, Baseline: string(before), Proposed: string(proposed)}
	missingGroups := false
	for _, stream := range snapshot.identityStreams {
		request.Streams = append(request.Streams, dispatcharr.ImpactStream{Name: stream.Name, GroupName: stream.GroupName})
		if stream.GroupName == "" {
			missingGroups = true
		}
	}
	job := &supplementalImpactJob{inputs: inputs, snapshot: snapshotIdentity(snapshot), inventory: s.inventoryObservationToken(), state: state, connection: conn.Fingerprint(), reset: s.lineup.builder.ResetGeneration(), done: make(chan struct{})}
	key := supplementalHash([]any{request, job.snapshot, job.inventory, state, configurationToken(config), conn.Fingerprint(), inputs.policy, inputs.index, inputs.tmdb, inputs.choices, fmt.Sprintf("%p", inputs.guide), matcher.Revision, dispatcharr.ImpactProfile, dispatcharr.SupplementalRevision})
	job.response = supplementalImpactResponse{ID: rand.Text(), Fingerprint: key, JSONHash: supplementalHash(json.RawMessage(proposed)), InventoryHash: snapshot.inventoryDigest, ConsumerRevision: matcher.Revision, Profile: dispatcharr.ImpactProfile, State: "running", Rows: []supplementalImpactRow{}, Message: "Checking prospective JSON against the same immutable successful inventory. Ordinary choices remain available."}
	if !s.impactCurrent(job) {
		http.Error(w, "Review changed while preparing impact; retry after your batch", 409)
		return
	}
	s.impactMu.Lock()
	if old := s.impact; old != nil && old.response.Fingerprint == key && (old.response.State == "running" || old.response.State == "complete") {
		response := old.response
		s.impactMu.Unlock()
		writeLineuparrJSON(w, 200, response)
		return
	}
	previous := s.impact
	if previous != nil && previous.response.State == "running" {
		previous.cancel()
	}
	parent := s.impactContext
	if parent == nil {
		parent = context.Background()
	}
	s.impactMu.Unlock()
	// A replacement may not overlap its predecessor's consumer process.
	if previous != nil && previous.done != nil {
		<-previous.done
	}
	if !s.impactCurrent(job) || parent.Err() != nil {
		http.Error(w, "Review changed while replacing validation", 409)
		return
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	s.impactMu.Lock()
	if automatic && s.impactRefreshes > 0 {
		s.impactMu.Unlock()
		cancel()
		http.Error(w, "Primary refresh has priority", 409)
		return
	}
	job.cancel = cancel
	initialResponse := job.response
	s.impact = job
	s.impactMu.Unlock()
	decisions := s.lineup.builder.SupplementalDecisions(config.Fingerprint())
	primary := s.lineup.builder.MatchDecisions(config.Fingerprint())
	run := s.impactRunner
	if run == nil {
		run = dispatcharr.CheckSupplementalImpact
	}
	go func() {
		defer close(job.done)
		defer cancel()
		// Token-only observation cancels invalidated work even for non-HTTP guide/
		// evidence changes. No draft construction or Python runs in this watcher.
		watcherCtx := ctx
		go func() {
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-watcherCtx.Done():
					return
				case <-ticker.C:
					if !s.impactCurrent(job) {
						cancel()
						return
					}
				}
			}
		}()
		ctx = dispatcharr.WithMatchProgress(ctx, func(done, total int) {
			s.impactMu.Lock()
			defer s.impactMu.Unlock()
			if s.impact == job && job.response.State == "running" {
				job.response.Completed = done
				job.response.Total = total
			}
		})
		result, err := run(ctx, request)
		current := s.impactCurrent(job)
		rows := []supplementalImpactRow{}
		if err == nil {
			rows = compareSupplementalImpact(draft, snapshot.identityStreams, decisions, primary, result, conn.Fingerprint())
		}
		publish := func() error {
			s.impactMu.Lock()
			defer s.impactMu.Unlock()
			if s.impact != job || job.response.State != "running" {
				return nil
			}
			if ctx.Err() != nil {
				return builder.ErrStaleReview
			}
			job.response.State = "complete"
			job.response.Rows = rows
			for _, row := range rows {
				if len(row.Extra) > 0 || len(row.Missed) > 0 || len(row.ProtectedConflicts) > 0 || len(row.SameNameConflicts) > 0 {
					job.response.Unresolved = true
				}
			}
			s.impactCompleted = job
			job.response.CheckedAt = time.Now().UTC()
			job.response.Message = "Validated this captured inventory and prospective JSON. Extra names require Confirm/Deny. Equal raw names share account outcomes; future inventory and custom consumer settings remain outside this check."
			if missingGroups {
				job.response.Message += " Some captured streams lack group-country metadata, limiting country-filter coverage."
			}
			return nil
		}
		published := false
		if err == nil && current {
			// Same configuration → evidence/guide → builder → review/inventory →
			// impact ordering as durable saves. No stale callback can publish a
			// successful result between its currentness check and a newer save.
			published = s.saveWhileCurrent(conn, config, func() error {
				return s.commitAdmittedDecision(reviewAdmission{inputs: job.inputs}, func() error {
					return s.lineup.builder.WhileReviewState(job.state, func() error {
						return s.whileAdmissionSnapshot(reviewAdmission{snapshot: job.snapshot, inventory: job.inventory}, publish)
					})
				})
			}, httptest.NewRecorder())
		}
		if published {
			return
		}
		s.impactMu.Lock()
		defer s.impactMu.Unlock()
		if s.impact != job || job.response.State != "running" {
			return
		}
		job.response.State = "stale"
		job.response.Message = "Inputs changed during validation; result discarded. Reload review to recover, or Refresh streams when the inventory changed."
		if current && err != nil {
			job.response.State = "failed"
			if ctx.Err() != nil {
				job.response.State = "cancelled"
			}
			job.response.Message = "Validation did not complete: " + err.Error() + ". Reload review to retry. Saved choices and generation remain available."
		}

	}()
	writeLineuparrJSON(w, 202, initialResponse)
}
func (s *dispatcharrServer) automaticImpactSnapshotCurrent(snapshot dispatcharrMatchSnapshot, inputs reviewInputsRevision, config appconfig.Config, conn dispatcharr.Config) bool {
	return inputs.config == config && supplementalEvidenceCurrent(snapshot, inputs) && snapshot.warning == "" && !snapshot.fetchedAt.IsZero() && snapshot.dispatcharrFingerprint == conn.Fingerprint() && snapshot.lineupFingerprint == config.Fingerprint() && snapshot.guide == s.lineup.state.GetForSource(config.Fingerprint()) && !s.inventoryDrift(snapshot)
}

func (s *dispatcharrServer) buildCapturedImpactDraft(w http.ResponseWriter, r *http.Request, snapshot dispatcharrMatchSnapshot, config appconfig.Config, inputs reviewInputsRevision, state string) (*builder.Draft, bool) {
	s.lineup.draftMu.Lock()
	defer s.lineup.draftMu.Unlock()
	draft, err := s.lineup.builder.BuildCaptured(r.Context(), builder.LineupContext{SourceFingerprint: config.Fingerprint(), Country: config.Gracenote.Country, PostalCode: config.Gracenote.PostalCode, ProviderName: config.Gracenote.ProviderName, LineupID: config.Gracenote.LineupID}, copyReviewInputs(snapshot.reviewInputs), snapshot.reviewSources)
	if err != nil {
		http.Error(w, "Unable to prepare captured review: "+err.Error(), 500)
		return nil, false
	}
	if inputs != s.lineup.reviewInputsRevision() || state != s.lineup.builder.ReviewStateToken() {
		http.Error(w, "Review changed while preparing captured inputs", 409)
		return nil, false
	}
	draft.SourceFingerprint = config.Fingerprint()
	draft.ResetGeneration = s.lineup.builder.ResetGeneration()
	draft.PublicationGeneration = s.lineup.builder.PublicationGeneration()
	if s.lineup.automaticProjection != nil {
		s.lineup.automaticProjection(draft, config)
		if inputs != s.lineup.reviewInputsRevision() || state != s.lineup.builder.ReviewStateToken() {
			http.Error(w, "Review changed while projecting captured inputs", 409)
			return nil, false
		}
		draft.AutomaticLease.StateToken = state
		draft.AutomaticLease.GuideToken = fmt.Sprintf("%p", inputs.guide)
	}
	return draft, true
}

func compareSupplementalImpact(draft *builder.Draft, inventory []dispatcharr.IdentityStream, decisions map[string]builder.SupplementalDecision, primary map[string]builder.MatchDecision, result dispatcharr.ImpactResult, connection string) []supplementalImpactRow {
	// The export's stable per-category ordering retains target IDs internally.
	// Names and serialized numbers are not unique identifiers (005.10 and 5.1
	// serialize alike); never join or overwrite results using those display fields.
	exported := builder.ExportFromDraft(draft)
	before, after := map[string]map[string]bool{}, map[string]map[string]bool{}
	for _, phase := range []struct {
		rows []dispatcharr.ImpactMatch
		dest map[string]map[string]bool
	}{{result.Baseline, before}, {result.Proposed, after}} {
		for _, r := range phase.rows {
			entries := exported.Categories[r.Category]
			if r.EntryIndex < 0 || r.EntryIndex >= len(entries) {
				continue
			}
			set := map[string]bool{}
			for _, name := range r.Names {
				set[name] = true
			}
			phase.dest[entries[r.EntryIndex].ChannelID] = set
		}
	}
	rows := []supplementalImpactRow{}
	for _, c := range draft.Channels {
		if !c.Included {
			continue
		}
		selected := map[string]bool{}
		selectedNames := map[string]bool{}
		deniedNames := map[string]bool{}
		saved := []builder.SupplementalDecision{}
		for _, d := range decisions {
			if d.ChannelID == c.ID && d.StationID == c.StationID && d.PlacementID == c.PlacementID && d.RawCallSign == c.CallSign && d.DispatcharrFingerprint == connection {
				saved = append(saved, d)
				if d.Decision == "confirmed" {
					selected[d.StreamFingerprint] = true
					selectedNames[supplementalLiteral(d.StreamName)] = true
				} else {
					deniedNames[supplementalLiteral(d.StreamName)] = true
				}
			}
		}
		row := supplementalImpactRow{ChannelID: c.ID, ChannelName: c.Name, ChannelNumber: c.Number, Selected: []supplementalImpactPhysical{}, Matched: []supplementalImpactPhysical{}, Extra: []supplementalImpactPhysical{}, Missed: []supplementalImpactPhysical{}, ProtectedConflicts: []string{}, SameNameConflicts: []string{}}
		baseline, proposed := before[c.ID], after[c.ID]
		for _, d := range saved {
			if d.Decision == "confirmed" {
				physical := supplementalImpactPhysical{d.StreamName, d.M3UAccountID, d.StreamID}
				row.Selected = append(row.Selected, physical)
				present := false
				for _, stream := range inventory {
					if stream.Fingerprint == d.StreamFingerprint {
						present = true
						break
					}
				}
				if !present || !proposed[d.StreamName] {
					row.Missed = append(row.Missed, physical)
				}
			}
		}
		for _, stream := range inventory {
			physical := supplementalImpactPhysical{stream.Name, stream.AccountID, stream.ID}
			if proposed[stream.Name] {
				row.Matched = append(row.Matched, physical)
				if !selected[stream.Fingerprint] && !baseline[stream.Name] {
					row.Extra = append(row.Extra, physical)
				}
			}
			literal := supplementalLiteral(stream.Name)
			if selectedNames[literal] && !selected[stream.Fingerprint] {
				row.SameNameConflicts = appendUniqueReviewString(row.SameNameConflicts, stream.Name+" expands across unselected physical streams/accounts")
			}
			if selectedNames[literal] && deniedNames[literal] {
				row.SameNameConflicts = appendUniqueReviewString(row.SameNameConflicts, stream.Name+" has opposing saved outcomes; current schema cannot represent them")
			}
		}
		for name := range baseline {
			if !proposed[name] {
				row.ProtectedConflicts = append(row.ProtectedConflicts, "Previously matched name lost: "+name)
			}
		}
		for _, d := range primary {
			if d.ChannelID != c.ID {
				continue
			}
			if d.Decision == "confirmed" && !proposed[d.StreamName] {
				row.ProtectedConflicts = appendUniqueReviewString(row.ProtectedConflicts, "Saved primary confirmation is missing: "+d.StreamName)
			}
			if d.Decision == "denied" && proposed[d.StreamName] {
				row.ProtectedConflicts = appendUniqueReviewString(row.ProtectedConflicts, "Saved primary denial still matches: "+d.StreamName)
			}
		}
		sort.Strings(row.ProtectedConflicts)
		sort.Strings(row.SameNameConflicts)
		// Embedded aliases are shared by exported name in the unchanged consumer.
		// Include collateral additions/losses and protected conflicts even on a
		// sibling target that has no supplemental decision of its own. Its extras
		// enter the existing explicit channel-scoped review path, never auto-Deny.
		if len(saved) > 0 || len(row.Extra) > 0 || len(row.ProtectedConflicts) > 0 {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ChannelID < rows[j].ChannelID })
	return rows
}
