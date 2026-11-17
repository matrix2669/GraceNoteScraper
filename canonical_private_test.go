package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

// Opt-in historical projection replay. This is NOT a full Build reconstruction:
// the private capture contains rendered drafts, not all original source inputs.
// The ordinary full Build / rematch / rebuilt exclusions path has independent
// end-to-end fixtures in canonical_validation_test.go.
func TestCanonicalPrivateCapturedNameOnlySelection(t *testing.T) {
	dir := os.Getenv("CANONICAL_REPLAY_DIR")
	if dir == "" {
		t.Skip("private captured projection not requested")
	}
	read := func(name string, out any) {
		t.Helper()
		b, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil {
			t.Fatal(e)
		}
		if e = json.Unmarshal(b, out); e != nil {
			t.Fatal(e)
		}
	}
	var base, all builder.Draft
	read("draft.json", &base)
	read("resolved-east-policy.json", &all)
	var captured []struct {
		ID        int64    `json:"id"`
		Name      string   `json:"name"`
		TVGID     string   `json:"tvg_id"`
		AccountID int64    `json:"m3u_account_id"`
		GroupID   *int64   `json:"channel_group_id"`
		GroupName string   `json:"group_name"`
		Number    *float64 `json:"stream_chno"`
	}
	read("inventory.json", &captured)
	streams := make([]dispatcharr.Stream, 0, len(captured))
	for _, r := range captured {
		streams = append(streams, dispatcharr.Stream{ID: r.ID, Name: r.Name, TVGID: r.TVGID, M3UAccountID: r.AccountID, ChannelGroupID: r.GroupID, GroupName: r.GroupName, StreamChannelNo: r.Number})
	}
	var state struct {
		MatchDecisions        map[string]builder.MatchDecision        `json:"matchDecisions"`
		SupplementalDecisions map[string]builder.SupplementalDecision `json:"supplementalDecisions"`
	}
	read("protected_decisions.json", &state)
	connection := ""
	for _, d := range state.MatchDecisions {
		if connection != "" && connection != d.DispatcharrFingerprint {
			t.Fatal("mixed captured connections")
		}
		connection = d.DispatcharrFingerprint
	}
	snapshot := dispatcharrMatchSnapshot{identityStreams: dispatcharr.ProjectIdentityInventory(streams)}
	targets := map[string]builder.DraftChannel{}
	proposed := map[string]builder.DraftChannel{}
	for _, c := range base.Channels {
		targets[c.ID] = c
	}
	for _, c := range all.Channels {
		proposed[c.ID] = c
	}
	check := func(after *builder.Draft) map[string]canonicalDelta {
		req, b, p, err := canonicalRequest(&base, after, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		result, err := dispatcharr.CheckSupplementalImpact(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		deltas, err := canonicalImpactDeltas(b, p, result, snapshot.identityStreams, targets, base.SourceFingerprint, connection, state.MatchDecisions, state.SupplementalDecisions)
		if err != nil {
			t.Fatal(err)
		}
		return deltas
	}
	deltas := check(&all)
	selected, err := cloneCanonicalDraft(&base)
	if err != nil {
		t.Fatal(err)
	}
	held, admitted, unresolved := 0, 0, 0
	for i := range selected.Channels {
		c := &selected.Channels[i]
		p, ok := proposed[c.ID]
		if !ok || !c.Included || c.Name == p.Name {
			continue
		}
		d := deltas[c.ID]
		unresolved += len(d.Unresolved)
		if canonicalSafeDelta(d) {
			c.Name = p.Name
			c.NameSource = p.NameSource
			c.NameMethod = p.NameMethod
			admitted++
		} else {
			held++
		}
	}
	final := check(selected)
	additions := 0
	for id, d := range final {
		if !canonicalSafeDelta(d) {
			t.Fatalf("unsafe selected name-only outcome %s %+v", id, d)
		}
		additions += len(d.Added)
	}
	// This immutable historical fixture contains a saved WETV confirmation. Do
	// not silently treat a different/missing capture as evidence for that choice.
	wetv := final["59296358"]
	if len(wetv.Added) != 1 || wetv.Added[0].Name != "WETV" {
		t.Fatalf("captured WETV confirm not admitted: %+v", wetv)
	}
	t.Logf("historical name-only selection: %d included targets, %d streams; admitted %d names, held %d names, %d unresolved physical additions; selected JSON adds %d confirmed pairs, zero loss/deny/unapproved additions. Not full captured Build or automatic-exclusion reconstruction.", len(final), len(streams), admitted, held, unresolved, additions)
}
