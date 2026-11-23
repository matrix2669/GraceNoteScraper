package lineuparr

import "time"

// CategoryReview retains the first automatic proposal, not a repeatedly
// rewritten baseline. Its fields are assigned by the server, never the client.
type CategoryReview struct {
	Proposed   string    `json:"proposed"`
	Source     string    `json:"source"`
	Method     string    `json:"method"`
	Priority   int       `json:"priority"`
	Chosen     string    `json:"chosen"`
	ReviewedAt time.Time `json:"reviewedAt"`
}

func ReviewCategory(channel DraftChannel, chosen string) *CategoryReview {
	if channel.CategoryReview != nil {
		copy := *channel.CategoryReview
		copy.Chosen = chosen
		copy.ReviewedAt = time.Now().UTC()
		return &copy
	}
	if !channel.NeedsCategoryReview {
		return nil
	}
	return &CategoryReview{Proposed: channel.Category, Source: channel.CategorySource, Method: channel.CategoryMethod, Priority: channel.CategoryPriority, Chosen: chosen, ReviewedAt: time.Now().UTC()}
}
