package lineuparr

import "testing"

func TestSDWithRepeatedHDStationPositions(t *testing.T) {
	channels := []DraftChannel{
		{ID: "sd", StationID: "11164", Number: "42", CallSign: "TNT", Name: "TNT", NameSource: "gracenote", Included: true},
		{ID: "hd1", StationID: "42642", Number: "407", CallSign: "TNTHD", Name: "TNTHD", NameSource: "gracenote", Included: true},
		{ID: "hd2", StationID: "42642", Number: "1404", CallSign: "TNTHD", Name: "TNTHD", NameSource: "gracenote", Included: true},
	}
	got := findDuplicateSuggestions(channels)
	if len(got) != 1 || got[0].RemoveID != "sd" || got[0].KeepID != "hd1" {
		t.Fatalf("suggestions: %+v", got)
	}
	channels[1].Included = false
	got = findDuplicateSuggestions(channels)
	if len(got) != 1 || got[0].KeepID != "hd2" {
		t.Fatalf("included keeper: %+v", got)
	}
	channels[2].StationID = "different"
	if got := findDuplicateSuggestions(channels); len(got) != 0 {
		t.Fatalf("ambiguous stations accepted: %+v", got)
	}
}
