package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	"github.com/daniel-widrick/GraceNoteScraper/marketindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

type fakeProviderFinder struct {
	response *web.ProviderResponse
	err      error
	country  string
	postal   string
	language string
}

type fakeMarketGridFetcher struct{}

func (fakeMarketGridFetcher) FetchGrid(_ context.Context, _ web.Preferences, _ int64) (*web.GridResponse, error) {
	return &web.GridResponse{}, nil
}

type fakeProviderChannelCounter struct {
	count int
	calls int
}

func (f *fakeProviderChannelCounter) CountChannels(_ context.Context, _, _, _ string, _ web.Provider) (int, error) {
	f.calls++
	return f.count, nil
}

func (f *fakeProviderFinder) FindProviders(_ context.Context, country, postalCode, language string) (*web.ProviderResponse, error) {
	f.country = country
	f.postal = postalCode
	f.language = language
	return f.response, f.err
}

func TestSetupProviderFlow(t *testing.T) {
	store, err := appconfig.LoadStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("LoadStore() error = %v", err)
	}
	provider := web.Provider{
		Type:       "CABLE",
		Device:     "X",
		LineupID:   "USA-NY67791-DEFAULT",
		Name:       "Verizon Fios - Digital",
		Location:   "Huntington",
		PostalCode: "11743",
		HeadendID:  "NY67791",
	}
	finder := &fakeProviderFinder{response: &web.ProviderResponse{Providers: []web.Provider{provider}}}
	counter := &fakeProviderChannelCounter{count: 343}
	callbackCount := 0
	server := &setupServer{
		store:          store,
		providers:      finder,
		channelCounter: counter,
		onProviderSaved: func(changed bool) {
			if !changed {
				t.Error("first save reported unchanged")
			}
			callbackCount++
		},
	}

	request := httptest.NewRequest(http.MethodGet, "/api/setup/providers?postalCode=11743", nil)
	recorder := httptest.NewRecorder()
	server.handleProviders(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("provider lookup status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if finder.country != "USA" || finder.postal != "11743" || finder.language != "en-us" {
		t.Fatalf("lookup = %q/%q/%q", finder.country, finder.postal, finder.language)
	}
	var providerResponse web.ProviderResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &providerResponse); err != nil {
		t.Fatalf("decoding provider response: %v", err)
	}
	if counter.calls != 1 || !providerResponse.Providers[0].ChannelCountKnown || providerResponse.Providers[0].ChannelCount != 343 {
		t.Fatalf("channel count response = %+v, calls = %d", providerResponse.Providers[0], counter.calls)
	}

	payload := `{
      "country":"USA",
      "postalCode":"11743",
      "language":"en-us",
      "provider":{
        "type":"CABLE",
        "device":"X",
        "lineupId":"USA-NY67791-DEFAULT",
        "name":"Verizon Fios - Digital",
        "location":"Huntington",
        "timezone":"",
        "isDefaultProvider":"BLANK",
        "postalCode":"11743",
        "headendId":"NY67791"
      }
    }`
	request = httptest.NewRequest(http.MethodPost, "/api/setup/provider", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	server.handleProvider(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("provider save status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if callbackCount != 1 {
		t.Fatalf("callback count = %d, want 1", callbackCount)
	}
	config, configured, source := store.Get()
	if !configured || source != "file" || config.Gracenote.LineupID != provider.LineupID {
		t.Fatalf("saved config = %+v, configured=%v source=%q", config, configured, source)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/setup/config", nil)
	recorder = httptest.NewRecorder()
	server.handleConfig(recorder, request)
	var response setupConfigResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decoding config response: %v", err)
	}
	if !response.Configured || response.Gracenote == nil || response.Gracenote.ProviderName != provider.Name {
		t.Fatalf("unexpected config response: %+v", response)
	}
}

func TestSetupProviderRejectsInvalidContentType(t *testing.T) {
	store, _ := appconfig.LoadStore(filepath.Join(t.TempDir(), "config.json"))
	server := &setupServer{store: store}
	request := httptest.NewRequest(http.MethodPost, "/api/setup/provider", strings.NewReader(`{}`))
	recorder := httptest.NewRecorder()
	server.handleProvider(recorder, request)
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnsupportedMediaType)
	}
}

