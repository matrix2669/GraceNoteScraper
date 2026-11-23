package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

// Optional offline cost measurement. Reads captured safe metadata only; never
// opens retained stores, contacts a provider or executes the matcher.
func TestRetainedReviewProjectionCost(t *testing.T) {
	inputPath, outputPath := os.Getenv("REVIEW_COST_INPUT"), os.Getenv("REVIEW_COST_DRAFT")
	if inputPath == "" || outputPath == "" {
		t.Skip("private captured inventory/draft not supplied")
	}
	read := func(path string, dst any) {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, dst); err != nil {
			t.Fatal(err)
		}
	}
	var input struct {
		Streams   []dispatcharr.Stream
		Decisions []builder.MatchDecision
		Config    appconfig.Config
	}
	var output struct {
		Draft   builder.Draft
		Derived dispatcharr.AutomaticResult
	}
	read(inputPath, &input)
	read(outputPath, &output)
	feeds, codes := stationFeedCatalog(&output.Draft, input.Config)
	targets := []dispatcharr.FeedTarget{}
	for _, c := range output.Draft.Channels {
		if c.Included {
			targets = append(targets, dispatcharr.FeedTarget{ID: c.ID, Name: c.Name, Number: c.Number, Country: "US", Feed: feeds[c.ID]})
		}
	}
	decisions := []dispatcharr.Decision{}
	for _, d := range input.Decisions {
		decisions = append(decisions, dispatcharr.Decision{ChannelID: d.ChannelID, Source: "retained-copy", StreamHash: d.StreamFingerprint, StreamName: d.StreamName, Decision: d.Decision})
	}
	streams := dispatcharr.ProjectIdentityInventory(input.Streams)
	start := time.Now()
	for i := 0; i < 3; i++ {
		rows := dispatcharr.DeriveInventoryFeedExclusions("retained-copy", streams, targets, codes, decisions, output.Derived.Rows)
		if len(rows) != len(output.Derived.InventoryRows) {
			t.Fatalf("inventory evidence changed: %d vs %d", len(rows), len(output.Derived.InventoryRows))
		}
	}
	t.Logf("%d streams, %d targets, %d saved decisions: three inventory projections %s (no matcher/fetch)", len(streams), len(targets), len(decisions), time.Since(start))
}
