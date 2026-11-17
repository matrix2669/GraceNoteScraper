package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
)

func TestSupplementalCarryoverImpactUsesLiteralCurrentName(t *testing.T) {
	for _, choice := range []string{"confirmed", "denied"} {
		t.Run(choice, func(t *testing.T) {
			svc, draft, d, inventory := repairFixture(t, "101")
			before, _ := json.Marshal(builder.ExportFromDraft(draft))
			d.Decision = choice
			repairSave(t, svc, d)
			svc.ProjectSupplementalDecisions(draft, "source", "conn")
			// Replacement has a new physical identity but retains the literal name.
			inventory[0].ID += 1000
			inventory[0].Fingerprint = "replacement"
			result := repairImpact(t, before, draft, inventory)
			rows := compareSupplementalImpact(draft, inventory, svc.SupplementalDecisions("source"), nil, result, "conn")
			if len(rows) != 1 || len(rows[0].Missed) != 0 || len(rows[0].Extra) != 0 || len(rows[0].SameNameConflicts) != 0 {
				t.Fatalf("replacement treated as missing, extra or expansion: %+v", rows)
			}
			saved := svc.SupplementalDecisions("source")[d.Key]
			if saved.StreamID != d.StreamID || saved.StreamFingerprint != d.StreamFingerprint || saved.StreamName != d.StreamName {
				t.Fatalf("history rewritten: %+v", saved)
			}
			// Missing literal names must still warn on a confirmation.
			if choice == "confirmed" {
				rows = compareSupplementalImpact(draft, nil, svc.SupplementalDecisions("source"), nil, result, "conn")
				if len(rows) != 1 || len(rows[0].Missed) != 1 {
					t.Fatalf("missing name hidden: %+v", rows)
				}
			}
		})
	}
}

func TestSupplementalCarryoverImpactAmbiguousExpansion(t *testing.T) {
	svc, draft, d, inventory := repairFixture(t, "101")
	before, _ := json.Marshal(builder.ExportFromDraft(draft))
	repairSave(t, svc, d)
	svc.ProjectSupplementalDecisions(draft, "source", "conn")
	replacement := inventory[0]
	replacement.ID += 100
	replacement.AccountID++
	replacement.Fingerprint = "new"
	inventory = append(inventory, replacement)
	result := repairImpact(t, before, draft, inventory)
	rows := compareSupplementalImpact(draft, inventory, svc.SupplementalDecisions("source"), nil, result, "conn")
	if len(rows) != 1 || len(rows[0].SameNameConflicts) == 0 {
		t.Fatalf("additional same-name member not disclosed: %+v", rows)
	}
}

