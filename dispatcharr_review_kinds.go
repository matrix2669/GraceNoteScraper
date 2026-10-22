package main

import "github.com/daniel-widrick/GraceNoteScraper/dispatcharr"

// Presentation/decision scope only. Scores and matcher bytes are unchanged;
// automatic qualification never writes a synthetic saved confirmation.
func classifyReviewCandidates(candidates []dispatcharr.Candidate, automatic automaticMatchView) {
	pending := map[string]bool{}
	for _, row := range automatic.Rows {
		if row.PendingReview && !row.Conflict {
			pending[row.Key] = true
		}
	}
	for i := range candidates {
		c := &candidates[i]
		c.ReviewKind = reviewCandidateKind(c.Score, pending[c.Key])
	}
}

// Feed uncertainty does not erase the different positive/negative export rules
// on either side of 95. Keep both classes visible as distinct review decisions.
func reviewCandidateKind(score int, pendingFeed bool) string {
	kind := "inclusion"
	if score >= 95 {
		kind = "qualified"
	}
	if pendingFeed {
		return "feed-" + kind
	}
	return kind
}
