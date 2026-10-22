package lineuparr

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestCaseVariantRepresentationsHaveDeterministicOrder(t *testing.T) {
	// Case-sensitive consumer representations must not inherit Go map iteration
	// order when their folded identities compare equal.
	if !aliasLess("THEGRIO", "TheGrio") || aliasLess("TheGrio", "THEGRIO") {
		t.Fatal("case variants need a deterministic tie breaker")
	}
	for _, values := range [][]string{{"TheGrio", "THEGRIO", "22", "METV PLUS"}, {"22", "THEGRIO", "METV PLUS", "TheGrio"}} {
		sort.SliceStable(values, func(i, j int) bool { return aliasLess(values[i], values[j]) })
		if !reflect.DeepEqual(values, []string{"METV PLUS", "THEGRIO", "TheGrio", "22"}) {
			t.Fatalf("unexpected alias order: %v", values)
		}
	}
	var first DraftChannel
	for attempt := 0; attempt < 20; attempt++ {
		c := newChannelWork(InputChannel{Key: "s", StationID: "s", CallSign: "CALL", ExternalAliases: []AttributedAlias{{Value: "TheGrio", Source: "fixture", Method: "exact row"}}})
		finalizeChannel(c)
		if attempt == 0 {
			first = c.draft
		} else if !reflect.DeepEqual(c.draft.Aliases, first.Aliases) || !reflect.DeepEqual(c.draft.AliasEvidence, first.AliasEvidence) {
			t.Fatalf("finalized aliases or evidence changed order: %+v", c.draft)
		}
	}
}

func TestAdmittedRepresentationPreservesPlusAndCase(t *testing.T) {
	c := newChannelWork(InputChannel{Key: "s", StationID: "s", CallSign: "CALL", Affiliate: "METV+", ExternalAliases: []AttributedAlias{{Value: "TheGrio", Source: "fixture", Method: "exact row"}}})
	finalizeChannel(c)
	if !hasAlias(c.draft.Aliases, "METV PLUS") || !hasAlias(c.draft.Aliases, "THEGRIO") {
		t.Fatalf("missing representation %+v", c.draft.Aliases)
	}
	if hasAlias(c.draft.Aliases, "METV") {
		t.Fatal("lost plus identity")
	}
}
func TestCompoundDescriptorRequiresTypedStationProof(t *testing.T) {
	for _, bound := range []bool{false, true} {
		p := ServiceIdentityEvidence{Value: "Family Brand RetroPlex", SourceID: "fixture", RootStationID: "s", SourceRowID: "row", SourceRevision: "rev"}
		if bound {
			p.BindingKind = "station-id-catalog-v1"
		}
		c := newChannelWork(InputChannel{Key: "s", StationID: "s", CallSign: "RETROHD", ExternalAliases: []AttributedAlias{{Value: "RETRO", Source: "schedule"}, {Value: p.Value, Source: p.SourceID, Identity: &p}}})
		finalizeChannel(c)
		if hasAlias(c.draft.Aliases, "RETROPLEX") != bound || hasAlias(c.draft.Aliases, "RETRO") == bound {
			t.Fatalf("proof=%v aliases=%+v", bound, c.draft.Aliases)
		}
	}
}
func hasAlias(values []string, want string) bool {
	for _, v := range values {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}

func TestRepresentationsRespectSavedSuppression(t *testing.T) {
	c := newChannelWork(InputChannel{Key: "s", CallSign: "CALL", Affiliate: "METV+", ExternalAliases: []AttributedAlias{{Value: "TheGrio", Source: "fixture"}}})
	finalizeChannel(c)
	applyAliasSuppressions(&c.draft, []string{"METV+", "TheGrio"})
	if hasAlias(c.draft.Aliases, "METV PLUS") || hasAlias(c.draft.Aliases, "THEGRIO") {
		t.Fatal("derived alias bypassed saved suppression", c.draft.Aliases)
	}
}

func TestConflictingCompoundDescriptorsStayUnresolved(t *testing.T) {
	c := newChannelWork(InputChannel{Key: "s", StationID: "s", CallSign: "RETROHD", ExternalAliases: []AttributedAlias{{Value: "Retro", Source: "schedule"}}})
	for _, name := range []string{"Brand RetroPlex", "Other RetroCinema"} {
		c.draft.ServiceEvidence = append(c.draft.ServiceEvidence, ServiceIdentityEvidence{Value: name, SourceID: "fixture", RootStationID: "s", SourceRowID: name, SourceRevision: "rev", BindingKind: "station-id-catalog-v1"})
	}
	finalizeChannel(c)
	if hasAlias(c.draft.Aliases, "RETROPLEX") || hasAlias(c.draft.Aliases, "RETROCINEMA") || !hasAlias(c.draft.Aliases, "RETRO") {
		t.Fatal("conflicting descriptors resolved", c.draft.Aliases)
	}
}
