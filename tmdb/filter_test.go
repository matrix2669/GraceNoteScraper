package tmdb

import "testing"

func TestTMDBSkipReasonSkipsObviousBroadcastContent(t *testing.T) {
	tests := []struct {
		name   string
		title  string
		reason string
	}{
		{name: "news", title: "SBS News", reason: "news"},
		{name: "news prefix", title: "News Q", reason: "news"},
		{name: "shopping", title: "Modern Estate & Period Jewelry with Sophia", reason: "shopping"},
		{name: "shopping savings", title: "Rise & Shine Savings - Plexaderm Skincare", reason: "shopping"},
		{name: "religious", title: "Climbing Higher with Bishop Eric Lambert", reason: "religious"},
		{name: "religious programming", title: "Religious Programming", reason: "religious"},
		{name: "sports pregame", title: "WNBA on ION Pregame Show", reason: "sports"},
		{name: "sports golf", title: "PGA Korn Ferry Tour Golf", reason: "sports"},
		{name: "sports qualifiers", title: "CONCACAF World Cup Qualifiers Today", reason: "sports"},
		{name: "filler", title: "Filler Everyday Evenings", reason: "filler"},
		{name: "paid programming", title: "Paid Programming", reason: "filler"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := cacheKey(tt.title, false)
			if got := tmdbSkipReason(key); got != tt.reason {
				t.Fatalf("tmdbSkipReason(%q) = %q, want %q", key, got, tt.reason)
			}
		})
	}
}

func TestTMDBSkipReasonKeepsCatalogTitles(t *testing.T) {
	titles := []string{
		"Hangout with Yoo",
		"National Parks: USA: New Beginnings",
		"Great News",
		"The Newsroom",
		"Sports Night",
		"Friday Night Lights",
		"Formula 1: Drive to Survive",
		"Racing Wives",
		"Law & Order: Special Victims Unit",
	}

	for _, title := range titles {
		t.Run(title, func(t *testing.T) {
			key := cacheKey(title, false)
			if got := tmdbSkipReason(key); got != "" {
				t.Fatalf("tmdbSkipReason(%q) = %q, want eligible", key, got)
			}
		})
	}
}

func TestTMDBSkipReasonNeverFiltersMovies(t *testing.T) {
	titles := []string{
		"Good News",
		"The Gospel",
		"Friday Night Lights",
		"Jewelry",
		"Champions League",
	}

	for _, title := range titles {
		t.Run(title, func(t *testing.T) {
			key := cacheKey(title, true)
			if got := tmdbSkipReason(key); got != "" {
				t.Fatalf("tmdbSkipReason(%q) = %q, movies must remain eligible", key, got)
			}
		})
	}
}
