package main

import (
	"context"
	"encoding/json"
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type addressProvidersFixture struct{}

func (addressProvidersFixture) FindProviders(context.Context, string, string, string) (*web.ProviderResponse, error) {
	return &web.ProviderResponse{Providers: []web.Provider{{Name: "Xfinity", LineupID: "X1", HeadendID: "X1", Device: "X"}, {Name: "DISH", LineupID: "D1", HeadendID: "D1", Device: "X"}}}, nil
}
func (addressProvidersFixture) FetchGrid(context.Context, web.Preferences, int64) (*web.GridResponse, error) {
	return &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: "S1", ChannelNo: "2", CallSign: "WCBS"}}}, nil
}

type addressEvidenceFixture struct {
	requests chan lineupindex.ProviderEvidenceRequest
}

func (f addressEvidenceFixture) FetchProviderEvidence(_ context.Context, r lineupindex.ProviderEvidenceRequest) (lineupindex.ProviderEvidenceResult, error) {
	f.requests <- r
	return lineupindex.ProviderEvidenceResult{}, nil
}

func TestPostalAddressPreflightSaveAndScan(t *testing.T) {
	server := newLineuparrTestServer(t, true)
	requests := make(chan lineupindex.ProviderEvidenceRequest, 4)
	index, err := lineupindex.NewService(lineupindex.ServiceConfig{Path: filepath.Join(t.TempDir(), "index.json"), Providers: addressProvidersFixture{}, Grids: addressProvidersFixture{}, Evidence: addressEvidenceFixture{requests}})
	if err != nil {
		t.Fatal(err)
	}
	server.marketIndex = index
	server.addressSearcher = &fakeProviderAddressSearcher{}
	request := httptest.NewRequest("GET", "/api/lineuparr/provider-address/config", nil)
	response := httptest.NewRecorder()
	server.handleProviderAddressConfig(response, request)
	var config providerAddressConfigResponse
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &config) != nil || !config.Required || !strings.Contains(config.ProviderLabel, "Xfinity") {
		t.Fatal(response.Body.String())
	}
	post := func(path, body string, handler http.HandlerFunc) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	failed := post("/run", `{"action":"postal"}`, server.handleAliasIndexRun)
	if failed.Code != 400 || index.Snapshot().Job.Running {
		t.Fatal("missing address started a scan", failed.Body.String())
	}
	// The browser must select only this schema, not copy the geocoder's id.
	saved := post("/config", `{"fingerprint":"`+config.Fingerprint+`","address":{"formattedAddress":"1 Test Street, 11743","streetAddress":"1 Test Street","postalCode":"11743","countryCode":"US"}}`, server.handleProviderAddressConfig)
	if saved.Code != 200 {
		t.Fatal(saved.Body.String())
	}
	response = httptest.NewRecorder()
	server.handleProviderAddressConfig(response, request)
	var restored providerAddressConfigResponse
	if json.Unmarshal(response.Body.Bytes(), &restored) != nil || restored.Address == nil {
		t.Fatal("saved address not restored")
	}
	started := post("/run", `{"action":"postal","sourceFingerprint":"`+config.Fingerprint+`"}`, server.handleAliasIndexRun)
	if started.Code != 202 {
		t.Fatal(started.Body.String())
	}
	for i := 0; i < 2; i++ {
		select {
		case r := <-requests:
			if (r.Provider.Name == "Xfinity") != (r.ServiceAddress.StreetAddress != "") {
				t.Fatal("address routed to wrong provider", r.Provider.Name)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("scan did not reach provider adapters")
		}
	}
	for i := 0; i < 300 && index.Snapshot().Job.Running; i++ {
		time.Sleep(time.Millisecond * 10)
	}
	if index.Snapshot().Job.Running || index.Snapshot().Job.LastError != "" {
		t.Fatal("scan failed", index.Snapshot().Job)
	}
}
