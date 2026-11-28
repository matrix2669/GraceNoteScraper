package guide

import "testing"

func TestAppendCanonicalCategoryAddsAndNormalizes(t *testing.T) {
	categories := []Category{{Name: "series", Lang: "en"}, {Name: "sports", Lang: "en"}}
	categories = appendCanonicalCategory(categories, "Sports", "en")

	if len(categories) != 2 {
		t.Fatalf("duplicate category added: %+v", categories)
	}
	if categories[1].Name != "Sports" {
		t.Fatalf("existing category was not canonicalized: %+v", categories[1])
	}

	categories = appendCanonicalCategory(categories, "News", "en")
	if len(categories) != 3 || categories[2].Name != "News" {
		t.Fatalf("new category was not appended: %+v", categories)
	}
}
