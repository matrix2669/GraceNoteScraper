package providersource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/channelcategory"
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

func TestBundledSpectrumCatalogIsCompleteAndMapped(t *testing.T) {
	catalog, err := parseBundledSpectrumCatalog(bundledSpectrumCatalogData)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Entries) != 378 {
		t.Fatalf("bundled entries = %d, want 378", len(catalog.Entries))
	}
	entries := spectrumCatalogEntries(catalog.Entries)
	want := map[string]struct {
		name, category string
	}{
		"32645":  {"ESPN", channelcategory.Sports},
		"59615":  {"Freeform - East", channelcategory.Entertainment},
		"59539":  {"TBN", channelcategory.Faith},
		"103890": {"FLIX - East", channelcategory.Movies},
	}
	for _, entry := range entries {
		if expected, ok := want[entry.StationIDs[0]]; ok {
			if entry.Name != expected.name || entry.Category != expected.category {
				t.Fatalf("entry %s = %+v", entry.StationIDs[0], entry)
			}
			delete(want, entry.StationIDs[0])
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing bundled examples: %+v", want)
	}
}

func TestSpectrumCategoryFactRetainsRawProviderCategory(t *testing.T) {
	catalog, err := parseBundledSpectrumCatalog(bundledSpectrumCatalogData)
	if err != nil {
		t.Fatal(err)
	}
	result := matchCatalog(lineupindex.ProviderEvidenceRequest{Grid: &web.GridResponse{Channels: []web.JSONChannel{
		{ChannelID: "103890", CallSign: "FLIX"},
	}}}, catalogSource{
		ID: "spectrum-official-lineup", Label: "Spectrum national channel catalog", URL: spectrumGuideURL,
		Entries: spectrumCatalogEntries(catalog.Entries),
	})
	for _, fact := range result.Facts {
		if fact.Kind == lineupindex.FactCategory && fact.StationID == "103890" {
			if fact.Value != channelcategory.Movies || fact.RawValue != "Premiums" {
				t.Fatalf("Spectrum category fact = %+v", fact)
			}
			return
		}
	}
	t.Fatalf("missing Spectrum category fact: %+v", result.Facts)
}

func TestSpectrumCatalogJoinsEveryProviderOnlyByStationID(t *testing.T) {
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("fresh bundled catalog should not make a network request")
		return nil, errors.New("unexpected request")
	})}
	service := newServiceWithOptions(client, Options{Now: func() time.Time { return now }})
	result, err := service.FetchProviderEvidence(context.Background(), lineupindex.ProviderEvidenceRequest{
		Provider: web.Provider{Name: "Unlisted cable provider"},
		Grid: &web.GridResponse{Channels: []web.JSONChannel{
			{ChannelID: "59615", CallSign: "NOTFREEFORM"},
			{ChannelID: "OTHER", CallSign: "Freeform - East"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	aliases := make(map[string]bool)
	categories := make(map[string]string)
	for _, fact := range result.Facts {
		if fact.Kind == lineupindex.FactAlias {
			aliases[fact.StationID+"\x00"+fact.Value] = true
		} else if fact.Kind == lineupindex.FactCategory {
			categories[fact.StationID] = fact.Value
		}
		if !strings.Contains(fact.Method, "exact Gracenote station ID") {
			t.Fatalf("fact was not exact-ID evidence: %+v", fact)
		}
	}
	if !aliases["59615\x00Freeform - East"] || categories["59615"] != channelcategory.Entertainment {
		t.Fatalf("exact-ID evidence = %+v", result.Facts)
	}
	if categories["OTHER"] != "" {
		t.Fatalf("name-only row received Spectrum evidence: %+v", result.Facts)
	}
	if len(result.Sources) != 1 || result.Sources[0].Matched != 1 {
		t.Fatalf("sources = %+v", result.Sources)
	}
}

func TestSpectrumRefreshSuccessIsCachedForSevenDays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spectrum.json")
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	payload := spectrumPayload(400)
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.URL.String() != spectrumCatalogURL || request.Header.Get("Referer") != spectrumGuideURL {
			t.Fatalf("Spectrum request = %s, Referer=%q", request.URL, request.Header.Get("Referer"))
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload)), Request: request}, nil
	})}
	service := newServiceWithOptions(client, Options{SpectrumCatalogPath: path, Now: func() time.Time { return now }})
	request := lineupindex.ProviderEvidenceRequest{
		EvidenceRunID: "one", Provider: web.Provider{Name: "Unknown Cable"},
		Grid: &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: "900000"}}},
	}
	result, err := service.FetchProviderEvidence(context.Background(), request)
	if err != nil || calls != 1 || len(result.Sources) != 1 || result.Sources[0].Matched != 1 {
		t.Fatalf("first refresh result=%+v calls=%d err=%v", result, calls, err)
	}
	service.EndProviderEvidenceRun("one")
	request.EvidenceRunID = "two"
	if _, err := service.FetchProviderEvidence(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("same-week retrievals = %d, want 1", calls)
	}

	restartCalls := 0
	restarted := newServiceWithOptions(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		restartCalls++
		return nil, errors.New("unexpected request")
	})}, Options{SpectrumCatalogPath: path, Now: func() time.Time { return now.Add(6 * 24 * time.Hour) }})
	request.EvidenceRunID = "restart"
	if _, err := restarted.FetchProviderEvidence(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if restartCalls != 0 {
		t.Fatalf("restart retrievals = %d, want 0", restartCalls)
	}
}

