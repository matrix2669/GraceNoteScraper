package main

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/guide"
	"github.com/daniel-widrick/GraceNoteScraper/tmdb"
)

func initialGuideFixture() *guide.TVGuide {
	return &guide.TVGuide{
		Channels: []guide.Channel{{ID: "one", IconURL: "https://tmsimg.com/channel.png", DisplayNames: []guide.DisplayName{{Name: "1 ONE"}, {Name: "1"}, {Name: "ONE"}}}},
		Programs: []guide.Program{{Channel: "one", Title: "News", Description: "Unavailable", IconSrc: "https://tmsimg.com/program.png", Images: []guide.Image{{URL: "https://tmsimg.com/program.png"}}, EpisodeNumbers: []guide.EpisodeNumber{{System: "onscreen", EpisodeNumber: "S1E1"}}, Categories: []guide.Category{{Name: "news"}}, Subtitles: []guide.Subtitle{{Type: "teletext"}}}},
	}
}

func TestInitialGuideIsPublishedBeforeEnrichment(t *testing.T) {
	var state GuideState
	var stages []string
	var base *guide.TVGuide
	persist := func(g *guide.TVGuide) (bool, error) {
		stages = append(stages, "publish")
		state.Update(g)
		return true, nil
	}
	result, err := runGuideCycle(nil, true, time.Now(), func(withTMDB bool, save guidePersister) (*guide.TVGuide, error) {
		if withTMDB {
			t.Fatal("TMDB blocked base guide")
		}
		stages = append(stages, "scrape")
		base = initialGuideFixture()
		_, err := save(base)
		return base, err
	}, func(g *guide.TVGuide) error {
		stages = append(stages, "enrich")
		if state.Get() != base || !base.TMDBPending {
			t.Fatal("base guide unavailable during enrichment")
		}
		if g == base {
			t.Fatal("published guide passed to enrichment")
		}
		g.Programs[0].Description = "Enriched description"
		g.Programs[0].Images[0].URL = "changed"
		g.Programs[0].EpisodeNumbers[0].EpisodeNumber = "changed"
		g.Programs[0].Categories[0].Name = "changed"
		g.Programs[0].Subtitles[0].Type = "changed"
		g.Channels[0].DisplayNames[0].Name = "changed"
		if !reflect.DeepEqual(base.Programs, initialGuideFixture().Programs) || base.Channels[0].DisplayNames[0].Name != "1 ONE" {
			t.Fatal("reader graph mutated")
		}
		return nil
	}, persist, func() bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stages, []string{"scrape", "publish", "enrich", "publish"}) {
		t.Fatal(stages)
	}
	if state.Get() != result || result.TMDBPending || !result.TMDBPendingSince.IsZero() {
		t.Fatal("final guide not published complete")
	}
	if !base.TMDBPending {
		t.Fatal("old snapshot mutated")
	}
}

func TestNormalRefreshAndNoTokenKeepSinglePublication(t *testing.T) {
	for _, tc := range []struct {
		name     string
		existing *guide.TVGuide
		enabled  bool
	}{
		{"scheduled", initialGuideFixture(), true}, {"no-token", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writes := 0
			_, err := runGuideCycle(tc.existing, tc.enabled, time.Now(), func(enabled bool, persist guidePersister) (*guide.TVGuide, error) {
				if enabled != tc.enabled {
					t.Fatal("changed normal enrichment behavior")
				}
				g := initialGuideFixture()
				_, err := persist(g)
				return g, err
			}, func(*guide.TVGuide) error { t.Fatal("unexpected background pass"); return nil }, func(g *guide.TVGuide) (bool, error) {
				writes++
				if g.TMDBPending {
					t.Fatal("unexpected marker")
				}
				return true, nil
			}, nil)
			if err != nil || writes != 1 {
				t.Fatalf("writes=%d err=%v", writes, err)
			}
		})
	}
}

