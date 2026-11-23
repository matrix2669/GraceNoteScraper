package channelcategory

import (
	"fmt"
	"testing"
	"time"
)

func TestLanguageEvidenceRequiresDominanceCoverageAndTitleDiversity(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.September, 7, 0, 0, 0, 0, loc)
	fixture := func(nonEnglishHours, unknownHours int, titleCount int) []LanguageEvent {
		var events []LanguageEvent
		hour := 0
		for a := start; a.Before(start.AddDate(0, 0, 14)); a = a.Add(time.Hour) {
			language := "en"
			if hour%10 < nonEnglishHours {
				language = "es"
			} else if hour%10 < nonEnglishHours+unknownHours {
				language = ""
			}
			title := ""
			if titleCount > 0 {
				title = fmt.Sprintf("Programme %d", hour%titleCount)
			}
			events = append(events, LanguageEvent{Start: a, Stop: a.Add(time.Hour), Title: title, OriginalLanguage: language})
			hour++
		}
		return events
	}

	if result := AssessLanguage(fixture(7, 0, 10), start, loc); result.Category != International || result.Priority != 3 || result.Days != 10 || result.Coverage != 1 || result.NonEnglishShare <= 0.6 {
		t.Fatalf("qualifying language evidence = %+v", result)
	}
	if result := AssessLanguage(fixture(6, 0, 10), start, loc); result.Category != "" {
		t.Fatalf("60 percent should not qualify: %+v", result)
	}
	if result := AssessLanguage(fixture(2, 6, 10), start, loc); result.Category != "" || result.Coverage >= 0.5 {
		t.Fatalf("low coverage should not qualify: %+v", result)
	}
	if result := AssessLanguage(fixture(7, 0, 7), start, loc); result.Category != "" || result.DistinctTitles != 7 {
		t.Fatalf("low title diversity should not qualify: %+v", result)
	}
	sevenDays := fixture(7, 0, 10)
	for index := range sevenDays {
		if !sevenDays[index].Start.In(loc).Before(start.AddDate(0, 0, 9)) {
			sevenDays[index].OriginalLanguage = ""
		}
	}
	if result := AssessLanguage(sevenDays, start, loc); result.Category != "" || result.Days >= LanguageMinimumDays {
		t.Fatalf("language data on too few weekdays should not qualify: %+v", result)
	}
	if result := AssessLanguage(fixture(7, 0, 10), start, nil); result.Category != "" {
		t.Fatalf("missing timezone should not qualify: %+v", result)
	}
}

func TestLanguageEvidenceIgnoresNoLanguageCodesAndDuplicateEvents(t *testing.T) {
	loc := time.UTC
	start := time.Date(2026, time.September, 7, 0, 0, 0, 0, loc)
	var events []LanguageEvent
	for hour := 0; hour < 14*24; hour++ {
		a := start.Add(time.Duration(hour) * time.Hour)
		language := "es-MX"
		if hour%4 == 0 {
			language = "xx"
		}
		events = append(events, LanguageEvent{Start: a, Stop: a.Add(time.Hour), Title: fmt.Sprintf("Programme %d", hour%10), OriginalLanguage: language})
	}
	events = append(events, events[0])
	result := AssessLanguage(events, start, loc)
	if result.Category != International || result.Coverage != 0.75 || result.NonEnglishShare != 1 {
		t.Fatalf("language normalization/deduplication = %+v", result)
	}
}
