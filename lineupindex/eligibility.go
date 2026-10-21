package lineupindex

import (
	"encoding/hex"
	"strings"
	"time"
)

const ActiveEvidencePolicy = "station-evidence-eligibility-v2"

// EvidenceContext identifies the selected source, never a provider family or ZIP
// alone. Historical observations remain in the retained index, not this view.
type EvidenceContext struct {
	Country, PostalCode, LineupID, Device, Fingerprint, Headend, Language string
	RawCallSigns                                                          map[string][]string
}

// EvidenceView is an immutable projection for all active draft consumers. It has
// no scanner, persistence or network capabilities.
type EvidenceView struct {
	aliases    map[string][]AliasCandidate
	categories map[string]CategoryCandidate
	snapshot   Snapshot
}

func (v EvidenceView) AliasesForStations(ids []string) map[string][]AliasCandidate {
	out := map[string][]AliasCandidate{}
	for _, id := range ids {
		values := append([]AliasCandidate(nil), v.aliases[id]...)
		for i := range values {
			values[i].LineupKeys = append([]string(nil), values[i].LineupKeys...)
		}
		out[id] = values
	}
	return out
}
func (v EvidenceView) CategoriesForStations(ids []string) map[string]CategoryCandidate {
	out := map[string]CategoryCandidate{}
	for _, id := range ids {
		if c, ok := v.categories[id]; ok {
			c.SourceIDs = append([]string(nil), c.SourceIDs...)
			c.SourceLabels = append([]string(nil), c.SourceLabels...)
			c.Methods = append([]string(nil), c.Methods...)
			out[id] = c
		}
	}
	return out
}
func (v EvidenceView) Snapshot() Snapshot {
	out := v.snapshot
	if out.PostalScan != nil {
		p := *out.PostalScan
		p.Sources = append([]EvidenceSourceRecord(nil), p.Sources...)
		out.PostalScan = &p
	}
	return out
}

func currentEvidenceScope(idx *Index, key string, c EvidenceContext) bool {
	l := idx.Lineups[key]
	return c.Fingerprint != "" && c.LineupID != "" && l != nil && l.Country == c.Country && l.PostalCode == c.PostalCode && l.LineupID == c.LineupID && l.Device == c.Device && l.HeadendID == c.Headend && l.Language == c.Language
}
func officialOrigin(idx *Index, source, key string) bool {
	l := idx.Lineups[key]
	family := providerFamilyKey(source)
	return l != nil && strings.HasSuffix(source, "-official-lineup") && family != "" && family == providerFamilyKey(l.ProviderName) && originScopeUnsafe(idx, source, []string{key}, "") == ""
}
func rawStationIdentity(st *Station, value string) bool {
	if st == nil {
		return false
	}
	for _, n := range st.Names {
		if n.Kind == NameCallSign && !n.Conflict && normalizeEPGCallSign(n.Value) == normalizeEPGCallSign(value) {
			return true
		}
	}
	return false
}
func scopedFact(idx *Index, sid string, f StationFact, c EvidenceContext) bool {
	// Legacy keys are observations, not a trustworthy row binding. Require an
	// attributable original row and root, even in the selected lineup.
	return !strings.Contains(f.Method, "number-policy-provider-alias-v3") && usableFact(f) && !strings.HasPrefix(f.SourceID, weekdayEPGSourcePrefix) && f.SourceRowID != "" && f.RootSourceStationID == sid && currentEvidenceScope(idx, f.RootSourceLineupKey, c) && officialOrigin(idx, f.SourceID, f.RootSourceLineupKey) && (f.EvidencePolicy == ProviderIdentityPolicy || strings.Contains(f.Method, ProviderSourceAlignmentV1) || strings.Contains(f.Method, "identity-policy-v2"))
}
func reusableDirectFact(idx *Index, sid string, f StationFact) bool {
	if !usableFact(f) || strings.HasPrefix(f.SourceID, weekdayEPGSourcePrefix) || f.SourceRowID == "" {
		return false
	}
	// A typed exact-ID source is station-bound and does not assert availability,
	// positions, or a bridge to another station. Retained row/revision is required.
	if f.StationBound && f.SourceRevision != "" && f.SourceID == "spectrum-official-lineup" && (f.RootSourceStationID == "" || f.RootSourceStationID == sid) && strings.Contains(f.Method, "exact Gracenote station ID") {
		return true
	}
	if f.RootSourceStationID != sid || !officialOrigin(idx, f.SourceID, f.RootSourceLineupKey) || strings.Contains(f.Method, "number-policy-provider-alias-v3") {
		return false
	}
	// Re-evaluate historical exact identity using the independent raw GN name;
	// a policy/method label alone cannot establish the station association.
	if !strings.Contains(f.Method, "identity-policy-v2") {
		return false
	}
	st := idx.Stations[sid]
	for _, a := range st.Facts {
		if a.Kind == FactAlias && usableFact(a) && a.SourceID == f.SourceID && a.SourceRowID == f.SourceRowID && a.RootSourceLineupKey == f.RootSourceLineupKey && a.RootSourceStationID == sid && rawStationIdentity(st, a.Value) {
			return true
		}
	}
	return false
}

