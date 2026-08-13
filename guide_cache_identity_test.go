package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/web"
)

func setGuideSourceEnv(t *testing.T, lineup string) web.GuideSource {
	t.Helper()
	t.Setenv("GN_COUNTRY", "USA")
	t.Setenv("GN_ZIPCODE", "11743")
	t.Setenv("GN_HEADEND", "NY67791")
	t.Setenv("GN_LINEUP", lineup)
	t.Setenv("GN_DEVICE", "X")
	t.Setenv("GN_LANGUAGE", "en")
	return web.CurrentGuideSource()
}

func TestGuideCachePersistsAndValidatesSource(t *testing.T) {
	expected := setGuideSourceEnv(t, "NY67791-X")
	original := guideCache{SavedAt: time.Date(2026, 7, 23, 15, 0, 0, 0, time.UTC)}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal guide cache: %v", err)
	}

	var wire struct {
		Source web.GuideSource `json:"source"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("inspect guide cache JSON: %v", err)
	}
	if wire.Source != expected {
		t.Fatalf("saved source = %#v, want %#v", wire.Source, expected)
	}

	var restored guideCache
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("reload matching guide cache: %v", err)
	}

	setGuideSourceEnv(t, "OTHER-LINEUP")
	if err := json.Unmarshal(data, &restored); err == nil || !strings.Contains(err.Error(), "source mismatch") {
		t.Fatalf("expected source mismatch error, got %v", err)
	}
}

func TestGuideCacheRejectsLegacyCacheWithoutSource(t *testing.T) {
	setGuideSourceEnv(t, "NY67791-X")
	legacy := []byte(`{"saved_at":"2026-07-23T15:00:00Z","guide":{"Channels":[],"Programs":[]}}`)

	var restored guideCache
	if err := json.Unmarshal(legacy, &restored); err == nil || !strings.Contains(err.Error(), "no Gracenote source fingerprint") {
		t.Fatalf("expected missing fingerprint error, got %v", err)
	}
}
