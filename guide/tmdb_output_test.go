package guide

import "testing"

func TestPromotedKeywordCategory(t *testing.T) {
	tests := []struct {
		keyword string
		want    string
		ok      bool
	}{
		{keyword: "sitcom", want: "Sitcom", ok: true},
		{keyword: "Situation Comedy", want: "Sitcom", ok: true},
		{keyword: "  TRUE   CRIME ", want: "True Crime", ok: true},
		{keyword: "home renovation", want: "Home Improvement", ok: true},
		{keyword: "police investigation", want: "Police", ok: true},
		{keyword: "mother daughter relationship", want: "", ok: false},
		{keyword: "duringcreditsstinger", want: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.keyword, func(t *testing.T) {
			got, ok := promotedKeywordCategory(tt.keyword)
			if ok != tt.ok {
				t.Fatalf("promotedKeywordCategory(%q) ok = %v, want %v", tt.keyword, ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("promotedKeywordCategory(%q) = %q, want %q", tt.keyword, got, tt.want)
			}
		})
	}
}

func TestNormalizeCategoryName(t *testing.T) {
	got := normalizeCategoryName("  Home &amp;   Garden  ")
	if got != "home & garden" {
		t.Fatalf("normalizeCategoryName() = %q, want %q", got, "home & garden")
	}
}
