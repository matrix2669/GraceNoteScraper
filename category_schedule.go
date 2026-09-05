package main

import (
	"fmt"
	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/channelcategory"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	lineuparrbuilder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"time"
)

func (s *lineuparrServer) weekdayCategoryHints(g *guide.TVGuide, c appconfig.Config) map[string]*lineuparrbuilder.AttributedCategory {
	result := map[string]*lineuparrbuilder.AttributedCategory{}
	if s.marketIndex == nil {
		return result
	}
	loc := s.marketIndex.LineupTimezone(c.Gracenote.Country, c.Gracenote.PostalCode, c.Gracenote.LineupID, c.Gracenote.Device)
	if loc == nil {
		return result
	}
	rows := map[string][]channelcategory.ScheduleEvent{}
	var first time.Time
	for _, p := range g.Programs {
		a, err := time.Parse("20060102150405 -0700", p.Start)
		if err != nil {
			continue
		}
		b, err := time.Parse("20060102150405 -0700", p.Stop)
		if err != nil || !b.After(a) {
			continue
		}
		if first.IsZero() || a.Before(first) {
			first = a
		}
		e := channelcategory.ScheduleEvent{Start: a, Stop: b, Title: p.Title}
		for _, cat := range p.Categories {
			e.Filters = append(e.Filters, cat.Name)
		}
		rows[p.Channel] = append(rows[p.Channel], e)
	}
	if first.IsZero() {
		return result
	}
	for id, events := range rows {
		a := channelcategory.AssessSchedule(events, first, loc)
		if a.Category == "" {
			continue
		}
		result[id] = &lineuparrbuilder.AttributedCategory{Value: a.Category, Source: "gracenote-schedule", Label: "Weekday schedule inference", Priority: 3, Method: fmt.Sprintf("14-day weekdays in %s; %.1f%% usable coverage across %d days; %d programmes; mean %.1f minutes; movie %.1f%%, sports %.1f%%, news %.1f%%, family %.1f%%; priority-3; category-quality-v1", a.Timezone, a.Coverage*100, a.Days, a.Programs, a.AverageMinutes, a.Shares["movie"]*100, a.Shares["sports"]*100, a.Shares["news"]*100, a.Shares["family"]*100)}
	}
	return result
}
