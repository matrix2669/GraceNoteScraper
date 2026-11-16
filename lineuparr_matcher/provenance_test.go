package lineuparrmatcher

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestCanonicalSourceHashes(t *testing.T) {
	for name, expected := range map[string]string{
		"provider_prefixes.py": "078644e564413ce4bf85615e5833debe24bc3130bb77e49d211426b140a5f8b4",
		"fuzzy_matcher.py":     "0692ca7234762229712546dcd7203ac4392af3df6403de5ad6b637991f783b3e",
		"aliases.py":           "756d3bafc79a93ab2439a5a0b8d56f02528eb97f3cc7bedb2deddc7af45e52f8",
		"matching_core.py":     "33ca1c61dad02d16a9362dab4e4f3b1edb3347db0a377eae592b44812449231d",
	} {
		data, err := Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != expected {
			t.Fatalf("%s: %s", name, got)
		}
	}
}