func TestSupplementalCarryoverReviewScopeAndNames(t *testing.T) {
	s, review := supplementalServerFixture(t)
	g := review.Supplemental.Candidates[0]
	body := supplementalSaveBody(review, g)
	if w := supplementalRequest(t, s, "POST", "/decision", body); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cfg, _, _ := s.lineup.store.Get()
	stored := s.lineup.builder.SupplementalDecisions(cfg.Fingerprint())
	var old builder.SupplementalDecision
	for _, d := range stored {
		old = d
		break
	}
	replacement := old
	replacement.StreamID++
	replacement.StreamFingerprint = "new-id"
	replacement.Key = builder.SupplementalKey(replacement)
	if !supplementalChoiceCovers(old, replacement) {
		t.Fatal("replacement scope changed")
	}
	for _, field := range []string{"source", "connection", "channel", "station", "placement", "callsign"} {
		changed := replacement
		switch field {
		case "source":
			changed.SourceFingerprint += "x"
		case "connection":
			changed.DispatcharrFingerprint += "x"
		case "channel":
			changed.ChannelID += "x"
		case "station":
			changed.StationID += "x"
		case "placement":
			changed.PlacementID += "x"
		case "callsign":
			changed.RawCallSign += "x"
		}
		if supplementalChoiceCovers(old, changed) {
			t.Fatalf("%s scope ignored", field)
		}
	}
	for _, name := range []string{old.StreamName + " West", old.StreamName + " SD", strings.Replace(old.StreamName, "|", ":", 1)} {
		changed := replacement
		changed.StreamName = name
		if supplementalChoiceCovers(old, changed) {
			t.Fatalf("distinct name collapsed: %q", name)
		}
	}
	for _, choice := range []string{"confirmed", "denied"} {
		old.Decision = choice
		replacement.StreamName = "  " + strings.ToLower(old.StreamName) + "  "
		if !supplementalChoiceCovers(old, replacement) {
			t.Fatalf("%s did not carry", choice)
		}
	}
	// Undo still removes original saved group and a retry cannot resurrect it.
	if w := supplementalRequest(t, s, "DELETE", "/decision", map[string]string{"key": g.Key}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := supplementalRequest(t, s, "POST", "/decision", body); w.Code != 409 {
		t.Fatalf("late retry %d", w.Code)
	}
}

func TestPrimaryCoverageDistinguishesPresentUnscoredAndMissing(t *testing.T) {
	for _, tt := range []struct {
		inventory []dispatcharr.IdentityStream
		want      string
	}{
		{[]dispatcharr.IdentityStream{{Name: " us:   service hd "}}, "present, but no scored candidate"},
		{[]dispatcharr.IdentityStream{{Name: "US: Service SD"}}, "absent from this inventory"},
	} {
		history := []dispatcharrReviewDecision{{ChannelID: "one", Decision: "confirmed", StreamNames: []string{"US: Service HD"}}}
		annotateUnsupportedConfirmations(history, nil, tt.inventory)
		if !history[0].NeedsReview || !strings.Contains(history[0].ReviewWarning, tt.want) {
			t.Fatalf("warning: %+v", history[0])
		}
	}
}

func TestSupplementalReviewSuppressesReplacementAcrossAccounts(t *testing.T) {
	for _, choice := range []string{"confirmed", "denied"} {
		t.Run(choice, func(t *testing.T) {
			s, review := supplementalServerFixture(t)
			g := review.Supplemental.Candidates[0]
			body := supplementalSaveBody(review, g)
			body["decision"] = choice
			if w := supplementalRequest(t, s, "POST", "/decision", body); w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			cfg, _, _ := s.lineup.store.Get()
			w := httptest.NewRecorder()
			draft, _, _, ok := s.lineup.buildDraft(w, httptest.NewRequest("GET", "/draft", nil))
			if !ok {
				t.Fatal(w.Code, w.Body.String())
			}
			inputs := s.lineup.reviewInputsRevision()
			rows, complete := s.prepareSupplemental(draft, cfg, s.snapshot, inputs)
			replacement := append([]dispatcharr.SupplementalCandidate(nil), rows...)
			changed := 0
			for i := range replacement {
				if replacement[i].Target.ID == g.ChannelID && supplementalLiteral(replacement[i].Stream.Name) == supplementalLiteral(g.StreamName) {
					replacement[i].Stream.ID += 1000
					replacement[i].Stream.Fingerprint += "-replacement"
					changed++
				}
			}
			if changed == 0 {
				t.Fatal("fixture did not include saved name")
			}
			s.supplementalPreparation = supplementalPreparation{inputs: inputs, snapshot: snapshotIdentity(s.snapshot), candidates: replacement, complete: complete}
			got, _ := s.buildSupplementalReview(draft, cfg, s.snapshot, inputs, dispatcharr.AutomaticResult{}, true, 1000)
			for _, candidate := range got.Candidates {
				if candidate.ChannelID == g.ChannelID && supplementalLiteral(candidate.StreamName) == supplementalLiteral(g.StreamName) {
					t.Fatalf("replacement offered again: %+v", candidate)
				}
			}
		})
	}
}

func TestSupplementalImpactDoesNotInventAmbiguousReplacement(t *testing.T) {
	svc, draft, d, inventory := repairFixture(t, "101")
	before, _ := json.Marshal(builder.ExportFromDraft(draft))
	repairSave(t, svc, d)
	svc.ProjectSupplementalDecisions(draft, "source", "conn")
	inventory[0].ID += 100
	inventory[0].Fingerprint = "replacement-a"
	other := inventory[0]
	other.ID++
	other.AccountID++
	other.Fingerprint = "replacement-b"
	inventory = append(inventory, other)
	result := repairImpact(t, before, draft, inventory)
	rows := compareSupplementalImpact(draft, inventory, svc.SupplementalDecisions("source"), nil, result, "conn")
	if len(rows) != 1 || len(rows[0].Extra) != 0 || len(rows[0].SameNameConflicts) == 0 {
		t.Fatalf("ambiguous replacement assigned arbitrarily: %+v", rows)
	}
}

func TestReplacementStillMissedWhenActualConsumerRejectsName(t *testing.T) {
	svc, draft, d, inventory := repairFixture(t, "101")
	before, _ := json.Marshal(builder.ExportFromDraft(draft))
	repairSave(t, svc, d)
	// An independently suppressed positive alias is not restored merely because
	// the old confirmation has a current same-name physical replacement.
	if err := svc.SetAliasSuppressed("source", d.ChannelID, d.StreamName, true); err != nil {
		t.Fatal(err)
	}
	svc.ProjectSupplementalDecisions(draft, "source", "conn")
	inventory[0].ID += 1000
	inventory[0].Fingerprint = "replacement"
	result := repairImpact(t, before, draft, inventory)
	for _, match := range result.Proposed {
		for _, name := range match.Names {
			if name == d.StreamName {
				t.Fatal("fixture consumer unexpectedly matched the suppressed name")
			}
		}
	}
	rows := compareSupplementalImpact(draft, inventory, svc.SupplementalDecisions("source"), nil, result, "conn")
	if len(rows) != 1 || len(rows[0].Missed) != 1 || rows[0].Missed[0].Name != d.StreamName {
		t.Fatalf("actual consumer miss hidden by name carryover: %+v", rows)
	}
}
