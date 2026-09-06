package channelcategory

import (
	"sort"
	"strings"
	"time"
)

const (
	LanguageMinimumDays       = 8
	LanguageMinimumCoverage   = 0.50
	LanguageMinimumTitles     = 8
	LanguageNonEnglishMinimum = 0.60
)

type LanguageEvent struct {
	Start, Stop      time.Time
	Title            string
	OriginalLanguage string
}

type LanguageAssessment struct {
	Category          string    `json:"category,omitempty"`
	Priority          int       `json:"priority,omitempty"`
	WindowStart       time.Time `json:"windowStart"`
	WindowEnd         time.Time `json:"windowEnd"`
	Timezone          string    `json:"timezone"`
	Days              int       `json:"days"`
	Programs          int       `json:"programs"`
	LanguagePrograms  int       `json:"languagePrograms"`
	DistinctTitles    int       `json:"distinctTitles"`
	ScheduledMinutes  float64   `json:"scheduledMinutes"`
	LanguageMinutes   float64   `json:"languageMinutes"`
	NonEnglishMinutes float64   `json:"nonEnglishMinutes"`
	Coverage          float64   `json:"coverage"`
	NonEnglishShare   float64   `json:"nonEnglishShare"`
	Reason            string    `json:"reason"`
}

// AssessLanguage evaluates TMDB original-language evidence over the same
// fourteen-day weekday window as schedule classification. Original language
// describes a matched title, not a channel's audio feed, so qualifying results
// remain provisional priority-3 evidence and require review.
func AssessLanguage(events []LanguageEvent, start time.Time, location *time.Location) LanguageAssessment {
	r := LanguageAssessment{}
	if location == nil {
		r.Reason = "Lineup timezone unavailable"
		return r
	}
	local := start.In(location)
	r.WindowStart = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	r.WindowEnd = r.WindowStart.AddDate(0, 0, 14)
	r.Timezone = location.String()
	weekday := func(t time.Time) bool { return t.Weekday() != time.Saturday && t.Weekday() != time.Sunday }
	ordered := append([]LanguageEvent(nil), events...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Start.Equal(ordered[j].Start) {
			return ordered[i].Stop.Before(ordered[j].Stop)
		}
		return ordered[i].Start.Before(ordered[j].Start)
	})
	seen := map[string]bool{}
	days := map[string]bool{}
	titles := map[string]bool{}
	lastStop := time.Time{}
	for _, event := range ordered {
		if !event.Stop.After(event.Start) {
			continue
		}
		key := event.Start.UTC().Format(time.RFC3339Nano) + "|" + event.Stop.UTC().Format(time.RFC3339Nano) + "|" + event.Title
		if seen[key] {
			continue
		}
		seen[key] = true
		a, b := event.Start, event.Stop
		if a.Before(r.WindowStart) {
			a = r.WindowStart
		}
		if b.After(r.WindowEnd) {
			b = r.WindowEnd
		}
		if !b.After(a) {
			continue
		}
		if a.Before(lastStop) {
			r.Reason = "Overlapping programme intervals"
			return r
		}
		lastStop = b
		minutes := 0.0
		eventDays := make([]string, 0, 2)
		for a.Before(b) {
			d := a.In(location)
			next := time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, location)
			stop := b
			if next.Before(stop) {
				stop = next
			}
			if weekday(d) {
				minutes += stop.Sub(a).Minutes()
				eventDays = append(eventDays, d.Format("2006-01-02"))
			}
			a = stop
		}
		if minutes == 0 {
			continue
		}
		r.Programs++
		r.ScheduledMinutes += minutes
		language, usable := usableOriginalLanguage(event.OriginalLanguage)
		if !usable {
			continue
		}
		r.LanguagePrograms++
		r.LanguageMinutes += minutes
		for _, day := range eventDays {
			days[day] = true
		}
		if title := strings.ToLower(strings.Join(strings.Fields(event.Title), " ")); title != "" {
			titles[title] = true
		}
		if language != "en" {
			r.NonEnglishMinutes += minutes
		}
	}
	r.Days = len(days)
	r.DistinctTitles = len(titles)
	if r.ScheduledMinutes > 0 {
		r.Coverage = r.LanguageMinutes / r.ScheduledMinutes
	}
	if r.LanguageMinutes > 0 {
		r.NonEnglishShare = r.NonEnglishMinutes / r.LanguageMinutes
	}
	if r.Days < LanguageMinimumDays || r.Coverage < LanguageMinimumCoverage || r.DistinctTitles < LanguageMinimumTitles {
		r.Reason = "Insufficient TMDB language coverage"
		return r
	}
	if r.NonEnglishShare <= LanguageNonEnglishMinimum {
		r.Reason = "Non-English original-language airtime is not dominant"
		return r
	}
	r.Category = International
	r.Priority = 3
	r.Reason = "Provisional TMDB original-language inference"
	return r
}

func usableOriginalLanguage(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if cut := strings.IndexAny(value, "-_ "); cut >= 0 {
		value = value[:cut]
	}
	switch value {
	case "", "xx", "zxx", "und":
		return "", false
	default:
		return value, true
	}
}
