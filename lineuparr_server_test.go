package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	lineuparrbuilder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

func newLineuparrTestServer(t *testing.T, configured bool) *lineuparrServer {
	t.Helper()
	store, err := appconfig.LoadStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if configured {
		if err := store.Save(appconfig.Config{
			Version: appconfig.CurrentVersion,
			Gracenote: appconfig.GracenoteConfig{
				Country: "USA", PostalCode: "11743", Language: "en-us", ProviderType: "CABLE", Device: "X",
				LineupID: "USA-TEST", ProviderName: "Test Cable", HeadendID: "TEST",
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	stateStore, err := lineuparrbuilder.LoadStateStore(filepath.Join(t.TempDir(), "lineuparr.json"))
	if err != nil {
		t.Fatal(err)
	}
	builder := lineuparrbuilder.NewService(stateStore, lineuparrbuilder.ServiceOptions{CacheDir: filepath.Join(t.TempDir(), "cache")})
	state := &GuideState{}
	config, configured, _ := store.Get()
	fingerprint := ""
	if configured {
		fingerprint = config.Fingerprint()
	}
	state.UpdateForSource(&guide.TVGuide{LineupChannels: []guide.Channel{
		{ID: "100", PlacementID: "1001", ChannelNo: "2", CallSign: "TWO", Affiliate: "Two Network", EventCallSigns: []string{"TWO"}},
		{ID: "100", PlacementID: "1002", ChannelNo: "502", CallSign: "TWOHD", Affiliate: "Two Network", EventCallSigns: []string{"TWOHD"}},
	}}, fingerprint)
	return &lineuparrServer{store: store, state: state, builder: builder}
}

func TestLineuparrPageRedirectsUntilConfigured(t *testing.T) {
	server := newLineuparrTestServer(t, false)
	request := httptest.NewRequest(http.MethodGet, "/lineuparr", nil)
	recorder := httptest.NewRecorder()
	server.handlePage(recorder, request)
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/setup" {
		t.Fatalf("response = %d location %q", recorder.Code, recorder.Header().Get("Location"))
	}
}

func TestLineuparrPageAndDraftUseRawProviderPositions(t *testing.T) {
	server := newLineuparrTestServer(t, true)
	request := httptest.NewRequest(http.MethodGet, "/lineuparr", nil)
	recorder := httptest.NewRecorder()
	server.handlePage(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "Shape your current lineup") {
		t.Fatalf("page response = %d body %q", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "Queued — click to cancel") {
		t.Fatal("queued alias action is missing from the Lineuparr page")
	}

	request = httptest.NewRequest(http.MethodGet, "/api/lineuparr/draft", nil)
	recorder = httptest.NewRecorder()
	server.handleDraft(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("draft response = %d body %s", recorder.Code, recorder.Body.String())
	}
	var draft lineuparrbuilder.Draft
	if err := json.Unmarshal(recorder.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	if draft.Total != 2 || draft.Included != 2 || draft.Channels[0].ID == draft.Channels[1].ID {
		t.Fatalf("draft positions = %+v", draft.Channels)
	}
}

func TestProviderAddressConfigUsesActiveLineupPostalCode(t *testing.T) {
	server := newLineuparrTestServer(t, true)
	config, _, _ := server.store.Get()
	config.Gracenote.ProviderName = "Optimum of Woodbury - Digital Rebuild"
	if err := server.store.Save(config); err != nil {
		t.Fatal(err)
	}
	server.googleMapsBrowserAPIKey = "browser-key"

	request := httptest.NewRequest(http.MethodGet, "/api/lineuparr/provider-address/config", nil)
	recorder := httptest.NewRecorder()
	server.handleProviderAddressConfig(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("address config response = %d body %s", recorder.Code, recorder.Body.String())
	}
	var response providerAddressConfigResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Required || !response.Enabled || response.ProviderID != "optimum" || response.PostalCode != "11743" || response.CountryCode != "us" || response.BrowserAPIKey != "browser-key" {
		t.Fatalf("address config = %+v", response)
	}
	if !strings.Contains(response.Message, "browser only") {
		t.Fatalf("address privacy message = %q", response.Message)
	}
}

func TestProviderAddressConfigDoesNotExposeKeyToPostalOnlyProvider(t *testing.T) {
	server := newLineuparrTestServer(t, true)
	config, _, _ := server.store.Get()
	config.Gracenote.ProviderName = "Verizon FiOS"
	if err := server.store.Save(config); err != nil {
		t.Fatal(err)
	}
	server.googleMapsBrowserAPIKey = "browser-key"

	request := httptest.NewRequest(http.MethodGet, "/api/lineuparr/provider-address/config", nil)
	recorder := httptest.NewRecorder()
	server.handleProviderAddressConfig(recorder, request)
	var response providerAddressConfigResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Required || response.Enabled || response.BrowserAPIKey != "" || response.ProviderID != "verizon-fios" {
		t.Fatalf("address config = %+v", response)
	}
}

func TestLineuparrChannelChoiceAndExport(t *testing.T) {
	server := newLineuparrTestServer(t, true)
	payload := `{"channelId":"1001","included":false,"category":"Local"}`
	request := httptest.NewRequest(http.MethodPost, "/api/lineuparr/channel", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.handleChannel(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("channel response = %d body %s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/lineuparr/export", nil)
	recorder = httptest.NewRecorder()
	server.handleExport(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("export response = %d body %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Disposition"); !strings.Contains(got, "US_Test-Cable-11743_lineup.json") {
		t.Fatalf("Content-Disposition = %q", got)
	}
	var export lineuparrbuilder.ExportFile
	if err := json.Unmarshal(recorder.Body.Bytes(), &export); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, channels := range export.Categories {
		count += len(channels)
	}
	if count != 1 {
		t.Fatalf("exported channel count = %d, categories = %+v", count, export.Categories)
	}
}

func TestLineuparrDuplicatePlacementKeysRemainEditable(t *testing.T) {
	server := newLineuparrTestServer(t, true)
	config, _, _ := server.store.Get()
	server.state.UpdateForSource(&guide.TVGuide{LineupChannels: []guide.Channel{
		{ID: "100", PlacementID: "repeat", ChannelNo: "2", CallSign: "TWO"},
		{ID: "101", PlacementID: "repeat", ChannelNo: "3", CallSign: "THREE"},
	}}, config.Fingerprint())

	request := httptest.NewRequest(http.MethodGet, "/api/lineuparr/draft", nil)
	recorder := httptest.NewRecorder()
	server.handleDraft(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("draft response = %d body %s", recorder.Code, recorder.Body.String())
	}
	var draft lineuparrbuilder.Draft
	if err := json.Unmarshal(recorder.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	if len(draft.Channels) != 2 || draft.Channels[0].ID == draft.Channels[1].ID {
		t.Fatalf("duplicate placement IDs were not made unique: %+v", draft.Channels)
	}

	payload := `{"channelId":"repeat-2","included":false}`
	request = httptest.NewRequest(http.MethodPost, "/api/lineuparr/channel", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	server.handleChannel(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("editing second duplicate response = %d body %s", recorder.Code, recorder.Body.String())
	}
}

func TestLineuparrBulkChangesRequireJSON(t *testing.T) {
	server := newLineuparrTestServer(t, true)
	request := httptest.NewRequest(http.MethodPost, "/api/lineuparr/restore-all", nil)
	recorder := httptest.NewRecorder()
	server.handleRestoreAll(recorder, request)
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("restore without JSON response = %d", recorder.Code)
	}
}
