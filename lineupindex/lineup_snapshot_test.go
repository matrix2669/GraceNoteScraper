package lineupindex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/web"
)

func TestLineupSnapshotPersistsOnlyReusableIdentityEvidence(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	service := &Service{snapshotDir: root, now: func() time.Time { return now }}
	lineup := LineupRecord{
		Key: "USA|11743|L1", LineupID: "L1", HeadendID: "H1", ProviderName: "Provider One",
		Country: "USA", PostalCode: "11743", Language: "en-us", Status: StatusRunning,
	}
	grid := &web.GridResponse{Channels: []web.JSONChannel{{
		ID: "position-1", ChannelID: "S1", ChannelNo: "101", CallSign: "EXAMPLE",
		AffiliateName: "Example Network", AffiliateCallSign: "EXNET",
		Events: []web.JSONEvent{{CallSign: "EXAMPLEHD", Program: web.JSONProgram{Title: "must not persist"}}},
	}}}
	evidence := ProviderEvidenceResult{
		Facts: []ProviderFact{
			{StationID: "S1", Kind: FactAlias, Value: "Example Network", SourceID: "provider-one", SourceLabel: "Provider One official lineup", Method: "exact provider channel number"},
			{StationID: "S1", Kind: FactCategory, Value: "News & Weather", RawValue: "News & Info", MatchMethod: "exact category alias", MatchConfidence: 1, SourceID: "provider-one", SourceLabel: "Provider One official lineup", Method: "exact provider channel number"},
		},
		Sources: []EvidenceSourceRecord{{ID: "provider-one", Label: "Provider One official lineup", Status: StatusComplete, Matched: 1, Aliases: 1, Categories: 1}},
	}
	if err := service.writeLineupSnapshot(lineup, grid, evidence); err != nil {
		t.Fatal(err)
	}
	path := lineupSnapshotPath(root, "USA", "11743", lineup.Key)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "must not persist") || strings.Contains(string(data), "events\"") {
		t.Fatalf("snapshot persisted programme payload: %s", data)
	}
	var snapshot LineupSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.SchemaVersion != CurrentLineupSnapshotVersion || snapshot.CategoryTaxonomyVersion != 2 || len(snapshot.Channels) != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	channel := snapshot.Channels[0]
	if channel.Category != "News & Weather" || len(channel.CategoryEvidence) != 1 || channel.CategoryEvidence[0].RawValue != "News & Info" || channel.CategoryEvidence[0].MatchConfidence != 1 {
		t.Fatalf("channel category evidence = %+v", channel)
	}
	if len(channel.EventCallSigns) != 1 || channel.EventCallSigns[0] != "EXAMPLEHD" {
		t.Fatalf("event callsigns = %+v", channel.EventCallSigns)
	}
	if filepath.Dir(path) != filepath.Join(root, "USA", "11743") {
		t.Fatalf("snapshot path = %s", path)
	}
}

func TestLineupSnapshotMarksConflictingCategories(t *testing.T) {
	root := t.TempDir()
	service := &Service{snapshotDir: root, now: time.Now}
	lineup := LineupRecord{Key: "L1", Country: "USA", PostalCode: "11743"}
	grid := &web.GridResponse{Channels: []web.JSONChannel{{ChannelID: "S1"}}}
	evidence := ProviderEvidenceResult{Facts: []ProviderFact{
		{StationID: "S1", Kind: FactCategory, Value: "Sports", SourceID: "one"},
		{StationID: "S1", Kind: FactCategory, Value: "News & Weather", SourceID: "two"},
	}}
	if err := service.writeLineupSnapshot(lineup, grid, evidence); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(lineupSnapshotPath(root, "USA", "11743", "L1"))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot LineupSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if !snapshot.Channels[0].CategoryConflict || snapshot.Channels[0].Category != "" {
		t.Fatalf("conflicting snapshot channel = %+v", snapshot.Channels[0])
	}
}
