package lineupindex

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"
)

const ProviderIdentityPolicy = "provider-identity-v1"
const EvidenceQuarantinePolicy = "provider-evidence-quarantine-v2"

type QuarantinedEvidence struct {
	AdjudicationID string              `json:"adjudicationId,omitempty"`
	Assignment     *EvidenceAssignment `json:"assignment,omitempty"`
	StationID      string              `json:"stationId"`
	SourceID       string              `json:"sourceId"`
	SourceRowID    string              `json:"sourceRowId,omitempty"`
	Kind           string              `json:"kind"`
	Reason         string              `json:"reason"`
}
type EvidenceAudit struct {
	Adjudications []EvidenceAdjudication `json:"adjudications"`
	Policy        string                 `json:"policy"`
	Facts         []QuarantinedEvidence  `json:"facts"`
}

// AuditIndexFile is a dry run: it neither migrates the schema nor opens clients,
// writes files, or reads private configuration or saved builder decisions.
func AuditIndexFile(path string) (EvidenceAudit, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return EvidenceAudit{}, e
	}
	var idx Index
	if e = json.Unmarshal(b, &idx); e != nil {
		return EvidenceAudit{}, e
	}
	if idx.SchemaVersion < 1 || idx.SchemaVersion > CurrentIndexVersion {
		return EvidenceAudit{}, fmt.Errorf("unsupported evidence schema %d", idx.SchemaVersion)
	}
	return auditEvidence(&idx, false), nil
}

// ApplyEvidenceQuarantine annotates only unexported, nonserialized view fields.
// Retained records, schema, manual choices and successful refresh scopes remain
// intact. Rolling back the binary restores its former view without restoring data.
func ApplyEvidenceQuarantine(idx *Index) EvidenceAudit { return auditEvidence(idx, true) }

func rawServiceKey(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
	for _, suffix := range []string{"hd", "sd"} {
		if strings.HasSuffix(s, suffix) && len(s) > len(suffix) {
			return strings.TrimSuffix(s, suffix)
		}
	}
	return s
}
func stationRawKeys(s *Station) map[string]bool {
	out := map[string]bool{}
	if s == nil {
		return out
	}
	for _, n := range s.Names {
		if isCallSignKind(n.Kind) && !n.Conflict {
			out[rawServiceKey(n.Value)] = true
		}
	}
	return out
}
func sharesRawKeys(a, b map[string]bool) bool {
	for k := range a {
		if k != "" && b[k] {
			return true
		}
	}
	return false
}
func originScopeUnsafe(idx *Index, source string, keys []string, method string) string {
	if source == "xfinity-official-lineup" && strings.Contains(method, "number-policy-provider-alias-v3") {
		return "address response has no verified Gracenote headend binding"
	}
	if source == "verizon-fios-official-lineup" {
		for _, k := range keys {
			if l := idx.Lineups[k]; l != nil && strings.Contains(strings.ToLower(l.ProviderName), "frontier") {
				return "Verizon source applied to Frontier lineup"
			}
		}
	}
	return ""
}
func auditEvidence(idx *Index, apply bool) EvidenceAudit {
	report := EvidenceAudit{Adjudications: adjudicationAudit(idx), Policy: EvidenceQuarantinePolicy, Facts: []QuarantinedEvidence{}}
	type root struct {
		station, value, row, scope, source string
		names                              map[string]bool
	}
	bad := []root{}
	reasons := map[*StationFact]string{}
	for sid, st := range idx.Stations {
		for i := range st.Facts {
			f := &st.Facts[i]
			if apply {
				f.quarantineReason = ""
				f.viewPeerStationID = ""
			}
			keys := append([]string(nil), f.LineupKeys...)
			if f.RootSourceLineupKey != "" {
				keys = append(keys, f.RootSourceLineupKey)
			}
			why := originScopeUnsafe(idx, f.SourceID, keys, f.Method)
			if why == "" && f.RootSourceID != "" {
				why = originScopeUnsafe(idx, f.RootSourceID, keys, f.Method)
			}
			if why == "" && strings.HasPrefix(f.SourceID, weekdayEPGSourcePrefix) && f.RootSourceID == "xfinity-official-lineup" && f.EvidencePolicy != ProviderIdentityPolicy {
				verified := false
				if root := idx.Stations[f.RootSourceStationID]; root != nil {
					for _, rf := range root.Facts {
						if rf.SourceID == f.RootSourceID && rf.SourceRowID != "" && rf.SourceRowID == f.SourceRowID && strings.Contains(rf.Method, "identity-policy-v2") && !strings.Contains(rf.Method, "number-policy-provider-alias-v3") {
							verified = true
						}
					}
				}
				if !verified {
					why = "legacy Xfinity derived alias has no independently verified source row"
				}
			}
			if adjudication := rejectedFact(sid, *f); adjudication != nil {
				reasons[f] = adjudication.Reason
				continue // Never enter the older, deliberately broader byRow propagation.
			}
			if why != "" {
				reasons[f] = why
				scope := f.RootSourceLineupKey
				if scope == "" && len(keys) > 0 {
					scope = keys[0]
				}
				scopes := []string{scope}
				if f.RootSourceLineupKey == "" && len(keys) > 0 {
					scopes = keys
				}
				for _, unsafeScope := range scopes {
					bad = append(bad, root{sid, normalizeName(f.Value), f.SourceRowID, unsafeScope, f.SourceID, stationRawKeys(st)})
				}
			}
		}
	}
	byRow := map[string][]root{}
	byStation := map[string][]root{}
	byRaw := map[string][]root{}
	for _, r := range bad {
		if r.row != "" {
			byRow[r.row] = append(byRow[r.row], r)
		}
		byStation[r.station] = append(byStation[r.station], r)
		for key := range r.names {
			byRaw[key] = append(byRaw[key], r)
		}
	}
	for sid, st := range idx.Stations {
		names := stationRawKeys(st)
		for i := range st.Facts {
			f := &st.Facts[i]
			why := reasons[f]
			if why == "" && strings.HasPrefix(f.SourceID, weekdayEPGSourcePrefix) {
				relevant := append([]root(nil), byRow[f.SourceRowID]...)
				relevant = append(relevant, byStation[f.RootSourceStationID]...)
				if f.SourceRowID == "" && f.RootSourceStationID == "" {
					for key := range names {
						relevant = append(relevant, byRaw[key]...)
					}
				}
				for _, r := range relevant {
					rowMatch := r.row != "" && f.SourceRowID == r.row
					rootMatch := f.SourceRowID == "" && f.RootSourceStationID == r.station && f.RootSourceID == r.source && f.RootSourceLineupKey == r.scope && (f.Kind == FactCategory || normalizeName(f.Value) == r.value)
					// Older copies lost row/root metadata. Limit reevaluation to the same raw
					// service and scan scope, never invalidate a good alias merely by its text.
					legacy := f.SourceRowID == "" && f.RootSourceStationID == "" && sharesRawKeys(names, r.names) && (f.Kind == FactCategory || normalizeName(f.Value) == r.value)
					scopeMatch := false
					for _, k := range f.LineupKeys {
						if k == r.scope {
							scopeMatch = true
						}
					}
					if rowMatch || rootMatch || legacy && scopeMatch {
						why = "derived from quarantined provider assignment"
						break
					}
				}
			}
			if apply {
				f.quarantineReason = why
			}
			if why != "" {
				item := QuarantinedEvidence{StationID: sid, SourceID: f.SourceID, SourceRowID: f.SourceRowID, Kind: f.Kind, Reason: why}
				if a := rejectedFact(sid, *f); a != nil {
					binding := a.Assignment
					item.AdjudicationID = a.ID
					item.Assignment = &binding
				}
				report.Facts = append(report.Facts, item)
			}
		}
	}
	for i := range idx.CategoryRelations {
		r := &idx.CategoryRelations[i]
		keys := append([]string(nil), r.LineupKeys...)
		keys = append(keys, r.SourceLineupKey)
		why := originScopeUnsafe(idx, r.SourceID, keys, r.Method)
		adjudication := rejectedRelation(*r)
		if adjudication != nil {
			why = adjudication.Reason
		}
		if apply {
			r.quarantineReason = why
		}
		if why != "" {
			item := QuarantinedEvidence{StationID: r.StationID, SourceID: r.SourceID, SourceRowID: r.SourceRowID, Kind: "categoryRelation", Reason: why}
			if adjudication != nil {
				binding := adjudication.Assignment
				item.AdjudicationID = adjudication.ID
				item.Assignment = &binding
			}
			report.Facts = append(report.Facts, item)
		}
	}
	sort.Slice(report.Facts, func(i, j int) bool {
		a, b := report.Facts[i], report.Facts[j]
		return a.StationID+"\x00"+a.SourceID+"\x00"+a.SourceRowID+"\x00"+a.Kind+a.Reason < b.StationID+"\x00"+b.SourceID+"\x00"+b.SourceRowID+"\x00"+b.Kind+b.Reason
	})
	if apply {
		reevaluateLegacyPairs(idx)
	}
	return report
}