func TestPendingEnrichmentRestartsFromPersistedGuide(t *testing.T) {
	t.Chdir(t.TempDir())
	g := initialGuideFixture()
	g.TMDBPending = true
	g.TMDBPendingSince = time.Now().Add(-time.Hour)
	if err := saveGuideCache(g, "source"); err != nil {
		t.Fatal(err)
	}
	loaded := loadGuideCache("source")
	if loaded.Status != guideCacheReady || !resumableTMDB(loaded.Guide, time.Now()) {
		t.Fatalf("cache=%+v", loaded)
	}
	filtered := filterGuideChannels(loaded.Guide, map[string]bool{"1": true})
	if !resumableTMDB(filtered, time.Now()) {
		t.Fatal("filter lost pending metadata")
	}
	result, err := runGuideCycle(filtered, true, time.Now(), func(bool, guidePersister) (*guide.TVGuide, error) { t.Fatal("redownloaded grids"); return nil, nil }, func(g *guide.TVGuide) error { g.Programs[0].StarRating = "9/10"; return nil }, func(g *guide.TVGuide) (bool, error) { return true, saveGuideCache(g, "source") }, nil)
	if err != nil || result.TMDBPending {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if loadGuideCache("source").Guide.TMDBPending {
		t.Fatal("pending marker survived completion")
	}
	if loadGuideCache("other").Status != guideCacheSourceChanged {
		t.Fatal("wrong source loaded")
	}
}

func TestPendingAgeBoundaryAndLegacyGuides(t *testing.T) {
	now := time.Now()
	for _, age := range []time.Duration{-time.Second, 0, time.Hour, 24*time.Hour - time.Nanosecond, 24 * time.Hour, 25 * time.Hour} {
		g := initialGuideFixture()
		g.TMDBPending = true
		g.TMDBPendingSince = now.Add(-age)
		if resumableTMDB(g, now) != (age >= 0 && age < 24*time.Hour) {
			t.Fatalf("age=%s", age)
		}
	}
	legacy := initialGuideFixture()
	if resumableTMDB(legacy, now) {
		t.Fatal("legacy cache treated as pending")
	}
	legacy.TMDBPending = true
	legacy.TMDBPendingSince = now.Add(-24 * time.Hour)
	called := false
	_, err := runGuideCycle(legacy, true, now, func(enabled bool, _ guidePersister) (*guide.TVGuide, error) {
		called = enabled
		return initialGuideFixture(), nil
	}, func(*guide.TVGuide) error { t.Fatal("resumed expired guide"); return nil }, nil, nil)
	if err != nil || !called {
		t.Fatal("expired guide did not use normal full refresh")
	}
}

func TestEnrichmentFailureOrSourceChangeRetainsBase(t *testing.T) {
	for _, mode := range []string{"enrichment-error", "write-error", "source-change", "rejected-write", "already-changed"} {
		t.Run(mode, func(t *testing.T) {
			base := initialGuideFixture()
			base.TMDBPending = true
			base.TMDBPendingSince = time.Now()
			active := mode != "already-changed"
			writes := 0
			result, err := runGuideCycle(base, true, time.Now(), func(bool, guidePersister) (*guide.TVGuide, error) { t.Fatal("unexpected scrape"); return nil, nil }, func(g *guide.TVGuide) error {
				g.Programs[0].Title = "modified"
				if mode == "enrichment-error" {
					return errors.New("interrupted")
				}
				if mode == "source-change" {
					active = false
				}
				return nil
			}, func(*guide.TVGuide) (bool, error) {
				writes++
				if mode == "write-error" {
					return false, errors.New("disk failure")
				}
				return false, nil
			}, func() bool { return active })
			if err == nil || result != base || !base.TMDBPending || base.Programs[0].Title != "News" {
				t.Fatalf("base not retained: %v", err)
			}
			if (mode == "source-change" || mode == "already-changed" || mode == "enrichment-error") && writes != 0 {
				t.Fatal("published failed or stale enrichment")
			}
		})
	}
}

func TestInitialEmptyGuideDoesNotPublish(t *testing.T) {
	_, err := runGuideCycle(nil, true, time.Now(), func(_ bool, persist guidePersister) (*guide.TVGuide, error) {
		g := &guide.TVGuide{}
		_, err := persist(g)
		return g, err
	}, func(*guide.TVGuide) error { t.Fatal("enriched empty guide"); return nil }, func(*guide.TVGuide) (bool, error) { t.Fatal("published empty guide"); return true, nil }, nil)
	if err == nil {
		t.Fatal("empty guide accepted")
	}
}

func TestBackgroundWorkerStopsStartingTitlesWhenSourceChanges(t *testing.T) {
	keys := make([]tmdbTitleKey, 100)
	var current atomic.Bool
	current.Store(true)
	var count atomic.Int32
	lookupTMDBTitlesWhile(keys, 4, func(string, bool) tmdb.CacheEntry { count.Add(1); current.Store(false); return tmdb.CacheEntry{} }, current.Load)
	if count.Load() < 1 || count.Load() > 4 {
		t.Fatalf("started %d lookups", count.Load())
	}
	if err := enrichProgramThumbnailsWhile(nil, nil, func() bool { return false }); !errors.Is(err, errScrapeSourceChanged) {
		t.Fatal(err)
	}
}

func TestBackgroundEnrichmentDoesNotRaceWithReaders(t *testing.T) {
	base := initialGuideFixture()
	base.TMDBPending = true
	base.TMDBPendingSince = time.Now()
	var state GuideState
	state.Update(base)
	var readers sync.WaitGroup
	readers.Add(1)
	go func() {
		defer readers.Done()
		for i := 0; i < 100; i++ {
			_, _ = json.Marshal(state.Get())
		}
	}()
	_, err := runGuideCycle(base, true, time.Now(), nil, func(g *guide.TVGuide) error {
		for i := 0; i < 100; i++ {
			g.Programs[0].Images[0].URL = "updated"
			g.Programs[0].Title = "enriched"
		}
		return nil
	}, func(g *guide.TVGuide) (bool, error) { state.Update(g); return true, nil }, nil)
	readers.Wait()
	if err != nil {
		t.Fatal(err)
	}
}

func TestProxyRewriteIsIdempotentOnResume(t *testing.T) {
	g := initialGuideFixture()
	rewriteGuideImageURLs(g, "http://guide:8080")
	icon := g.Channels[0].IconURL
	program := g.Programs[0].IconSrc
	rewriteGuideImageURLs(g, "http://guide:8080/")
	if icon != g.Channels[0].IconURL || program != g.Programs[0].IconSrc {
		t.Fatal("nested proxy")
	}
	g.Programs[0].IconSrc = "https://image.tmdb.org/new.jpg"
	rewriteGuideImageURLs(g, "http://guide:8080")
	if !strings.Contains(g.Programs[0].IconSrc, "image.tmdb.org") || !strings.HasPrefix(g.Programs[0].IconSrc, "http://guide:8080/img?url=") {
		t.Fatal("new artwork not proxied")
	}
}

func TestGuideAvailabilityIsIndependentOfBackgroundBusy(t *testing.T) {
	s := newScrapeStatus(false, 0, 0)
	s.start("start")
	s.available(1, 2)
	s.update("tmdb_background", "Guide ready", 1, 10, 1, 2)
	if got := s.snapshotValue(); !got.Running || !got.GuideReady || got.Completed != 1 {
		t.Fatalf("status=%+v", got)
	}
	s.fail("retry")
	if !s.snapshotValue().GuideReady {
		t.Fatal("failure hid available guide")
	}
	s.queue("new lineup")
	if s.snapshotValue().GuideReady {
		t.Fatal("new source retained ready flag")
	}
}
