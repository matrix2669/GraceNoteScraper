package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os/exec"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	matcher "github.com/daniel-widrick/GraceNoteScraper/lineuparr_matcher"
)

func canonicalServerFixture(t *testing.T, confirm bool) *dispatcharrServer {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 required for unchanged consumer")
	}
	s, f := newDispatcharrTestServer(t, true)
	cfg, _, _ := s.lineup.store.Get()
	conn, _ := s.config.Get()
	s.lineup.state.UpdateForSource(&guide.TVGuide{LineupChannels: []guide.Channel{
		{ID: "59296", PlacementID: "wetv", ChannelNo: "649", CallSign: "WEHD"},
		{ID: "58690", PlacementID: "nhl", ChannelNo: "587", CallSign: "NHLHD"},
		{ID: "65687", PlacementID: "mgm", ChannelNo: "895", CallSign: "MGMHD"},
	}}, cfg.Fingerprint())
	f.streams = []dispatcharr.Stream{{ID: 196220, M3UAccountID: 11, Name: "WETV", GroupName: "US"}, {ID: 187821, M3UAccountID: 3, Name: "NHL  |  4K -", GroupName: "US"}, {ID: 2, M3UAccountID: 3, Name: "US| EPIX (24/7 MOVIES & SERIES)", GroupName: "US"}}
	s.matcher = dispatcharr.MatchStreamCandidatesWithLineuparr
	s.lineup.automaticProjection = s.projectAutomaticExclusions
	s.lineup.derivedRevision = s.derivedRevision
	if confirm {
		d := builder.MatchDecision{Key: "wetv-confirm", Decision: "confirmed", DispatcharrFingerprint: conn.Fingerprint(), StreamFingerprint: f.streams[0].Fingerprint(), StreamKey: f.streams[0].Key(), StreamID: 196220, M3UAccountID: 11, ChannelID: "wetv", StreamName: "WETV", Score: 100, NameScore: 100, MatcherVersion: matcher.Revision}
		if err := s.lineup.builder.SetMatchDecision(cfg.Fingerprint(), d); err != nil {
			t.Fatal(err)
		}
	}
	w := httptest.NewRecorder()
	if _, ok := s.buildReview(w, httptest.NewRequest("GET", "/review", nil), true); !ok {
		t.Fatal(w.Code, w.Body.String())
	}
	return s
}
func canonicalNames(t *testing.T, s *dispatcharrServer) (*builder.Draft, map[string]string) {
	t.Helper()
	w := httptest.NewRecorder()
	d, _, _, ok := s.lineup.buildDraft(w, httptest.NewRequest("GET", "/draft", nil))
	if !ok {
		t.Fatal(w.Code, w.Body.String())
	}
	names := map[string]string{}
	for _, c := range d.Channels {
		names[c.ID] = c.Name
	}
	return d, names
}
func TestCanonicalFullBuildValidatedWETVAndOrdinaryReviewLeads(t *testing.T) {
	s := canonicalServerFixture(t, true)
	cfg, _, _ := s.lineup.store.Get()
	beforeChoices := s.lineup.builder.ReviewStateToken()
	before, names := canonicalNames(t, s)
	if names["wetv"] != "WEHD" || names["nhl"] != "NHLHD" || names["mgm"] != "MGMHD" {
		t.Fatal("unvalidated names leaked", names)
	}
	if len(before.CanonicalNameProposals) != 3 {
		t.Fatalf("actual registry proposal count %d", len(before.CanonicalNameProposals))
	}
	s.validateCanonicalNames(context.Background())
	after, names := canonicalNames(t, s)
	if names["wetv"] != "WE tv" || names["nhl"] != "NHLHD" || names["mgm"] != "MGMHD" {
		t.Fatalf("unexpected gated names: %+v proof:%+v", names, s.canonical)
	}
	if s.lineup.builder.ReviewStateToken() != beforeChoices {
		t.Fatal("validation changed saved choices")
	}
	channels, _ := dispatcharrSnapshotChannels(after)
	if !dispatcharrInputsSubset(channels, s.snapshot.channels) || s.snapshot.version != matcher.Revision {
		t.Fatal("new name reused old primary scores")
	}
	if len(s.canonical.leads) < 2 {
		t.Fatalf("unreviewed effects not surfaced %+v", s.canonical.leads)
	}
	w := httptest.NewRecorder()
	review, ok := s.buildReview(w, httptest.NewRequest("GET", "/review", nil), false)
	if !ok {
		t.Fatal(w.Code, w.Body.String())
	}
	if review.response.Supplemental.CandidateStreamCount < 2 {
		t.Fatalf("canonical effects absent from ordinary review: %+v", review.response.Supplemental)
	}
	// Actual exported JSON, unchanged consumer, all included targets.
	request, b, p, err := canonicalRequest(before, after, s.canonical.base)
	if err != nil {
		t.Fatal(err)
	}
	result, err := dispatcharr.CheckSupplementalImpact(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]builder.DraftChannel{}
	for _, c := range before.Channels {
		targets[c.ID] = c
	}
	conn, _ := s.config.Get()
	deltas, err := canonicalImpactDeltas(b, p, result, s.snapshot.identityStreams, targets, cfg.Fingerprint(), conn.Fingerprint(), s.lineup.builder.MatchDecisions(cfg.Fingerprint()), nil)
	if err != nil || len(deltas["wetv"].Added) != 1 || !canonicalSafeDelta(deltas["wetv"]) || len(deltas["nhl"].Added) != 0 || len(deltas["mgm"].Added) != 0 {
		t.Fatalf("unsafe actual export %+v %v", deltas, err)
	}
	// Current proof is reused, not a subprocess on reads or recovery signals.
	calls := 0
	s.impactRunner = func(context.Context, dispatcharr.ImpactRequest) (dispatcharr.ImpactResult, error) {
		calls++
		return dispatcharr.ImpactResult{}, nil
	}
	s.validateCanonicalNames(context.Background())
	_, _ = canonicalNames(t, s)
	if calls != 0 {
		t.Fatal("repeat validation for same inputs")
	}
}
func TestCanonicalUnconfirmedNamesNeverExportAndConfirmRevalidates(t *testing.T) {
	s := canonicalServerFixture(t, false)
	s.validateCanonicalNames(context.Background())
	_, names := canonicalNames(t, s)
	if names["wetv"] != "WEHD" || names["nhl"] != "NHLHD" || names["mgm"] != "MGMHD" {
		t.Fatal("unconfirmed rename", names)
	}
	cfg, _, _ := s.lineup.store.Get()
	conn, _ := s.config.Get()
	stream := s.snapshot.safeStreams[0]
	d := builder.MatchDecision{Key: "confirmed", Decision: "confirmed", DispatcharrFingerprint: conn.Fingerprint(), StreamFingerprint: stream.Fingerprint(), StreamKey: stream.Key(), StreamID: stream.ID, M3UAccountID: stream.M3UAccountID, ChannelID: "wetv", StreamName: "WETV", Score: 100, NameScore: 100}
	if err := s.lineup.builder.SetMatchDecision(cfg.Fingerprint(), d); err != nil {
		t.Fatal(err)
	}
	s.validateCanonicalNames(context.Background())
	_, names = canonicalNames(t, s)
	if names["wetv"] != "WE tv" {
		t.Fatal("existing stream confirm did not resolve naming", names)
	}
}
func TestCanonicalStaleChoiceCannotPublishNames(t *testing.T) {
	s := canonicalServerFixture(t, true)
	s.validateCanonicalNames(context.Background())
	d, names := canonicalNames(t, s)
	if names["wetv"] != "WE tv" {
		t.Fatal(names)
	}
	cfg, _, _ := s.lineup.store.Get()
	if err := s.lineup.builder.SetAliasSuppressed(cfg.Fingerprint(), "wetv", "WE TV", true); err != nil {
		t.Fatal(err)
	}
	called := false
	current, err := s.commitAutomaticDraft(d, func() error { called = true; return nil })
	if current || err == nil || called {
		t.Fatal("stale renamed publication accepted")
	}
	_, names = canonicalNames(t, s)
	if names["wetv"] != "WEHD" {
		t.Fatal("suppressed transformed name leaked", names)
	}
	s.validateCanonicalNames(context.Background())
	_, names = canonicalNames(t, s)
	if names["wetv"] != "WEHD" {
		t.Fatal("suppression was reintroduced", names)
	}
}
func TestCanonicalFailedFinalConsumerPassKeepsOldSnapshot(t *testing.T) {
	s := canonicalServerFixture(t, true)
	before := snapshotIdentity(s.snapshot)
	calls := 0
	s.impactRunner = func(ctx context.Context, r dispatcharr.ImpactRequest) (dispatcharr.ImpactResult, error) {
		calls++
		result, err := dispatcharr.CheckSupplementalImpact(ctx, r)
		if calls == 2 && err == nil {
			for i := range result.Proposed {
				if result.Proposed[i].Name == "WE tv" {
					result.Proposed[i].Names = append(result.Proposed[i].Names, "NHL  |  4K -")
				}
			}
		}
		return result, err
	}
	s.validateCanonicalNames(context.Background())
	_, names := canonicalNames(t, s)
	if calls != 2 || names["wetv"] != "WEHD" || snapshotIdentity(s.snapshot) != before {
		t.Fatalf("aggregate conflict activated names: %d %+v", calls, names)
	}
}
func TestCanonicalGlobalAliasPoolCrossCategory(t *testing.T) {
	b := builder.ExportFile{Categories: map[string][]builder.ExportChannel{"Entertainment": {{ChannelID: "a", Name: "WEHD", Aliases: []string{"WE TV"}}}, "Sports": {{ChannelID: "b", Name: "WE tv", Aliases: []string{"Zqx Sports Only"}}}}}
	raw, _ := json.Marshal(b)
	var p builder.ExportFile
	_ = json.Unmarshal(raw, &p)
	entries := p.Categories["Entertainment"]
	entries[0].Name = "WE tv"
	p.Categories["Entertainment"] = entries
	bb, _ := json.Marshal(b)
	pp, _ := json.Marshal(p)
	result, err := dispatcharr.CheckSupplementalImpact(context.Background(), dispatcharr.ImpactRequest{Country: "US", Baseline: string(bb), Proposed: string(pp), Streams: []dispatcharr.ImpactStream{{Name: "Zqx Sports Only", GroupName: "US"}}})
	if err != nil {
		t.Fatal(err)
	}
	var before, after []string
	for _, r := range result.Baseline {
		if r.Category == "Entertainment" {
			before = r.Names
		}
	}
	for _, r := range result.Proposed {
		if r.Category == "Entertainment" {
			after = r.Names
		}
	}
	if len(before) != 0 || !reflect.DeepEqual(after, []string{"Zqx Sports Only"}) {
		t.Fatalf("regression setup differs %v %v", before, after)
	}
}
func TestCanonicalOrdinarySupplementalChoicesResolveEffects(t *testing.T) {
	for _, decision := range []string{"confirmed", "denied"} {
		t.Run(decision, func(t *testing.T) {
			s := canonicalServerFixture(t, false)
			s.validateCanonicalNames(context.Background())
			w := httptest.NewRecorder()
			review, ok := s.buildReview(w, httptest.NewRequest("GET", "/review", nil), false)
			if !ok {
				t.Fatal(w.Code, w.Body.String())
			}
			var selected *supplementalGroup
			for i := range review.response.Supplemental.Candidates {
				g := &review.response.Supplemental.Candidates[i]
				if g.ChannelID == "wetv" && g.Alias == "WETV" {
					selected = g
					break
				}
			}
			if selected == nil {
				t.Fatalf("no ordinary WETV choice: %+v", review.response.Supplemental)
			}
			body := supplementalSaveBody(review.response, *selected)
			body["decision"] = decision
			saved := supplementalRequest(t, s, "POST", "/decision", body)
			if saved.Code != 200 {
				t.Fatal(saved.Code, saved.Body.String())
			}
			s.validateCanonicalNames(context.Background())
			draft, names := canonicalNames(t, s)
			if names["wetv"] != "WE tv" {
				t.Fatalf("%s did not resolve name effects: %+v proof:%+v", decision, names, s.canonical)
			}
			// Deny remains a literal exclusion, Confirm remains an ordinary raw alias.
			for _, c := range draft.Channels {
				if c.ID == "wetv" {
					if decision == "denied" && !slices.Contains(c.ExcludedAliases, "WETV") {
						t.Fatalf("deny lost: %+v", c)
					}
					if decision == "confirmed" && !slices.Contains(c.Aliases, "WETV") {
						t.Fatalf("confirm lost: %+v", c)
					}
				}
			}
		})
	}
}
func TestCanonicalPreparationFailuresAndConcurrentChoices(t *testing.T) {
	for _, event := range []string{"consumer-error", "primary-error", "cancel", "choice", "inventory", "source"} {
		t.Run(event, func(t *testing.T) {
			s := canonicalServerFixture(t, true)
			before := snapshotIdentity(s.snapshot)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			s.impactRunner = func(ctx context.Context, req dispatcharr.ImpactRequest) (dispatcharr.ImpactResult, error) {
				calls++
				switch event {
				case "consumer-error":
					return dispatcharr.ImpactResult{}, errors.New("consumer failed")
				case "cancel":
					cancel()
				case "choice":
					cfg, _, _ := s.lineup.store.Get()
					if err := s.lineup.builder.SetAliasSuppressed(cfg.Fingerprint(), "wetv", "WE TV", true); err != nil {
						t.Fatal(err)
					}
				case "inventory":
					s.cache.mu.Lock()
					s.cache.digest = "new inventory"
					s.cache.fetchedAt = time.Now().Add(time.Second)
					s.cache.mu.Unlock()
				case "source":
					cfg, _, _ := s.lineup.store.Get()
					old := *s.lineup.state.Get()
					s.lineup.state.UpdateForSource(&old, cfg.Fingerprint())
				}
				return dispatcharr.CheckSupplementalImpact(ctx, req)
			}
			if event == "primary-error" {
				s.matcher = func(context.Context, string, string, []dispatcharr.MatchChannel, []dispatcharr.Stream, map[string]dispatcharr.Decision) (dispatcharr.CandidateSet, string, error) {
					return dispatcharr.CandidateSet{}, "", errors.New("primary failed")
				}
			}
			s.validateCanonicalNames(ctx)
			if snapshotIdentity(s.snapshot) != before {
				t.Fatal("failed or stale preparation replaced snapshot")
			}
			_, names := canonicalNames(t, s)
			if names["wetv"] != "WEHD" {
				t.Fatal("failed or stale proof activated", names)
			}
			if event == "consumer-error" || event == "primary-error" {
				if s.canonical == nil || !s.canonical.failed {
					t.Fatal("missing explicit unavailable proof")
				}
			}
			if calls > 1 {
				t.Fatalf("unbounded failed work: %d", calls)
			}
		})
	}
}
func TestCanonicalOnePrimaryPassAndRefreshPriority(t *testing.T) {
	s := canonicalServerFixture(t, true)
	calls := 0
	s.matcher = func(ctx context.Context, fp, country string, channels []dispatcharr.MatchChannel, streams []dispatcharr.Stream, decisions map[string]dispatcharr.Decision) (dispatcharr.CandidateSet, string, error) {
		calls++
		return dispatcharr.MatchStreamCandidatesWithLineuparr(ctx, fp, country, channels, streams, decisions)
	}
	s.validateCanonicalNames(context.Background())
	if calls != 1 {
		t.Fatalf("primary calls %d", calls)
	}
	s.beginImpactRefresh()
	defer s.endImpactRefresh()
	s.validateCanonicalNames(context.Background())
	if calls != 1 || s.canonical != nil {
		t.Fatal("background admission overlapped refresh")
	}
}
func TestCanonicalStatusCannotHideUnresolvedOrFailedEffects(t *testing.T) {
	s := canonicalServerFixture(t, true)
	s.validateCanonicalNames(context.Background())
	r := s.impactResponse()
	if r.State != "complete" || !r.Unresolved {
		t.Fatalf("hidden unresolved name effects: %+v", r)
	}
	cfg, _, _ := s.lineup.store.Get()
	if err := s.lineup.builder.SetAliasSuppressed(cfg.Fingerprint(), "wetv", "WE TV", true); err != nil {
		t.Fatal(err)
	}
	if got := s.impactResponse().State; got != "stale" {
		t.Fatal(got)
	}
	s = canonicalServerFixture(t, true)
	s.impactRunner = func(context.Context, dispatcharr.ImpactRequest) (dispatcharr.ImpactResult, error) {
		return dispatcharr.ImpactResult{}, errors.New("unavailable")
	}
	s.validateCanonicalNames(context.Background())
	if got := s.impactResponse().State; got != "failed" {
		t.Fatal(got)
	}
}
func TestCanonicalBackgroundCancellationReleasesRefresh(t *testing.T) {
	s := canonicalServerFixture(t, true)
	started := make(chan struct{})
	done := make(chan struct{})
	s.impactRunner = func(ctx context.Context, _ dispatcharr.ImpactRequest) (dispatcharr.ImpactResult, error) {
		close(started)
		<-ctx.Done()
		return dispatcharr.ImpactResult{}, ctx.Err()
	}
	before := snapshotIdentity(s.snapshot)
	go func() { defer close(done); s.validateCanonicalNames(context.Background()) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("validation did not start")
	}
	s.beginImpactRefresh()
	defer s.endImpactRefresh()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh did not cancel bounded work")
	}
	if s.canonical != nil || snapshotIdentity(s.snapshot) != before {
		t.Fatal("cancelled proof changed snapshot")
	}
}

