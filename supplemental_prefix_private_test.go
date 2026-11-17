package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

// Optional read-only replay against explicitly supplied sanitized private captures.
func TestPrefixPrivateCaptureUnchangedBeforeChoice(t *testing.T) {
	dir := os.Getenv("PREFIX_REPLAY_DIR")
	if dir == "" {
		t.Skip("no authorized private replay capture")
	}
	read := func(name string) []byte {
		b, e := os.ReadFile(dir + "/" + name)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	stateBytes := read("protected_decisions.json")
	var state builder.State
	if e := json.Unmarshal(stateBytes, &state); e != nil {
		t.Fatal(e)
	}
	if len(state.MatchDecisions) != 850 || len(state.SupplementalDecisions) != 185 {
		t.Fatalf("unexpected protected capture: %d/%d", len(state.MatchDecisions), len(state.SupplementalDecisions))
	}
	var draft builder.Draft
	if e := json.Unmarshal(read("active_targets.json"), &draft); e != nil {
		t.Fatal(e)
	}
	var raw []struct {
		ID        int64    `json:"id"`
		Name      string   `json:"name"`
		TVGID     string   `json:"tvg_id"`
		Account   int64    `json:"m3u_account_id"`
		Group     *int64   `json:"channel_group_id"`
		GroupName string   `json:"group_name"`
		Number    *float64 `json:"stream_chno"`
	}
	if e := json.Unmarshal(read("inventory.json"), &raw); e != nil {
		t.Fatal(e)
	}
	streams := make([]dispatcharr.Stream, 0, len(raw))
	for _, s := range raw {
		streams = append(streams, dispatcharr.Stream{ID: s.ID, Name: s.Name, TVGID: s.TVGID, M3UAccountID: s.Account, ChannelGroupID: s.Group, GroupName: s.GroupName, StreamChannelNo: s.Number})
	}
	before, e := json.Marshal(builder.ExportFromDraft(&draft))
	if e != nil {
		t.Fatal(e)
	}
	cfg := appconfig.Config{}
	cfg.Gracenote.Country = "US"
	server := &dispatcharrServer{}
	candidates, complete := server.prepareSupplemental(&draft, cfg, dispatcharrMatchSnapshot{identityStreams: dispatcharr.ProjectIdentityInventory(streams)}, reviewInputsRevision{})
	if !complete {
		t.Fatal("incomplete retrieval")
	}
	after, _ := json.Marshal(builder.ExportFromDraft(&draft))
	if string(before) != string(after) {
		t.Fatal("retrieval mutated export")
	}
	if !reflect.DeepEqual(stateBytes, read("protected_decisions.json")) {
		t.Fatal("protected capture mutated")
	}
	request := dispatcharr.ImpactRequest{Country: "US", Baseline: string(before), Proposed: string(after)}
	for _, s := range streams {
		request.Streams = append(request.Streams, dispatcharr.ImpactStream{Name: s.Name, GroupName: s.GroupName})
	}
	impact, e := dispatcharr.CheckSupplementalImpact(context.Background(), request)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(impact.Baseline, impact.Proposed) {
		t.Fatal("consumer outcomes changed before choices")
	}
	matched := map[string]map[string]bool{}
	exported := builder.ExportFromDraft(&draft)
	for _, row := range impact.Baseline {
		entries := exported.Categories[row.Category]
		if row.EntryIndex < 0 || row.EntryIndex >= len(entries) {
			t.Fatal("impact index outside export")
		}
		id := entries[row.EntryIndex].ChannelID
		matched[id] = map[string]bool{}
		for _, name := range row.Names {
			matched[id][name] = true
		}
	}
	physical := map[string]bool{}
	for _, stream := range streams {
		physical[stream.Fingerprint()] = true
	}
	priorDenied, baselineMisses := map[string]bool{}, map[string]bool{}
	classify := func(channel, name, fingerprint, decision string) {
		key := fmt.Sprintf("%s\x00%s", channel, fingerprint)
		present := physical[fingerprint] && matched[channel][name]
		if decision == "denied" && present {
			priorDenied[key] = true
		}
		if decision == "confirmed" && !present {
			baselineMisses[key] = true
		}
	}
	for _, d := range state.MatchDecisions {
		classify(d.ChannelID, d.StreamName, d.StreamFingerprint, d.Decision)
	}
	for _, d := range state.SupplementalDecisions {
		classify(d.ChannelID, d.StreamName, d.StreamFingerprint, d.Decision)
	}
	t.Logf("private capture: %d primary, %d supplemental decisions intact, %d streams, %d targets, %d proposals; newly matching denied physical pairs=0, previously matching denied physical pairs=%d, lost confirmations=0, baseline missing confirmations=%d; consumer revision %s", len(state.MatchDecisions), len(state.SupplementalDecisions), len(streams), len(draft.Channels), len(candidates), len(priorDenied), len(baselineMisses), impact.Revision)
}
