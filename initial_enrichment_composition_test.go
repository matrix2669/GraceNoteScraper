package main

import (
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/guide"
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
)

// These assertions exercise the combination of the focused TMDB owner with
// the full-provider Lineuparr builder and local-ZIP job coordinator.
func TestPendingTMDBCompositionKeepsBuilderAvailableAndScansQueued(t *testing.T) {
	server := newLineuparrTestServer(t, true)
	config, _, _ := server.store.Get()
	base := initialGuideFixture()
	base.LineupChannels = server.state.Get().LineupChannels
	base.TMDBPending = true
	base.TMDBPendingSince = time.Now()
	server.state.UpdateForSource(base, config.Fingerprint())
	status := newScrapeStatus(false, 0, 0)
	status.start("Initial guide")
	status.available(len(base.Channels), len(base.Programs))
	starter := &fakeAliasJobStarter{}
	queue := newAliasJobQueue(status, starter)
	if _, err := queue.Queue(lineupindex.RunRequest{Action: "postal", Country: "USA", PostalCode: "11743"}); err != nil {
		t.Fatal(err)
	}
	_, err := runGuideCycle(base, true, time.Now(), nil, func(enriched *guide.TVGuide) error {
		recorder := httptest.NewRecorder()
		draft, _, _, ok := server.buildDraft(recorder, httptest.NewRequest("GET", "/api/lineuparr/draft", nil))
		if !ok || len(draft.Channels) != 2 {
			t.Fatalf("pending guide cannot build full provider lineup: %s", recorder.Body.String())
		}
		if !reflect.DeepEqual(enriched.LineupChannels, base.LineupChannels) {
			t.Fatal("background copy lost provider positions")
		}
		queue.TryStart()
		if starter.requestCount() != 0 || !queue.View().Queued {
			t.Fatal("provider scan started before enrichment finished")
		}
		return nil
	}, func(g *guide.TVGuide) (bool, error) {
		server.state.UpdateForSource(g, config.Fingerprint())
		return true, nil
	}, func() bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	status.ready(len(base.Channels), len(base.Programs))
	queue.TryStart()
	if starter.requestCount() != 1 || queue.View().Queued {
		t.Fatal("queued scan did not start after completion")
	}
}