func (s *Service) EvidenceForLineup(c EvidenceContext) EvidenceView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	idx := s.index
	idx.Stations = make(map[string]*Station, len(s.index.Stations))
	idx.CategoryRelations = nil
	for sid, original := range s.index.Stations {
		st := *original
		st.Names = nil
		st.Facts = nil
		for _, value := range c.RawCallSigns[sid] {
			st.Names = append(st.Names, StationName{Kind: NameCallSign, Value: value, Normalized: normalizeName(value)})
		}
		for _, f := range original.Facts {
			proofIndex := s.index
			proofIndex.Stations = map[string]*Station{sid: &st}
			proofStation := *original
			proofStation.Names = st.Names
			proofIndex.Stations[sid] = &proofStation
			if reusablePairFact(sid, f, c.RawCallSigns) || reusableDirectFact(&proofIndex, sid, f) || scopedFact(&s.index, sid, f, c) {
				st.Facts = append(st.Facts, f)
			}
		}
		idx.Stations[sid] = &st
	}
	for _, r := range s.index.CategoryRelations {
		if r.quarantineReason != "" {
			continue
		}
		f := StationFact{Kind: FactCategory, SourceID: r.SourceID, SourceRowID: r.SourceRowID, SourceRevision: r.SourceRevision, StationBound: r.StationBound, RootSourceStationID: r.StationID, RootSourceLineupKey: r.SourceLineupKey, LineupKeys: r.LineupKeys, Method: r.Method, EvidencePolicy: r.EvidencePolicy}
		proofIndex := s.index
		proofIndex.Stations = map[string]*Station{}
		if original := s.index.Stations[r.StationID]; original != nil {
			ps := *original
			ps.Names = idx.Stations[r.StationID].Names
			proofIndex.Stations[r.StationID] = &ps
		}
		if reusableDirectFact(&proofIndex, r.StationID, f) || scopedFact(&s.index, r.StationID, f, c) {
			idx.CategoryRelations = append(idx.CategoryRelations, r)
		}
	}
	// The projection is materialized entirely under the original lock. Underlying
	// retained records and saved decisions are never annotated or rewritten.
	projection := &Service{index: idx, currentStations: func() map[string][]string { return c.RawCallSigns }}
	ids := make([]string, 0, len(c.RawCallSigns))
	for id := range c.RawCallSigns {
		ids = append(ids, id)
	}
	// Materialize every DTO while the original mutex still guards all shared
	// record maps. Nothing in the returned view references those mutable maps.
	aliases := projection.AliasesForStations(ids)
	for id, values := range aliases {
		for i := range values {
			if values[i].StationBound && values[i].SourceID == "spectrum-official-lineup" && values[i].SourceRevision != "" && values[i].SourceRowID != "" {
				values[i].BindingKind = "station-id-catalog-v1"
			}
		}
		aliases[id] = values
	}
	return EvidenceView{aliases: aliases, categories: projection.CategoriesForStations(ids), snapshot: projection.SnapshotForPostal(c.Country, c.PostalCode)}
}

func reusablePairFact(sid string, f StationFact, current map[string][]string) bool {
	p := f.PairProof
	if !usableFact(f) || f.Kind != FactAlias || p == nil || !strings.HasPrefix(f.SourceID, weekdayEPGSourcePrefix) || f.EvidencePolicy != ProviderIdentityPolicy || p.TargetStationID != sid || p.PeerStationID == "" || p.PeerStationID == sid || p.PeerStationID != f.PeerStationID || p.PeerCallSign != f.Value || len(p.WindowStarts) < 2 {
		return false
	}
	matches := func(values, proof []string) bool {
		for _, a := range values {
			for _, b := range proof {
				if normalizeName(a) == normalizeName(b) {
					return true
				}
			}
		}
		return false
	}
	if !matches(current[sid], p.TargetCallSigns) {
		return false
	}
	if values := current[p.PeerStationID]; len(values) > 0 && !matches(values, []string{p.PeerCallSign}) {
		return false
	}
	revision, err := hex.DecodeString(p.ProgrammeRevision)
	if err != nil || len(revision) != 32 {
		return false
	}
	zone, zoneErr := time.LoadLocation(p.Timezone)
	if zoneErr != nil || p.Timezone == "" {
		return false
	}
	for i := 1; i < len(p.WindowStarts); i++ {
		if p.WindowStarts[i]-p.WindowStarts[i-1] >= epgBlockHours*60*60 && time.Unix(p.WindowStarts[i], 0).In(zone).Format("2006-01-02") != time.Unix(p.WindowStarts[i-1], 0).In(zone).Format("2006-01-02") {
			return true
		}
	}
	return false
}
