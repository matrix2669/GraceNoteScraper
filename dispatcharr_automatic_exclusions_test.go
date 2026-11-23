package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

func automaticServer(t *testing.T) (*dispatcharrServer, *fakeDispatcharrAPI) {
	s, api := affiliateTestServer(t)
	s.lineup.automaticProjection = s.projectAutomaticExclusions
	s.lineup.commitDraft = s.commitAutomaticDraft
	return s, api
}
func automaticDraft(t *testing.T, s *dispatcharrServer) *builder.Draft {
	t.Helper()
	w := httptest.NewRecorder()
	d, _, _, ok := s.lineup.buildDraft(w, httptest.NewRequest("GET", "/", nil))
	if !ok {
		t.Fatal(w.Code, w.Body.String())
	}
	return d
}
func automaticReview(t *testing.T, s *dispatcharrServer) dispatcharrReviewResponse {
	t.Helper()
	w := httptest.NewRecorder()
	s.handleReview(w, httptest.NewRequest("GET", "/api/lineuparr/dispatcharr/review?limit=5000", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var d dispatcharrReviewResponse
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	return d
}
func TestAutomaticAffiliateViewExportAndSnapshotReplacement(t *testing.T) {
	s, api := automaticServer(t)
	config, _, _ := s.lineup.store.Get()
	token := s.lineup.builder.ReviewStateToken()
	review := automaticReview(t, s)
	if len(review.Automatic.Rows) != 8 || review.CandidateStreamCount != 1 {
		t.Fatal(review)
	}
	draft := automaticDraft(t, s)
	if draft.AutomaticExclusionCount != 8 || len(draft.Channels[0].AutomaticExcludedAliases) != 8 {
		t.Fatal(draft.Channels)
	}
	if s.lineup.builder.ReviewStateToken() != token || len(s.lineup.builder.MatchDecisions(config.Fingerprint())) != 0 {
		t.Fatal("derived view wrote manual state")
	}
	w := httptest.NewRecorder()
	s.lineup.handleExport(w, httptest.NewRequest("GET", "/api/lineuparr/export", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "excluded_aliases") {
		t.Fatal(w.Code, w.Body.String())
	}
	if api.streamCalls != 1 {
		t.Fatal("read-only paths fetched streams")
	}
	// Successful explicit refresh replaces the scope, removing disappeared names.
	api.streams = []dispatcharr.Stream{{ID: 21, Name: "US: CBS (WCBS)", M3UAccountID: 3}}
	snapshotReview(t, s, true)
	if d := automaticDraft(t, s); d.AutomaticExclusionCount != 0 {
		t.Fatal("removed streams retained", d)
	}
	api.streams = append(api.streams, dispatcharr.Stream{ID: 30, Name: "US: CBS (KTVT)", M3UAccountID: 7})
	snapshotReview(t, s, true)
	if d := automaticDraft(t, s); d.AutomaticExclusionCount != 1 {
		t.Fatal("new stream not recomputed", d)
	}
	s.clearCandidateCache()
	if d := automaticDraft(t, s); d.AutomaticExclusionCount != 0 || d.AutomaticMatchingStatus != "refresh-required" {
		t.Fatal("restart retained active automatic state", d)
	}
	if s.lineup.builder.ReviewStateToken() != token {
		t.Fatal("automatic refresh persisted decisions")
	}
}
func TestAutomaticConfirmationPreservedAndConflictsVisible(t *testing.T) {
	s, _ := automaticServer(t)
	cfg, _, _ := s.lineup.store.Get()
	c := s.snapshot.candidates[0]
	d := builder.MatchDecision{Key: c.Key, Decision: "confirmed", MatcherVersion: c.MatcherVersion, DispatcharrFingerprint: c.Source, StreamFingerprint: c.StreamHash, StreamKey: c.StreamKey, ChannelID: c.ChannelID, StreamName: c.StreamName, NormalizedAlias: c.NormalizedAlias, Score: 100, NameScore: 100}
	if err := s.lineup.builder.SetMatchDecision(cfg.Fingerprint(), d); err != nil {
		t.Fatal(err)
	}
	review := automaticReview(t, s)
	conflicts := 0
	for _, row := range review.Automatic.Rows {
		if row.Conflict {
			conflicts++
			if row.Exported {
				t.Fatal("confirmation overwritten")
			}
		}
	}
	if conflicts != 1 {
		t.Fatal(review.Automatic)
	}
	if draft := automaticDraft(t, s); draft.AutomaticExclusionCount != 7 {
		t.Fatal(draft.AutomaticExclusionCount)
	}
	if s.lineup.builder.MatchDecisions(cfg.Fingerprint())[c.Key].Decision != "confirmed" {
		t.Fatal("saved choice changed")
	}
	// Authentication/endpoint changes cannot silently reverse an explicit saved
	// constituent. History remains available for Undo; automatic evidence is new.
	d.DispatcharrFingerprint = "previous-connection"
	if err := s.lineup.builder.SetMatchDecision(cfg.Fingerprint(), d); err != nil {
		t.Fatal(err)
	}
	prior := automaticReview(t, s)
	priorConflicts := 0
	for _, row := range prior.Automatic.Rows {
		if row.Conflict {
			priorConflicts++
			if row.Exported {
				t.Fatal("prior confirmation excluded")
			}
		}
	}
	if priorConflicts != 1 || len(prior.Decisions) != 1 || prior.Decisions[0].Decision != "confirmed" {
		t.Fatal(prior)
	}
	if draft := automaticDraft(t, s); draft.AutomaticExclusionCount != 7 {
		t.Fatal("prior-source confirmation reversed", draft.AutomaticExclusionCount)
	}
	if err := s.lineup.builder.ClearMatchDecisions(cfg.Fingerprint(), []string{c.Key}); err != nil {
		t.Fatal(err)
	}
	if draft := automaticDraft(t, s); draft.AutomaticExclusionCount != 8 {
		t.Fatal("undo did not recompute", draft.AutomaticExclusionCount)
	}
}
func feedDraftChannel(id, station, code, label string) builder.DraftChannel {
	return builder.DraftChannel{ID: id, StationID: station, CallSign: code, Name: code, Number: id, Included: true,
		AliasEvidence:   []builder.AliasEvidence{{Value: label, Sources: []string{"catalog"}}},
		ServiceEvidence: []builder.ServiceIdentityEvidence{{Value: label, SourceID: "catalog", SourceRowID: "row-" + station, SourceRevision: "revision", RootStationID: station, BindingKind: "station-id-catalog-v1"}}}
}
func TestStationFeedCatalogGenericBindingsAndAmbiguity(t *testing.T) {
	east := feedDraftChannel("899", "east-station", "HBOHD", "HBO - East")
	west := feedDraftChannel("901", "west-station", "HBOHDP", "HBO - West")
	news := feedDraftChannel("100", "news-station", "N24PHD", "News24 - Pacific")
	brand := feedDraftChannel("101", "brand-station", "FSWHD", "Fox Sports West")
	draft := &builder.Draft{Channels: []builder.DraftChannel{east, west, news, brand}}
	feeds, codes := stationFeedCatalog(draft, appconfig.Config{})
	if feeds[west.ID].Region != "west" || feeds[east.ID].Region != "east" || feeds[news.ID].Region != "west" || feeds[brand.ID].Region != "unknown" {
		t.Fatal(feeds)
	}
	if codes["HBOHDP"].Region != "west" || codes["N24PHD"].Region != "west" {
		t.Fatal(codes)
	}
	// Identically named but non-station-bound or non-active facts are not proof.
	draft.Channels[1].ServiceEvidence = nil
	feeds, _ = stationFeedCatalog(draft, appconfig.Config{})
	if feeds[west.ID].Region != "unknown" {
		t.Fatal("missing provenance silently removed protection", feeds)
	}
	draft.Channels[1] = west
	opposite := west.ServiceEvidence[0]
	opposite.Value = "HBO - East"
	draft.Channels[1].ServiceEvidence = append(draft.Channels[1].ServiceEvidence, opposite)
	draft.Channels[1].AliasEvidence = append(draft.Channels[1].AliasEvidence, builder.AliasEvidence{Value: opposite.Value, Sources: []string{"catalog"}})
	feeds, _ = stationFeedCatalog(draft, appconfig.Config{})
	if feeds[west.ID].Region != "unknown" {
		t.Fatal("conflicting target proof lost hold", feeds)
	}
}
func TestAutomaticDraftLeaseRejectsGuideStateAndSnapshotChanges(t *testing.T) {
	for _, kind := range []string{"state", "guide", "snapshot"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := automaticServer(t)
			d := automaticDraft(t, s)
			cfg, _, _ := s.lineup.store.Get()
			switch kind {
			case "state":
				c := s.snapshot.candidates[0]
				if err := s.lineup.builder.SetMatchDecision(cfg.Fingerprint(), builder.MatchDecision{Key: c.Key, Decision: "denied", DispatcharrFingerprint: c.Source, StreamFingerprint: c.StreamHash, StreamKey: c.StreamKey, ChannelID: c.ChannelID, StreamName: c.StreamName, Score: 100, NameScore: 100}); err != nil {
					t.Fatal(err)
				}
			case "guide":
				s.lineup.state.UpdateForSource(&guide.TVGuide{}, cfg.Fingerprint())
			case "snapshot":
				snapshotReview(t, s, true)
			}
			wrote := false
			current, err := s.commitAutomaticDraft(d, func() error { wrote = true; return nil })
			if err == nil || current || wrote {
				t.Fatal("stale lease accepted", current, err, wrote)
			}
		})
	}
}
func TestAutomaticBuildBindsPreBuildStateAndLegacyExport(t *testing.T) {
	for _, kind := range []string{"state", "guide"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := automaticServer(t)
			orig := s.lineup.automaticProjection
			cfg, _, _ := s.lineup.store.Get()
			s.lineup.automaticProjection = func(d *builder.Draft, c appconfig.Config) {
				if kind == "guide" {
					s.lineup.state.UpdateForSource(&guide.TVGuide{}, cfg.Fingerprint())
				} else {
					x := s.snapshot.candidates[0]
					if err := s.lineup.builder.SetMatchDecision(cfg.Fingerprint(), builder.MatchDecision{Key: x.Key, Decision: "confirmed", DispatcharrFingerprint: x.Source, StreamFingerprint: x.StreamHash, StreamKey: x.StreamKey, ChannelID: x.ChannelID, StreamName: x.StreamName, Score: 100, NameScore: 100}); err != nil {
						t.Fatal(err)
					}
				}
				orig(d, c)
			}
			w := httptest.NewRecorder()
			s.lineup.handleExport(w, httptest.NewRequest("GET", "/api/lineuparr/export", nil))
			if w.Code != 409 || strings.Contains(w.Body.String(), "excluded_aliases") {
				t.Fatal("legacy export endorsed stale draft", w.Code, w.Body.String())
			}
		})
	}
}
func TestAutomaticPublishLeaseHoldsManualChoicesThroughCommit(t *testing.T) {
	s, _ := automaticServer(t)
	d := automaticDraft(t, s)
	cfg, _, _ := s.lineup.store.Get()
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := s.commitAutomaticDraft(d, func() error { close(started); <-release; return nil })
		done <- err
	}()
	<-started
	changed := make(chan error, 1)
	x := s.snapshot.candidates[0]
	go func() {
		changed <- s.lineup.builder.SetMatchDecision(cfg.Fingerprint(), builder.MatchDecision{Key: x.Key, Decision: "denied", DispatcharrFingerprint: x.Source, StreamFingerprint: x.StreamHash, StreamKey: x.StreamKey, ChannelID: x.ChannelID, StreamName: x.StreamName, Score: 100, NameScore: 100})
	}()
	select {
	case err := <-changed:
		t.Fatalf("manual state changed during publication: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-changed; err != nil {
		t.Fatal(err)
	}
	if _, err := s.commitAutomaticDraft(d, func() error { return errors.New("must not run") }); err == nil {
		t.Fatal("old lease reused")
	}
}

