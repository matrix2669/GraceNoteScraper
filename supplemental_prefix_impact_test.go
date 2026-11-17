package main

import (
	"encoding/json"
	"reflect"
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
		streams = append(streams, dispatcharr.Stream{ID: int64(i + 1), M3UAccountID: 3, ChannelGroupID: &group, Name: "AT&T: " + n + " HD"})
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
	if c.Stream.Name != "AT&T: Alpha Network HD" || !strings.Contains(c.Reason, "repeated provider prefix") {
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
	exported := builder.ExportFromDraft(draft)
	foundRaw := false
	for _, entries := range exported.Categories {
		for _, entry := range entries {
			for _, alias := range entry.Aliases {
				if alias == c.Stream.Name {
					foundRaw = true
				}
			}
		}
	}
	if !foundRaw {
		t.Fatal("raw name absent from export")
	}
	impact := repairImpact(t, before, draft, inventory)
	rows := compareSupplementalImpact(draft, inventory, svc.SupplementalDecisions("source"), nil, impact, "conn")
	if len(rows) != 1 || len(rows[0].Matched) != 2 {
		t.Fatalf("unchanged consumer did not report cross-account effect: %+v", rows)
	}
}

func TestPacificFeedLabelsCannotCreateConsumerAliases(t *testing.T) {
	for _, prefix := range []string{"Pacific", "Pacific Coast"} {
		t.Run(prefix, func(t *testing.T) {
			svc, draft, _, _ := repairFixture(t, "101")
			names := []string{"Alpha Network", "Bravo Network", "Charlie Network", "Delta Network", "Echo Network"}
			group := int64(7)
			var streams []dispatcharr.Stream
			var targets []dispatcharr.SupplementalTarget
			for i, name := range names {
				id := string(rune('a' + i))
				if i == 0 {
					id = "one"
				}
				streams = append(streams, dispatcharr.Stream{ID: int64(i + 1), M3UAccountID: 3, ChannelGroupID: &group, Name: prefix + ": " + name + " HD"})
				targets = append(targets, dispatcharr.SupplementalTarget{ID: id, Country: "US", Labels: []dispatcharr.SupplementalLabel{{Value: name, Evidence: "independent"}}})
			}
			inventory := dispatcharr.ProjectIdentityInventory(streams)
			before, _ := json.Marshal(builder.ExportFromDraft(draft))
			proposals, complete := dispatcharr.RetrieveSupplemental(targets, inventory, nil)
			if !complete || len(proposals) != 0 {
				t.Fatalf("feed prefix stripped into proposals: %+v", proposals)
			}
			if len(svc.SupplementalDecisions("source")) != 0 {
				t.Fatal("feed names saved without a choice")
			}
			after, _ := json.Marshal(builder.ExportFromDraft(draft))
			if string(before) != string(after) {
				t.Fatal("feed exclusion changed export")
			}
			result := repairImpact(t, before, draft, inventory)
			if !reflect.DeepEqual(result.Baseline, result.Proposed) {
				t.Fatal("unchanged consumer changed outcomes")
			}
		})
	}
}
