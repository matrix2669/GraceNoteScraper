package main

import (
	"errors"
	"sync"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
)

type fakeAliasJobStarter struct {
	mu       sync.Mutex
	requests []lineupindex.RunRequest
	err      error
}

func (f *fakeAliasJobStarter) Start(request lineupindex.RunRequest) (lineupindex.JobView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, request)
	return lineupindex.JobView{Running: f.err == nil, Action: request.Action}, f.err
}

func (f *fakeAliasJobStarter) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func TestAliasJobQueueWaitsForReadyGuideAndCanBeCancelled(t *testing.T) {
	status := newScrapeStatus(true, 335, 115409)
	status.start("Downloading guide data")
	starter := &fakeAliasJobStarter{}
	queue := newAliasJobQueue(status, starter)
	view, err := queue.Queue(lineupindex.RunRequest{Action: "postal", Country: "USA", PostalCode: "11743"})
	if err != nil || !view.Queued || !view.GuideBusy || view.Action != "postal" {
		t.Fatalf("queued view = %+v err=%v", view, err)
	}
	queue.TryStart()
	if starter.requestCount() != 0 {
		t.Fatal("queued scan started while the guide was busy")
	}
	if !queue.Cancel() || queue.View().Queued {
		t.Fatal("queued scan was not cancelled")
	}
}

func TestAliasJobQueueStartsOnceAfterGuideIsReady(t *testing.T) {
	status := newScrapeStatus(false, 0, 0)
	status.queue("Guide build queued")
	starter := &fakeAliasJobStarter{}
	queue := newAliasJobQueue(status, starter)
	if _, err := queue.Queue(lineupindex.RunRequest{Action: "postal", Country: "USA", PostalCode: "11743"}); err != nil {
		t.Fatal(err)
	}
	status.ready(335, 115409)
	queue.TryStart()
	if starter.requestCount() != 1 || queue.View().Queued {
		t.Fatalf("starter calls = %d queue = %+v", starter.requestCount(), queue.View())
	}
	queue.TryStart()
	if starter.requestCount() != 1 {
		t.Fatal("completed queue started more than once")
	}
}

func TestAliasJobQueueRetainsQueuedWorkWhileAnotherScanRuns(t *testing.T) {
	status := newScrapeStatus(true, 335, 115409)
	starter := &fakeAliasJobStarter{err: lineupindex.ErrAlreadyRunning}
	queue := newAliasJobQueue(status, starter)
	if _, err := queue.Queue(lineupindex.RunRequest{Action: "postal", Country: "USA", PostalCode: "11743"}); err != nil {
		t.Fatal(err)
	}
	queue.TryStart()
	if !queue.View().Queued {
		t.Fatal("queued work was discarded while another scan was running")
	}
	starter.err = errors.New("permanent failure")
	queue.TryStart()
	view := queue.View()
	if view.Queued || view.LastError != "permanent failure" {
		t.Fatalf("failed queue state = %+v", view)
	}
}

func TestAliasQueueAllowsPublishedBackgroundTMDBOnly(t *testing.T) {
	for _, stage := range []string{"idle", "queued", "starting", "gracenote", "logos", "tmdb", "saving", "error", "tmdb_background", "ready"} {
		t.Run(stage, func(t *testing.T) {
			status := newScrapeStatus(false, 0, 0)
			status.update(stage, "test", 1, 10, 335, 1000)
			if stage == "ready" {
				status.ready(335, 1000)
			}
			starter := &fakeAliasJobStarter{}
			queue := newAliasJobQueue(status, starter)
			view, err := queue.Queue(lineupindex.RunRequest{Action: "postal", Country: "USA", PostalCode: "11743"})
			allowed := stage == "tmdb_background" || stage == "ready"
			if err != nil || view.GuideBusy == allowed {
				t.Fatalf("view=%+v err=%v", view, err)
			}
			queue.TryStart()
			if (starter.requestCount() == 1) != allowed || queue.View().Queued == allowed {
				t.Fatalf("starts=%d view=%+v", starter.requestCount(), queue.View())
			}
		})
	}
	for _, counts := range [][2]int{{0, 0}, {1, 0}, {0, 1}} {
		if !guideBlocksAliasScan(scrapeStatusSnapshot{Running: true, Stage: "tmdb_background", Channels: counts[0], Programs: counts[1]}) {
			t.Fatalf("incomplete base guide allowed: %v", counts)
		}
	}
}

func TestAliasQueueReleasesAtBackgroundTransitionWithoutWaitingForTMDB(t *testing.T) {
	status := newScrapeStatus(false, 0, 0)
	status.start("Downloading")
	starter := &fakeAliasJobStarter{}
	queue := newAliasJobQueue(status, starter)
	if _, err := queue.Queue(lineupindex.RunRequest{Action: "postal", Country: "USA", PostalCode: "11743"}); err != nil {
		t.Fatal(err)
	}
	queue.TryStart()
	if starter.requestCount() != 0 {
		t.Fatal("started before base publication")
	}
	status.update("tmdb_background", "Guide available", 0, 100, 335, 1000)
	queue.TryStart()
	queue.TryStart()
	if starter.requestCount() != 1 || queue.View().Queued || !status.snapshotValue().Running {
		t.Fatal("scan must start once while TMDB is still running")
	}
	starter.err = lineupindex.ErrAlreadyRunning
	if _, err := queue.Queue(lineupindex.RunRequest{Action: "postal", Country: "USA", PostalCode: "11743"}); err != nil {
		t.Fatal(err)
	}
	queue.TryStart()
	if !queue.View().Queued || !queue.Cancel() {
		t.Fatal("duplicate scan must remain queued and cancellable")
	}
}
