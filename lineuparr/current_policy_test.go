package lineuparr

import (
	"github.com/daniel-widrick/GraceNoteScraper/lineupindex"
	"testing"
)

func TestCurrentQuarantinePolicyKeepsValidIdentityProof(t *testing.T) {
	c := DraftChannel{StationID: "synthetic"}
	p := ServiceIdentityEvidence{Value: "Synthetic Service", SourceID: "verizon-fios-official-lineup", SourceRowID: "valid-row", RootStationID: c.StationID, RootLineupKey: "valid-scope", Policy: lineupindex.EvidenceQuarantinePolicy + "-reevaluated"}
	if !safeIdentityProof(c, p) {
		t.Fatal("current eligible proof rejected by builder policy reader")
	}
	p.Policy = "provider-evidence-quarantine-v1-reevaluated"
	if safeIdentityProof(c, p) {
		t.Fatal("old quarantine view treated as current proof")
	}
	p.Policy = lineupindex.ProviderIdentityPolicy
	if !safeIdentityProof(c, p) {
		t.Fatal("valid provider identity policy changed")
	}
}
