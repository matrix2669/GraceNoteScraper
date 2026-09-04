package main

import (
	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAliasQueuePostalScanUsesConfiguredLocation(t *testing.T) {
	catalog := lineupindex.SeedCatalog{}
	finder := &fakeProviderFinder{response: &web.ProviderResponse{Providers: []web.Provider{}}}
	service, err := lineupindex.NewService(lineupindex.ServiceConfig{
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
		if current := service.Snapshot(); !current.Job.Running && current.Job.CompletedAt != "" {
			if finder.country != "USA" || finder.postal != "11743" || finder.language != "en-us" {
				t.Fatalf("lookup = %q/%q/%q", finder.country, finder.postal, finder.language)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("postal scan did not finish: %+v", service.Snapshot().Job)
}

func TestQueueRejectsRankedRequests(t *testing.T) {
	for _, action := range []string{"continue", "refresh", "rebuild", ""} {
		if _, err := normalizeQueuedAliasRequest(lineupindex.RunRequest{Action: action}); err == nil {
			t.Fatalf("queued forbidden action %q", action)
		}
	}
}
