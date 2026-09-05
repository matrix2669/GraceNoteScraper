package providersource

import (
	"context"
	"net/http"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/channelcategory"
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

func TestXfinityLiveFieldContract(t *testing.T) {
	entries, err := parseXfinity([]byte(`{"channels":[
	 {"channelNumber":205,"channelName":"Example News","callSign":"EXNEWS","channelShortName":"Example News TV","stationName":"Example News Network","stationId":"4784595261472352117","genreId":"9","genreName":"Music","hdCallSign":"OTHERHD","hdStationId":"98765","hdChannelNumber":1205},
	 {"channelNumber":206,"channelName":"Example Unknown","genreName":"Unknown"},
	 {"channelNumber":207,"channelName":"Example PPV","genreName":"Unknown","ppv":true}
	]}`))
	if err != nil || len(entries) != 3 {
		t.Fatalf("parse: %v %+v", err, entries)
	}
	e := entries[0]
	if len(e.Numbers) != 1 || e.Numbers[0] != "205" || len(e.CallSigns) != 1 || e.CallSigns[0] != "EXNEWS" || len(e.Aliases) != 2 {
		t.Fatalf("wrong feed fields: %+v", e)
	}
	if entries[1].Category != "" || entries[2].Category != "PPV & Events" || !entries[2].EventFeed {
		t.Fatalf("unknown/PPV: %+v", entries)
	}
	result := matchCatalog(lineupindex.ProviderEvidenceRequest{Grid: &web.GridResponse{Channels: []web.JSONChannel{
		{ChannelNo: "205", ChannelID: "12345", CallSign: "EXNEWS"},
	}}}, catalogSource{ID: "xfinity-official-lineup", Entries: entries})
	found := false
	for _, f := range result.Facts {
		if f.StationID != "12345" {
			t.Fatalf("provider ID leaked: %+v", f)
		}
		if f.Kind == lineupindex.FactCategory && f.Value == channelcategory.NewsWeather {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing mapped category: %+v", result)
	}
	wrong := matchCatalog(lineupindex.ProviderEvidenceRequest{Grid: &web.GridResponse{Channels: []web.JSONChannel{
		{ChannelNo: "205", ChannelID: "4784595261472352117", CallSign: "UNRELATED"},
	}}}, catalogSource{ID: "xfinity-official-lineup", Entries: entries})
	if len(wrong.Facts) != 0 {
		t.Fatalf("number or provider ID alone matched: %+v", wrong)
	}
}

func TestXfinityGenres(t *testing.T) {
	for raw, want := range map[string]string{"Music": channelcategory.Music, "News & Info": channelcategory.NewsWeather, "Help & Services": channelcategory.Other, "ON DEMAND": channelcategory.Other} {
		got, ok := channelcategory.Resolve(raw)
		if !ok || got.Category != want {
			t.Errorf("%s: %+v %v", raw, got, ok)
		}
	}
}

func TestExcludedProvidersNeverFetch(t *testing.T) {
	s := newService(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("excluded provider requested network")
		return nil, nil
	})})
	for _, name := range []string{"AFN Satellite", "GLORYSTAR"} {
		r, err := s.FetchProviderEvidence(context.Background(), lineupindex.ProviderEvidenceRequest{Provider: web.Provider{Name: name}, Grid: &web.GridResponse{}})
		if err != nil || len(r.Facts) != 0 || len(r.Sources) != 0 {
			t.Fatalf("%s: %+v %v", name, r, err)
		}
	}
}