func TestCanonicalAttestedHBOFeedLabelsReachActualExport(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 required for unchanged consumer")
	}
	s, f := newDispatcharrTestServer(t, true)
	cfg, _, _ := s.lineup.store.Get()
	s.lineup.state.UpdateForSource(&guide.TVGuide{LineupChannels: []guide.Channel{
		{ID: "59368", PlacementID: "east", ChannelNo: "800", CallSign: "HBOHTHD"},
		{ID: "59355", PlacementID: "west", ChannelNo: "801", CallSign: "HBOHHDP"},
		{ID: "53158", PlacementID: "local", ChannelNo: "2.1", CallSign: "WKTVDT", Affiliate: "NATIONAL BROADCASTING COMPANY"},
		{ID: "other", PlacementID: "subchannel", ChannelNo: "2.2", CallSign: "WKTVDT2", Affiliate: "NATIONAL BROADCASTING COMPANY"},
	}}, cfg.Fingerprint())
	f.streams = []dispatcharr.Stream{
		{ID: 1, M3UAccountID: 11, Name: "HBOHTHD", GroupName: "US"},
		{ID: 2, M3UAccountID: 11, Name: "HBO Hits - West", GroupName: "US"},
		{ID: 3, M3UAccountID: 11, Name: "WKTVDT", GroupName: "US"},
		{ID: 4, M3UAccountID: 11, Name: "WKTVDT2", GroupName: "US"},
	}
	s.matcher = dispatcharr.MatchStreamCandidatesWithLineuparr
	s.lineup.automaticProjection = s.projectAutomaticExclusions
	s.lineup.derivedRevision = s.derivedRevision
	w := httptest.NewRecorder()
	if _, ok := s.buildReview(w, httptest.NewRequest("GET", "/review", nil), true); !ok {
		t.Fatal(w.Code, w.Body.String())
	}
	before, initial := canonicalNames(t, s)
	if initial["east"] != "HBOHTHD" || initial["west"] != "HBOHHDP" {
		t.Fatalf("label escaped validation: %v", initial)
	}
	state := s.lineup.builder.ReviewStateToken()
	s.validateCanonicalNames(context.Background())
	after, names := canonicalNames(t, s)
	if !reflect.DeepEqual(names, map[string]string{"east": "HBO Hits", "west": "HBO Hits - West", "local": "WKTVDT", "subchannel": "WKTVDT2"}) {
		t.Fatalf("unexpected effective labels: %v (reasons: %v)", names, s.canonical.reasons)
	}
	if got := s.lineup.builder.ReviewStateToken(); got != state {
		t.Fatal("validation changed saved choices")
	}
	req, b, p, err := canonicalRequest(before, after, s.canonical.base)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := dispatcharr.CheckSupplementalImpact(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]builder.DraftChannel{}
	for _, c := range before.Channels {
		targets[c.ID] = c
	}
	conn, _ := s.config.Get()
	deltas, err := canonicalImpactDeltas(b, p, actual, s.snapshot.identityStreams, targets, cfg.Fingerprint(), conn.Fingerprint(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for id, d := range deltas {
		if len(d.Added)+len(d.Lost) != 0 {
			t.Fatalf("unexpected renamed export stream effect for %s: %+v", id, d)
		}
	}
}
