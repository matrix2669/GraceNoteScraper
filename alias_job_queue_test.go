package main

import (
	"errors"
	"sync"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/marketindex"
)

type fakeAliasJobStarter struct {
	mu       sync.Mutex
	requests []marketindex.RunRequest
	err      error
}

func (f *fakeAliasJobStarter) Start(request marketindex.RunRequest) (marketindex.JobView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, request)
	return marketindex.JobView{Running: f.err == nil, Action: request.Action}, f.err
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
	view, err := queue.Queue(marketindex.RunRequest{Action: "continue"})
	if err != nil || !view.Queued || !view.GuideBusy || view.Action != "continue" {
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
	if _, err := queue.Queue(marketindex.RunRequest{Action: "refresh", Ranks: []int{7}, BatchSize: 1}); err != nil {
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
	starter := &fakeAliasJobStarter{err: marketindex.ErrAlreadyRunning}
	queue := newAliasJobQueue(status, starter)
	if _, err := queue.Queue(marketindex.RunRequest{Action: "rebuild"}); err != nil {
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
