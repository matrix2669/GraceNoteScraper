package channelcategory

import (
	"sort"
	"strings"
	"time"
)

// ScheduleEvent deliberately uses source programme filters, not inferred Series
// metadata or TMDB fields. Times are the complete programme boundaries.
type ScheduleEvent struct {
	Start, Stop time.Time
	Title       string
	Filters     []string
}

type ScheduleAssessment struct {
	Category       string             `json:"category,omitempty"`
	Priority       int                `json:"priority,omitempty"`
	WindowStart    time.Time          `json:"windowStart"`
	WindowEnd      time.Time          `json:"windowEnd"`
	Timezone       string             `json:"timezone"`
	UsableMinutes  float64            `json:"usableMinutes"`
	Coverage       float64            `json:"coverage"`
	Days           int                `json:"days"`
	Programs       int                `json:"programs"`
	AverageMinutes float64            `json:"averageMinutes"`
	Shares         map[string]float64 `json:"shares"`
	Reason         string             `json:"reason"`
}

// AssessSchedule evaluates fourteen local calendar days and only weekday
// airtime. Missing timezone is a failure, never an implicit server timezone.
// Ordinal priority is not a calibrated probability of correctness.
func AssessSchedule(events []ScheduleEvent, start time.Time, location *time.Location) ScheduleAssessment {
	r := ScheduleAssessment{Shares: map[string]float64{}}
	if location == nil {
		r.Reason = "Lineup timezone unavailable"
		return r
	}
	local := start.In(location)
	r.WindowStart = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	r.WindowEnd = r.WindowStart.AddDate(0, 0, 14)
	r.Timezone = location.String()
	weekday := func(t time.Time) bool { return t.Weekday() != time.Saturday && t.Weekday() != time.Sunday }
	expected := 0.0
	for d := r.WindowStart; d.Before(r.WindowEnd); d = d.AddDate(0, 0, 1) {
		if weekday(d) {
			expected += d.AddDate(0, 0, 1).Sub(d).Minutes()
		}
	}
	ordered := append([]ScheduleEvent(nil), events...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Start.Equal(ordered[j].Start) {
			return ordered[i].Stop.Before(ordered[j].Stop)
		}
		return ordered[i].Start.Before(ordered[j].Start)
	})
	days := map[string]bool{}
	seen := map[string]bool{}
	length := 0.0
	lastStop := time.Time{}
	for _, e := range ordered {
		if !e.Stop.After(e.Start) {
			continue
		}
		key := e.Start.UTC().Format(time.RFC3339Nano) + "|" + e.Stop.UTC().Format(time.RFC3339Nano) + "|" + e.Title
		if seen[key] {
			continue
		}
		seen[key] = true
		a, b := e.Start, e.Stop
		if a.Before(r.WindowStart) {
			a = r.WindowStart
		}
		if b.After(r.WindowEnd) {
			b = r.WindowEnd
		}
		if !b.After(a) {
			continue
		}
		// Overlapping non-identical events would double count airtime. Refuse
		// to classify instead of arbitrarily choosing a programme.
		if a.Before(lastStop) {
			r.Reason = "Overlapping programme intervals"
			return r
		}
		lastStop = b
		switch strings.ToLower(strings.Join(strings.Fields(e.Title), " ")) {
		case "paid programming", "off air", "off-air", "to be announced", "no data available", "sign off":
			continue
		}
		minutes := 0.0
		for a.Before(b) {
			d := a.In(location)
			next := time.Date(d.Year(), d.Month(), d.Day()+1, 0, 0, 0, 0, location)
			stop := b
			if next.Before(stop) {
				stop = next
			}
			if weekday(d) {
				minutes += stop.Sub(a).Minutes()
				days[d.Format("2006-01-02")] = true
			}
			a = stop
		}
		if minutes == 0 {
			continue
		}
		r.UsableMinutes += minutes
		r.Programs++
		length += e.Stop.Sub(e.Start).Minutes()
		filters := map[string]bool{}
		for _, f := range e.Filters {
			f = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(f)), "filter-")
			filters[f] = true
		}
		for _, f := range []string{"movie", "sports", "news", "family", "entertainment"} {
			if filters[f] {
				r.Shares[f] += minutes
			}
		}
	}
	r.Days = len(days)
	if r.Programs > 0 {
		r.AverageMinutes = length / float64(r.Programs)
	}
	if expected > 0 {
		r.Coverage = r.UsableMinutes / expected
	}
	if r.UsableMinutes > 0 {
		for k, v := range r.Shares {
			r.Shares[k] = v / r.UsableMinutes
		}
	}
	if r.Days < 8 || r.Coverage < 0.8 || r.Programs < 20 {
		r.Reason = "Insufficient usable weekday coverage"
		return r
	}
	// Format labels never establish Entertainment, Faith or Music. A failed
	// rule supplies no negative category evidence.
	switch {
	case r.Shares["sports"] >= 0.8 && r.Shares["sports"]-r.Shares["news"] >= 0.15:
		r.Category = Sports
	case r.Shares["news"] >= 0.8 && r.Shares["news"]-r.Shares["sports"] >= 0.15:
		r.Category = NewsWeather
	case r.Shares["family"] >= 0.8:
		r.Category = KidsFamily
	case r.Shares["movie"] > 0.5 && r.AverageMinutes > 60:
		r.Category = Movies
	case r.Shares["entertainment"] >= 0.8:
		r.Category = Entertainment
	default:
		r.Reason = "No supported dominant content category"
		return r
	}
	r.Priority = 3
	r.Reason = "Provisional fourteen-day weekday schedule inference"
	return r
}
