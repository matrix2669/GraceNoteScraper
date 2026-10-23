package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"github.com/daniel-widrick/GraceNoteScraper/guide"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

// The old replay manifest intentionally omitted postal/headend/language. Use
// its captured selected-lineup record, rather than inventing an active context.
func retainedConfig(t *testing.T, path string, cfg appconfig.Config) appconfig.Config {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var idx lineupindex.Index
	if err = json.Unmarshal(data, &idx); err != nil {
		t.Fatal(err)
	}
	l := idx.Lineups[cfg.Gracenote.LineupID]
	if l == nil {
		t.Fatal("captured selected lineup missing")
	}
	cfg.Gracenote.Country = l.Country
	cfg.Gracenote.PostalCode = l.PostalCode
	cfg.Gracenote.HeadendID = l.HeadendID
	cfg.Gracenote.Language = l.Language
	cfg.Gracenote.Device = l.Device
	return cfg
}

func retainedEvidenceView(t *testing.T, path string, cfg appconfig.Config, channels []builder.DraftChannel) (*lineupindex.Service, lineupindex.EvidenceView) {
	t.Helper()
	cfg = retainedConfig(t, path, cfg)
	svc, err := lineupindex.NewService(lineupindex.ServiceConfig{Path: path, Providers: &fakeProviderFinder{response: &web.ProviderResponse{}}, Grids: fakeMarketGridFetcher{}})
	if err != nil {
		t.Fatal(err)
	}
	raw := map[string][]string{}
	for _, c := range channels {
		raw[c.StationID] = append(raw[c.StationID], c.CallSign)
	}
	view := svc.EvidenceForLineup(lineupindex.EvidenceContext{Country: cfg.Gracenote.Country, PostalCode: cfg.Gracenote.PostalCode, LineupID: cfg.Gracenote.LineupID, Device: cfg.Gracenote.Device, Headend: cfg.Gracenote.HeadendID, Language: cfg.Gracenote.Language, Fingerprint: cfg.Fingerprint(), RawCallSigns: raw})
	return svc, view
}

