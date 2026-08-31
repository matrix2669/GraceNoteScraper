package providersource

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/marketindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestOfficialSourceID(t *testing.T) {
	tests := map[string]string{
		"Optimum of Woodbury - Digital": "optimum-official-lineup",
		"Cablevision":                   "optimum-official-lineup",
		"Verizon FiOS":                  "verizon-fios-official-lineup",
		"DIRECTV New York":              "directv-official-lineup",
		"DISH Network":                  "dish-official-lineup",
		"AFN Satellite":                 "afn-official-guide",
		"GLORYSTAR":                     "glorystar-official-lineup",
		"AT&T U-Verse":                  "att-uverse-official-lineup",
		"Comcast Xfinity":               "xfinity-official-lineup",
		"Charter Spectrum":              "spectrum-official-lineup",
		"Broadstream":                   "broadstar-official-lineup",
		"Unknown Cable":                 "",
	}
	for providerName, want := range tests {
		t.Run(providerName, func(t *testing.T) {
			if got := OfficialSourceID(providerName); got != want {
				t.Fatalf("OfficialSourceID(%q) = %q, want %q", providerName, got, want)
			}
		})
	}
}

type failingBody struct {
	message string
}

func (body failingBody) Read([]byte) (int, error) { return 0, errors.New(body.message) }
func (failingBody) Close() error                  { return nil }

