package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/geocode"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	lineuparrbuilder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"github.com/daniel-widrick/GraceNoteScraper/marketindex"
)

type fakeProviderAddressSearcher struct {
	query       string
	postalCode  string
	countryCode string
	results     []geocode.AddressResult
}

func (f *fakeProviderAddressSearcher) Search(_ context.Context, query, postalCode, countryCode string) ([]geocode.AddressResult, error) {
	f.query = query
	f.postalCode = postalCode
	f.countryCode = countryCode
	return append([]geocode.AddressResult(nil), f.results...), nil
}

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

func TestLineuparrDuplicateRemovalAcceptsReviewedSubset(t *testing.T) {
	server := newLineuparrTestServer(t, true)
	request := httptest.NewRequest(http.MethodPost, "/api/lineuparr/remove-duplicates", strings.NewReader(`{"channelIds":["1001"]}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.handleRemoveDuplicates(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"removed":1`) {
		t.Fatalf("selective duplicate response = %d body %s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/lineuparr/draft", nil)
	recorder = httptest.NewRecorder()
	server.handleDraft(recorder, request)
	var draft lineuparrbuilder.Draft
	if err := json.Unmarshal(recorder.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	if draft.Channels[0].Included || !draft.Channels[1].Included {
		t.Fatalf("reviewed duplicate inclusion = %+v", draft.Channels)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/lineuparr/remove-duplicates", strings.NewReader(`{"channelIds":["not-a-suggestion"]}`))
	request.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	server.handleRemoveDuplicates(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown duplicate response = %d body %s", recorder.Code, recorder.Body.String())
	}
}

func TestProviderAddressConfigSkipsRegionalOptimumAddress(t *testing.T) {
	server := newLineuparrTestServer(t, true)
	config, _, _ := server.store.Get()
	config.Gracenote.ProviderName = "Optimum of Woodbury - Digital Rebuild"
	config.Gracenote.Location = "Hicksville"
	if err := server.store.Save(config); err != nil {
		t.Fatal(err)
	}
	server.addressSearcher = &fakeProviderAddressSearcher{}

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
	if response.Required || response.Enabled || response.ProviderID != "optimum" || response.PostalCode != "11743" || response.CountryCode != "us" || response.AttributionURL != "" {
		t.Fatalf("address config = %+v", response)
	}
	if response.Message != "" {
		t.Fatalf("address message = %q", response.Message)
	}
}

func TestProviderAddressConfigUsesActiveLineupPostalCode(t *testing.T) {
	server := newLineuparrTestServer(t, true)
	config, _, _ := server.store.Get()
	config.Gracenote.ProviderName = "Optimum"
	config.Gracenote.Location = "Dallas"
	config.Gracenote.PostalCode = "75001"
	if err := server.store.Save(config); err != nil {
		t.Fatal(err)
	}
	server.addressSearcher = &fakeProviderAddressSearcher{}

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
	if !response.Required || !response.Enabled || response.ProviderID != "optimum" || response.PostalCode != "75001" || response.CountryCode != "us" || response.AttributionURL == "" {
		t.Fatalf("address config = %+v", response)
	}
	if !strings.Contains(response.Message, "not persisted") {
		t.Fatalf("address privacy message = %q", response.Message)
	}
}

func TestProviderAddressConfigDoesNotOfferSearchToPostalOnlyProvider(t *testing.T) {
	server := newLineuparrTestServer(t, true)
	config, _, _ := server.store.Get()
	config.Gracenote.ProviderName = "Verizon FiOS"
	if err := server.store.Save(config); err != nil {
		t.Fatal(err)
	}
	server.addressSearcher = &fakeProviderAddressSearcher{}

	request := httptest.NewRequest(http.MethodGet, "/api/lineuparr/provider-address/config", nil)
	recorder := httptest.NewRecorder()
	server.handleProviderAddressConfig(recorder, request)
	var response providerAddressConfigResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Required || response.Enabled || response.AttributionURL != "" || response.ProviderID != "verizon-fios" {
		t.Fatalf("address config = %+v", response)
	}
}

func TestProviderAddressSearchUsesActiveLineupLocation(t *testing.T) {
	server := newLineuparrTestServer(t, true)
	config, _, _ := server.store.Get()
	config.Gracenote.ProviderName = "Optimum"
	config.Gracenote.Location = "Dallas"
	config.Gracenote.PostalCode = "75001"
	if err := server.store.Save(config); err != nil {
		t.Fatal(err)
	}
	searcher := &fakeProviderAddressSearcher{results: []geocode.AddressResult{{
		ID: "way:1", FormattedAddress: "1 Main Street, Dallas, TX 75001", PostalCode: "75001",
	}}}
	server.addressSearcher = searcher

	request := httptest.NewRequest(http.MethodPost, "/api/lineuparr/provider-address/search", strings.NewReader(`{"query":"1 Main Street"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.handleProviderAddressSearch(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("address search response = %d body %s", recorder.Code, recorder.Body.String())
	}
	if searcher.query != "1 Main Street" || searcher.postalCode != "75001" || searcher.countryCode != "us" {
		t.Fatalf("search inputs = %+v", searcher)
	}
	if !strings.Contains(recorder.Body.String(), "1 Main Street") || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("address search response = %s", recorder.Body.String())
	}
}

func TestValidateEphemeralProviderAddress(t *testing.T) {
	address, err := validateEphemeralProviderAddress(marketindex.ProviderAddress{
		FormattedAddress: "  123 Main Street, Huntington, NY 11743  ", StreetAddress: "123 Main Street",
		City: "Huntington", State: "NY", PostalCode: "11743", CountryCode: "us",
	}, "11743")
	if err != nil || address.FormattedAddress != "123 Main Street, Huntington, NY 11743" || address.CountryCode != "US" {
		t.Fatalf("validated address = %+v, error = %v", address, err)
	}
	if _, err := validateEphemeralProviderAddress(marketindex.ProviderAddress{FormattedAddress: "123 Main Street, Boston, MA 02108", PostalCode: "02108"}, "11743"); err == nil {
		t.Fatal("accepted an address outside the active lineup postal code")
	}
}

func TestValidateEphemeralProviderAddressAcceptsUSZIPPlusFour(t *testing.T) {
	address, err := validateEphemeralProviderAddress(marketindex.ProviderAddress{
		FormattedAddress: "1 Main Street, Huntington, NY 11743-1234",
		StreetAddress:    "1 Main Street", City: "Huntington", State: "NY",
		PostalCode: "11743-1234", CountryCode: "us",
	}, "11743")
	if err != nil || address.CountryCode != "US" {
		t.Fatalf("address = %+v, error = %v", address, err)
	}
}

func TestScheduleCategoryHintsRequireDominantUsefulGuideTime(t *testing.T) {
	programs := make([]guide.Program, 0, 18)
	for range 8 {
		programs = append(programs, guide.Program{
			Channel: "sports", Length: "60", Categories: []guide.Category{{Name: "sports"}},
		})
	}
	for range 2 {
		programs = append(programs, guide.Program{
			Channel: "sports", Length: "60", Categories: []guide.Category{{Name: "series"}},
		})
	}
	for range 8 {
		programs = append(programs, guide.Program{
			Channel: "mixed", Length: "60", Categories: []guide.Category{{Name: "news"}},
		})
	}
	for range 4 {
		programs = append(programs, guide.Program{
			Channel: "mixed", Length: "60", Categories: []guide.Category{{Name: "movie"}},
		})
	}
	hints := scheduleCategoryHints(&guide.TVGuide{
		Channels: []guide.Channel{{ID: "sports"}, {ID: "mixed"}}, Programs: programs,
	})
	if hint := hints["sports"]; hint == nil || hint.Value != "Sports" || !strings.Contains(hint.Method, "80%") {
		t.Fatalf("sports hint = %+v", hint)
	}
	if hint := hints["mixed"]; hint != nil {
		t.Fatalf("mixed guide should remain unresolved, got %+v", hint)
	}
}

func TestScheduleCategoryHintsTreatFamilyAsKidsOnlyForKidsNetworks(t *testing.T) {
	programs := make([]guide.Program, 0, 16)
	for _, channelID := range []string{"kids", "general"} {
		for range 8 {
			programs = append(programs, guide.Program{
				Channel: channelID, Length: "60", Categories: []guide.Category{{Name: "family"}},
			})
		}
	}
	hints := scheduleCategoryHints(&guide.TVGuide{
		Channels: []guide.Channel{
			{ID: "kids", CallSign: "NICKJR"},
			{ID: "general", CallSign: "HALLMARK"},
		},
		Programs: programs,
	})
	if hint := hints["kids"]; hint == nil || hint.Value != "Kids" {
		t.Fatalf("kids hint = %+v", hint)
	}
	if hint := hints["general"]; hint != nil {
		t.Fatalf("general family network should remain unresolved, got %+v", hint)
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