func TestSetupPageIsEmbedded(t *testing.T) {
	server := &setupServer{}
	request := httptest.NewRequest(http.MethodGet, "/setup", nil)
	recorder := httptest.NewRecorder()
	server.handlePage(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "GraceNoteScraper setup") {
		t.Fatal("setup page marker not found")
	}
	for _, expected := range []string{`id="changeButton"`, `id="guideStatus"`, `id="chooserPanel"`, `id="xmltvGuideLink"`, `id="xmltvCopyStatus"`, "XMLTV guide URL", "new URL('/xmlguide.xmltv', window.location.href)", "navigator.clipboard.writeText(xmltvGuideURL)", "channelCountKnown"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("setup page is missing %q", expected)
		}
	}
}

func TestSetupScrapeStatus(t *testing.T) {
	status := newScrapeStatus(false, 0, 0)
	status.start("Starting guide download")
	status.update("gracenote", "Downloading guide data (3 of 56)", 2, 56, 335, 4100)
	server := &setupServer{scrapeStatus: status}
	request := httptest.NewRequest(http.MethodGet, "/api/setup/status", nil)
	recorder := httptest.NewRecorder()
	server.handleScrapeStatus(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var response scrapeStatusSnapshot
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Running || response.Stage != "gracenote" || response.Completed != 2 || response.Total != 56 || response.Channels != 335 {
		t.Fatalf("unexpected scrape status: %+v", response)
	}
}

func TestLineuparrAliasIndexFlow(t *testing.T) {
	catalog, err := marketindex.LoadSeeds("")
	if err != nil {
		t.Fatalf("LoadSeeds() error = %v", err)
	}
	finder := &fakeProviderFinder{response: &web.ProviderResponse{Providers: []web.Provider{}}}
	service, err := marketindex.NewService(marketindex.ServiceConfig{
		Path:      filepath.Join(t.TempDir(), "market_index.json"),
		Catalog:   catalog,
		Providers: finder,
		Grids:     fakeMarketGridFetcher{},
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	server := &lineuparrServer{marketIndex: service}

	request := httptest.NewRequest(http.MethodGet, "/api/lineuparr/alias-index", nil)
	recorder := httptest.NewRecorder()
	server.handleAliasIndex(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("market index status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var snapshot marketindex.Snapshot
	if err := json.Unmarshal(recorder.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decoding market-index response: %v", err)
	}
	if snapshot.Catalog.MarketCount != 100 || snapshot.Summary.CompletedMarkets != 0 {
		t.Fatalf("initial snapshot = %+v", snapshot.Summary)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/lineuparr/alias-index/run", strings.NewReader(`{"action":"continue","batchSize":1}`))
	request.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	server.handleAliasIndexRun(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("market run status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if current := service.Snapshot(); !current.Job.Running && current.Summary.CompletedMarkets == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("market index did not finish: %+v", service.Snapshot().Job)
}

func TestLineuparrPostalScanUsesConfiguredLocation(t *testing.T) {
	catalog, err := marketindex.LoadSeeds("")
	if err != nil {
		t.Fatalf("LoadSeeds() error = %v", err)
	}
	finder := &fakeProviderFinder{response: &web.ProviderResponse{Providers: []web.Provider{}}}
	service, err := marketindex.NewService(marketindex.ServiceConfig{
		Path:      filepath.Join(t.TempDir(), "market_index.json"),
		Catalog:   catalog,
		Providers: finder,
		Grids:     fakeMarketGridFetcher{},
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	store, err := appconfig.LoadStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(appconfig.Config{
		Version: appconfig.CurrentVersion,
		Gracenote: appconfig.GracenoteConfig{
			Country: "USA", PostalCode: "11743", Language: "en-us", ProviderType: "CABLE",
			Device: "X", LineupID: "USA-TEST", ProviderName: "Test Cable", HeadendID: "TEST",
		},
	}); err != nil {
		t.Fatal(err)
	}
	server := &lineuparrServer{marketIndex: service, store: store}
	request := httptest.NewRequest(http.MethodPost, "/api/lineuparr/alias-index/run", strings.NewReader(`{"action":"postal","country":"CAN","postalCode":"M5V"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.handleAliasIndexRun(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("postal run status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if current := service.SnapshotForPostal("USA", "11743"); !current.Job.Running && current.PostalScan != nil {
			if finder.country != "USA" || finder.postal != "11743" || finder.language != "en-us" {
				t.Fatalf("lookup = %q/%q/%q", finder.country, finder.postal, finder.language)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("postal scan did not finish: %+v", service.SnapshotForPostal("USA", "11743").Job)
}

func TestGuideRedirectsToSetupUntilConfigured(t *testing.T) {
	store, _ := appconfig.LoadStore(filepath.Join(t.TempDir(), "config.json"))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()
	handleIndex(store)(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusSeeOther)
	}
	if location := recorder.Header().Get("Location"); location != "/setup" {
		t.Fatalf("Location = %q, want /setup", location)
	}

	if err := store.Save(appconfig.Config{
		Version: appconfig.CurrentVersion,
		Gracenote: appconfig.GracenoteConfig{
			Country:      "USA",
			PostalCode:   "11743",
			Language:     "en-us",
			ProviderType: "CABLE",
			Device:       "X",
			LineupID:     "USA-NY67791-DEFAULT",
			ProviderName: "Verizon Fios - Digital",
			HeadendID:    "NY67791",
		},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	recorder = httptest.NewRecorder()
	handleIndex(store)(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("configured status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestGuideCacheRequiresMatchingSource(t *testing.T) {
	t.Chdir(t.TempDir())
	want := &guide.TVGuide{LineupChannels: []guide.Channel{{ID: "station", PlacementID: "position", ChannelNo: "10", CallSign: "TEST"}}}
	if err := saveGuideCache(want, "source-one"); err != nil {
		t.Fatalf("saveGuideCache() error = %v", err)
	}

	mismatch := loadGuideCache("source-two")
	if mismatch.Status != guideCacheSourceChanged || mismatch.Guide != nil {
		t.Fatalf("mismatched cache = %+v", mismatch)
	}
	got := loadGuideCache("source-one")
	if got.Status != guideCacheReady || got.Guide == nil {
		t.Fatalf("matching cache = %+v", got)
	}
	if len(got.Guide.LineupChannels) != 1 || got.Guide.LineupChannels[0].PlacementID != "position" {
		t.Fatalf("cached provider positions = %+v", got.Guide.LineupChannels)
	}
	if matches, err := filepath.Glob("guide-cache-*.tmp"); err != nil || len(matches) != 0 {
		t.Fatalf("temporary cache files = %v, error = %v", matches, err)
	}
}

func TestLoadGuideCacheRejectsIncompatibleOrIncompleteGuide(t *testing.T) {
	t.Chdir(t.TempDir())

	writeCache := func(cache guideCache) {
		t.Helper()
		data, err := json.Marshal(cache)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		if err := os.WriteFile(guideCachePath, data, 0644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
	}

	writeCache(guideCache{
		Version:           guideCacheVersion - 1,
		SavedAt:           time.Now(),
		SourceFingerprint: "source-one",
		Guide:             guide.TVGuide{LineupChannels: []guide.Channel{{PlacementID: "position"}}},
	})
	incompatible := loadGuideCache("source-one")
	if incompatible.Status != guideCacheCorrupt || incompatible.Err == nil {
		t.Fatalf("incompatible cache = %+v", incompatible)
	}

	writeCache(guideCache{
		Version:           guideCacheVersion,
		SavedAt:           time.Now(),
		SourceFingerprint: "source-one",
		Guide:             guide.TVGuide{},
	})
	incomplete := loadGuideCache("source-one")
	if incomplete.Status != guideCacheCorrupt || incomplete.Err == nil {
		t.Fatalf("incomplete cache = %+v", incomplete)
	}
}

func TestLoadGuideCacheReportsMissingAndCorrupt(t *testing.T) {
	t.Chdir(t.TempDir())

	missing := loadGuideCache("source-one")
	if missing.Status != guideCacheMissing || missing.Err != nil {
		t.Fatalf("missing cache = %+v", missing)
	}

	if err := os.WriteFile(guideCachePath, []byte("not-json"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	corrupt := loadGuideCache("source-one")
	if corrupt.Status != guideCacheCorrupt || corrupt.Err == nil {
		t.Fatalf("corrupt cache = %+v", corrupt)
	}

	if err := os.Remove(guideCachePath); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := os.Mkdir(guideCachePath, 0755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	unreadable := loadGuideCache("source-one")
	if unreadable.Status != guideCacheUnreadable || unreadable.Err == nil {
		t.Fatalf("unreadable cache = %+v", unreadable)
	}
	if strings.Contains(unreadable.Err.Error(), "not-json") {
		t.Fatalf("unreadable cache retained stale parse error: %v", unreadable.Err)
	}
}

func TestPlanGuideStartupUsesFreshCacheForRemainingInterval(t *testing.T) {
	want := &guide.TVGuide{}
	plan := planGuideStartup(guideCacheLoadResult{
		Guide:  want,
		Age:    3 * time.Hour,
		Status: guideCacheReady,
	}, nil)

	if plan.Guide != want {
		t.Fatal("fresh cache guide was not retained")
	}
	if plan.NextScrapeIn != 21*time.Hour {
		t.Fatalf("next scrape = %s, want 21h", plan.NextScrapeIn)
	}
	if plan.Warn || plan.InvalidateArtifacts || !strings.Contains(plan.Message, "fresh") {
		t.Fatalf("fresh plan = %+v", plan)
	}
}

func TestPlanGuideStartupServesStaleCacheDuringRefresh(t *testing.T) {
	want := &guide.TVGuide{}
	plan := planGuideStartup(guideCacheLoadResult{
		Guide:  want,
		Age:    25 * time.Hour,
		Status: guideCacheReady,
	}, nil)

	if plan.Guide != want {
		t.Fatal("stale cache guide was not retained")
	}
	if plan.NextScrapeIn != immediateGuideRefreshWait {
		t.Fatalf("next scrape = %s, want %s", plan.NextScrapeIn, immediateGuideRefreshWait)
	}
	if plan.Warn || plan.InvalidateArtifacts || !strings.Contains(plan.Message, "stale") {
		t.Fatalf("stale plan = %+v", plan)
	}
}

func TestPlanGuideStartupRejectsUnservableOrWrongSourceCache(t *testing.T) {
	want := &guide.TVGuide{}
	missingXML := planGuideStartup(guideCacheLoadResult{
		Guide:  want,
		Age:    time.Hour,
		Status: guideCacheReady,
	}, &os.PathError{Op: "stat", Path: "xmlguide.xmltv", Err: os.ErrNotExist})
	if missingXML.Guide != nil || !missingXML.Warn || missingXML.InvalidateArtifacts {
		t.Fatalf("missing XMLTV plan = %+v", missingXML)
	}
	if !strings.Contains(missingXML.Message, "xmlguide.xmltv is missing") {
		t.Fatalf("missing XMLTV message = %q", missingXML.Message)
	}

	wrongSource := planGuideStartup(guideCacheLoadResult{Status: guideCacheSourceChanged}, nil)
	if wrongSource.Guide != nil || wrongSource.Warn || !wrongSource.InvalidateArtifacts {
		t.Fatalf("wrong-source plan = %+v", wrongSource)
	}
	if wrongSource.NextScrapeIn != immediateGuideRefreshWait {
		t.Fatalf("wrong-source next scrape = %s", wrongSource.NextScrapeIn)
	}
}

func TestRestoreMissingGuideFileFromCache(t *testing.T) {
	t.Chdir(t.TempDir())
	want := &guide.TVGuide{}
	cache := guideCacheLoadResult{
		Guide:  want,
		Age:    2 * time.Hour,
		Status: guideCacheReady,
	}

	xmltvErr, rebuilt := restoreMissingGuideFile(cache, &os.PathError{
		Op:   "stat",
		Path: "xmlguide.xmltv",
		Err:  os.ErrNotExist,
	})
	if xmltvErr != nil || !rebuilt {
		t.Fatalf("restoreMissingGuideFile() error = %v, rebuilt = %v", xmltvErr, rebuilt)
	}
	data, err := os.ReadFile("xmlguide.xmltv")
	if err != nil {
		t.Fatalf("ReadFile(xmlguide.xmltv) error = %v", err)
	}
	if !strings.Contains(string(data), "<tv") {
		t.Fatalf("rebuilt XMLTV does not contain a tv root: %q", data)
	}

	plan := planGuideStartup(cache, xmltvErr)
	if plan.Guide != want || plan.NextScrapeIn != 22*time.Hour {
		t.Fatalf("restored startup plan = %+v", plan)
	}
}

func TestGuideFilterPreservesFullProviderLineup(t *testing.T) {
	g := &guide.TVGuide{
		Channels: []guide.Channel{
			{ID: "one", ChannelNo: "1", DisplayNames: []guide.DisplayName{{Name: "1 ONE"}, {Name: "1"}, {Name: "ONE"}}},
			{ID: "two", ChannelNo: "2", DisplayNames: []guide.DisplayName{{Name: "2 TWO"}, {Name: "2"}, {Name: "TWO"}}},
		},
		Programs:       []guide.Program{{Channel: "one"}, {Channel: "two"}},
		LineupChannels: []guide.Channel{{PlacementID: "one-sd"}, {PlacementID: "one-hd"}, {PlacementID: "two"}},
	}
	filtered := filterGuideChannels(g, map[string]bool{"1": true})
	if len(filtered.Channels) != 1 || len(filtered.Programs) != 1 {
		t.Fatalf("filtered guide = %+v", filtered)
	}
	if len(filtered.LineupChannels) != 3 {
		t.Fatalf("provider lineup was filtered with the guide: %+v", filtered.LineupChannels)
	}
}

func TestGuideStateRequiresMatchingSource(t *testing.T) {
	state := &GuideState{}
	g := &guide.TVGuide{LineupChannels: []guide.Channel{{PlacementID: "position"}}}
	state.UpdateForSource(g, "source-one")
	if state.GetForSource("source-one") != g {
		t.Fatal("matching source did not return guide")
	}
	if state.GetForSource("source-two") != nil {
		t.Fatal("guide leaked to a different source")
	}
}

func TestMergeLineupChannelCollectsEventCallsigns(t *testing.T) {
	merged := mergeLineupChannel(
		guide.Channel{ID: "one", EventCallSigns: []string{"ONE"}},
		guide.Channel{ID: "one", PlacementID: "position", ChannelNo: "1", CallSign: "ONE", EventCallSigns: []string{"one", "ONEDT"}},
	)
	if merged.PlacementID != "position" || len(merged.EventCallSigns) != 2 || merged.EventCallSigns[1] != "ONEDT" {
		t.Fatalf("merged channel = %+v", merged)
	}
}
