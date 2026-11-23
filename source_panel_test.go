package main

import (
	"os"
	"strings"
	"testing"
)

func TestSourcePanelStartsClosedWithoutAddressAutoExpansion(t *testing.T) {
	page, err := os.ReadFile("lineuparr.html")
	if err != nil {
		t.Fatal(err)
	}
	body := string(page)
	if strings.Contains(body, "els.sourcePanel.open = true") || !strings.Contains(body, "if (section.dataset.sectionKey === 'sources')") {
		t.Fatal("sources must stay closed by default, including address-gated providers")
	}
}