func identityViewPolicy(fact StationFact) string {
	if fact.EvidencePolicy != "" {
		return fact.EvidencePolicy
	}
	return EvidenceQuarantinePolicy + "-reevaluated"
}
func identityPeer(fact StationFact, stationID string) string {
	if fact.StationBound {
		return ""
	}
	if fact.viewPeerStationID != "" {
		return fact.viewPeerStationID
	}
	if fact.PeerStationID != "" {
		return fact.PeerStationID
	}
	if strings.HasPrefix(fact.SourceID, weekdayEPGSourcePrefix) && strings.Contains(fact.Method, "identity-policy-v2") && fact.RootSourceStationID != stationID {
		return fact.RootSourceStationID
	}
	return ""
}

// Old schedule copies did not persist pair IDs. Recover only reciprocal raw
// callsign transfers from the same confirmed scan, not arbitrary shared names.
func reevaluateLegacyPairs(idx *Index) {
	owners := map[string][]string{}
	for sid, st := range idx.Stations {
		for key := range stationRawKeys(st) {
			if key != "" {
				owners[key] = append(owners[key], sid)
			}
		}
	}
	for sid, st := range idx.Stations {
		keys := stationRawKeys(st)
		for i := range st.Facts {
			f := &st.Facts[i]
			if f.quarantineReason != "" || !strings.HasPrefix(f.SourceID, weekdayEPGSourcePrefix) || !strings.Contains(f.Method, "identity-policy-v2") || f.RootSourceStationID != "" || f.Kind != FactAlias {
				continue
			}
			peers := map[string]bool{}
			for _, peer := range owners[rawServiceKey(f.Value)] {
				if peer == sid {
					continue
				}
				for _, rf := range idx.Stations[peer].Facts {
					if rf.Kind == FactAlias && rf.quarantineReason == "" && rf.SourceID == f.SourceID && strings.Contains(rf.Method, "identity-policy-v2") && keys[rawServiceKey(rf.Value)] {
						peers[peer] = true
					}
				}
			}
			if len(peers) == 1 {
				for peer := range peers {
					f.viewPeerStationID = peer
				}
			}
		}
	}
}
