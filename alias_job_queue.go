package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/marketindex"
)

var errAliasJobAlreadyQueued = errors.New("an alias-index job is already queued; cancel it before choosing another")

type aliasJobStarter interface {
	Start(marketindex.RunRequest) (marketindex.JobView, error)
}

type aliasQueueView struct {
	Queued      bool      `json:"queued"`
	Action      string    `json:"action,omitempty"`
	Ranks       []int     `json:"ranks,omitempty"`
	QueuedAt    time.Time `json:"queuedAt,omitempty"`
	GuideBusy   bool      `json:"guideBusy"`
	GuideStage  string    `json:"guideStage,omitempty"`
	GuideStatus string    `json:"guideStatus,omitempty"`
	LastError   string    `json:"lastError,omitempty"`
}

type aliasJobQueue struct {
	mu          sync.Mutex
	guideStatus *scrapeStatus
	starter     aliasJobStarter
	queued      *marketindex.RunRequest
	queuedAt    time.Time
	lastError   string
}

func newAliasJobQueue(status *scrapeStatus, starter aliasJobStarter) *aliasJobQueue {
	if starter == nil {
		return nil
	}
	return &aliasJobQueue{guideStatus: status, starter: starter}
}

func (q *aliasJobQueue) Queue(request marketindex.RunRequest) (aliasQueueView, error) {
	request, err := normalizeQueuedAliasRequest(request)
	if err != nil {
		return q.View(), err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.queued != nil {
		return q.viewLocked(), errAliasJobAlreadyQueued
	}
	copy := request
	copy.Ranks = append([]int(nil), request.Ranks...)
	q.queued = &copy
	q.queuedAt = time.Now().UTC()
	q.lastError = ""
	return q.viewLocked(), nil
}

func (q *aliasJobQueue) Cancel() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.queued == nil {
		return false
	}
	q.queued = nil
	q.queuedAt = time.Time{}
	q.lastError = ""
	return true
}

func (q *aliasJobQueue) ClearError() {
	q.mu.Lock()
	q.lastError = ""
	q.mu.Unlock()
}

func (q *aliasJobQueue) View() aliasQueueView {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.viewLocked()
}

func (q *aliasJobQueue) viewLocked() aliasQueueView {
	view := aliasQueueView{LastError: q.lastError}
	if q.guideStatus != nil {
		status := q.guideStatus.snapshotValue()
		view.GuideBusy = status.Running || status.Stage != "ready"
		view.GuideStage = status.Stage
		view.GuideStatus = status.Message
	}
	if q.queued != nil {
		view.Queued = true
		view.Action = q.queued.Action
		view.Ranks = append([]int(nil), q.queued.Ranks...)
		view.QueuedAt = q.queuedAt
	}
	return view
}

func (q *aliasJobQueue) TryStart() {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.queued == nil {
		return
	}
	if q.guideStatus != nil {
		status := q.guideStatus.snapshotValue()
		if status.Running || status.Stage != "ready" {
			return
		}
	}
	_, err := q.starter.Start(*q.queued)
	if errors.Is(err, marketindex.ErrAlreadyRunning) {
		return
	}
	q.queued = nil
	q.queuedAt = time.Time{}
	if err != nil {
		q.lastError = err.Error()
	} else {
		q.lastError = ""
	}
}

func (q *aliasJobQueue) Run(ctx context.Context) {
	if q == nil {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			q.TryStart()
		}
	}
}

func normalizeQueuedAliasRequest(request marketindex.RunRequest) (marketindex.RunRequest, error) {
	request.Action = strings.ToLower(strings.TrimSpace(request.Action))
	if request.Action == "" {
		request.Action = "continue"
	}
	if request.Action == "postal" {
		if strings.TrimSpace(request.Country) == "" || strings.TrimSpace(request.PostalCode) == "" {
			return marketindex.RunRequest{}, errors.New("postal scan requires country and postal code")
		}
		request.BatchSize = 0
		request.Ranks = nil
		return request, nil
	}
	if request.BatchSize == 0 {
		request.BatchSize = marketindex.DefaultBatchSize
	}
	if request.BatchSize < 1 || request.BatchSize > marketindex.MaxBatchSize {
		return marketindex.RunRequest{}, errors.New("batch size must be between 1 and 25")
	}
	switch request.Action {
	case "continue", "rebuild":
		request.Ranks = nil
	case "refresh":
		if len(request.Ranks) == 0 || len(request.Ranks) > marketindex.MaxBatchSize {
			return marketindex.RunRequest{}, errors.New("refresh requires between 1 and 25 market ranks")
		}
	default:
		return marketindex.RunRequest{}, errors.New("unsupported alias-index action")
	}
	return request, nil
}