func automaticFeedFixture(t *testing.T, score int, denySavedScore int) (*dispatcharrServer, *builder.Draft) {
	t.Helper()
	s, _ := newDispatcharrTestServer(t, true)
	cfg, _, _ := s.lineup.store.Get()
	conn, _ := s.config.Get()
	g := &guide.TVGuide{LineupChannels: []guide.Channel{{ID: "east-station", PlacementID: "899", ChannelNo: "899", CallSign: "HBOHD"}, {ID: "west-station", PlacementID: "901", ChannelNo: "901", CallSign: "HBOHDP"}}}
	s.lineup.state.UpdateForSource(g, cfg.Fingerprint())
	draft := &builder.Draft{SourceFingerprint: cfg.Fingerprint(), CountryCode: "US", Channels: []builder.DraftChannel{feedDraftChannel("899", "east-station", "HBOHD", "HBO - East"), feedDraftChannel("901", "west-station", "HBOHDP", "HBO - West")}}
	candidates := []dispatcharr.Candidate{}
	for i, name := range []string{"US: HBO East HD", "US: HBO HD", "US: HBOHDP"} {
		c := dispatcharr.Candidate{StreamName: name, StreamHash: name, NormalizedAlias: dispatcharr.NormalizeAliasName(name), M3UAccountID: 3}
		c.Key = fmt.Sprint(i)
		c.ChannelID = "901"
		c.Source = conn.Fingerprint()
		c.NameScore = score
		c.Score = score
		c.StreamKey = c.Key
		c.ChannelName = "HBOHDP"
		c.ChannelNumber = "901"
		candidates = append(candidates, c)
	}
	if denySavedScore >= 0 {
		c := candidates[0]
		if err := s.lineup.builder.SetMatchDecision(cfg.Fingerprint(), builder.MatchDecision{Key: "old-deny", Decision: "denied", DispatcharrFingerprint: c.Source, StreamFingerprint: c.StreamHash, StreamKey: c.StreamKey, ChannelID: c.ChannelID, StreamName: c.StreamName, NameScore: denySavedScore, Score: denySavedScore}); err != nil {
			t.Fatal(err)
		}
	}
	channels, _ := dispatcharrSnapshotChannels(draft)
	s.snapshot = dispatcharrMatchSnapshot{guide: g, channels: channels, dispatcharrFingerprint: conn.Fingerprint(), lineupFingerprint: cfg.Fingerprint(), digest: dispatcharrInputDigest(channels), candidates: candidates, streamCount: 3, fetchedAt: time.Now().UTC(), version: "c766a6bb582ca8ef28ae9548d83af1e61f6cde5d"}
	s.projectAutomaticExclusions(draft, cfg)
	return s, draft
}
func TestUnqualifiedRegionalStationFactRemainsManual(t *testing.T) {
	for _, label := range []string{"Starz West", "News24 West", "Fox Sports West"} {
		channel := feedDraftChannel("one", "station", "CODE", label)
		feeds, codes := stationFeedCatalog(&builder.Draft{Channels: []builder.DraftChannel{channel}}, appconfig.Config{})
		if feeds["one"].Region != "unknown" || len(codes) != 0 {
			t.Fatal("unqualified brand promoted to feed", feeds, codes)
		}
		c := dispatcharr.Candidate{Key: "candidate", ChannelID: "one", StreamName: label, NameScore: 71}
		rows := dispatcharr.DeriveAutomaticExclusions([]dispatcharr.Candidate{c}, nil, feeds, codes, nil)
		if len(rows.Rows) != 1 || !rows.Rows[0].PendingReview || rows.Hidden[c.Key] {
			t.Fatal(rows)
		}
	}
}

