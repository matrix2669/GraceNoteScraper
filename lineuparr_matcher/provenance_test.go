package lineuparrmatcher

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestCanonicalSourceHashes(t *testing.T) {
	for name, expected := range map[string]string{
		"fuzzy_matcher.py": "04c66ac8156f5cbdd7a8c43a9ea0b8ae83ef1a89888d15da5e6a5ae3fa39ee91",
		"matching_core.py": "33ca1c61dad02d16a9362dab4e4f3b1edb3347db0a377eae592b44812449231d",
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
