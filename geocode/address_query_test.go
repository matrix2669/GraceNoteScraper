package geocode

import "testing"

func TestStreetAndFullAddressQueries(t *testing.T) {
	for _, query := range []string{"1 Test Street, Fort Lauderdale FL 33308", "1 Test Street 33308-1234"} {
		if addressQuery(query, "33308") != query {
			t.Fatal("duplicated ZIP", query)
		}
	}
	if addressQuery("1 Test Street", "33308") != "1 Test Street, 33308" {
		t.Fatal("missing ZIP context")
	}
}
