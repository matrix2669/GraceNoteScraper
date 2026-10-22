package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

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
}

func (s *dispatcharrServer) impactCurrent(job *supplementalImpactJob) bool {
	if job == nil || job.inputs != s.lineup.reviewInputsRevision() || job.state != s.lineup.builder.ReviewStateToken() || job.inventory != s.inventoryObservationToken() {
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
	var response supplementalImpactResponse
	if job != nil {
		response = job.response
	}
	s.impactMu.Unlock()
	if job == nil {
		return supplementalImpactResponse{State: "unchecked", Profile: dispatcharr.ImpactProfile, Message: "No supplemental impact has been checked in this process. Generation remains available."}
	}
	if !s.impactCurrent(job) {
		response.State = "stale"
		response.Message = "Inputs or decisions changed. This prior impact is stale; explicitly check again after your batch. Future inventory is not protected."
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
	inputs := s.lineup.reviewInputsRevision()
	state := s.lineup.builder.ReviewStateToken()
	draft, config, _, ok := s.lineup.buildDraft(w, r)
	if !ok {
		return
	}
	conn, connected := s.config.Get()
	s.reviewMu.RLock()
	snapshot := s.snapshot
	s.reviewMu.RUnlock()
	channels, _ := dispatcharrSnapshotChannels(draft)
	if !supplementalEvidenceCurrent(snapshot, inputs) || !connected || snapshot.warning != "" || snapshot.fetchedAt.IsZero() || snapshot.dispatcharrFingerprint != conn.Fingerprint() || snapshot.lineupFingerprint != config.Fingerprint() || snapshot.guide != s.lineup.state.GetForSource(config.Fingerprint()) || !dispatcharrInputsSubset(channels, snapshot.channels) || s.inventoryDrift(snapshot) {
		http.Error(w, "Refresh streams before checking supplemental export impact against a current successful inventory", 409)
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
	proposed, _ := json.Marshal(builder.ExportFromDraft(draft))
	before, _ := json.Marshal(builder.ExportFromDraft(&baseline))
	request := dispatcharr.ImpactRequest{Country: draft.CountryCode, Baseline: string(before), Proposed: string(proposed)}
	missingGroups := false
	for _, stream := range snapshot.identityStreams {
		request.Streams = append(request.Streams, dispatcharr.ImpactStream{Name: stream.Name, GroupName: stream.GroupName})
		if stream.GroupName == "" {
			missingGroups = true
		}
	}
	job := &supplementalImpactJob{inputs: inputs, snapshot: snapshotIdentity(snapshot), inventory: s.inventoryObservationToken(), state: state, connection: conn.Fingerprint()}
	key := supplementalHash([]any{request, job.snapshot, job.inventory, state, configurationToken(config), conn.Fingerprint(), inputs.index, inputs.tmdb, inputs.choices, fmt.Sprintf("%p", inputs.guide), matcher.Revision, dispatcharr.ImpactProfile, dispatcharr.SupplementalRevision})
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
	if s.impact != nil && s.impact.response.State == "running" {
		s.impact.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	job.cancel = cancel
	s.impact = job
	s.impactMu.Unlock()
	decisions := s.lineup.builder.SupplementalDecisions(config.Fingerprint())
	primary := s.lineup.builder.MatchDecisions(config.Fingerprint())
	initialResponse := job.response
	run := s.impactRunner
	if run == nil {
		run = dispatcharr.CheckSupplementalImpact
	}
	go func() {
		defer cancel()
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
		s.impactMu.Lock()
		defer s.impactMu.Unlock()
		if s.impact != job || job.response.State != "running" {
			return
		}
		if !current {
			job.response.State = "stale"
			job.response.Message = "Inputs changed during the check; result discarded. Check again after your batch."
			return
		}
		if err != nil {
			job.response.State = "failed"
			if ctx.Err() != nil {
				job.response.State = "cancelled"
			}
			job.response.Message = "Impact did not complete: " + err.Error() + ". No decisions or export changed; retry explicitly."
			return
		}
		job.response.State = "complete"
		job.response.Rows = rows
		job.response.CheckedAt = time.Now().UTC()
		job.response.Message = "Checked only this captured inventory and prospective JSON. Review selected, extra, missed and protected conflicts below. Reload review to Confirm or Deny newly exposed extras; every saved choice makes this impact stale. Full raw aliases remain fuzzy; equal raw names cannot split account outcomes. New future names can leak through and require another check. Profile: pinned Exact95, built-in/country/embedded aliases, specific numbering, quality filtering off; custom consumer settings can differ."
		if missingGroups {
			job.response.Message += " Some captured streams lack group-country metadata; country filtering may differ from the installed consumer. This is a profile-limited check, not a complete live-consumer guarantee."
		}
	}()
	writeLineuparrJSON(w, 202, initialResponse)
}
func compareSupplementalImpact(draft *builder.Draft, inventory []dispatcharr.IdentityStream, decisions map[string]builder.SupplementalDecision, primary map[string]builder.MatchDecision, result dispatcharr.ImpactResult, connection string) []supplementalImpactRow {
	key := func(name, number string) string { return name + "\x00" + number }
	before, after := map[string]map[string]bool{}, map[string]map[string]bool{}
	for _, phase := range []struct {
		rows []dispatcharr.ImpactMatch
		dest map[string]map[string]bool
	}{{result.Baseline, before}, {result.Proposed, after}} {
		for _, r := range phase.rows {
			set := map[string]bool{}
			for _, name := range r.Names {
				set[name] = true
			}
			phase.dest[key(r.Name, r.Number)] = set
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
		if len(saved) == 0 {
			continue
		}
		row := supplementalImpactRow{ChannelID: c.ID, ChannelName: c.Name, ChannelNumber: c.Number, Selected: []supplementalImpactPhysical{}, Matched: []supplementalImpactPhysical{}, Extra: []supplementalImpactPhysical{}, Missed: []supplementalImpactPhysical{}, ProtectedConflicts: []string{}, SameNameConflicts: []string{}}
		baseline, proposed := before[key(c.Name, c.Number)], after[key(c.Name, c.Number)]
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
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ChannelID < rows[j].ChannelID })
	return rows
}
