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
		// Categories is an output-facing field and may include injected TMDB or
		// channel-level labels. Only response-level filters are valid schedule
		// evidence; old guides without RawFilters intentionally contribute none.
		e.Filters = append(e.Filters, p.RawFilters...)
		rows[p.Channel] = append(rows[p.Channel], e)
	}
	if first.IsZero() {
		return result
	}
	for id, events := range rows {
		a := channelcategory.AssessSchedule(events, first, loc)
		confirmedCategories := independentCategoryNames(a)
		if a.Category == "" && len(confirmedCategories) == 0 {
			continue
		}
		priority := 3
		source := "gracenote-schedule"
		label := "Weekday schedule inference"
		independentConfirmation := false
		method := fmt.Sprintf("14-day weekdays in %s; %.1f%% usable coverage across %d days; %d programmes; mean %.1f minutes; movie %.1f%%, sports %.1f%%, news %.1f%%, family %.1f%%; priority-3; category-quality-v1", a.Timezone, a.Coverage*100, a.Days, a.Programs, a.AverageMinutes, a.Shares["movie"]*100, a.Shares["sports"]*100, a.Shares["news"]*100, a.Shares["family"]*100)
		// A strict >80% target share with the existing >=80% usable weekday
		// coverage gate is independent schedule confirmation. Exactly 80% target
		// share remains a proposal and therefore remains reviewable.
		share := a.Shares["entertainment"]
		switch a.Category {
		case channelcategory.Movies:
			share = a.Shares["movie"]
		case channelcategory.Sports:
			share = a.Shares["sports"]
		case channelcategory.NewsWeather:
			share = a.Shares["news"]
		case channelcategory.KidsFamily:
			share = a.Shares["family"]
		}
		if a.Category != "" && containsCategory(confirmedCategories, a.Category) {
			independentConfirmation = true
			method = fmt.Sprintf("%s; independent schedule confirmation: %.1f%% target airtime with >=80%% usable weekday coverage; category-quality-v1", method, share*100)
		}
		result[id] = &lineuparrbuilder.AttributedCategory{Value: a.Category, Source: source, Label: label, Priority: priority, Method: method, IndependentConfirmation: independentConfirmation, IndependentCategories: confirmedCategories}
	}
	return result
}

func independentCategoryNames(a channelcategory.ScheduleAssessment) []string {
	if a.Days < 8 || a.Programs < 20 || a.Coverage < 0.8 {
		return nil
	}
	checks := []struct {
		category string
		share    float64
	}{
		{channelcategory.NewsWeather, a.Shares["news"]},
		{channelcategory.Sports, a.Shares["sports"]},
		{channelcategory.KidsFamily, a.Shares["family"]},
		{channelcategory.Movies, a.Shares["movie"]},
		{channelcategory.Entertainment, a.Shares["entertainment"]},
	}
	result := make([]string, 0, len(checks))
	for _, check := range checks {
		if check.share > 0.8 {
			result = append(result, check.category)
		}
	}
	return result
}

func mergeConfirmedCategories(existing, additional []string) []string {
	result := append([]string(nil), existing...)
	for _, candidate := range additional {
		found := false
		for _, value := range result {
			if value == candidate {
				found = true
				break
			}
		}
		if !found {
			result = append(result, candidate)
		}
	}
	return result
}
