package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	body := recorder.Body.String()
	if recorder.Code != http.StatusOK || !strings.Contains(body, "Shape your current lineup") {
		t.Fatalf("page response = %d body %q", recorder.Code, recorder.Body.String())
	}
	for _, expected := range []string{`href="/favicon.svg"`, `id="visible-count"`, `id="view-toggle"`, `id="batch-toggle"`, `id="program-dialog"`, `document.createElement('select')`, `cursor: not-allowed`, `No SD/HD pair was identified`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("page is missing editor control %q", expected)
		}
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

func TestLineuparrBatchCategoryUpdateIsValidatedAndAtomic(t *testing.T) {
	server := newLineuparrTestServer(t, true)
	payload := `{"channelIds":["1001","1002"],"category":"Sports"}`
	request := httptest.NewRequest(http.MethodPost, "/api/lineuparr/categories", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.handleCategories(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("batch category response = %d body %s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/lineuparr/draft", nil)
	recorder = httptest.NewRecorder()
	server.handleDraft(recorder, request)
	var draft lineuparrbuilder.Draft
	if err := json.Unmarshal(recorder.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	for _, channel := range draft.Channels {
		if channel.Category != "Sports" || channel.CategorySource != "user" {
			t.Fatalf("batch category was not applied: %+v", draft.Channels)
		}
	}

	server = newLineuparrTestServer(t, true)
	payload = `{"channelIds":["1001","missing"],"category":"Movies"}`
	request = httptest.NewRequest(http.MethodPost, "/api/lineuparr/categories", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	server.handleCategories(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown batch channel response = %d body %s", recorder.Code, recorder.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/lineuparr/draft", nil)
	recorder = httptest.NewRecorder()
	server.handleDraft(recorder, request)
	if err := json.Unmarshal(recorder.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	for _, channel := range draft.Channels {
		if channel.Category == "Movies" {
			t.Fatalf("invalid batch partially updated channels: %+v", draft.Channels)
		}
	}
}

func TestLineuparrChannelProgramsUseOnlyActiveGuide(t *testing.T) {
	server := newLineuparrTestServer(t, true)
	config, _, _ := server.store.Get()
	programs := []guide.Program{{Channel: "200", Start: "20990101190000 +0000", Stop: "20990101200000 +0000", Title: "Other Channel"}}
	base := time.Date(2099, time.January, 1, 0, 0, 0, 0, time.UTC)
	for index := 25; index >= 0; index-- {
		start := base.Add(time.Duration(index) * time.Hour)
		programs = append(programs, guide.Program{
			Channel: "100", Start: start.Format("20060102150405 -0700"), Stop: start.Add(time.Hour).Format("20060102150405 -0700"),
			Title: fmt.Sprintf("Programme %02d", index), Description: "Headlines", Categories: []guide.Category{{Name: "News"}},
		})
	}
	server.state.UpdateForSource(&guide.TVGuide{
		LineupChannels: []guide.Channel{
			{ID: "100", PlacementID: "1001", ChannelNo: "2", CallSign: "TWO", Affiliate: "Two Network"},
			{ID: "200", PlacementID: "2001", ChannelNo: "3", CallSign: "THREE"},
		},
		Programs: programs,
	}, config.Fingerprint())

	request := httptest.NewRequest(http.MethodGet, "/api/lineuparr/channel-programs?channelId=1001", nil)
	recorder := httptest.NewRecorder()
	server.handleChannelPrograms(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("program response = %d body %s", recorder.Code, recorder.Body.String())
	}
	var response lineuparrChannelProgramsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.StationID != "100" || len(response.Programs) != 24 || response.Programs[0].Title != "Programme 00" || response.Programs[23].Title != "Programme 23" || response.Programs[0].Category != "News" {
		t.Fatalf("program response = %+v", response)
	}
}

func TestFaviconUsesExistingGNBrand(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/favicon.svg", nil)
	recorder := httptest.NewRecorder()
	handleFavicon(recorder, request)
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "image/svg+xml" || !strings.Contains(recorder.Body.String(), ">GN</text>") {
		t.Fatalf("favicon response = %d %q body %q", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
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
