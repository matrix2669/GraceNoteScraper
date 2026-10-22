package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	matcher "github.com/daniel-widrick/GraceNoteScraper/lineuparr_matcher"
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/tmdb"
	"github.com/daniel-widrick/GraceNoteScraper/web"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

type noReviewNetwork struct{}

func (noReviewNetwork) FindProviders(context.Context, string, string, string) (*web.ProviderResponse, error) {
	panic("unexpected provider request")
}
func (noReviewNetwork) FetchGrid(context.Context, web.Preferences, int64) (*web.GridResponse, error) {
	panic("unexpected grid request")
}
func (noReviewNetwork) RoundTrip(*http.Request) (*http.Response, error) {
	panic("unexpected HTTP request")
}

// This optional fixture reads copies only. No private content is embedded in the
// test or its receipts. The original 404 captured scored rows retain their raw
// metadata; the extra 150 neutral scored rows are explicitly synthetic.
func retainedReviewBurstFixture(t *testing.T) (*dispatcharrServer, []string, map[string]any) {
	t.Helper()
	root := os.Getenv("REVIEW_RETAINED_ROOT")
	if root == "" {
		t.Skip("isolated retained fixture not supplied")
	}
	read := func(path string, dst any) {
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if e = json.Unmarshal(b, dst); e != nil {
			t.Fatal(e)
		}
	}
	private := filepath.Join(root, "retained-private")
	temp := t.TempDir()
	cp := func(name string) string {
		b, e := os.ReadFile(filepath.Join(private, name))
		if e != nil {
			t.Fatal(e)
		}
		p := filepath.Join(temp, name)
		if e = os.WriteFile(p, b, 0600); e != nil {
			t.Fatal(e)
		}
		return p
	}
	cs, e := appconfig.LoadStore(cp("config.json"))
	if e != nil {
		t.Fatal(e)
	}
	config, ok, _ := cs.Get()
	if !ok {
		t.Fatal("unconfigured fixture")
	}
	bs, e := builder.LoadStateStore(cp("lineuparr_state.json"))
	if e != nil {
		t.Fatal(e)
	}
	svc := builder.NewService(bs, builder.ServiceOptions{CacheDir: filepath.Join(temp, "sources"), HTTPClient: &http.Client{Transport: noReviewNetwork{}}, UseEmbeddedCatalogs: true})
	var envelope struct {
		SourceFingerprint string        `json:"source_fingerprint"`
		Guide             guide.TVGuide `json:"guide"`
	}
	read(filepath.Join(private, "guide_cache.json"), &envelope)
	if envelope.SourceFingerprint != config.Fingerprint() {
		t.Fatal("source mismatch")
	}
	state := &GuideState{}
	state.UpdateForSource(&envelope.Guide, config.Fingerprint())
	ix, e := lineupindex.NewService(lineupindex.ServiceConfig{Path: filepath.Join(private, "market_index.json"), SnapshotDir: filepath.Join(temp, "no-snapshots"), Providers: noReviewNetwork{}, Grids: noReviewNetwork{}})
	if e != nil {
		t.Fatal(e)
	}
	tc := tmdb.NewClient("diagnostic-only-invalid", filepath.Join(private, "tmdb_cache.json"))
	ls := &lineuparrServer{store: cs, state: state, builder: svc, marketIndex: ix, tmdbCachedEvidence: tc.CachedCategoryEvidence, tmdbEvidenceRevision: tc.CategoryEvidenceRevision, tmdbWhileEvidence: tc.WhileCategoryEvidenceRevision}
	dc, e := dispatcharr.LoadConfigStore(filepath.Join(temp, "dispatcharr.json"))
	if e != nil {
		t.Fatal(e)
	}
	if e = dc.Save(dispatcharr.Config{BaseURL: "https://fixture.invalid", Username: "fixture", Password: "fixture"}); e != nil {
		t.Fatal(e)
	}
	connection, _ := dc.Get()
	ds := &dispatcharrServer{lineup: ls, config: dc, client: &fakeDispatcharrAPI{}}
	ls.automaticProjection = ds.projectAutomaticExclusions
	ls.commitDraft = ds.commitAutomaticDraft
	ls.derivedRevision = ds.derivedRevision
	var captured struct {
		Streams []dispatcharr.Stream `json:"streams"`
	}
	read(filepath.Join(root, "..", "dryrun-20261021", "repair-input.private.json"), &captured)
	var retained struct {
		Derived dispatcharr.AutomaticResult `json:"derived"`
	}
	read(filepath.Join(root, "..", "dryrun-20261021", "f8c0189-output.private.json"), &retained)

	w0 := httptest.NewRecorder()
	draft, _, _, built := ls.buildDraft(w0, httptest.NewRequest("GET", "/api/lineuparr/draft", nil))
	if !built {
		t.Fatalf("draft status %d", w0.Code)
	}
	channels, _ := dispatcharrSnapshotChannels(draft)
	streams := map[string]dispatcharr.Stream{}
	for _, st := range captured.Streams {
		streams[st.Key()] = st
	}
	cands := []dispatcharr.Candidate{}
	for _, row := range retained.Derived.Rows {
		c := row.Candidate
		st, ok := streams[fmt.Sprintf("%d:%d", c.M3UAccountID, c.StreamID)]
		if !ok {
			continue
		}
		c.Source = connection.Fingerprint()
		c.StreamKey = st.Key()
		c.StreamHash = st.Fingerprint()
		c.NameScore = c.Score
		c.NormalizedAlias = dispatcharr.NormalizeStreamAlias(st)
		c.ReviewGeneration = "probe-generation"
		c.MatcherVersion = matcher.Revision
		cands = append(cands, c)
	}
	// Captured safe scored subset plus 150 explicitly synthetic neutral groups:
	// no matcher is invoked and synthetic scores are never production evidence.
	feeds, _ := stationFeedCatalog(draft, config)
	var target dispatcharr.MatchChannel
	for _, ch := range channels {
		if feeds[ch.ID].Region == "" {
			target = ch
			break
		}
	}
	if target.ID == "" {
		t.Fatal("missing neutral fixture target")
	}
	for i := 0; i < 150; i++ {
		st := dispatcharr.Stream{ID: int64(-i - 1), Name: fmt.Sprintf("Diagnostic Fixture %d", i), M3UAccountID: -1}
		cands = append(cands, dispatcharr.Candidate{Key: fmt.Sprintf("diagnostic-%03d", i), Source: connection.Fingerprint(), StreamKey: st.Key(), StreamHash: st.Fingerprint(), StreamID: st.ID, StreamName: st.Name, NormalizedAlias: dispatcharr.NormalizeStreamAlias(st), M3UAccountID: -1, ChannelID: target.ID, ChannelNumber: target.Number, ChannelName: target.Name, Score: 100, NameScore: 100, ReviewGeneration: "probe-generation", MatcherVersion: matcher.Revision})
	}
	ds.snapshot = dispatcharrMatchSnapshot{guide: &envelope.Guide, dispatcharrFingerprint: connection.Fingerprint(), lineupFingerprint: config.Fingerprint(), channels: channels, digest: dispatcharrInputDigest(channels), candidates: cands, identityStreams: dispatcharr.ProjectIdentityInventory(captured.Streams), streamCount: len(captured.Streams), inventoryDigest: streamInventoryDigest(captured.Streams), fetchedAt: time.Now().UTC(), version: matcher.Revision}
	ds.cache.observe(connection.Fingerprint(), captured.Streams)
	getReview := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		ds.handleReview(w, httptest.NewRequest("GET", "/api/lineuparr/dispatcharr/review?limit=100", nil))
		return w
	}
	w := getReview()
	if w.Code != 200 {
		t.Fatalf("review status %d", w.Code)
	}
	keys := []string{}
	for i := 0; i < 100; i++ {
		needle := fmt.Sprintf("diagnostic-%03d", i)
		for key, ids := range ds.review.groups {
			for _, id := range ids {
				if id == needle {
					keys = append(keys, key)
				}
			}
		}
	}
	if len(keys) != 100 {
		t.Fatalf("synthetic groups %d", len(keys))
	}

	return ds, keys, map[string]any{"inventoryStreams": len(captured.Streams), "guidePrograms": len(envelope.Guide.Programs), "includedTargets": len(channels), "capturedScoredSubset": len(cands) - 150, "syntheticCandidates": 150, "retainedDecisionsBefore": 820}
}
func postReviewChoice(s *dispatcharrServer, key, decision string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]any{"key": key, "decision": decision, "tvgIds": []string{}})
	r := httptest.NewRequest("POST", "/api/lineuparr/dispatcharr/decision", strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Builder-Generation", fmt.Sprint(s.lineup.builder.ResetGeneration()))
	w := httptest.NewRecorder()
	s.lineup.guardBuilder(s.handleDecision)(w, r)
	return w
}
func TestRetainedReviewWholeHandlers(t *testing.T) {
	if os.Getenv("REVIEW_RETAINED_ROOT") == "" {
		t.Skip("isolated retained fixture not supplied")
	}
	receipts := []map[string]any{}
	for _, count := range []int{20, 50, 100} {
		cold := time.Now()
		s, keys, dimensions := retainedReviewBurstFixture(t)
		coldMS := float64(time.Since(cold)) / float64(time.Millisecond)
		times := []float64{}
		start := time.Now()
		// Holding every derived builder mutex proves that warm POST does not enter
		// guide preparation, draft rebuilding, or automatic inventory projection.
		s.lineup.preparationMu.Lock()
		s.lineup.draftMu.Lock()
		s.automaticMu.Lock()
		done := make(chan error, 1)
		go func() {
			for i, key := range keys[:count] {
				choice := "denied"
				if i%2 == 0 {
					choice = "confirmed"
				}
				at := time.Now()
				w := postReviewChoice(s, key, choice)
				times = append(times, float64(time.Since(at))/float64(time.Millisecond))
				if w.Code != 200 {
					done <- fmt.Errorf("POST %d status %d", i, w.Code)
					return
				}
			}
			done <- nil
		}()
		var err error
		select {
		case err = <-done:
		case <-time.After(15 * time.Second):
			err = fmt.Errorf("warm writes entered a derived path or exceeded bounded drain")
		}
		s.automaticMu.Unlock()
		s.lineup.draftMu.Unlock()
		s.lineup.preparationMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		drain := float64(time.Since(start)) / float64(time.Millisecond)
		at := time.Now()
		review := automaticReview(t, s)
		refill := float64(time.Since(at)) / float64(time.Millisecond)
		at = time.Now()
		_ = automaticDraft(t, s)
		draftMS := float64(time.Since(at)) / float64(time.Millisecond)
		if count == 100 {
			w := httptest.NewRecorder()
			s.handleReview(w, httptest.NewRequest("GET", "/api/lineuparr/dispatcharr/review?limit=100", nil))
			if w.Code != 200 {
				t.Fatal("final comparison review failed")
			}
			if err := os.WriteFile(filepath.Join(os.Getenv("REVIEW_RETAINED_ROOT"), "throughput-final-response.private.json"), w.Body.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
		}
		ordered := append([]float64(nil), times...)
		sort.Float64s(ordered)
		p95 := ordered[(95*len(ordered)+99)/100-1]
		if p95 >= 100 {
			t.Fatalf("warm p95 %.2fms exceeds100ms", p95)
		}
		if count == 100 && drain >= 24423.3 {
			t.Fatalf("100 save drain %.2fms fails90percent reduction", drain)
		}
		receipts = append(receipts, map[string]any{"count": count, "dimensions": dimensions, "coldSetupMS": coldMS, "drainMS": drain, "p95MS": p95, "acknowledgementsMS": times, "refillMS": refill, "reconciliationMS": draftMS, "remaining": review.CandidateCount})
	}
	if root := os.Getenv("REVIEW_RETAINED_ROOT"); root != "" {
		data, _ := json.MarshalIndent(receipts, "", "  ")
		if err := os.WriteFile(filepath.Join(root, "throughput-after.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func syntheticReviewBurstFixture(t *testing.T) (*dispatcharrServer, []string, map[string]any) {
	s, _ := newDispatcharrTestServer(t, true)
	config, _, _ := s.lineup.store.Get()
	conn, _ := s.config.Get()
	draft := automaticDraft(t, s)
	channels, _ := dispatcharrSnapshotChannels(draft)
	target := channels[0]
	candidates := []dispatcharr.Candidate{}
	streams := []dispatcharr.Stream{}
	for i := 0; i < 150; i++ {
		st := dispatcharr.Stream{ID: int64(i + 1), Name: fmt.Sprintf("Fixture %03d", i), M3UAccountID: 1}
		streams = append(streams, st)
		candidates = append(candidates, dispatcharr.Candidate{Key: fmt.Sprintf("fixture-%03d", i), Source: conn.Fingerprint(), StreamKey: st.Key(), StreamHash: st.Fingerprint(), StreamID: st.ID, StreamName: st.Name, NormalizedAlias: dispatcharr.NormalizeStreamAlias(st), M3UAccountID: 1, ChannelID: target.ID, ChannelNumber: target.Number, ChannelName: target.Name, Score: 100, NameScore: 100, ReviewGeneration: "integration-fixture", MatcherVersion: matcher.Revision})
	}
	s.snapshot = dispatcharrMatchSnapshot{guide: s.lineup.state.Get(), dispatcharrFingerprint: conn.Fingerprint(), lineupFingerprint: config.Fingerprint(), channels: channels, digest: dispatcharrInputDigest(channels), candidates: candidates, identityStreams: dispatcharr.ProjectIdentityInventory(streams), inventoryDigest: streamInventoryDigest(streams), streamCount: len(streams), fetchedAt: time.Now().UTC(), version: matcher.Revision}
	s.cache.observe(conn.Fingerprint(), streams)
	s.lineup.automaticProjection = s.projectAutomaticExclusions
	s.lineup.commitDraft = s.commitAutomaticDraft
	s.lineup.derivedRevision = s.derivedRevision
	review := automaticReview(t, s)
	keys := []string{}
	for _, c := range review.Candidates[:100] {
		keys = append(keys, c.Key)
	}
	return s, keys, map[string]any{"inventoryStreams": 150, "syntheticCandidates": 150}
}
func TestReviewLifecycleBrowserIntegration(t *testing.T) {
	node := os.Getenv("REVIEW_BROWSER_NODE")
	if node == "" {
		t.Skip("browser driver not requested")
	}
	s, keys, dimensions := syntheticReviewBurstFixture(t)
	if os.Getenv("REVIEW_RETAINED_ROOT") != "" {
		s, keys, dimensions = retainedReviewBurstFixture(t)
	}
	review := automaticReview(t, s)
	all := map[string]dispatcharr.CandidateGroup{}
	for _, c := range review.Candidates {
		all[c.Key] = c
	}
	review.Candidates = nil
	for _, key := range keys {
		review.Candidates = append(review.Candidates, all[key])
	}
	review.VisibleLimit = 100
	draft := automaticDraft(t, s)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!doctype html><title>Isolated review fixture</title>"))
	})
	mux.HandleFunc("/fixture", func(w http.ResponseWriter, r *http.Request) {
		writeLineuparrJSON(w, 200, map[string]any{"draft": draft, "review": review, "dimensions": dimensions})
	})
	mux.HandleFunc("/api/lineuparr/dispatcharr/decision", s.lineup.guardBuilder(s.handleDecision))
	mux.HandleFunc("/api/lineuparr/dispatcharr/review", s.lineup.guardBuilder(s.handleReview))
	mux.HandleFunc("/api/lineuparr/draft", s.lineup.guardBuilder(s.lineup.handleDraft))
	server := httptest.NewServer(mux)
	defer server.Close()
	command := exec.Command(node, "tools/test_review_handler_integration.cjs")
	command.Env = append(os.Environ(), "REVIEW_HANDLER_URL="+server.URL)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("browser integration: %v\n%s", err, output)
	}
	t.Log(string(output))
}