// This optional check consumes copied retained files only. It verifies the real
// EvidenceView -> draft -> matcher -> JSON path and never opens live state.
func TestAdjudicationRetainedProjection(t *testing.T) {
	path := os.Getenv("ADJUDICATION_REPLAY_INDEX")
	if path == "" {
		t.Skip("private retained copy not supplied")
	}
	inputPath := os.Getenv("DRYRUN_REPLAY_INPUT")
	var in struct {
		Draft  builder.Draft    `json:"draft"`
		Config appconfig.Config `json:"config"`
	}
	read := func(path string, dst any) []byte {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(data, dst); err != nil {
			t.Fatal(err)
		}
		return data
	}
	read(inputPath, &in)
	in.Config = retainedConfig(t, path, in.Config)
	if in.Config.Fingerprint() != in.Draft.SourceFingerprint {
		t.Fatal("captured lineup context/source mismatch")
	}
	var idx lineupindex.Index
	beforeIndex := read(path, &idx)
	audit := lineupindex.ApplyEvidenceQuarantine(&idx)
	a := audit.Adjudications[0]
	if a.ContextStatus != "verified" {
		t.Fatal("full runtime audit context not verified", a.ContextStatus)
	}
	index, view := retainedEvidenceView(t, path, in.Config, in.Draft.Channels)
	ids := []string{}
	current := map[string]bool{}
	for _, c := range in.Draft.Channels {
		ids = append(ids, c.StationID)
		current[c.StationID] = true
	}
	aliases := view.AliasesForStations(ids)
	valid, rejected := 0, 0
	for sid, st := range idx.Stations {
		if !current[sid] {
			continue
		}
		for _, f := range st.Facts {
			if f.SourceID != a.Assignment.SourceID || f.RootSourceLineupKey != a.Assignment.RootLineupKey || !strings.Contains(f.Method, "number-policy-provider-alias-v3") {
				continue
			}
			present := false
			for _, v := range aliases[sid] {
				if v.SourceID == f.SourceID && v.SourceRowID == f.SourceRowID && v.RootSourceLineupKey == f.RootSourceLineupKey && v.RootSourceStationID == f.RootSourceStationID {
					present = true
				}
			}
			if sid == a.Assignment.RootStationID && f.SourceRowID == a.Assignment.SourceRowID {
				if present {
					t.Fatal("adjudicated row active")
				}
				rejected++
			} else {
				if !present {
					t.Fatal("valid local recovery lost", sid, f.SourceRowID)
				}
				valid++
			}
		}
	}
	if valid != 48 || rejected != 1 {
		t.Fatal("local recovery scope changed", valid, rejected)
	}
	s := newLineuparrTestServer(t, true)
	if err := s.store.Save(in.Config); err != nil {
		t.Fatal(err)
	}
	g := &guide.TVGuide{}
	for _, c := range in.Draft.Channels {
		g.LineupChannels = append(g.LineupChannels, guide.Channel{ID: c.StationID, PlacementID: c.ID, ChannelNo: c.Number, CallSign: c.CallSign})
	}
	s.state.UpdateForSource(g, in.Config.Fingerprint())
	stateBytes, err := os.ReadFile(os.Getenv("ADJUDICATION_REPLAY_STATE"))
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(statePath, stateBytes, 0600)
	state, err := builder.LoadStateStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	s.builder = builder.NewService(state, builder.ServiceOptions{CacheDir: t.TempDir()})
	s.marketIndex = index
	draft := automaticDraft(t, &dispatcharrServer{lineup: s})
	var target builder.DraftChannel
	for _, c := range draft.Channels {
		if c.StationID == a.Assignment.RootStationID {
			target = c
		}
	}
	invalid := a.OriginalAssertions[0].Value
	for _, v := range target.Aliases {
		if v == invalid {
			t.Fatal("invalid support reappeared in draft")
		}
	}
	if target.Category != "News & Weather" || !target.Included {
		t.Fatal("chosen category/inclusion changed", target.Category, target.Included)
	}
	independent := false
	for _, v := range target.ServiceEvidence {
		if v.Value == "N12" {
			independent = true
		}
	}
	if !independent {
		t.Fatal("independent raw pair proof lost")
	}
	matchChannels, _ := dispatcharrSnapshotChannels(draft)
	var channel dispatcharr.MatchChannel
	for _, c := range matchChannels {
		if c.ID == target.ID {
			channel = c
		}
	}
	data, _ := json.Marshal(channel)
	if strings.Contains(string(data), invalid) {
		t.Fatal("matcher target kept invalid alias")
	}
	exported, _ := json.Marshal(builder.ExportFromDraft(draft))
	if strings.Contains(string(exported), invalid) {
		t.Fatal("export retained invalid alias")
	}
	streams := []dispatcharr.Stream{{ID: 1, M3UAccountID: 3, Name: invalid}, {ID: 2, M3UAccountID: 3, Name: "N12"}}
	after, _, err := dispatcharr.MatchStreamCandidatesWithLineuparr(context.Background(), "isolated-provenance", "US", []dispatcharr.MatchChannel{channel}, streams, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range after.All {
		if c.StreamHash == streams[0].Fingerprint() {
			t.Fatal("rejected alias still matches synthetic alias stream")
		}
	}
	// The old captured target independently demonstrates the before case using
	// the actual pinned matcher, without changing any new scoring thresholds.
	prior := in.Draft
	prior.Channels = []builder.DraftChannel{}
	for _, c := range in.Draft.Channels {
		if c.StationID == a.Assignment.RootStationID {
			prior.Channels = append(prior.Channels, c)
		}
	}
	oldChannels, _ := dispatcharrSnapshotChannels(&prior)
	before, _, err := dispatcharr.MatchStreamCandidatesWithLineuparr(context.Background(), "isolated-provenance", "US", oldChannels, streams, nil)
	if err != nil {
		t.Fatal(err)
	}
	wasMatched := false
	keptIndependent := false
	for _, c := range before.All {
		if c.StreamHash == streams[0].Fingerprint() {
			wasMatched = true
		}
	}
	for _, c := range after.All {
		if c.StreamHash == streams[1].Fingerprint() {
			keptIndependent = true
		}
	}
	if !wasMatched || !keptIndependent {
		t.Fatal("before/after matcher evidence incomplete", wasMatched, keptIndependent)
	}
	afterIndex, _ := os.ReadFile(path)
	afterState, _ := os.ReadFile(statePath)
	if string(beforeIndex) != string(afterIndex) || string(stateBytes) != string(afterState) {
		t.Fatal("retained state/index bytes changed")
	}
	report := map[string]any{"audit": audit, "validOtherLocalRecoveries": valid, "rejectedAssignments": rejected, "newTarget": target, "oldMatcher": before.All, "newMatcher": after.All, "stateBytesUnchanged": true, "indexBytesUnchanged": true, "chosenCategoryPreserved": true, "independentPairProofPreserved": true}
	if out := os.Getenv("ADJUDICATION_REPLAY_REPORT"); out != "" {
		data, _ := json.MarshalIndent(report, "", "  ")
		if err = os.WriteFile(out, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("actual retained EvidenceView/draft/matcher/JSON: invalid assignment absent;48other local recoveries and independent N12 retained; saved state/index bytes unchanged")
}
