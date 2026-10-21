package main

import (
	"context"
	"encoding/json"
	"github.com/daniel-widrick/GraceNoteScraper/appconfig"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Optional private-copy replay. No fixture data or machine paths are committed.
// Only a fresh temporary state store is constructed; no retained state is opened.
func TestRetainedDryRunReplay(t *testing.T) {
	path := os.Getenv("DRYRUN_REPLAY_INPUT")
	if path == "" {
		t.Skip("private retained fixture not supplied")
	}
	var in struct {
		Draft      builder.Draft           `json:"draft"`
		ProofDraft builder.Draft           `json:"proofDraft"`
		Decisions  []builder.MatchDecision `json:"decisions"`
		Streams    []dispatcharr.Stream    `json:"streams"`
		IndexPath  string                  `json:"indexPath"`
		Config     appconfig.Config        `json:"config"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &in); err != nil {
		t.Fatal(err)
	}
	temp := t.TempDir()
	store, err := builder.LoadStateStore(filepath.Join(temp, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	svc := builder.NewService(store, builder.ServiceOptions{CacheDir: filepath.Join(temp, "sources")})
	if err = svc.SetMatchDecisions(in.Draft.SourceFingerprint, in.Decisions); err != nil {
		t.Fatal(err)
	}
	header := in.ProofDraft
	if header.SourceFingerprint != in.Draft.SourceFingerprint {
		t.Fatal("mixed source fixtures")
	}
	fullRows := map[string]builder.DraftChannel{}
	for _, c := range header.Channels {
		fullRows[c.ID] = c
	}
	inputs := []builder.InputChannel{}
	for _, c := range in.Draft.Channels {
		full := fullRows[c.ID]
		input := builder.InputChannel{Key: c.ID, StationID: c.StationID, PlacementID: full.PlacementID, Number: c.Number, CallSign: c.CallSign, Affiliate: ""}
		for _, a := range c.AliasEvidence {
			for _, method := range a.Methods {
				if method == "event callsign" {
					input.EventCallSigns = append(input.EventCallSigns, a.Value)
				}
			}
		}
		baseline := map[string]bool{}
		for _, value := range c.Aliases {
			baseline[strings.ToLower(value)] = true
		}
		// Reconstruct raw affiliation only from the current captured attribution,
		// not an older guide row; omissions and explicit suppressions stay omitted.
		for _, a := range c.AliasEvidence {
			for _, method := range a.Methods {
				if method == "affiliate name" {
					for _, source := range a.Sources {
						if source == "gracenote" {
							input.Affiliate = a.Value
						}
					}
				}
			}
		}
		for _, a := range full.SuppressedAliasEvidence {
			if err := svc.SetAliasSuppressed(in.Draft.SourceFingerprint, c.ID, a.Value, true); err != nil {
				t.Fatal(err)
			}
		}
		for _, raw := range []string{input.CallSign, input.StationID, input.PlacementID, strings.TrimSpace(input.Number + " " + input.CallSign)} {
			if raw != "" && !baseline[strings.ToLower(raw)] {
				if err := svc.SetAliasSuppressed(in.Draft.SourceFingerprint, c.ID, raw, true); err != nil {
					t.Fatal(err)
				}
			}
		}
		// Baseline aliases were captured from the active v1 evidence view.
		// Replay the policy delta on that exact admitted set rather than using
		// an older index snapshot as if it contained later scan results.
		for _, a := range c.AliasEvidence {
			numberOnly, independent := false, false
			for _, m := range a.Methods {
				if strings.Contains(m, "number-policy-provider-alias-v3") {
					numberOnly = true
				} else {
					independent = true
				}
			}
			if numberOnly && !independent {
				continue
			}
			for _, source := range a.Sources {
				if source == "gracenote" || source == "dispatcharr-confirmed" {
					continue
				}
				if numberOnly && !strings.HasPrefix(source, "gracenote-weekday-epg-") {
					continue
				}
				proofs := []builder.ServiceIdentityEvidence{}
				for _, proof := range full.ServiceEvidence {
					if proof.Value == a.Value && proof.SourceID == source {
						proofs = append(proofs, proof)
					}
				}
				if len(proofs) == 0 {
					input.ExternalAliases = append(input.ExternalAliases, builder.AttributedAlias{Value: a.Value, Source: source, Method: strings.Join(a.Methods, "; ")})
				}
				for i := range proofs {
					proof := proofs[i]
					input.ExternalAliases = append(input.ExternalAliases, builder.AttributedAlias{Value: a.Value, Source: source, Method: strings.Join(a.Methods, "; "), Identity: &proof})
				}
			}
		}
		// Current raw affiliation is independent selected-guide identity. No method
		// string is parsed into a reusable descriptor; optional catalogs stay off.
		inputs = append(inputs, input)
	}
	out, err := svc.Build(context.Background(), builder.LineupContext{SourceFingerprint: in.Draft.SourceFingerprint, Country: "US", ProviderName: header.ProviderName, LineupID: header.LineupID, PostalCode: header.PostalCode}, inputs)
	if err != nil {
		t.Fatal(err)
	}
	originals := map[string]builder.DraftChannel{}
	for _, c := range in.Draft.Channels {
		originals[c.ID] = c
	}
	for i := range out.Channels {
		old := originals[out.Channels[i].ID]
		out.Channels[i].Included = old.Included
		out.Channels[i].Name = old.Name
		out.Channels[i].Category = old.Category
		out.Channels[i].Number = old.Number
	}
	channels, _ := dispatcharrSnapshotChannels(out)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	matches, version, err := dispatcharr.MatchStreamCandidatesWithLineuparr(ctx, "retained-copy", "US", channels, in.Streams, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := in.Config
	feeds, codes := stationFeedCatalog(out, cfg)
	targets := map[string]dispatcharr.AffiliateTarget{}
	for _, c := range out.Channels {
		targets[c.ID] = dispatcharr.AffiliateTarget{Country: "US", CallSign: c.CallSign}
	}
	decisions := []dispatcharr.Decision{}
	for _, d := range in.Decisions {
		decisions = append(decisions, dispatcharr.Decision{ChannelID: d.ChannelID, Source: "retained-copy", StreamHash: d.StreamFingerprint, StreamName: d.StreamName, Decision: d.Decision})
	}
	derived := dispatcharr.DeriveAutomaticExclusions(matches.All, targets, feeds, codes, decisions)
	for _, row := range derived.Rows {
		if row.Conflict {
			continue
		}
		for i := range out.Channels {
			if out.Channels[i].ID == row.Candidate.ChannelID {
				out.Channels[i].ExcludedAliases = append(out.Channels[i].ExcludedAliases, row.Candidate.StreamName)
			}
		}
	}
	output := os.Getenv("DRYRUN_REPLAY_OUTPUT")
	if output == "" {
		t.Fatal("explicit output path required")
	}
	b, err := json.Marshal(struct {
		Draft   *builder.Draft              `json:"draft"`
		Export  builder.ExportFile          `json:"export"`
		Derived dispatcharr.AutomaticResult `json:"derived"`
		Version string                      `json:"version"`
	}{out, builder.ExportFromDraft(out), derived, version})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(output, b, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("replayed %d channels/%d physical streams; %d scored pairs; %d derived rows", len(channels), len(in.Streams), len(matches.All), len(derived.Rows))
}
