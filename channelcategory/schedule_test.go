package channelcategory

import (
	"testing"
	"time"
)

func TestScheduleWeekdayEvidence(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 9, 7, 0, 0, 0, 0, loc)
	fixture := func(filter string) []ScheduleEvent {
		var events []ScheduleEvent
		for a := start; a.Before(start.AddDate(0, 0, 14)); a = a.Add(2 * time.Hour) {
			events = append(events, ScheduleEvent{Start: a, Stop: a.Add(2 * time.Hour), Title: "Programme", Filters: []string{filter}})
		}
		return events
	}
	for filter, want := range map[string]string{"movie": Movies, "sports": Sports, "news": NewsWeather, "family": KidsFamily, "Series": "", "talk": ""} {
		r := AssessSchedule(fixture(filter), start, loc)
		if r.Category != want || r.Days != 10 || r.Coverage != 1 {
			t.Fatalf("%s: %+v", filter, r)
		}
	}
	if r := AssessSchedule(fixture("movie")[:20], start, loc); r.Category != "" {
		t.Fatal(r)
	}
	if r := AssessSchedule(fixture("movie"), start, nil); r.Category != "" {
		t.Fatal(r)
	}
	duplicate := fixture("movie")
	duplicate = append(duplicate, duplicate[0])
	if r := AssessSchedule(duplicate, start, loc); r.Category != Movies || r.Coverage != 1 {
		t.Fatal(r)
	}
	overlap := fixture("movie")
	overlap = append(overlap, ScheduleEvent{Start: start.Add(time.Minute), Stop: start.Add(time.Hour), Title: "Conflicting"})
	if r := AssessSchedule(overlap, start, loc); r.Category != "" || r.Reason != "Overlapping programme intervals" {
		t.Fatal(r)
	}
}
