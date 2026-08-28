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
