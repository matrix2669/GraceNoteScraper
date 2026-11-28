package main

import (
	"fmt"

	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	matcher "github.com/daniel-widrick/GraceNoteScraper/lineuparr_matcher"
)

// canonicalDelta compares every occurrence and physical stream, not display
// names or serialized numbers as identifiers. It is a proof input, not a lease.
type canonicalDelta struct {
	Added, Lost []supplementalImpactPhysical
	Unresolved  []supplementalImpactPhysical
	Conflict    []supplementalImpactPhysical
}

func canonicalImpactDeltas(baseline, proposed builder.ExportFile, result dispatcharr.ImpactResult, inventory []dispatcharr.IdentityStream, targets map[string]builder.DraftChannel, source, connection string, primary map[string]builder.MatchDecision, supplemental map[string]builder.SupplementalDecision) (map[string]canonicalDelta, error) {
	out := map[string]canonicalDelta{}
	if result.Revision != matcher.Revision {
		return out, fmt.Errorf("unexpected consumer revision")
	}
	streamNames := map[string]bool{}
	for _, s := range inventory {
		streamNames[s.Name] = true
	}
	decode := func(export builder.ExportFile, rows []dispatcharr.ImpactMatch) (map[string]map[string]bool, error) {
		values := map[string]map[string]bool{}
		expected := 0
		for _, entries := range export.Categories {
			expected += len(entries)
		}
		if len(rows) != expected {
			return nil, fmt.Errorf("incomplete consumer result")
		}
		for _, r := range rows {
			entries, ok := export.Categories[r.Category]
			if !ok || r.EntryIndex < 0 || r.EntryIndex >= len(entries) {
				return nil, fmt.Errorf("invalid consumer occurrence")
			}
			id := entries[r.EntryIndex].ChannelID
			if id == "" || values[id] != nil {
				return nil, fmt.Errorf("ambiguous target occurrence")
			}
			names := map[string]bool{}
			for _, name := range r.Names {
				if !streamNames[name] {
					return nil, fmt.Errorf("unknown consumer stream")
				}
				names[name] = true
			}
			values[id] = names
		}
		return values, nil
	}
	before, err := decode(baseline, result.Baseline)
	if err != nil {
		return out, err
	}
	after, err := decode(proposed, result.Proposed)
	if err != nil {
		return out, err
	}
	if len(before) != len(after) {
		return out, fmt.Errorf("target sets differ")
	}
	for id, old := range before {
		target, exists := targets[id]
		if !exists {
			return nil, fmt.Errorf("unknown target")
		}
		delta := canonicalDelta{}
		now, ok := after[id]
		if !ok {
			return out, fmt.Errorf("target missing")
		}
		for _, stream := range inventory {
			if old[stream.Name] == now[stream.Name] {
				continue
			}
			physical := supplementalImpactPhysical{Name: stream.Name, AccountID: stream.AccountID, StreamID: stream.ID}
			if old[stream.Name] {
				delta.Lost = append(delta.Lost, physical)
				continue
			}
			delta.Added = append(delta.Added, physical)
			name := supplementalLiteral(stream.Name)
			confirmed, denied := false, false
			for _, d := range primary {
				if d.ChannelID == id && d.DispatcharrFingerprint == connection && name != "" && supplementalLiteral(d.StreamName) == name {
					if d.Decision == "confirmed" {
						confirmed = true
					}
					if d.Decision == "denied" {
						denied = true
					}
				}
			}
			for _, d := range supplemental {
				if d.SourceFingerprint == source && d.DispatcharrFingerprint == connection && d.ChannelID == id && d.StationID == target.StationID && d.PlacementID == target.PlacementID && d.RawCallSign == target.CallSign && name != "" && supplementalLiteral(d.StreamName) == name {
					if d.Decision == "confirmed" {
						confirmed = true
					}
					if d.Decision == "denied" {
						denied = true
					}
				}
			}
			if denied {
				delta.Conflict = append(delta.Conflict, physical)
			} else if !confirmed {
				delta.Unresolved = append(delta.Unresolved, physical)
			}
		}
		out[id] = delta
	}
	return out, nil
}

func canonicalImpactDelta(b, p builder.ExportFile, r dispatcharr.ImpactResult, i []dispatcharr.IdentityStream, t builder.DraftChannel, source, connection string, primary map[string]builder.MatchDecision, supp map[string]builder.SupplementalDecision) (canonicalDelta, error) {
	targets := map[string]builder.DraftChannel{t.ID: t}
	all, err := canonicalImpactDeltas(b, p, r, i, targets, source, connection, primary, supp)
	return all[t.ID], err
}
