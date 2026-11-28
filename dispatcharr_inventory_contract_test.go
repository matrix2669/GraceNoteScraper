package main

import (
	"context"
	"encoding/json"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"os"
	"testing"
)

func TestInventoryFeedJSONConsumerFixture(t *testing.T) {
	path := os.Getenv("INVENTORY_EXPORT_FIXTURE")
	if path == "" {
		t.Skip("isolated consumer fixture not requested")
	}
	definitions := []struct{ code, family, region string }{
		{"TRAVHD", "Travel Channel", "east"}, {"FXHD", "FX", "east"}, {"HBOHDP", "HBO", "west"}, {"PASHOHW", "Paramount+ with Showtime", "west"}, {"MAXHDP", "Cinemax", "west"},
	}
	streams := []dispatcharr.Stream{}
	for _, d := range definitions {
		for _, suffix := range []string{" HD", " EAST HD", " WEST HD"} {
			if d.region == "east" && suffix == " EAST HD" {
				continue // Travel/FX reproduction has only generic and opposing names.
			}
			streams = append(streams, dispatcharr.Stream{ID: int64(len(streams) + 1), Name: "US: " + d.family + suffix, M3UAccountID: 3})
		}
	}
	streams = append(streams, dispatcharr.Stream{ID: 99, Name: "TV| Travel Channel WEST ᴿᴬᵂ", M3UAccountID: 7})
	channels := []dispatcharr.MatchChannel{}
	targets := []dispatcharr.FeedTarget{}
	feeds := map[string]dispatcharr.FeedIdentity{}
	draft := &builder.Draft{}
	for _, d := range definitions {
		aliases := []string{d.family}
		channels = append(channels, dispatcharr.MatchChannel{ID: d.code, Name: d.code, Aliases: aliases})
		feed := dispatcharr.FeedIdentity{Family: dispatcharr.StreamFeed(d.family, nil).Family, Region: d.region, Evidence: d.family + " - " + d.region}
		feeds[d.code] = feed
		targets = append(targets, dispatcharr.FeedTarget{ID: d.code, Name: d.code, Country: "US", Feed: feed})
		draft.Channels = append(draft.Channels, builder.DraftChannel{ID: d.code, Name: d.code, Category: "Test", Included: true, Aliases: aliases})
	}
	matches, _, err := dispatcharr.MatchStreamCandidatesWithLineuparr(context.Background(), "fixture", "US", channels, streams, nil)
	if err != nil {
		t.Fatal(err)
	}
	decisions := []dispatcharr.Decision{{ChannelID: "MAXHDP", Source: "fixture", StreamName: "US: Cinemax EAST HD", Decision: "confirmed"}}
	scored := dispatcharr.DeriveAutomaticExclusions(matches.All, nil, feeds, nil, decisions)
	add := func(id, raw string) {
		for i := range draft.Channels {
			if draft.Channels[i].ID == id {
				draft.Channels[i].ExcludedAliases = append(draft.Channels[i].ExcludedAliases, raw)
			}
		}
	}
	for _, r := range scored.Rows {
		if !r.Conflict {
			add(r.Candidate.ChannelID, r.Candidate.StreamName)
		}
	}
	before := builder.ExportFromDraft(draft)
	rows := dispatcharr.DeriveInventoryFeedExclusions("fixture", dispatcharr.ProjectIdentityInventory(streams), targets, nil, decisions, scored.Rows)
	for _, r := range rows {
		if !r.Conflict {
			add(r.ChannelID, r.StreamName)
		}
	}
	// The same real export boundary escapes pattern syntax, never a regex or wildcard.
	escaped := &builder.Draft{Channels: []builder.DraftChannel{{Name: "Literal", Included: true, ExcludedAliases: []string{`US: Example * \\ WEST HD`}}}}
	data, err := json.Marshal(map[string]any{"before": before, "after": builder.ExportFromDraft(draft), "literal": builder.ExportFromDraft(escaped), "streams": streams, "inventoryRows": rows})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