func TestAutomaticFeedJSONConsumerFixture(t *testing.T) {
	fixtures := []map[string]any{}
	for _, score := range []int{0, 70, 71, 80, 94, 95, 100} {
		_, draft := automaticFeedFixture(t, score, -1)
		names := draft.Channels[1].ExcludedAliases
		if len(names) != 2 {
			t.Fatalf("score%d: exclusions %v", score, names)
		}
		fixtures = append(fixtures, map[string]any{"syntheticReviewNameScore": score, "lineup": builder.ExportFromDraft(draft)})
	}
	// A low-score old manual Deny must not disable the independent current rule.
	_, draft := automaticFeedFixture(t, 100, 70)
	if len(draft.Channels[1].AutomaticExcludedAliases) != 2 {
		t.Fatal("saved70/current100 denial created a coverage hole", draft.Channels[1])
	}
	// Actual unchanged consumer returns 71 for this unresolved Pacific candidate.
	s, comedy := automaticFeedFixture(t, 71, -1)
	comedy.Channels[1] = feedDraftChannel("901", "west-station", "HBOCPHD", "HBO Comedy - West")
	c := s.snapshot.candidates[1]
	c.StreamName = "TV| HBO COMEDY ᴿᴬᵂ"
	c.StreamHash = c.StreamName
	c.NormalizedAlias = dispatcharr.NormalizeAliasName(c.StreamName)
	opposing := c
	opposing.Key = "actual71-opposing"
	opposing.StreamName = "HBO COMEDY RAW EAST"
	opposing.StreamHash = opposing.StreamName
	opposing.NormalizedAlias = dispatcharr.NormalizeAliasName(opposing.StreamName)
	s.snapshot.candidates = []dispatcharr.Candidate{c, opposing}
	s.snapshot.channels, _ = dispatcharrSnapshotChannels(comedy)
	s.snapshot.digest = dispatcharrInputDigest(s.snapshot.channels)
	comedy.AutomaticExclusionCount = 0
	cfg, _, _ := s.lineup.store.Get()
	s.projectAutomaticExclusions(comedy, cfg)
	if len(comedy.Channels[1].AutomaticExcludedAliases) != 2 {
		t.Fatal("actual71 unresolved feed not held", comedy.Channels[1])
	}
	_, evidence := s.automaticView(comedy, cfg, s.snapshot)
	pending, definite := 0, 0
	for _, row := range evidence.Rows {
		if row.PendingReview {
			pending++
		} else {
			definite++
		}
	}
	if pending != 1 || definite != 1 {
		t.Fatal("actual71 unknown/opposing classification", evidence)
	}
	fixtures = append(fixtures, map[string]any{"case": "actual-consumer-71", "lineup": builder.ExportFromDraft(comedy)})
	if output := os.Getenv("AUTOMATIC_EXPORT_FIXTURE"); output != "" {
		data, err := json.Marshal(fixtures)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAutomaticPendingRawNameSurvivesOldGroupConfirmation(t *testing.T) {
	s, _ := automaticFeedFixture(t, 100, -1)
	cfg, _, _ := s.lineup.store.Get()
	s.lineup.automaticProjection = func(d *builder.Draft, c appconfig.Config) {
		for i := range d.Channels {
			if d.Channels[i].CallSign == "HBOHDP" {
				proof := feedDraftChannel(d.Channels[i].ID, d.Channels[i].StationID, "HBOHDP", "HBO - West")
				d.Channels[i].AliasEvidence = proof.AliasEvidence
				d.Channels[i].ServiceEvidence = proof.ServiceEvidence
			}
		}
		s.projectAutomaticExclusions(d, c)
	}
	actual := automaticDraft(t, s)
	s.snapshot.channels, _ = dispatcharrSnapshotChannels(actual)
	s.snapshot.digest = dispatcharrInputDigest(s.snapshot.channels)
	c := s.snapshot.candidates[1]
	if err := s.lineup.builder.SetMatchDecision(cfg.Fingerprint(), builder.MatchDecision{Key: "old", Decision: "confirmed", DispatcharrFingerprint: c.Source, ChannelID: c.ChannelID, StreamName: "US: HBO", StreamFingerprint: "old-hash", StreamKey: "old-stream", NormalizedAlias: dispatcharr.NormalizeAliasName("US: HBO"), Score: 100, NameScore: 100}); err != nil {
		t.Fatal(err)
	}
	review := automaticReview(t, s)
	var key string
	for _, group := range review.Candidates {
		if strings.Contains(strings.Join(group.StreamNames, " | "), "HBO HD") {
			key = group.Key
		}
	}
	if key == "" {
		t.Fatalf("new unresolved constituent hidden: %+v", review)
	}
	body, _ := json.Marshal(map[string]any{"key": key, "decision": "confirmed", "tvgIds": []string{}})
	req := httptest.NewRequest("POST", "/api/lineuparr/dispatcharr/decision", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handleDecision(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	found := false
	for _, d := range s.lineup.builder.MatchDecisions(cfg.Fingerprint()) {
		if d.StreamName == c.StreamName && d.Decision == "confirmed" {
			found = true
		}
	}
	if !found {
		t.Fatal("new raw confirmation missing")
	}
	if s.lineup.builder.MatchDecisions(cfg.Fingerprint())["old"].Decision != "confirmed" {
		t.Fatal("old choice changed")
	}
}