func TestDISHOfficialServiceMatchesExactChannelNumber(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != dishURL {
			t.Fatalf("request URL = %s", request.URL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"Channels":[{"name":"FOX News (FXNWS) HD","catg":"News & Info|","calltr":"FXNWS","ChannelNo":"205"},{"name":"SEC (SEC) HD","catg":"News & Info|","calltr":"SEC","ChannelNo":"404"}]}`)),
			Header:     make(http.Header),
		}, nil
	})}
	service := newService(client)
	result, err := service.FetchProviderEvidence(context.Background(), marketindex.ProviderEvidenceRequest{
		Provider: web.Provider{Name: "DISH Satellite", LineupID: "DISH"}, LineupKey: "DISH", PostalCode: "11743",
		Grid: &web.GridResponse{Channels: []web.JSONChannel{
			{ChannelID: "NEWS", ChannelNo: "205", CallSign: "FXNWS"},
			{ChannelID: "SEC", ChannelNo: "404", CallSign: "SEC"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	categories := make(map[string]string)
	aliases := make(map[string][]string)
	for _, fact := range result.Facts {
		if fact.Kind == marketindex.FactCategory {
			categories[fact.StationID] = fact.Value
		} else {
			aliases[fact.StationID] = append(aliases[fact.StationID], fact.Value)
		}
	}
	if categories["NEWS"] != "News & Weather" || categories["SEC"] != "Sports" {
		t.Fatalf("categories = %+v", categories)
	}
	if !contains(aliases["NEWS"], "FOX News") || !contains(aliases["NEWS"], "FXNWS") {
		t.Fatalf("aliases = %+v", aliases)
	}
	if len(result.Sources) != 1 || result.Sources[0].Matched != 2 {
		t.Fatalf("source status = %+v", result.Sources)
	}
}

func TestOfficialCategoryCoversSameNumberVariantsWithExactIdentity(t *testing.T) {
	request := marketindex.ProviderEvidenceRequest{Grid: &web.GridResponse{Channels: []web.JSONChannel{
		{ChannelID: "MTV-SD", ChannelNo: "160", CallSign: "MTV", Events: []web.JSONEvent{{CallSign: "MTV"}}},
		{ChannelID: "MTV-HD", ChannelNo: "160", CallSign: "MTVHD", Events: []web.JSONEvent{{CallSign: "MTV"}}},
		{ChannelID: "UNRELATED", ChannelNo: "160", CallSign: "OTHER", Events: []web.JSONEvent{{CallSign: "OTHER"}}},
	}}}
	result := matchCatalog(request, catalogSource{
		ID: "provider", Label: "Provider", Method: "official provider row",
		Entries: []catalogEntry{{Numbers: []string{"160"}, Name: "MTV", Category: "Music"}},
	})
	categories := make(map[string]string)
	aliases := make(map[string]bool)
	for _, fact := range result.Facts {
		if fact.Kind == marketindex.FactCategory {
			categories[fact.StationID] = fact.Value
		} else if fact.Kind == marketindex.FactAlias {
			aliases[fact.StationID] = true
		}
		if !strings.Contains(fact.Method, "exact provider channel number plus exact identity") {
			t.Fatalf("fact method = %q", fact.Method)
		}
	}
	if categories["MTV-SD"] != "Music" || categories["MTV-HD"] != "Music" || categories["UNRELATED"] != "" {
		t.Fatalf("categories = %+v", categories)
	}
	if len(aliases) != 0 {
		t.Fatalf("shared alias should remain suppressed across station IDs: %+v", aliases)
	}
	if len(result.Sources) != 1 || result.Sources[0].Matched != 2 || result.Sources[0].Categories != 2 {
		t.Fatalf("source status = %+v", result.Sources)
	}
}

func TestOptimumSnapshotRequiresMatchingPostalCode(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline test")
	})}
	service := newServiceWithOptions(client, Options{UseEmbeddedCatalogs: true})
	request := marketindex.ProviderEvidenceRequest{
		Provider:   web.Provider{Name: "Optimum of Woodbury - Digital Rebuild"},
		PostalCode: "11743",
		Grid:       &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: "ESPN", ChannelNo: "210", CallSign: "ESPN"}}},
	}
	result, err := service.FetchProviderEvidence(context.Background(), request)
	// Live and optional embedded sources remain isolated: an unavailable live
	// source reports its own error while the explicitly enabled snapshot can
	// still contribute exact facts.
	if err == nil {
		t.Fatal("expected the offline live source to report an error")
	}
	found := false
	for _, fact := range result.Facts {
		if fact.StationID == "ESPN" && fact.Kind == marketindex.FactCategory && fact.Value == "Sports" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Optimum category facts = %+v", result.Facts)
	}
	request.PostalCode = "75001"
	result, err = service.FetchProviderEvidence(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Facts) != 0 || len(result.Sources) != 1 || result.Sources[0].Status != "address-required" {
		t.Fatalf("out-of-market evidence = %+v", result)
	}
}

func TestEmbeddedProviderCatalogsAreOffByDefault(t *testing.T) {
	request := marketindex.ProviderEvidenceRequest{
		Provider: web.Provider{Name: "Optimum of Woodbury - Digital Rebuild"}, PostalCode: "11743",
		Grid: &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: "ESPN", ChannelNo: "210", CallSign: "ESPN"}}},
	}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline test")
	})}
	result, err := newService(client).FetchProviderEvidence(context.Background(), request)
	if err == nil {
		t.Fatal("expected the live source failure")
	}
	if len(result.Facts) != 0 || len(result.Sources) != 1 || result.Sources[0].Status != "error" {
		t.Fatalf("default embedded provider evidence = %+v", result)
	}
}

func TestEmbeddedCatalogOmitsObsoleteRegisteredPlaceholders(t *testing.T) {
	result, err := newServiceWithOptions(nil, Options{UseEmbeddedCatalogs: true}).FetchProviderEvidence(context.Background(), marketindex.ProviderEvidenceRequest{
		Provider: web.Provider{Name: "Xfinity"}, PostalCode: "11743",
		Grid: &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: "ONE", ChannelNo: "1"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sources) != 1 || result.Sources[0].Status != "address-required" {
		t.Fatalf("sources = %+v", result.Sources)
	}
}

func TestPublicHTMLParsers(t *testing.T) {
	directv := []byte(`<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"data":[{"props":{"channelData":{"channels":[{"name":"ESPN","chnlNum":"206","category":"Sports Channels"},{"name":"WNBA on ION 1","chnlNum":"305","category":"Sports Channels"}]}}}]}}}</script>`)
	entries, err := parseDIRECTV(directv)
	if err != nil || len(entries) != 2 || entries[0].Name != "ESPN" || entries[0].Category != "Sports" {
		t.Fatalf("DIRECTV entries = %+v, error = %v", entries, err)
	}
	if entries[1].Name != "WNBA on ION 1" || !entries[1].EventFeed || entries[1].Category != "PPV & Events" {
		t.Fatalf("DIRECTV event entry = %+v", entries[1])
	}
	glorystar := parseGlorystar([]byte(`<table><tr><td>101</td><td><img alt="TBN"></td><td><a>Trinity Broadcasting<br>Network</a></td><td>English</td></tr></table>`))
	if len(glorystar) != 1 || glorystar[0].Name != "Trinity Broadcasting Network" || glorystar[0].Category != "Faith" {
		t.Fatalf("Glorystar entries = %+v", glorystar)
	}
	xfinity, err := parseXfinity([]byte(`{"channels":[{"channelNumber":"205","channelName":"FOX News","category":"News"}]}`))
	if err != nil || len(xfinity) != 1 || xfinity[0].Numbers[0] != "205" {
		t.Fatalf("Xfinity entries = %+v, error = %v", xfinity, err)
	}
}

func TestEventFeedDoesNotAttachToPermanentChannelByNumber(t *testing.T) {
	request := marketindex.ProviderEvidenceRequest{Grid: &web.GridResponse{Channels: []web.JSONChannel{
		{ChannelID: "ION", ChannelNo: "305", CallSign: "IONDHD", AffiliateName: "ION: INDEPENDENT TELEVISION"},
	}}}
	result := matchCatalog(request, catalogSource{
		ID: "directv", Label: "DIRECTV", Entries: []catalogEntry{
			{Numbers: []string{"305"}, Name: "ION Television East HD", Category: "Entertainment"},
			{Numbers: []string{"305"}, Name: "WNBA on ION 1", Category: "PPV & Events", EventFeed: true},
			{Numbers: []string{"305"}, Name: "WNBA on ION 2", Category: "PPV & Events", EventFeed: true},
		},
	})
	for _, fact := range result.Facts {
		if strings.Contains(fact.Value, "WNBA") || fact.Value == "PPV & Events" {
			t.Fatalf("event evidence contaminated permanent ION station: %+v", result.Facts)
		}
	}
	if len(result.Sources) != 1 || result.Sources[0].Matched != 1 {
		t.Fatalf("source status = %+v", result.Sources)
	}
}

func TestAmbiguousProviderNumberRequiresExactIdentity(t *testing.T) {
	request := marketindex.ProviderEvidenceRequest{Grid: &web.GridResponse{Channels: []web.JSONChannel{
		{ChannelID: "ONE", ChannelNo: "50", CallSign: "FIRST"},
	}}}
	result := matchCatalog(request, catalogSource{ID: "provider", Label: "Provider", Entries: []catalogEntry{
		{Numbers: []string{"50"}, Name: "First Network"},
		{Numbers: []string{"50"}, Name: "Second Network"},
	}})
	if len(result.Facts) != 0 || result.Sources[0].Matched != 0 {
		t.Fatalf("ambiguous provider number produced evidence: %+v", result)
	}
}

func TestEmbeddedChannelParserRequiresExplicitChannelNumber(t *testing.T) {
	entries := parseEmbeddedChannelJSON([]byte(`<script type="application/ld+json">{"name":"Unrelated product","number":"205"}</script>`))
	if len(entries) != 0 {
		t.Fatalf("unrelated structured data became channel evidence: %+v", entries)
	}
	entries = parseEmbeddedChannelJSON([]byte(`<script type="application/json">{"channelName":"FOX News","channelNumber":"205"}</script>`))
	if len(entries) != 1 || entries[0].Name != "FOX News" {
		t.Fatalf("channel data = %+v", entries)
	}
}

func TestOptimumMarketPDFSelectionRejectsUnapprovedHosts(t *testing.T) {
	page := []byte(`<h3><a href="https://malicious.test/woodbury.pdf">Woodbury</a></h3><h3><a href="https://static.tvlistings.optimum.net/lineups/woodbury.pdf">Woodbury</a></h3>`)
	link, label := selectOptimumPDF(page, "Optimum of Woodbury")
	if link != "https://static.tvlistings.optimum.net/lineups/woodbury.pdf" || label != "Woodbury" {
		t.Fatalf("selected %q (%q)", link, label)
	}
	if link, _ := selectOptimumPDF([]byte(`<h3><a href="https://malicious.test/woodbury.pdf">Woodbury</a></h3>`), "Optimum of Woodbury"); link != "" {
		t.Fatalf("selected unapproved PDF URL %q", link)
	}
}

func TestProviderFetchRejectsOversizedResponse(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", maxHTMLBytes+1))),
			Request:    request,
		}, nil
	})}
	_, err := newService(client).fetchBytes(context.Background(), directvURL, "text/html", "test provider", maxHTMLBytes, false)
	if err == nil || !strings.Contains(err.Error(), "4 MiB") {
		t.Fatalf("oversized response error = %v", err)
	}
}

func TestPDFRowParsersPreserveNamesNumbersAndRenames(t *testing.T) {
	lines := []pdfLine{{Words: []pdfWord{
		{Text: "TVG (FanDuel TV)", X: 30, W: 80}, {Text: "602", X: 260, W: 20},
	}}}
	entries := parsePairedLines(lines, []pdfPair{{nameStart: 20, numberStart: 250, end: 320}})
	if len(entries) != 1 || entries[0].Name != "TVG (FanDuel TV)" || entries[0].Numbers[0] != "602" {
		t.Fatalf("paired PDF entries = %+v", entries)
	}
	name, aliases, _ := deriveNameEvidence("FYI (Biography)")
	if name != "FYI" || !contains(aliases, "Biography") {
		t.Fatalf("renamed network evidence = %q, %+v", name, aliases)
	}
}

func TestOptimumPDFRowsRetainSectionCategoriesAndResetAtPageBoundary(t *testing.T) {
	pairs := []pdfPair{
		{nameStart: 20, numberStart: 200, end: 250},
		{nameStart: 300, numberStart: 480, end: 530},
	}
	lines := []pdfLine{
		{Page: 1, Y: 100, Words: []pdfWord{{Text: "Networks", X: 20, W: 50}, {Text: "Ch.", X: 200, W: 12}}},
		{Page: 1, Y: 95, Words: []pdfWord{{Text: "A&E", X: 300, W: 20}, {Text: "46", X: 480, W: 12}}},
		{Page: 1, Y: 90, Words: []pdfWord{{Text: "AMC", X: 20, W: 20}, {Text: "43", X: 200, W: 12}}},
		{Page: 1, Y: 80, Words: []pdfWord{{Text: "Kids", X: 300, W: 25}, {Text: "Ch.", X: 480, W: 12}}},
		{Page: 1, Y: 70, Words: []pdfWord{{Text: "Nickelodeon", X: 300, W: 60}, {Text: "121", X: 480, W: 18}}},
		{Page: 1, Y: 60, Words: []pdfWord{{Text: "Sports", X: 20, W: 35}, {Text: "Ch.", X: 200, W: 12}}},
		{Page: 1, Y: 50, Words: []pdfWord{{Text: "ESPN", X: 20, W: 25}, {Text: "210", X: 200, W: 18}}},
		{Page: 1, Y: 40, Words: []pdfWord{{Text: "On Demand & PPV", X: 300, W: 100}, {Text: "Ch.", X: 480, W: 12}}},
		{Page: 1, Y: 30, Words: []pdfWord{{Text: "HBO On Demand", X: 300, W: 80}, {Text: "300", X: 480, W: 18}}},
		{Page: 1, Y: 20, Words: []pdfWord{{Text: "Networks", X: 20, W: 50}, {Text: "Ch.", X: 200, W: 12}}},
		{Page: 1, Y: 10, Words: []pdfWord{{Text: "132", X: 200, W: 18}}},
		{Page: 2, Y: 100, Words: []pdfWord{{Text: "Alphabetical Only", X: 20, W: 90}, {Text: "999", X: 200, W: 18}}},
	}
	entries := parseOptimumLines(lines, pairs)
	byNumber := make(map[string]catalogEntry)
	for _, entry := range entries {
		byNumber[entry.Numbers[0]] = entry
	}
	want := map[string]string{"43": "Networks", "46": "Networks", "121": "Kids", "132": "Networks", "210": "Sports", "300": "On Demand & PPV", "999": ""}
	for number, category := range want {
		entry, ok := byNumber[number]
		if !ok || entry.Category != category {
			t.Errorf("channel %s = %+v, want category %q", number, entry, category)
		}
		if (category == "") != (entry.CategoryMethod == "") {
			t.Errorf("channel %s category method = %q for category %q", number, entry.CategoryMethod, category)
		}
	}
}

func TestOptimumCategorizedPagesRequireMultipleSectionHeadings(t *testing.T) {
	pairs := []pdfPair{{nameStart: 20, numberStart: 200, end: 250}}
	lines := []pdfLine{
		{Page: 1, Y: 100, Words: []pdfWord{{Text: "Networks", X: 20, W: 50}}},
		{Page: 1, Y: 90, Words: []pdfWord{{Text: "Kids", X: 20, W: 25}}},
		{Page: 1, Y: 80, Words: []pdfWord{{Text: "Sports", X: 20, W: 35}}},
		{Page: 2, Y: 100, Words: []pdfWord{{Text: "Networks", X: 20, W: 50}}},
		{Page: 2, Y: 90, Words: []pdfWord{{Text: "AMC", X: 20, W: 20}, {Text: "43", X: 200, W: 12}}},
	}
	filtered := optimumCategorizedPages(lines, pairs)
	if len(filtered) != 3 {
		t.Fatalf("categorized page lines = %+v", filtered)
	}
	for _, line := range filtered {
		if line.Page != 1 {
			t.Fatalf("flat index page was accepted as categorized: %+v", line)
		}
	}
}

func TestMergeOptimumCategoryEvidenceUsesExactNumbersAndExpandsPositions(t *testing.T) {
	flat := []catalogEntry{
		{Numbers: []string{"2", "702"}, Name: "WCBS"},
		{Numbers: []string{"210"}, Name: "ESPN"},
		{Numbers: []string{"999"}, Name: "Uncategorized"},
	}
	categorized := []catalogEntry{
		{Numbers: []string{"2"}, Name: "CBS", Category: "Networks", CategoryMethod: "Optimum PDF section heading"},
		{Numbers: []string{"702"}, Name: "CBS HD", Category: "Networks", CategoryMethod: "Optimum PDF section heading"},
		{Numbers: []string{"210"}, Name: "ESPN", Category: "Sports", CategoryMethod: "Optimum PDF section heading"},
		{Numbers: []string{"850"}, Name: "Stingray Music", Category: "Music", CategoryMethod: "Optimum PDF section heading"},
	}
	entries := mergeOptimumCategoryEvidence(flat, categorized)
	byNumber := make(map[string]catalogEntry)
	for _, entry := range entries {
		if len(entry.Numbers) != 1 {
			t.Fatalf("entry was not expanded by provider position: %+v", entry)
		}
		byNumber[entry.Numbers[0]] = entry
	}
	for _, number := range []string{"2", "702"} {
		entry := byNumber[number]
		if entry.Name != "WCBS" || entry.Category != "Networks" {
			t.Errorf("channel %s = %+v", number, entry)
		}
	}
	if entry := byNumber["210"]; entry.Name != "ESPN" || entry.Category != "Sports" {
		t.Errorf("channel 210 = %+v", entry)
	}
	if entry := byNumber["850"]; entry.Name != "Stingray Music" || entry.Category != "Music" {
		t.Errorf("categorized-only range position = %+v", entry)
	}
	if entry := byNumber["999"]; entry.Name != "Uncategorized" || entry.Category != "" {
		t.Errorf("unclassified channel = %+v", entry)
	}
}

func TestOptimumCategoryEvidenceRejectsConflictingNumber(t *testing.T) {
	entries := mergeOptimumCategoryEvidence(
		[]catalogEntry{{Numbers: []string{"100"}, Name: "Example"}},
		[]catalogEntry{
			{Numbers: []string{"100"}, Name: "Example", Category: "Networks"},
			{Numbers: []string{"100"}, Name: "Example", Category: "Sports"},
		},
	)
	if len(entries) != 1 || entries[0].Category != "" {
		t.Fatalf("conflicting evidence was applied: %+v", entries)
	}
}

func TestXfinityErrorsNeverExposeServiceAddress(t *testing.T) {
	const address = "123 Private Street, Huntington, NY 11743"
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("transport included " + address)
	})}
	result, err := newService(client).FetchProviderEvidence(context.Background(), marketindex.ProviderEvidenceRequest{
		Provider: web.Provider{Name: "Xfinity"}, PostalCode: "11743", ServiceAddress: marketindex.ProviderAddress{FormattedAddress: address},
		Grid: &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: "ONE", ChannelNo: "1"}}},
	})
	if err == nil || strings.Contains(err.Error(), address) || len(result.Sources) != 1 || strings.Contains(result.Sources[0].URL, address) || strings.Contains(result.Sources[0].Message, address) {
		t.Fatalf("address leaked in result=%+v error=%v", result, err)
	}
}

func TestXfinityResponseReadErrorsNeverExposeServiceAddress(t *testing.T) {
	const address = "123 Private Street, Huntington, NY 11743"
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header), Request: request,
			Body: failingBody{message: "read failed for " + address},
		}, nil
	})}
	result, err := newService(client).FetchProviderEvidence(context.Background(), marketindex.ProviderEvidenceRequest{
		Provider: web.Provider{Name: "Xfinity"}, PostalCode: "11743",
		ServiceAddress: marketindex.ProviderAddress{FormattedAddress: address},
		Grid:           &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: "ONE", ChannelNo: "1"}}},
	})
	if err == nil || strings.Contains(err.Error(), address) || strings.Contains(result.Sources[0].Message, address) {
		t.Fatalf("address leaked in result=%+v error=%v", result, err)
	}
}

func TestOptimumAddressQualifiedLineupParser(t *testing.T) {
	entries, err := parseOptimumLineup([]byte(`{"serviceabilityDetailLineupResponse":{"networks":[{"channelNumber":210,"name":"ESPN"},{"channelNumber":"160","name":"FYI (Biography)"},{"channelNumber":"999","name":"Example OTT"}]}}`))
	if err != nil || len(entries) != 2 || entries[0].Name != "ESPN" || entries[1].Name != "FYI" || !contains(entries[1].Aliases, "Biography") {
		t.Fatalf("Optimum entries = %+v, error = %v", entries, err)
	}
}

func TestOptimumEasternMarketUsesOnlyPublishedRegionalZIPRanges(t *testing.T) {
	for _, postalCode := range []string{"06510", "07001", "11743", "19103"} {
		if !optimumEasternMarket(postalCode, "Optimum") {
			t.Fatalf("%s should use the regional market list", postalCode)
		}
	}
	if optimumEasternMarket("09601", "Optimum") {
		t.Fatal("09601 is not in a published regional market-list ZIP range")
	}
	if !optimumEasternMarket("28739", "Optimum Hendersonville") {
		t.Fatal("Hendersonville should use the regional market list")
	}
}

func TestOptimumAddressQualifiedServiceFlow(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != http.MethodPost {
			t.Fatalf("method = %s", request.Method)
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		switch request.URL.String() {
		case optimumLocationURL:
			if !strings.Contains(string(body), `"streetAddressLine1":"1 Main Street"`) || strings.Contains(request.URL.String(), "Main") {
				t.Fatalf("location request = %s %s", request.URL, body)
			}
			return jsonResponse(request, `{"serviceabilityByLocationResponse":{"serviceabilityDetail":{"site_id":150,"zipcode":75001}}}`), nil
		case optimumLineupURL:
			if !strings.Contains(string(body), `"site_id":150`) {
				t.Fatalf("lineup request = %s", body)
			}
			return jsonResponse(request, `{"serviceabilityDetailLineupResponse":{"networks":[{"channelNumber":210,"name":"ESPN"}]}}`), nil
		default:
			t.Fatalf("unexpected URL %s", request.URL)
		}
		return nil, nil
	})}
	result, err := newService(client).FetchProviderEvidence(context.Background(), marketindex.ProviderEvidenceRequest{
		Provider: web.Provider{Name: "Optimum Amarillo"}, PostalCode: "75001",
		ServiceAddress: marketindex.ProviderAddress{FormattedAddress: "1 Main Street, Dallas, TX 75001", StreetAddress: "1 Main Street", City: "Dallas", State: "TX", PostalCode: "75001", CountryCode: "US"},
		Grid:           &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: "ESPN-ID", ChannelNo: "210", CallSign: "ESPN"}}},
	})
	if err != nil || calls != 2 || len(result.Sources) != 1 || result.Sources[0].Matched != 1 {
		t.Fatalf("result = %+v, calls = %d, error = %v", result, calls, err)
	}
}

func jsonResponse(request *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}
}

func TestProviderCatalogDoesNotCreateAliasesSharedByDifferentStations(t *testing.T) {
	result := matchCatalog(marketindex.ProviderEvidenceRequest{Grid: &web.GridResponse{Channels: []web.JSONChannel{
		{ChannelID: "ONE", ChannelNo: "850"}, {ChannelID: "TWO", ChannelNo: "851"},
	}}}, catalogSource{
		ID: "music", Label: "Official music range", Entries: []catalogEntry{
			{Numbers: []string{"850"}, Name: "Stingray Music", Category: "Music"},
			{Numbers: []string{"851"}, Name: "Stingray Music", Category: "Music"},
		},
	})
	aliases := 0
	categories := 0
	for _, fact := range result.Facts {
		if fact.Kind == marketindex.FactAlias {
			aliases++
		} else if fact.Kind == marketindex.FactCategory {
			categories++
		}
	}
	if aliases != 0 || categories != 2 {
		t.Fatalf("facts = %+v", result.Facts)
	}
}

func TestProviderCatalogDeduplicatesFactsForRepeatedStationPositions(t *testing.T) {
	result := matchCatalog(marketindex.ProviderEvidenceRequest{Grid: &web.GridResponse{Channels: []web.JSONChannel{
		{ChannelID: "WCBS", ChannelNo: "2"}, {ChannelID: "WCBS", ChannelNo: "702"},
	}}}, catalogSource{
		ID: "optimum", Label: "Optimum official lineup", Entries: []catalogEntry{
			{Numbers: []string{"2"}, Name: "CBS", Category: "Networks"},
			{Numbers: []string{"702"}, Name: "CBS", Category: "Networks"},
		},
	})
	aliases := 0
	categories := 0
	for _, fact := range result.Facts {
		if fact.Kind == marketindex.FactAlias {
			aliases++
		} else if fact.Kind == marketindex.FactCategory {
			categories++
		}
	}
	if aliases != 1 || categories != 1 || result.Sources[0].Matched != 1 {
		t.Fatalf("facts = %+v, source = %+v", result.Facts, result.Sources)
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
