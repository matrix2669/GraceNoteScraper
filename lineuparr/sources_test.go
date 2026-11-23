package lineuparr

import (
	"strings"
	"testing"
)

func TestDefaultCatalogURLsSelectProviderBeforeCombined(t *testing.T) {
	urls := DefaultCatalogURLs("USA", "Verizon Fios - Digital")
	if len(urls) != 2 || !strings.Contains(urls[0], "Verizon-FIOS-All-11743") || !strings.Contains(urls[1], "US_Combined") {
		t.Fatalf("Verizon defaults = %v", urls)
	}
	urls = DefaultCatalogURLs("USA", "Spectrum")
	if len(urls) != 1 || !strings.Contains(urls[0], "US_Combined") {
		t.Fatalf("Spectrum defaults = %v", urls)
	}
}

func TestPublicSourceURLRemovesCredentialsAndQuery(t *testing.T) {
	got := publicSourceURL("https://user:secret@example.test/catalog.json?token=private#section")
	if got != "https://example.test" {
		t.Fatalf("publicSourceURL() = %q", got)
	}
	got = publicSourceURL(catalogBaseURL + "US_Combined_lineup.json?cache=refresh")
	if got != catalogBaseURL+"US_Combined_lineup.json" {
		t.Fatalf("known public catalog URL = %q", got)
	}
}
