package marketindex

import "testing"

func TestEmbeddedSeedsAreRankedAndUnique(t *testing.T) {
	catalog, err := LoadSeeds("")
	if err != nil {
		t.Fatalf("LoadSeeds() error = %v", err)
	}
	if len(catalog.Markets) != 100 {
		t.Fatalf("market count = %d, want 100", len(catalog.Markets))
	}
	if catalog.Markets[0].Rank != 1 || catalog.Markets[0].PostalCode != "10001" {
		t.Fatalf("first market = %+v", catalog.Markets[0])
	}
	if catalog.Markets[99].Rank != 100 || catalog.Markets[99].PostalCode != "46601" {
		t.Fatalf("last market = %+v", catalog.Markets[99])
	}
	if len(catalog.Digest) != 64 {
		t.Fatalf("digest length = %d, want 64", len(catalog.Digest))
	}
}
