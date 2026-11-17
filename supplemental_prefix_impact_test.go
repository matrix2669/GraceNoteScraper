package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

func TestLearnedPrefixExplicitChoiceUsesRawNameInActualConsumer(t *testing.T) {
	svc, draft, _, _ := repairFixture(t, "101")
	draft.Channels[0].Name = "Alpha Network"
	targets := []dispatcharr.SupplementalTarget{}
	names := []string{"Alpha Network", "Bravo Network", "Charlie Network", "Delta Network", "Echo Network"}
	group := int64(7)
	streams := []dispatcharr.Stream{}
	for i, n := range names {
		id := string(rune('a' + i))
		if i == 0 {
			id = "one"
		}
		targets = append(targets, dispatcharr.SupplementalTarget{ID: id, StationID: "station-one", PlacementID: "placement-one", CallSign: "RAW", Country: "US", Labels: []dispatcharr.SupplementalLabel{{Value: n, Evidence: "independent"}}})
		streams = append(streams, dispatcharr.Stream{ID: int64(i + 1), M3UAccountID: 3, ChannelGroupID: &group, Name: "Mystery: " + n + " HD"})
	}
	// Same raw name in a different account is not discovered there but is still
	// subject to the consumer's literal-name based export semantics.
	streams = append(streams, dispatcharr.Stream{ID: 42, M3UAccountID: 4, Name: streams[0].Name})
	inventory := dispatcharr.ProjectIdentityInventory(streams)
	before, _ := json.Marshal(builder.ExportFromDraft(draft))
	choices, complete := dispatcharr.RetrieveSupplemental(targets, inventory, nil)
	if !complete {
		t.Fatal("incomplete")
	}
	var c dispatcharr.SupplementalCandidate
	for _, row := range choices {
		if row.Target.ID == "one" && row.Stream.AccountID == 3 {
			c = row
		}
	}
	if c.Stream.Name == "" || !strings.Contains(c.Reason, "repeated provider prefix") {
		t.Fatal("learned proposal missing")
	}
	unchanged, _ := json.Marshal(builder.ExportFromDraft(draft))
	if string(before) != string(unchanged) || len(svc.SupplementalDecisions("source")) != 0 {
		t.Fatal("discovery mutated state or export")
	}
	d := supplementalDecision(c, "source", "conn", "snapshot")
	d.Decision = "confirmed"
	d.OperationID = "explicit"
	d.GroupKey = "supp:test"
	repairSave(t, svc, d)
	svc.ProjectSupplementalDecisions(draft, "source", "conn")
	proposed, _ := json.Marshal(builder.ExportFromDraft(draft))
	if !strings.Contains(string(proposed), c.Stream.Name) {
		t.Fatal("raw name absent from export")
	}
	impact := repairImpact(t, before, draft, inventory)
	rows := compareSupplementalImpact(draft, inventory, svc.SupplementalDecisions("source"), nil, impact, "conn")
	if len(rows) != 1 || len(rows[0].Matched) != 2 {
		t.Fatalf("unchanged consumer did not report cross-account effect: %+v", rows)
	}
}
