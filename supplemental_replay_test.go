package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"os"
	"testing"
	"time"
)

// Optional authorized private-copy test. Inputs and reports are never source
// fixtures; feedback is ground truth for this isolated temporary store only.
func runRetainedSupplementalReplay(t *testing.T, svc *builder.Service, draft *builder.Draft, streams []dispatcharr.Stream, primary []dispatcharr.Candidate, automatic dispatcharr.AutomaticResult, cfg appconfig.Config) {
	path := os.Getenv("SUPPLEMENTAL_REPLAY_MANIFEST")
	if path == "" {
		return
	}
	t.Helper()
	var manifest struct {
		Rows []struct {
			StationID   string `json:"stationId"`
			PlacementID string `json:"placementId"`
			RawName     string `json:"rawName"`
			Feedback    string `json:"feedback"`
			Physical    []struct {
				ID      int64 `json:"streamId"`
				Account int64 `json:"accountId"`
			} `json:"physical"`
		} `json:"rows"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	source := draft.SourceFingerprint
	before := svc.MatchDecisions(source)
	inventory := dispatcharr.ProjectIdentityInventory(streams)
	ds := &dispatcharrServer{}
	snapshot := dispatcharrMatchSnapshot{identityStreams: inventory, candidates: primary, fetchedAt: time.Now(), dispatcharrFingerprint: "retained-copy", lineupFingerprint: source}
	started := time.Now()
	candidates, complete := ds.prepareSupplemental(draft, cfg, snapshot, reviewInputsRevision{})
	retrieved := len(candidates)
	candidates = filterSupplementalAutomatic(candidates, automatic)
	elapsed := time.Since(started)
	if output := os.Getenv("SUPPLEMENTAL_RETRIEVAL_OUTPUT"); output != "" {
		data, err := json.Marshal(candidates)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if !complete {
		t.Fatal("retrieval incomplete")
	}
	key := func(station, placement string, id, account int64) string {
		return fmt.Sprintf("%s/%s/%d:%d", station, placement, account, id)
	}
	lookup := map[string]dispatcharr.SupplementalCandidate{}
	for _, c := range candidates {
		lookup[key(c.Target.StationID, c.Target.PlacementID, c.Stream.ID, c.Stream.AccountID)] = c
	}
	channelByID := map[string]builder.DraftChannel{}
	for _, c := range draft.Channels {
		channelByID[c.ID] = c
	}
	for _, c := range primary {
		target := channelByID[c.ChannelID]
		for _, stream := range inventory {
			if stream.Fingerprint == c.StreamHash {
				lookup[key(target.StationID, target.PlacementID, stream.ID, stream.AccountID)] = dispatcharr.SupplementalCandidate{Target: dispatcharr.SupplementalTarget{ID: target.ID, StationID: target.StationID, PlacementID: target.PlacementID, Number: target.Number, Name: target.Name, CallSign: target.CallSign}, Stream: stream, Rank: 1, Reason: "already primary", Evidence: "retained primary"}
			}
		}
	}
	missing := []string{}
	accepted := []builder.SupplementalDecision{}
	acceptedCount, reviewCount, reviewRetrieved := 0, 0, 0
	acceptedPrimary := 0
	targets40 := map[string]bool{}
	for _, row := range manifest.Rows {
		targets40[row.StationID+"/"+row.PlacementID] = true
		if row.Feedback == "review_requested" {
			reviewCount++
		}
		foundReview := false
		for _, physical := range row.Physical {
			c, found := lookup[key(row.StationID, row.PlacementID, physical.ID, physical.Account)]
			if row.Feedback == "review_requested" && found {
				foundReview = true
			}
			if row.Feedback != "accepted" {
				continue
			}
			acceptedCount++
			if !found {
				missing = append(missing, key(row.StationID, row.PlacementID, physical.ID, physical.Account)+" "+row.RawName)
				continue
			}
			if c.Reason == "already primary" {
				acceptedPrimary++
			}
			d := supplementalDecision(c, source, "retained-copy", "retained-snapshot")
			d.Decision = "confirmed"
			d.OperationID = "test-only-feedback"
			d.GroupKey = "supp:test:" + d.Key
			accepted = append(accepted, d)
		}
		if foundReview {
			reviewRetrieved++
		}
	}
	if len(missing) > 0 {
		t.Fatalf("accepted physical pairs missing %d/%d: %v", len(missing), acceptedCount, missing)
	}
	if acceptedCount != 44 || reviewCount != 17 || reviewRetrieved != 17 || len(targets40) != 40 {
		t.Fatalf("manifest acceptance counts physical=%d news=%d/%d targets=%d", acceptedCount, reviewRetrieved, reviewCount, len(targets40))
	}
	baselineJSON, _ := json.Marshal(builder.ExportFromDraft(draft))
	if err = svc.SaveSupplementalDecisions(source, svc.ReviewInputToken(), accepted, false, func(write func() error) error { return write() }); err != nil {
		t.Fatal(err)
	}
	svc.ProjectSupplementalDecisions(draft, source, "retained-copy")
	after := svc.MatchDecisions(source)
	old, _ := json.Marshal(before)
	now, _ := json.Marshal(after)
	denials := 0
	for _, d := range before {
		if d.Decision == "denied" && targets40[channelByID[d.ChannelID].StationID+"/"+channelByID[d.ChannelID].PlacementID] {
			denials++
		}
	}
	if string(old) != string(now) || denials != 15 {
		t.Fatalf("prior decisions changed/expected15: %d", len(before))
	}
	proposedJSON, _ := json.Marshal(builder.ExportFromDraft(draft))
	req := dispatcharr.ImpactRequest{Country: "US", Baseline: string(baselineJSON), Proposed: string(proposedJSON)}
	for _, s := range inventory {
		req.Streams = append(req.Streams, dispatcharr.ImpactStream{Name: s.Name, GroupName: s.GroupName})
	}
	result, err := dispatcharr.CheckSupplementalImpact(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	impact := compareSupplementalImpact(draft, inventory, svc.SupplementalDecisions(source), before, result, "retained-copy")
	// Run the 40-target consumer independently to expose cross-channel alias-map
	// interactions; the full 240-channel roundtrip remains the export authority.
	sub := *draft
	sub.Channels = nil
	for _, c := range draft.Channels {
		if targets40[c.StationID+"/"+c.PlacementID] {
			sub.Channels = append(sub.Channels, c)
		}
	}
	subset, _ := json.Marshal(builder.ExportFromDraft(&sub))
	req.Proposed = string(subset)
	req.Baseline = string(subset)
	subsetImpact, err := dispatcharr.CheckSupplementalImpact(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	report := map[string]any{"targets": 40, "channels": len(draft.Channels), "inventoryPhysical": len(inventory), "feedbackPhysical": acceptedCount, "acceptedRetrieved": len(accepted), "acceptedAlreadyPrimary": acceptedPrimary, "acceptedSupplementalOnly": len(accepted) - acceptedPrimary, "news12ReviewRetrieved": reviewRetrieved, "retrievalCandidates": len(candidates), "retrievedBeforeAutomaticFilter": retrieved, "automaticallyResolvedPairs": retrieved - len(candidates), "retrievalDurationMS": elapsed.Milliseconds(), "priorDecisionsUnchanged": len(before), "priorDenialsUnchanged": denials, "impact240": impact, "consumer240": result, "consumer40": subsetImpact, "export": json.RawMessage(proposedJSON), "missing": missing, "note": "Feedback applied only inside isolated temporary test store. No rejected, unendorsed or experimental mask auto-applied; full raw aliases can leave misses/extras."}
	output := os.Getenv("SUPPLEMENTAL_REPLAY_OUTPUT")
	if output == "" {
		t.Fatal("explicit private output required")
	}
	data, _ = json.MarshalIndent(report, "", "  ")
	if err = os.WriteFile(output, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("supplemental40/240: %d/44 physical accepted, %d/17 News12 review, %d proposals, retrieval %s, 15 decisions unchanged; consumer report %s", len(accepted), reviewRetrieved, len(candidates), elapsed, output)
}
