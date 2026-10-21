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
		switch {
		case pending[c.Key]:
			c.ReviewKind = "feed-review"
		case c.Score < 95:
			c.ReviewKind = "inclusion"
		default:
			c.ReviewKind = "qualified"
		}
	}
}
