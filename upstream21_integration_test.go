package main

import (
	"context"
	"errors"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	"github.com/daniel-widrick/GraceNoteScraper/scrape"
	"github.com/daniel-widrick/GraceNoteScraper/web"
	"testing"
)

type pr21Fetcher struct {
	fail  bool
	count int
}

func (f *pr21Fetcher) GetDataByTimeContext(ctx context.Context, _ int64) (*web.GridResponse, error) {
	f.count++
	if f.fail {
		return nil, errors.New("fixture unavailable")
	}
	ch := web.JSONChannel{ChannelID: "station", ID: "provider-row", ChannelNo: "2.1", CallSign: "WTESTDT2", StationFilters: []string{"filter-news"},
		Events: []web.JSONEvent{{CallSign: "FIRST", StartTime: "2026-09-13T00:00:00Z", EndTime: "2026-09-13T01:00:00Z", Duration: "60", SeriesID: "SH1", Program: web.JSONProgram{ID: "EP1", Title: "one"}}}}
	v := ch
	v.ChannelNo = "1002"
	v.Events = append([]web.JSONEvent(nil), ch.Events...)
	v.Events[0].CallSign = "SECOND"
	return &web.GridResponse{Channels: []web.JSONChannel{ch, v}}, nil
}
func TestPR21ForkAdapterPreservesRawAndLegacyContracts(t *testing.T) {
	t.Chdir(t.TempDir())
	f := &pr21Fetcher{}
	prev := newScrapeOptions
	t.Cleanup(func() { newScrapeOptions = prev })
	newScrapeOptions = func(web.Preferences) scrape.Options {
		return scrape.Options{Fetcher: f, Days: 1, SlotDelay: scrape.NoDelay}
	}
	pref := web.Preferences{Country: "ZZZ", ZipCode: "00000", Headend: "h", LineupId: "L", Device: "-", Language: "en-us"}
	var called int
	save := func(*guide.TVGuide) (bool, error) { called++; return true, nil }
	g, e := runScrape(pref, nil, "", nil, "fp", nil, save)
	if e != nil || g == nil {
		t.Fatalf("scrape %v %v", g, e)
	}
	if len(g.Channels) != 1 || len(g.Lineup) != 2 || len(g.LineupChannels) != 1 || len(g.Programs) != 1 || f.count != 4 || called != 1 {
		t.Fatalf("guide=%+v fetches=%d saved=%d", g, f.count, called)
	}
	if g.Lineup[0].ChannelNo != "2.1" || g.Lineup[1].ChannelNo != "1002" || g.Source.LineupID != "L" {
		t.Fatalf("positions/source: %+v %+v", g.Lineup, g.Source)
	}
	filtered := filterGuideChannels(g, map[string]bool{"2.1": true})
	if len(filtered.Lineup) != 1 || len(g.Lineup) != 2 {
		t.Fatal("raw view filtering mutated the guide or dropped positions")
	}
	f.fail = true
	if bad, err := runScrape(pref, nil, "", nil, "fp", nil, save); bad != nil || !errors.Is(err, scrape.ErrNoData) || called != 1 {
		t.Fatalf("all slots failed: %v %v %d", bad, err, called)
	}
	n := 0
	f.fail = false
	if bad, err := runScrape(pref, nil, "", nil, "fp", func() bool { n++; return n < 2 }, save); bad != nil || !errors.Is(err, errScrapeSourceChanged) || called != 1 {
		t.Fatalf("source switched: %v %v %d", bad, err, called)
	}
}
