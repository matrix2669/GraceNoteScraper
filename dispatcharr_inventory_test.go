package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

func inventoryTestServer(t *testing.T) (*dispatcharrServer, *fakeDispatcharrAPI, *int) {
	t.Helper()
	s, api := newDispatcharrTestServer(t, true)
	s.lineup.automaticProjection = s.projectAutomaticExclusions
	s.lineup.commitDraft = s.commitAutomaticDraft
	s.lineup.afterBuilderReset = s.clearCandidateCache
	cfg, _, _ := s.lineup.store.Get()
	s.lineup.state.UpdateForSource(&guide.TVGuide{LineupChannels: []guide.Channel{{ID: "59303", PlacementID: "travel", ChannelNo: "670", CallSign: "TRAVHD"}}}, cfg.Fingerprint())
	api.streams = []dispatcharr.Stream{{ID: 1, Name: "US: TRAVEL CHANNEL HD", M3UAccountID: 3}, {ID: 2, Name: "US: TRAVEL CHANNEL WEST HD", M3UAccountID: 3}, {ID: 3, Name: "US| TRAVEL CHANNEL WEST HD", M3UAccountID: 7}}
	wantChannels, _ := dispatcharrSnapshotChannels(automaticDraft(t, s))
	wantStreams := append([]dispatcharr.Stream(nil), api.streams...)
	runs := 0
	s.matcher = func(_ context.Context, source, country string, channels []dispatcharr.MatchChannel, streams []dispatcharr.Stream, decisions map[string]dispatcharr.Decision) (dispatcharr.CandidateSet, string, error) {
		runs++
		if country != "US" || decisions != nil || !reflect.DeepEqual(channels, wantChannels) || !reflect.DeepEqual(streams, wantStreams) {
			t.Fatal("matcher inputs changed")
		}
		stream := streams[0]
		c := dispatcharr.Candidate{Key: "scored", ChannelID: channels[0].ID, ChannelName: channels[0].Name, ChannelNumber: channels[0].Number, StreamName: stream.Name, StreamID: stream.ID, StreamKey: stream.Key(), StreamHash: stream.Fingerprint(), M3UAccountID: stream.M3UAccountID, Source: source, Score: 94, NameScore: 94, Reason: "real fixture score", MatcherVersion: "c766a6bb582ca8ef28ae9548d83af1e61f6cde5d"}
		return dispatcharr.CandidateSet{All: []dispatcharr.Candidate{c}}, c.MatcherVersion, nil
	}
	snapshotReview(t, s, true)
	return s, api, &runs
}
func TestInventorySnapshotOnePassUnchangedAndDeterministicExport(t *testing.T) {
	s, api, runs := inventoryTestServer(t)
	token := s.lineup.builder.ReviewStateToken()
	if len(s.snapshot.candidates) != 1 || s.snapshot.candidates[0].Score != 94 || s.snapshot.candidates[0].Reason != "real fixture score" || len(s.snapshot.identityStreams) != 3 {
		t.Fatal("snapshot altered matcher result")
	}
	for i := 0; i < 3; i++ {
		r := automaticReview(t, s)
		inventory := 0
		for _, row := range r.Automatic.Rows {
			if row.InventoryOnly {
				inventory++
				if row.PendingReview {
					t.Fatal("unscored review item")
				}
			}
		}
		if inventory != 2 || r.CandidateStreamCount != 1 {
			t.Fatalf("inventory %d candidates %d", inventory, r.CandidateStreamCount)
		}
	}
	a := automaticDraft(t, s)
	b := automaticDraft(t, s)
	ab, _ := json.Marshal(builder.ExportFromDraft(a))
	bb, _ := json.Marshal(builder.ExportFromDraft(b))
	if string(ab) != string(bb) || a.AutomaticExclusionCount != 3 {
		t.Fatal("unstable export", string(ab), string(bb))
	}
	if *runs != 1 || api.streamCalls != 1 || token != s.lineup.builder.ReviewStateToken() {
		t.Fatal("read rescored/fetched/wrote state")
	}
	api.streams[1].Name = "changed outside snapshot"
	if s.snapshot.identityStreams[1].Name != "US: TRAVEL CHANNEL WEST HD" {
		t.Fatal("inventory was mutable")
	}
	s.cache.observe(s.snapshot.dispatcharrFingerprint, api.streams)
	if automaticDraft(t, s).AutomaticExclusionCount != 0 {
		t.Fatal("drift used stale inventory")
	}
	wrote := false
	if _, err := s.commitAutomaticDraft(a, func() error { wrote = true; return nil }); err == nil || wrote {
		t.Fatal("stale export wrote")
	}
	fresh := automaticDraft(t, s)
	if _, err := s.commitAutomaticDraft(fresh, func() error { return nil }); err != nil {
		t.Fatal("known drift ceased to be advisory", err)
	}
}
func TestInventoryFailedRefreshAndConcurrentExportFence(t *testing.T) {
	s, api, runs := inventoryTestServer(t)
	before := automaticDraft(t, s)
	old := append([]dispatcharr.IdentityStream(nil), s.snapshot.identityStreams...)
	api.streams = append(api.streams, dispatcharr.Stream{ID: 4, Name: "TV: TRAVEL CHANNEL WEST RAW", M3UAccountID: 7})
	s.matcher = func(context.Context, string, string, []dispatcharr.MatchChannel, []dispatcharr.Stream, map[string]dispatcharr.Decision) (dispatcharr.CandidateSet, string, error) {
		return dispatcharr.CandidateSet{}, "", errors.New("isolated failure")
	}
	snapshotReview(t, s, true)
	if !reflect.DeepEqual(s.snapshot.identityStreams, old) || automaticDraft(t, s).AutomaticExclusionCount != 0 {
		t.Fatal("failed refresh advanced identity evidence")
	}
	if _, err := s.commitAutomaticDraft(before, func() error { t.Fatal("old draft published"); return nil }); err == nil {
		t.Fatal("failed refresh lease accepted")
	}
	if *runs != 1 {
		t.Fatal("unexpected original matcher invocation")
	}
	s.refreshMu.Lock()
	if _, err := s.commitAutomaticDraft(automaticDraft(t, s), func() error { t.Fatal("raced active refresh"); return nil }); err == nil {
		t.Fatal("refresh not fenced")
	}
	s.refreshMu.Unlock()
}
func TestInventoryResetRestoreInvalidateWithoutReacquisition(t *testing.T) {
	for _, withdraw := range []bool{false, true} {
		s, api, runs := inventoryTestServer(t)
		cfg, _, _ := s.lineup.store.Get()
		included := true
		if err := s.lineup.builder.UpdateChannel(cfg.Fingerprint(), s.snapshot.channels[0].ID, builder.ChannelUpdate{Included: &included}); err != nil {
			t.Fatal(err)
		}
		before := automaticDraft(t, s)
		reset := func(restore string) string {
			body, _ := json.Marshal(map[string]any{"sourceFingerprint": cfg.Fingerprint(), "token": s.lineup.builder.ReviewStateToken(), "confirm": true, "restore": restore, "withdrawPublished": withdraw})
			req := httptest.NewRequest("POST", "/api/lineuparr/reset", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			s.lineup.handleBuilderReset(w, req)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			var response struct {
				Backup string `json:"backup"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			return response.Backup
		}
		backup := reset("")
		reset(backup)
		if len(s.snapshot.identityStreams) != 0 || automaticDraft(t, s).AutomaticExclusionCount != 0 || *runs != 1 || api.streamCalls != 1 {
			t.Fatal("reset/restore retained or reacquired snapshot")
		}
		if _, err := s.commitAutomaticDraft(before, func() error { t.Fatal("pre-reset export wrote"); return nil }); err == nil {
			t.Fatal("reset lease accepted")
		}
	}
}
