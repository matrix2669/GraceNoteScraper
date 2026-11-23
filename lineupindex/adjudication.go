package lineupindex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

const EvidenceAdjudicationPolicy = "evidence-assignment-adjudication-v1"

// EvidenceAssignment identifies an assertion's original binding. Neither the
// copied value, destination, revision nor parser method can change this identity.
type EvidenceAssignment struct {
	SourceID      string `json:"sourceId"`
	SourceRowID   string `json:"sourceRowId"`
	RootLineupKey string `json:"rootLineupKey"`
	RootStationID string `json:"rootStationId"`
}
type EvidenceAssignmentContext struct {
	Country    string `json:"country"`
	PostalCode string `json:"postalCode"`
	LineupID   string `json:"lineupId"`
	Device     string `json:"device"`
	HeadendID  string `json:"headendId"`
	Language   string `json:"language"`
}
type AdjudicatedAssertion struct {
	Kind     string `json:"kind"`
	Value    string `json:"value"`
	RawValue string `json:"rawValue,omitempty"`
	Method   string `json:"method"`
}
type EvidenceAdjudication struct {
	Version             int                    `json:"version"`
	ID                  string                 `json:"id"`
	Disposition         string                 `json:"disposition"`
	Reason              string                 `json:"reason"`
	Authority           string                 `json:"authority"`
	Date                string                 `json:"date"`
	Assignment          EvidenceAssignment     `json:"assignment"`
	OriginalSourceLabel string                 `json:"originalSourceLabel"`
	OriginalSourceURL   string                 `json:"originalSourceUrl"`
	OriginalRevision    string                 `json:"originalRevision"`
	OriginalAssertions  []AdjudicatedAssertion `json:"originalAssertions"`
	// The commitment avoids seeding private postal/configuration data. Audit
	// projections expose the complete observed context and explicitly verify it.
	// Encoding v1: SHA256(JSON([country,postal,lineup,device,headend,language])),
	// exact retained strings, no case folding or inferred/default fields.
	ContextCommitment string                     `json:"contextCommitment"`
	ContextEncoding   string                     `json:"contextEncoding"`
	ObservedContext   *EvidenceAssignmentContext `json:"observedContext,omitempty"`
	ContextStatus     string                     `json:"contextStatus"`
}

// Minimal reviewed policy data, independent of mutable index records. Original
// values are audit payload only: no name or channel-number predicate uses them.
var reviewedAssignmentAdjudications = [...]EvidenceAdjudication{{
	Version: 1, ID: "provider-row-assignment-20261023-01", Disposition: "rejected",
	Reason:    "user-rejected unsupported provider-row assignment",
	Authority: "explicit operator adjudication", Date: "2026-10-23",
	Assignment:          EvidenceAssignment{SourceID: "verizon-fios-official-lineup", SourceRowID: "row-536ecc21b57b8f85c22dd4a2", RootLineupKey: "USA-NY67791-DEFAULT", RootStationID: "66077"},
	OriginalSourceLabel: "Verizon FiOS official lineup", OriginalSourceURL: "https://www.verizon.com/home/fios-tv/channel-lineup/", OriginalRevision: "catalog-0c26f3106b0f38b9c2db8cb4",
	OriginalAssertions: []AdjudicatedAssertion{
		{Kind: FactAlias, Value: "Estrella National TV", Method: "exact FiOS channel number from the public national lineup PDF; unique provider-local channel number; number-policy-provider-alias-v3; provider-source-alignment-v1"},
		{Kind: "categoryRelation", Value: "Local & Public", RawValue: "Local channels", Method: "exact FiOS channel number from the public national lineup PDF; unique provider-local channel number; number-policy-provider-alias-v3; provider-source-alignment-v1; provider category \"Local channels\" mapped by exact category alias"},
	},
	ContextCommitment: "092166e2504e5208b176d10099ad01e7f42691d3bb90c687a30d309e7c649d2f", ContextEncoding: "exact-lineup-context-json-array-sha256-v1",
}}

func assignmentForFact(stationID string, f StationFact) EvidenceAssignment {
	root := strings.TrimSpace(f.RootSourceID)
	derived := root != "" && root != strings.TrimSpace(f.SourceID) || strings.HasPrefix(f.SourceID, weekdayEPGSourcePrefix)
	if !derived {
		root = strings.TrimSpace(f.SourceID)
	}
	station := strings.TrimSpace(f.RootSourceStationID)
	if !derived && station == "" {
		station = strings.TrimSpace(stationID)
	}
	return EvidenceAssignment{root, strings.TrimSpace(f.SourceRowID), strings.TrimSpace(f.RootSourceLineupKey), station}
}
func assignmentForRelation(r ProviderCategoryRelation) EvidenceAssignment {
	root, scope, station := strings.TrimSpace(r.RootSourceID), strings.TrimSpace(r.RootSourceLineupKey), strings.TrimSpace(r.RootSourceStationID)
	if root == "" || root == strings.TrimSpace(r.SourceID) {
		root = strings.TrimSpace(r.SourceID)
		if scope == "" {
			scope = strings.TrimSpace(r.SourceLineupKey)
		}
		if station == "" {
			station = strings.TrimSpace(r.StationID)
		}
	}
	return EvidenceAssignment{root, strings.TrimSpace(r.SourceRowID), scope, station}
}
func rejectedAssignment(binding EvidenceAssignment) *EvidenceAdjudication {
	// Full tuple equality only. Missing lineage is not guessed from text or row.
	// Context mutation/absence cannot rehabilitate this already adjudicated tuple.
	for i := range reviewedAssignmentAdjudications {
		a := &reviewedAssignmentAdjudications[i]
		if a.Disposition == "rejected" && a.Assignment == binding {
			return a
		}
	}
	return nil
}
func rejectedFact(stationID string, f StationFact) *EvidenceAdjudication {
	return rejectedAssignment(assignmentForFact(stationID, f))
}
func rejectedRelation(r ProviderCategoryRelation) *EvidenceAdjudication {
	return rejectedAssignment(assignmentForRelation(r))
}
func adjudicationAudit(idx *Index) []EvidenceAdjudication {
	data, _ := json.Marshal(reviewedAssignmentAdjudications[:])
	var out []EvidenceAdjudication
	_ = json.Unmarshal(data, &out)
	for i := range out {
		a := &out[i]
		a.ContextStatus = "unavailable"
		if l := idx.Lineups[a.Assignment.RootLineupKey]; l != nil {
			c := EvidenceAssignmentContext{l.Country, l.PostalCode, l.LineupID, l.Device, l.HeadendID, l.Language}
			a.ObservedContext = &c
			data, _ := json.Marshal([]string{c.Country, c.PostalCode, c.LineupID, c.Device, c.HeadendID, c.Language})
			sum := sha256.Sum256(data)
			a.ContextStatus = "mismatched-or-incomplete"
			if hex.EncodeToString(sum[:]) == a.ContextCommitment {
				a.ContextStatus = "verified"
			}
		}
	}
	return out
}
