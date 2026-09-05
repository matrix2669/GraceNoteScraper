package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/providersource"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

type providerAddressTester interface {
	TestAddress(context.Context, lineupindex.ProviderEvidenceRequest) providersource.AddressCheck
}

type savedProviderAddress struct {
	lineupindex.ProviderAddress
	Checks   []providersource.AddressCheck `json:"checks,omitempty"`
	TestedAt string                        `json:"testedAt,omitempty"`
}

var usAddressTail = regexp.MustCompile(`^(.+?)\s+(\d{5}(?:-\d{4})?)$`)

// Parse from the postal end, preserving street words, directionals and units.
// Google Maps text is user input, not a postal-verification assertion.
func parsePastedProviderAddress(text, postal, country string) (lineupindex.ProviderAddress, error) {
	invalid := errors.New("Paste the full address: street, city, state ZIP (not a Maps link or place name). Keep unit details and use the lineup ZIP.")
	if len(text) > 500 || strings.Contains(text, "://") || !strings.EqualFold(country, "US") {
		return lineupindex.ProviderAddress{}, invalid
	}
	text = strings.Join(strings.Fields(text), " ")
	parts := strings.Split(text, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if len(parts) > 0 {
		last := strings.ToLower(parts[len(parts)-1])
		if last == "usa" || last == "us" || last == "united states" || last == "united states of america" {
			parts = parts[:len(parts)-1]
		}
	}
	if len(parts) < 3 {
		return lineupindex.ProviderAddress{}, invalid
	}
	// Also accept Google's comma-separated state, ZIP layout.
	if match := regexp.MustCompile(`^\d{5}(?:-\d{4})?$`).MatchString(parts[len(parts)-1]); match && len(parts) >= 4 {
		parts[len(parts)-2] += " " + parts[len(parts)-1]
		parts = parts[:len(parts)-1]
	}
	tail := usAddressTail.FindStringSubmatch(parts[len(parts)-1])
	if tail == nil {
		return lineupindex.ProviderAddress{}, invalid
	}
	state := tail[1]
	// State abbreviations are unambiguous; street directional words are not.
	states := strings.Split("AL=Alabama|AK=Alaska|AZ=Arizona|AR=Arkansas|CA=California|CO=Colorado|CT=Connecticut|DE=Delaware|DC=District of Columbia|FL=Florida|GA=Georgia|HI=Hawaii|ID=Idaho|IL=Illinois|IN=Indiana|IA=Iowa|KS=Kansas|KY=Kentucky|LA=Louisiana|ME=Maine|MD=Maryland|MA=Massachusetts|MI=Michigan|MN=Minnesota|MS=Mississippi|MO=Missouri|MT=Montana|NE=Nebraska|NV=Nevada|NH=New Hampshire|NJ=New Jersey|NM=New Mexico|NY=New York|NC=North Carolina|ND=North Dakota|OH=Ohio|OK=Oklahoma|OR=Oregon|PA=Pennsylvania|RI=Rhode Island|SC=South Carolina|SD=South Dakota|TN=Tennessee|TX=Texas|UT=Utah|VT=Vermont|VA=Virginia|WA=Washington|WV=West Virginia|WI=Wisconsin|WY=Wyoming|PR=Puerto Rico|VI=Virgin Islands|GU=Guam|AS=American Samoa|MP=Northern Mariana Islands", "|")
	known := false
	for _, entry := range states {
		pair := strings.SplitN(entry, "=", 2)
		if strings.EqualFold(state, pair[0]) || strings.EqualFold(state, pair[1]) {
			state = pair[0]
			known = true
			break
		}
	}
	if !known {
		return lineupindex.ProviderAddress{}, invalid
	}
	street := strings.Join(parts[:len(parts)-2], ", ")
	city := parts[len(parts)-2]
	if street == "" || city == "" {
		return lineupindex.ProviderAddress{}, invalid
	}
	a := lineupindex.ProviderAddress{StreetAddress: street, City: city, State: state, PostalCode: tail[2], CountryCode: "US"}
	a.FormattedAddress = street + ", " + city + ", " + state + " " + tail[2]
	return validateEphemeralProviderAddress(a, postal)
}

func (s *lineuparrServer) testPastedAddress(w http.ResponseWriter, r *http.Request, fingerprint, text string) {
	config, configured, _ := s.store.Get()
	if !configured || config.Fingerprint() != fingerprint {
		http.Error(w, "Provider changed; reload the page", http.StatusConflict)
		return
	}
	a, err := parsePastedProviderAddress(text, config.Gracenote.PostalCode, autocompleteCountryCode(config.Gracenote.Country))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.addressTester == nil {
		http.Error(w, "Provider address testing is unavailable", http.StatusServiceUnavailable)
		return
	}
	if time.Now().Before(s.nextAddressTest) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, "Please wait one minute between provider address tests.", http.StatusTooManyRequests)
		return
	}
	names, err := s.addressProviders(r.Context(), config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if len(names) == 0 {
		http.Error(w, "No provider in this ZIP requires an address", http.StatusConflict)
		return
	}
	s.nextAddressTest = time.Now().Add(time.Minute)
	record := savedProviderAddress{ProviderAddress: a, TestedAt: time.Now().UTC().Format(time.RFC3339)}
	seen := map[string]bool{}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	for _, name := range names {
		id := providersource.OfficialSourceID(name)
		if seen[id] {
			continue
		}
		seen[id] = true
		check := s.addressTester.TestAddress(ctx, lineupindex.ProviderEvidenceRequest{Provider: web.Provider{Name: name}, Country: config.Gracenote.Country, PostalCode: config.Gracenote.PostalCode, ServiceAddress: a})
		record.Checks = append(record.Checks, check)
	}
	if r.Context().Err() != nil {
		return
	}
	raw, _ := json.Marshal(record)
	if err := s.store.SaveAddress(fingerprint, raw); err != nil {
		http.Error(w, "Unable to save address; provider may have changed. Reload and retry.", http.StatusConflict)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeLineuparrJSON(w, http.StatusOK, record)
}

func (s *lineuparrServer) handleAddressHelpImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	data, err := lineuparrFS.ReadFile("assets/google-maps-address-help.png")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if r.Method == http.MethodGet {
		_, _ = w.Write(data)
	}
}
