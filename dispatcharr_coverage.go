package main

import (
	"crypto/sha256"
	"fmt"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"sort"
	"strings"
	"time"
)

type dispatcharrCoverageChannel struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Number string `json:"number"`
}
type dispatcharrCoverage struct {
	Status      string                       `json:"status"`
	Message     string                       `json:"message"`
	TargetCount int                          `json:"targetCount"`
	Unmatched   []dispatcharrCoverageChannel `json:"unmatched"`
}

func streamInventoryDigest(streams []dispatcharr.Stream) string {
	keys := make([]string, len(streams))
	for i, s := range streams {
		keys[i] = s.Fingerprint() + "\x00" + s.GroupName
		if s.ChannelGroupID == nil {
			keys[i] += "\x00missing"
		} else {
			keys[i] += fmt.Sprintf("\x00group:%d", *s.ChannelGroupID)
		}
	}
	sort.Strings(keys)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(keys, "\x00"))))
}
func (s *dispatcharrServer) reviewCoverage(channels []dispatcharr.MatchChannel, snapshot dispatcharrMatchSnapshot, valid bool) dispatcharrCoverage {
	c := dispatcharrCoverage{Status: "refresh-required", Message: "Refresh streams to establish coverage. No proposals does not mean every channel has a stream.", TargetCount: len(channels), Unmatched: []dispatcharrCoverageChannel{}}
	if !valid {
		return c
	}
	c.Status = "snapshot-only"
	c.Message = "Coverage describes only the last explicit Refresh streams, before decisions and exclusions. New, renamed or removed Dispatcharr streams are not checked automatically; refresh and regenerate JSON after inventory changes. Zero proposals means review is complete, not that every channel has a match."
	matched := map[string]bool{}
	for _, pair := range snapshot.candidates {
		matched[pair.ChannelID] = true
	}
	for _, channel := range channels {
		if !matched[channel.ID] {
			c.Unmatched = append(c.Unmatched, dispatcharrCoverageChannel{channel.ID, channel.Name, channel.Number})
		}
	}
	if s.inventoryDrift(snapshot) {
		c.Status = "inventory-changed"
		c.Message = "A later fetched inventory differs from this review. Refresh streams and regenerate JSON. Saved decisions remain unchanged."
	}

	return c
}

func (s *dispatcharrServer) inventoryDrift(snapshot dispatcharrMatchSnapshot) bool {
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()
	return s.cache.fingerprint == snapshot.dispatcharrFingerprint && s.cache.fetchedAt.After(snapshot.fetchedAt) && snapshot.inventoryDigest != "" && s.cache.inventoryDigestLocked() != snapshot.inventoryDigest
}

func (c *dispatcharrStreamCache) observationTokenLocked() string {
	return fmt.Sprintf("%s\x00%s\x00%s", c.fingerprint, c.fetchedAt.UTC().Format(time.RFC3339Nano), c.inventoryDigestLocked())
}

func (s *dispatcharrServer) inventoryObservationToken() string {
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()
	return s.cache.observationTokenLocked()
}

// Absence from a new complete candidate snapshot is a review warning, never
// an instruction to reverse a saved choice or invent a negative constraint.
func annotateUnsupportedConfirmations(history []dispatcharrReviewDecision, candidates []dispatcharr.Candidate, inventory []dispatcharr.IdentityStream) {
	supported := map[string]bool{}
	key := func(channel, raw string) string {
		return channel + "\x00" + strings.ToLower(strings.Join(strings.Fields(raw), " "))
	}
	for _, c := range candidates {
		supported[key(c.ChannelID, c.StreamName)] = true
	}
	present := map[string]bool{}
	for _, stream := range inventory {
		if literal := supplementalLiteral(stream.Name); literal != "" {
			present[literal] = true
		}
	}
	for i := range history {
		h := &history[i]
		if h.Decision != "confirmed" {
			continue
		}
		missing, unscored := false, false
		for _, raw := range h.StreamNames {
			if !present[supplementalLiteral(raw)] {
				missing = true
			} else if !supported[key(h.ChannelID, raw)] {
				unscored = true
			}
		}
		if missing {
			h.NeedsReview = true
			h.ReviewWarning = "Saved confirmation preserved, but one or more literal names are absent from this inventory. Inspect the names and Undo if inappropriate."
		} else if unscored {
			h.NeedsReview = true
			h.ReviewWarning = "Saved confirmation preserved. Its literal name is present, but no scored candidate exists for this target in this snapshot."
		}
	}
}

func (c *dispatcharrStreamCache) inventoryDigestLocked() string {
	if c.digest == "" {
		c.digest = streamInventoryDigest(c.streams)
	}
	return c.digest
}
