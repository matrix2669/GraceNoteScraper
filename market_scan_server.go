package main

import (
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"net/http"
)

func (s *lineuparrServer) handleMarkets(w http.ResponseWriter, r *http.Request) {
	if s.marketIndex == nil {
		http.Error(w, "Market enrichment is unavailable", 503)
		return
	}
	if r.Method == http.MethodGet {
		writeLineuparrJSON(w, http.StatusOK, s.marketIndex.MarketView())
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}
	var body struct {
		Rank *int `json:"rank"`
	}
	if !decodeLineuparrRequest(w, r, &body) {
		return
	}
	if s.beforeMarketScan != nil {
		if err := s.beforeMarketScan(); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
	}
	if s.store == nil || s.state == nil || s.state.Get() == nil {
		http.Error(w, "Build the selected guide before scanning another market", 409)
		return
	}
	config, configured, _ := s.store.Get()
	if !configured {
		http.Error(w, "Choose a provider first", 409)
		return
	}
	c := config.Gracenote
	comparison := &lineupindex.LineupRecord{Country: c.Country, PostalCode: c.PostalCode, Language: c.Language, ProviderName: c.ProviderName, LineupID: c.LineupID, HeadendID: c.HeadendID, Device: c.Device, Location: c.Location}
	var job lineupindex.JobView
	var err error
	if body.Rank != nil {
		job, err = s.marketIndex.StartMarket(*body.Rank, comparison)
	} else {
		job, err = s.marketIndex.StartNextMarket(comparison)
	}
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	writeLineuparrJSON(w, http.StatusAccepted, job)
}
