package geocode

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestNominatimSearchFiltersToCompleteActivePostalCodeAddresses(t *testing.T) {
	var request *http.Request
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		request = r.Clone(r.Context())
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`[
          {"place_id":1,"osm_type":"way","osm_id":10,"display_name":"1 Main Street, Dallas, TX 75001, USA","address":{"house_number":"1","road":"Main Street","city":"Dallas","state":"Texas","ISO3166-2-lvl4":"US-TX","postcode":"75001","country_code":"us"}},
          {"place_id":2,"display_name":"Main Street, Dallas, TX 75001, USA","address":{"road":"Main Street","postcode":"75001"}},
          {"place_id":3,"display_name":"2 Main Street, Dallas, TX 75002, USA","address":{"house_number":"2","road":"Main Street","postcode":"75002"}}
        ]`)),
			Request: r,
		}, nil
	})}

	searcher := NewNominatimClient(client, "https://nominatim.test")
	results, err := searcher.Search(context.Background(), "1 Main Street", "75001", "us")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != "way:10" || results[0].StreetAddress != "1 Main Street" || results[0].City != "Dallas" || results[0].State != "TX" || results[0].PostalCode != "75001" || results[0].CountryCode != "US" {
		t.Fatalf("results = %+v", results)
	}
	if request == nil || request.URL.Query().Get("countrycodes") != "us" || request.URL.Query().Get("layer") != "address" || !strings.Contains(request.URL.Query().Get("q"), "75001") {
		t.Fatalf("request = %+v", request)
	}
	if !strings.Contains(request.UserAgent(), "GraceNoteScraper") {
		t.Fatalf("user agent = %q", request.UserAgent())
	}
}

func TestNominatimSearchUsesMunicipalityWhenCityIsAbsent(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`[
          {"place_id":4,"display_name":"3 State Street, Springfield, IL 62701, USA","address":{"house_number":"3","road":"State Street","municipality":"Springfield","state":"Illinois","ISO3166-2-lvl4":"US-IL","postcode":"62701","country_code":"us"}}
        ]`)),
			Request: r,
		}, nil
	})}

	results, err := NewNominatimClient(client, "https://nominatim.test").Search(context.Background(), "3 State Street", "62701", "us")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].City != "Springfield" {
		t.Fatalf("results = %+v", results)
	}
}
