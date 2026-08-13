package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestGetDataByTimeRetriesTransientFailure(t *testing.T) {
	originalDelays := gridRetryDelays
	gridRetryDelays = []time.Duration{0, 0, 0}
	defer func() { gridRetryDelays = originalDelays }()

	attempts := 0
	client := &Client{
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			if attempts < 3 {
				return nil, errors.New("temporary timeout")
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader(`{"channels":[]}`)),
				Header:     make(http.Header),
			}, nil
		})},
		pref: Preferences{Country: "USA", ZipCode: "11743", Headend: "NY67791", LineupId: "NY67791-X", Device: "X", Language: "en"},
	}

	if _, err := client.GetDataByTime(time.Date(2026, 7, 25, 6, 0, 0, 0, time.UTC).Unix()); err != nil {
		t.Fatalf("GetDataByTime returned error after successful retry: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestLoadFallbackGridReturnsOnlyProgramsOverlappingRequestedWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guide_cache.json")
	windowStart := time.Date(2026, 7, 25, 6, 0, 0, 0, time.UTC)

	fixture := cachedGuideFile{
		SavedAt: windowStart.Add(-24 * time.Hour),
		Guide: cachedTVGuide{
			Channels: []cachedChannel{{
				ID:        "10139",
				CallSign:  "CNBC",
				ChannelNo: "102",
				IconURL:   "http://localhost:8080/img?url=https%3A%2F%2Fexample.com%2Fcnbc.png",
			}},
			Programs: []cachedProgram{
				{
					Start:       "20260725053000 +0000",
					Stop:        "20260725063000 +0000",
					Channel:     "10139",
					Title:       "Overlapping Business",
					Description: "Started before the failed window.",
					Length:      "60",
				},
				{
					Start:       "20260725070000 +0000",
					Stop:        "20260725080000 +0000",
					Channel:     "10139",
					Title:       "Morning Business",
					Description: "Business news.",
					Length:      "60",
					IconSrc:     "http://localhost:8080/img?url=http%3A%2F%2Fzap2it.tmsimg.com%2Fassets%2Fp12345_b_v10_aa.jpg",
					URL:         "https://tvlistings.gracenote.com/overview.html?programSeriesId=SH12345678&amp;tmsId=EP123456780001",
					Categories:  []cachedCategory{{Name: "news"}, {Name: "Series"}},
					New:         true,
				},
				{
					Start:       "20260725130000 +0000",
					Stop:        "20260725140000 +0000",
					Channel:     "10139",
					Title:       "Outside Window",
					Description: "Should not be returned.",
					Length:      "60",
				},
			},
		},
	}
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	grid, err := loadFallbackGrid(path, windowStart.Unix())
	if err != nil {
		t.Fatalf("loadFallbackGrid: %v", err)
	}
	if len(grid.Channels) != 1 || len(grid.Channels[0].Events) != 2 {
		t.Fatalf("fallback grid sizes = %d channels, %d events", len(grid.Channels), len(grid.Channels[0].Events))
	}
	if grid.Channels[0].Events[0].Program.Title != "Overlapping Business" {
		t.Fatalf("overlapping event was not retained: %q", grid.Channels[0].Events[0].Program.Title)
	}

	event := grid.Channels[0].Events[1]
	if event.Program.Title != "Morning Business" {
		t.Fatalf("event title = %q", event.Program.Title)
	}
	if event.Thumbnail != "p12345_b_v10_aa" {
		t.Fatalf("thumbnail ID = %q", event.Thumbnail)
	}
	if event.SeriesID != "SH12345678" || event.Program.ID != "EP123456780001" {
		t.Fatalf("program IDs = %q / %q", event.SeriesID, event.Program.ID)
	}
	if len(event.Filter) != 1 || event.Filter[0] != "filter-news" {
		t.Fatalf("filters = %v", event.Filter)
	}
	if len(event.Flag) != 1 || event.Flag[0] != "New" {
		t.Fatalf("flags = %v", event.Flag)
	}
}
