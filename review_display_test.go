package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

func TestReviewDisplayPrefersEligibleCurrentFamily(t *testing.T) {
	c := builder.DraftChannel{Name: "HBOHTHD", CallSign: "HBOHTHD", StationID: "station", AliasEvidence: []builder.AliasEvidence{{Value: "HBO Signature Historical Long Alias", Sources: []string{"catalog"}, Methods: []string{"historical provider alias"}}, {Value: "HBO2", Sources: []string{"attested"}}}, ServiceEvidence: []builder.ServiceIdentityEvidence{{RootStationID: "station", Value: "HBO2", ServiceFamily: "HBO Hits", SourceID: "attested", SourceRowID: "row", SourceRevision: "revision"}}}
	if got := reviewDisplayName(c); got != "HBO Hits" {
		t.Fatal(got)
	}
	c.AliasEvidence = c.AliasEvidence[:1]
	if got := reviewDisplayName(c); got != "HBOHTHD" {
		t.Fatalf("suppressed family still displayed: %s", got)
	}
	c.Name = "Current descriptive name"
	if got := reviewDisplayName(c); got != c.Name {
		t.Fatal(got)
	}
}

func TestReviewDisplaySharedCapturedNames(t *testing.T) {
	data, err := os.ReadFile("tools/fixtures/review_display_names.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name     string               `json:"name"`
			Channel  builder.DraftChannel `json:"channel"`
			Expected string               `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 23 {
		t.Fatalf("incomplete display fixture: %d", len(fixture.Cases))
	}
	for _, test := range fixture.Cases {
		t.Run(test.Name, func(t *testing.T) {
			before, err := json.Marshal(test.Channel)
			if err != nil {
				t.Fatal(err)
			}
			if got := reviewDisplayName(test.Channel); got != test.Expected {
				t.Fatalf("display name = %q, want %q", got, test.Expected)
			}
			after, err := json.Marshal(test.Channel)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("display resolution changed identity, aliases, provenance or choices")
			}
		})
	}
}