func TestSpectrumRefreshFailureKeepsLastGoodAndThrottlesRetries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spectrum.json")
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	calls := 0
	failingClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusForbidden, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("blocked")), Request: request}, nil
	})}
	service := newServiceWithOptions(failingClient, Options{SpectrumCatalogPath: path, Now: func() time.Time { return now }})
	request := lineupindex.ProviderEvidenceRequest{
		Provider: web.Provider{Name: "Unknown Cable"},
		Grid:     &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: "32645"}}},
	}
	result, err := service.FetchProviderEvidence(context.Background(), request)
	if err != nil || calls != 1 || len(result.Facts) == 0 {
		t.Fatalf("fallback result=%+v calls=%d err=%v", result, calls, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cached spectrumCatalogFile
	if json.Unmarshal(data, &cached) != nil || cached.CheckedAt == "" || cached.LastRefreshError == "" || len(cached.Entries) != 378 {
		t.Fatalf("fallback cache = %+v", cached)
	}

	restartCalls := 0
	restarted := newServiceWithOptions(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		restartCalls++
		return nil, errors.New("unexpected request")
	})}, Options{SpectrumCatalogPath: path, Now: func() time.Time { return now.Add(24 * time.Hour) }})
	if _, err := restarted.FetchProviderEvidence(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if restartCalls != 0 {
		t.Fatalf("failed refresh retried before seven days: %d", restartCalls)
	}
}

func TestSpectrumRefreshRejectsSuspiciousCatalogShrink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spectrum.json")
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(spectrumPayload(300))), Request: request}, nil
	})}
	service := newServiceWithOptions(client, Options{SpectrumCatalogPath: path, Now: func() time.Time { return now }})
	request := lineupindex.ProviderEvidenceRequest{
		Provider: web.Provider{Name: "Unknown Cable"},
		Grid:     &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: "32645"}, {ChannelID: "900000"}}},
	}
	result, err := service.FetchProviderEvidence(context.Background(), request)
	if err != nil || calls != 1 {
		t.Fatalf("refresh result=%+v calls=%d err=%v", result, calls, err)
	}
	if len(result.Sources) != 1 || result.Sources[0].Matched != 1 {
		t.Fatalf("suspicious refresh replaced bundled catalog: %+v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cached spectrumCatalogFile
	if json.Unmarshal(data, &cached) != nil || len(cached.Entries) != 378 || !strings.Contains(cached.LastRefreshError, "shrank") {
		t.Fatalf("retained cache = %+v", cached)
	}
}

func TestCorruptSpectrumCacheFallsBackToBundle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spectrum.json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion":1,"sourceUrl":"https://example.invalid","entries":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	service := newServiceWithOptions(nil, Options{SpectrumCatalogPath: path, Now: func() time.Time {
		return time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	}})
	if len(service.spectrum.file.Entries) != 378 || service.spectrum.origin != "bundled" {
		t.Fatalf("catalog state = origin %q, entries %d", service.spectrum.origin, len(service.spectrum.file.Entries))
	}
}

func TestSpectrumRefreshTokenForcesOneEarlyAttempt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spectrum.json")
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	newServiceForToken := func(token string, calls *int) *Service {
		return newServiceWithOptions(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			(*calls)++
			return &http.Response{StatusCode: http.StatusForbidden, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("blocked")), Request: request}, nil
		})}, Options{SpectrumCatalogPath: path, SpectrumRefreshToken: token, Now: func() time.Time { return now }})
	}
	request := lineupindex.ProviderEvidenceRequest{Provider: web.Provider{Name: "Unknown Cable"}, Grid: &web.GridResponse{}}
	firstCalls := 0
	if _, err := newServiceForToken("manual-1", &firstCalls).FetchProviderEvidence(context.Background(), request); err != nil || firstCalls != 1 {
		t.Fatalf("first force calls=%d err=%v", firstCalls, err)
	}
	sameCalls := 0
	if _, err := newServiceForToken("manual-1", &sameCalls).FetchProviderEvidence(context.Background(), request); err != nil || sameCalls != 0 {
		t.Fatalf("same token calls=%d err=%v", sameCalls, err)
	}
	newCalls := 0
	if _, err := newServiceForToken("manual-2", &newCalls).FetchProviderEvidence(context.Background(), request); err != nil || newCalls != 1 {
		t.Fatalf("new token calls=%d err=%v", newCalls, err)
	}
}

func spectrumPayload(count int) string {
	channels := make([]map[string]any, 0, count)
	for index := 0; index < count; index++ {
		channels = append(channels, map[string]any{
			"TMSID": fmt.Sprintf("%d", 900000+index), "ChannelName": fmt.Sprintf("Test Channel %d", index),
			"Genre": []string{"Entertainment"}, "SPPCategory": []string{"TV Select"},
		})
	}
	data, _ := json.Marshal(map[string]any{"CluInfo": map[string]any{"CLU": map[string]any{"Channels": channels}}})
	return string(data)
}
