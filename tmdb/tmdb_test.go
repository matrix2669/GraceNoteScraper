package tmdb

import "testing"

func TestNormalizeTitle(t *testing.T) {
	got := normalizeTitle("Law & Order: Special Victims Unit")
	want := "law order special victims unit"
	if got != want {
		t.Fatalf("normalizeTitle() = %q, want %q", got, want)
	}
}

func TestTitleSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		min  int
	}{
		{name: "exact", a: "The Equalizer", b: "The Equalizer", min: 100},
		{name: "punctuation", a: "Law & Order", b: "Law Order", min: 100},
		{name: "subtitle", a: "FBI", b: "FBI: Most Wanted", min: 85},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := titleSimilarity(tt.a, tt.b); got < tt.min {
				t.Fatalf("titleSimilarity(%q, %q) = %d, want >= %d", tt.a, tt.b, got, tt.min)
			}
		})
	}
}

func TestSelectBestResultPrefersExactTitle(t *testing.T) {
	results := []searchResult{
		{ID: 1, Name: "The Flash", VoteCount: 100000, Popularity: 1000},
		{ID: 2, Name: "Ghosts", VoteCount: 500, Popularity: 20},
	}

	got, score, ok := selectBestResult("Ghosts", results, false)
	if !ok {
		t.Fatal("selectBestResult rejected an exact match")
	}
	if got.ID != 2 {
		t.Fatalf("selected ID %d, want 2", got.ID)
	}
	if score != 100 {
		t.Fatalf("score = %d, want 100", score)
	}
}

func TestSelectBestResultRejectsWeakMatch(t *testing.T) {
	results := []searchResult{
		{ID: 1, Title: "A Completely Different Movie", VoteCount: 50000},
	}

	_, _, ok := selectBestResult("Local News at Eleven", results, true)
	if ok {
		t.Fatal("selectBestResult accepted an unrelated title")
	}
}

func TestCacheKeyNormalizesEquivalentTitles(t *testing.T) {
	a := cacheKey("Law & Order", false)
	b := cacheKey("law order", false)
	if a != b {
		t.Fatalf("cache keys differ: %q != %q", a, b)
	}
	if a == cacheKey("Law & Order", true) {
		t.Fatal("movie and TV cache keys must differ")
	}
}
