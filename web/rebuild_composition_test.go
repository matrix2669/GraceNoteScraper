package web

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// Setup cancellation must take precedence over dev's cached-grid fallback.
func TestRebuiltCancelledGridDoesNotReturnCachedData(t *testing.T) {
	useTempGridCache(t)
	prefs := testPreferences()
	at := time.Now().Unix()
	if err := saveGridCache(at, prefs.Source(), &GridResponse{}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &Client{Client: &http.Client{}, pref: prefs}
	grid, err := client.GetDataByTimeContext(ctx, at)
	if grid != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("grid=%v err=%v", grid, err)
	}
}
