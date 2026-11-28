package guide

import (
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/web"
)

func TestConvertChannelRetainsLineupAndEventIdentities(t *testing.T) {
	channel := ConvertChannel(web.JSONChannel{
		ID: "113310", ChannelID: "11331", ChannelNo: "2", CallSign: "WCBS", AffiliateName: "CBS TELEVISION NETWORK",
		Events: []web.JSONEvent{{CallSign: "WCBS"}, {CallSign: "WCBS"}, {CallSign: " WCBSDT "}},
	})
	if channel.ID != "11331" || channel.PlacementID != "113310" || channel.ChannelNo != "2" || channel.CallSign != "WCBS" {
		t.Fatalf("converted channel = %+v", channel)
	}
	if len(channel.EventCallSigns) != 2 || channel.EventCallSigns[0] != "WCBS" || channel.EventCallSigns[1] != "WCBSDT" {
		t.Fatalf("event callsigns = %v", channel.EventCallSigns)
	}
}

func TestConvertEventKeepsRawFiltersSeparateFromOutputCategories(t *testing.T) {
	p := ConvertEvent(web.JSONEvent{Filter: []string{"filter-news", "filter-new"}}, "64549", "en", "USA")
	if len(p.RawFilters) != 2 || p.RawFilters[0] != "news" || p.RawFilters[1] != "new" {
		t.Fatalf("raw response filters were lost: %v", p.RawFilters)
	}
	p.Categories = append(p.Categories, Category{Name: "Entertainment"})
	p.Categories[0].Name = "Movies"
	if p.RawFilters[0] != "news" || len(p.RawFilters) != 2 {
		t.Fatal("output category changes contaminated independent programme evidence")
	}
	legacy := ConvertEvent(web.JSONEvent{}, "64549", "en", "USA")
	if len(legacy.RawFilters) != 0 {
		t.Fatal("channel identity manufactured programme filters")
	}
}
