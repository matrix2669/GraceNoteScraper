package lineuparr

import "testing"

func TestSDWithRepeatedHDStationPositions(t *testing.T) {
	channels := []DraftChannel{
		{ID: "sd", StationID: "11164", Number: "42", CallSign: "TNT", Name: "TNT", NameSource: "gracenote", Included: true},
		{ID: "hd1", StationID: "42642", Number: "407", CallSign: "TNTHD", Name: "TNTHD", NameSource: "gracenote", Included: true},
		{ID: "hd2", StationID: "42642", Number: "1404", CallSign: "TNTHD", Name: "TNTHD", NameSource: "gracenote", Included: true},
	}
	got := findDuplicateSuggestions(channels)
	if len(got) != 2 || got[0].RemoveID != "sd" || got[0].KeepID != "hd1" || !got[1].Exact {
		t.Fatalf("suggestions: %+v", got)
	}
	channels[1].Included = false
	got = findDuplicateSuggestions(channels)
	if len(got) != 2 || got[0].KeepID != "hd2" {
		t.Fatalf("included keeper: %+v", got)
	}
	channels[2].StationID = "different"
	if got := findDuplicateSuggestions(channels); len(got) != 0 {
		t.Fatalf("ambiguous stations accepted: %+v", got)
	}
}

func TestDuplicateGroupsManualKeeperAndLastPositionGuard(t *testing.T) {
	channels := []DraftChannel{
		{ID: "sd", StationID: "1", Number: "42", CallSign: "TNT", NameSource: "gracenote", Included: true},
		{ID: "a", StationID: "2", Number: "407", CallSign: "TNTHD", NameSource: "gracenote", Included: true},
		{ID: "b", StationID: "2", Number: "1404", CallSign: "TNTHD", NameSource: "gracenote", Included: true},
	}
	suggestions := findDuplicateSuggestions(channels)
	groups := duplicateReviewGroups(channels, suggestions)
	if len(groups) != 1 || len(groups[0].ChannelIDs) != 3 {
		t.Fatalf("groups: %+v", groups)
	}
	draft := &Draft{Channels: channels, DuplicateSuggestions: suggestions}
	s := newTestService(t, "", "")
	if err := s.RemoveSuggestedDuplicateIDs("s", draft, []string{"sd", "a", "b"}); err == nil {
		t.Fatal("removed every position")
	}
	if len(s.store.Snapshot("s")) != 0 {
		t.Fatal("failed validation partially persisted")
	}
	if err := s.RemoveSuggestedDuplicateIDs("s", draft, []string{"sd", "a"}); err != nil {
		t.Fatal(err)
	}
	// A caller may choose 1404 instead of the review reference 407.
	if s.store.Snapshot("s")["b"].Included != nil {
		t.Fatal("keeper changed")
	}
	draft.Channels[2].Included = false
	if err := s.RemoveSuggestedDuplicateIDs("other", draft, []string{"sd", "a"}); err == nil {
		t.Fatal("excluded position counted as keeper")
	}
	channels[2].CallSign = "UNRELATED"
	if got := findDuplicateSuggestions(channels); len(got) != 1 {
		t.Fatalf("shared ID merged unrelated callsigns: %+v", got)
	}
}
