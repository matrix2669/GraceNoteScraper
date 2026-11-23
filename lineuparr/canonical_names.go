package lineuparr

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Proposals are never applied by Build. The caller must provide independently
// validated immutable evidence before a proposed name can enter consumer JSON.
func collectCanonicalNameProposals(result []DraftChannel, work []*channelWork, overrides map[string]ChannelOverride) []CanonicalNameProposal {
	byID := map[string]*channelWork{}
	for _, w := range work {
		byID[w.draft.ID] = w
	}
	owners := map[string][]string{}
	for _, c := range result {
		if c.Included {
			k := canonicalCollisionKey(c.Name)
			owners[k] = append(owners[k], c.ID)
		}
	}
	var proposals []CanonicalNameProposal
	for _, c := range result {
		w := byID[c.ID]
		if w == nil || w.input.PreferredName != nil || !c.Included {
			continue
		}
		options := map[string][]AttributedAlias{}
		for _, a := range w.input.CanonicalNameCandidates {
			p := a.Identity
			if p == nil || p.RootStationID == "" || p.RootStationID != c.StationID || p.SourceID != a.Source || p.SourceRowID == "" || p.SourceRevision == "" || !canonicalProofAllowed(p) || !containsFold(c.Aliases, a.Value) {
				continue
			}
			name := defaultEastName(a.Value)
			suppressed := false
			for _, v := range overrides[c.ID].SuppressedAliases {
				if suppressionEqual(v, a.Value) || suppressionEqual(v, name) {
					suppressed = true
					break
				}
			}
			if suppressed {
				continue
			}
			key := strings.ToLower(strings.Join(strings.Fields(a.Value), " "))
			if key != "" {
				options[key] = append(options[key], a)
			}
		}
		if len(options) != 1 {
			continue
		}
		for _, v := range options {
			sort.Slice(v, func(i, j int) bool {
				a, b := v[i], v[j]
				return a.Source+"\x00"+a.Identity.SourceRevision+"\x00"+a.Identity.SourceRowID+"\x00"+a.Value < b.Source+"\x00"+b.Identity.SourceRevision+"\x00"+b.Identity.SourceRowID+"\x00"+b.Value
			})
			a := v[0]
			name := defaultEastName(a.Value)
			if name == c.Name {
				continue
			}
			p := CanonicalNameProposal{TargetID: c.ID, BeforeName: c.Name, BeforeSource: c.NameSource, BeforeMethod: c.NameMethod, ProposedName: name, Descriptor: a.Value, Source: a.Source, Method: a.Method, Proof: *a.Identity, Status: "pending", Reason: "Awaiting current consumer validation"}
			// The consumer pools aliases by exact name across every category. A new
			// cross-target name collision cannot be validated as an isolated proposal.
			if len(owners[canonicalCollisionKey(name)]) > 0 || len(owners[canonicalCollisionKey(c.Name)]) > 1 {
				p.Status = "held"
				p.Reason = "Proposed name collides with an included target"
			}
			data, _ := json.Marshal([]any{p.TargetID, p.BeforeName, p.BeforeSource, p.BeforeMethod, p.ProposedName, p.Descriptor, p.Source, p.Method, p.Proof})
			p.Hash = fmt.Sprintf("%x", sha256.Sum256(data))
			proposals = append(proposals, p)
		}
	}
	// Also reject simultaneous proposals to the same conservative global key.
	counts := map[string]int{}
	for _, p := range proposals {
		counts[canonicalCollisionKey(p.ProposedName)]++
	}
	for i := range proposals {
		if counts[canonicalCollisionKey(proposals[i].ProposedName)] > 1 {
			proposals[i].Status = "held"
			proposals[i].Reason = "Proposed name collides with another proposal"
		}
	}
	sort.Slice(proposals, func(i, j int) bool { return proposals[i].TargetID < proposals[j].TargetID })
	return proposals
}
func canonicalCollisionKey(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}
func suppressionEqual(a, b string) bool {
	return strings.ToLower(cleanText(a)) == strings.ToLower(cleanText(b))
}
func canonicalProofAllowed(p *ServiceIdentityEvidence) bool {
	return (p.BindingKind == "station-id-catalog-v1" && p.Policy == "verified-portable-station-alias-v1") || (p.BindingKind == "user-attested-station-v1" && p.Policy == "user-attested-rename-v1")
}
func defaultEastName(name string) string {
	value := strings.TrimSpace(name)
	for _, suffix := range []string{" - East", " (East)"} {
		if len(value) > len(suffix) && strings.EqualFold(value[len(value)-len(suffix):], suffix) {
			return strings.TrimSpace(value[:len(value)-len(suffix)])
		}
	}
	return value
}

// ApplyCanonicalProposalsForValidation mutates only a caller-owned draft copy.
// It is intentionally not an admission decision and must never be applied to a
// served draft without a current, fenced consumer proof.
func ApplyCanonicalProposalsForValidation(draft *Draft, hashes map[string]bool) error {
	if draft == nil {
		return fmt.Errorf("nil draft")
	}
	byID := map[string]*DraftChannel{}
	for i := range draft.Channels {
		c := &draft.Channels[i]
		if byID[c.ID] != nil {
			return fmt.Errorf("duplicate target")
		}
		byID[c.ID] = c
	}
	selected := map[string]bool{}
	for _, p := range draft.CanonicalNameProposals {
		if !hashes[p.Hash] {
			continue
		}
		c := byID[p.TargetID]
		if selected[p.Hash] || c == nil || !c.Included || p.Status != "pending" || c.Name != p.BeforeName || c.NameSource != p.BeforeSource || c.NameMethod != p.BeforeMethod {
			return fmt.Errorf("stale canonical proposal")
		}
		c.Name = p.ProposedName
		c.NameSource = p.Source
		c.NameMethod = p.Method
		if p.Descriptor != p.ProposedName {
			c.NameMethod += "; default East feed annotation omitted"
		}
		selected[p.Hash] = true
	}
	for hash := range hashes {
		if !selected[hash] {
			return fmt.Errorf("unknown canonical proposal")
		}
	}
	return nil
}
