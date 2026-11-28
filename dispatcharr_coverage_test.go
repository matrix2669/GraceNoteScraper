package main

import (
	"context"
	"errors"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"testing"
	"time"
)

func TestCoverageIsSnapshotBoundAndAdvisory(t *testing.T) {
	s := &dispatcharrServer{}
	streams := []dispatcharr.Stream{{ID: 1, Name: "One", M3UAccountID: 1}, {ID: 2, Name: "Two", M3UAccountID: 2}}
	snapshot := dispatcharrMatchSnapshot{dispatcharrFingerprint: "connection", fetchedAt: time.Now(), inventoryDigest: streamInventoryDigest(streams), candidates: []dispatcharr.Candidate{{ChannelID: "one"}}}
	channels := []dispatcharr.MatchChannel{{ID: "one", Name: "One"}, {ID: "two", Name: "Two"}}
	c := s.reviewCoverage(channels, snapshot, true)
	if c.Status != "snapshot-only" || len(c.Unmatched) != 1 || c.Unmatched[0].ID != "two" {
		t.Fatal(c)
	}
	s.cache.fingerprint = "connection"
	s.cache.fetchedAt = snapshot.fetchedAt.Add(time.Minute)
	s.cache.streams = append([]dispatcharr.Stream(nil), streams...)
	s.cache.streams[1].Name = "Renamed" // same count still changed
	if s.reviewCoverage(channels, snapshot, true).Status != "inventory-changed" {
		t.Fatal("missed same-count drift")
	}
	if s.reviewCoverage(channels, snapshot, false).Status != "refresh-required" {
		t.Fatal("stale coverage")
	}
	s.cache.fingerprint = "different"
	if s.inventoryDrift(snapshot) {
		t.Fatal("other connection affected snapshot")
	}
}
func TestKnownInventoryDriftFencesExportLease(t *testing.T) {
	s, _ := automaticServer(t)
	draft := automaticDraft(t, s)
	s.snapshot.inventoryDigest = streamInventoryDigest([]dispatcharr.Stream{{ID: 1, Name: "Old"}})
	draft = automaticDraft(t, s)
	s.cache.fingerprint = s.snapshot.dispatcharrFingerprint
	s.cache.fetchedAt = s.snapshot.fetchedAt.Add(time.Minute)
	s.cache.streams = []dispatcharr.Stream{{ID: 1, Name: "New"}}
	if s.inventoryDrift(s.snapshot) == false {
		t.Fatal("fixture drift missing")
	}
	if _, err := s.commitAutomaticDraft(draft, func() error { t.Fatal("stale export wrote"); return nil }); err == nil {
		t.Fatal("stale lease accepted")
	}
	if review := automaticReview(t, s); review.ReviewStatus != "stale" || review.Coverage.Status != "inventory-changed" {
		t.Fatal("drift completed workflow", review.ReviewStatus, review.Coverage)
	}
	if automaticDraft(t, s).AutomaticMatchingStatus != "refresh-required" {
		t.Fatal("old derived rules remained active")
	}
	// Known drift is advisory for a newly requested export, but cannot authorize
	// an old draft or carry its stale derived exclusions into a fresh one.
	fresh := automaticDraft(t, s)
	wrote := false
	if _, err := s.commitAutomaticDraft(fresh, func() error { wrote = true; return nil }); err != nil || !wrote {
		t.Fatalf("known drift blocked fresh advisory generation: %v", err)
	}
	s.cache.mu.Lock()
	s.cache.streams[0].Name = "Changed again"
	s.cache.mu.Unlock()
	if _, err := s.commitAutomaticDraft(fresh, func() error { t.Fatal("inventory changed mid-export"); return nil }); err == nil {
		t.Fatal("new drift did not fence old export lease")
	}
}

func TestUnsupportedConfirmationsAreFlaggedWithoutOverwriting(t *testing.T) {
	history := []dispatcharrReviewDecision{{Key: "old", Decision: "confirmed", ChannelID: "one", StreamNames: []string{"US: Service East", "US| Service East"}}, {Key: "other", Decision: "denied", ChannelID: "two", StreamNames: []string{"Denied"}}}
	annotateUnsupportedConfirmations(history, []dispatcharr.Candidate{{ChannelID: "one", StreamName: "US: Service East"}})
	if !history[0].NeedsReview || history[0].Decision != "confirmed" || history[0].Key != "old" || history[1].NeedsReview {
		t.Fatal(history)
	}
}

func TestSuccessfulFetchThenMatcherFailureRetainsObservedDrift(t *testing.T) {
	s, api := automaticServer(t)
	original := append([]dispatcharr.Stream(nil), api.streams...)
	s.snapshot.inventoryDigest = streamInventoryDigest(original)
	api.streams = append([]dispatcharr.Stream(nil), original...)
	api.streams[0].Name = "Renamed inventory service"
	s.matcher = func(context.Context, string, string, []dispatcharr.MatchChannel, []dispatcharr.Stream, map[string]dispatcharr.Decision) (dispatcharr.CandidateSet, string, error) {
		return dispatcharr.CandidateSet{}, "", errors.New("isolated matcher failure")
	}
	review := snapshotReview(t, s, true)
	if review.ReviewStatus != "stale" || review.Coverage.Status != "inventory-changed" || review.Automatic.Status != "refresh-required" {
		t.Fatal("failed refresh hid observed drift", review.ReviewStatus, review.Coverage, review.Automatic.Status)
	}
	fresh := automaticDraft(t, s)
	if _, err := s.commitAutomaticDraft(fresh, func() error { return nil }); err != nil {
		t.Fatalf("matcher failure blocked fresh advisory generation: %v", err)
	}
}
