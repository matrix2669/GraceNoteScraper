package dispatcharr

import (
	"context"
	"errors"
	lineuparrmatcher "github.com/daniel-widrick/GraceNoteScraper/lineuparr_matcher"
	"os/exec"
	"testing"
)

func requirePython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Fatal("Python 3 is required for matcher integration tests")
	}
}

func TestPythonMatcherEmptyAndDuplicateNames(t *testing.T) {
	requirePython(t)
	channels := []MatchChannel{{ID: "cnn", Name: "CNN", EPGIDs: []string{"CNN.us"}}}
	for _, input := range []struct {
		channels []MatchChannel
		streams  []Stream
	}{
		{}, {channels: channels}, {streams: []Stream{{ID: 1, Name: "CNN", M3UAccountID: 3}}},
	} {
		got, version, err := MatchStreamCandidatesWithLineuparr(t.Context(), "source", "US", input.channels, input.streams, nil)
		if err != nil || len(got.All) != 0 || version != lineuparrmatcher.Revision {
			t.Fatalf("%+v %q %v", got, version, err)
		}
	}
	streams := []Stream{{ID: 1, Name: "US CNN HD", TVGID: "CNN.us", M3UAccountID: 3}, {ID: 2, Name: "US CNN HD", M3UAccountID: 4}}
	got, _, err := MatchStreamCandidatesWithLineuparr(t.Context(), "source", "US", channels, streams, nil)
	if err != nil || len(got.All) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, c := range got.All {
		if c.Score != 100 || c.NameScore != 100 || c.MatcherVersion != lineuparrmatcher.Revision {
			t.Fatal(c)
		}
	}
}

func TestPythonMatcherRetainsReturnedScoresAndCountryFilters(t *testing.T) {
	requirePython(t)
	channels := []MatchChannel{
		{ID: "boost", Name: "National Geographic Documentary Television Channel 123", Number: "123"},
		{ID: "bbc", Name: "BBC One"},
		{ID: "disney", Name: "Disney+"},
	}
	streams := []Stream{
		{ID: 1, M3UAccountID: 1, Name: "National Geographic Documentary Television Channel Live 123"},
		{ID: 2, M3UAccountID: 1, Name: "UK| BBC 1 HD"},
		{ID: 3, M3UAccountID: 1, Name: "Disney HD"},
	}
	got, _, err := MatchStreamCandidatesWithLineuparr(t.Context(), "source", "", channels, streams, nil)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Candidate{}
	for _, c := range got.All {
		byID[c.ChannelID] = c
	}
	if byID["boost"].Score != 96 || byID["boost"].NameScore != 96 {
		t.Fatalf("returned consumer score changed: %+v", got)
	}
	if byID["bbc"].Score != 100 {
		t.Fatalf("number word: %+v", got)
	}
	if c, ok := byID["disney"]; ok && c.NameScore >= 95 {
		t.Fatal(c)
	}
	got, _, err = MatchStreamCandidatesWithLineuparr(t.Context(), "source", "US", channels, streams, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got.All {
		if c.ChannelID == "bbc" {
			t.Fatal("country filter ignored")
		}
	}
}

func TestPythonMatcherCancellationAndMissingRuntime(t *testing.T) {
	requirePython(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := MatchStreamCandidatesWithLineuparr(ctx, "source", "", nil, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	t.Setenv("LINEUPARR_MATCHER_PYTHON", "missing-gracenote-python")
	if _, _, err := MatchStreamCandidatesWithLineuparr(t.Context(), "source", "", nil, nil, nil); err == nil {
		t.Fatal("missing runtime succeeded")
	}
}

func TestMatcherOutputBound(t *testing.T) {
	b := &limitedMatcherOutput{limit: 3}
	if _, err := b.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("d")); err == nil {
		t.Fatal("unbounded output")
	}
}
