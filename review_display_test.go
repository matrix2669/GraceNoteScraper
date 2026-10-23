package main

import (
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
