package lineuparr

import (
	"errors"
	"time"
)

// UpdateReviewedCategories preserves the original proposal for every batch row
// and writes the whole batch once. Callers validate active-lineup membership.
func (s *Service) UpdateReviewedCategories(fingerprint string, channels []DraftChannel, category string) error {
	category = cleanCategory(category)
	if category == "" || fingerprint == "" {
		return errors.New("valid source and master category required")
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	s.store.ensureSourceLocked(fingerprint)
	previous := make(map[string]ChannelOverride, len(s.store.state.Channels))
	for id, value := range s.store.state.Channels {
		previous[id] = value
	}
	for _, channel := range channels {
		override := s.store.state.Channels[channel.ID]
		override.Category = category
		override.CategoryReview = ReviewCategory(channel, category)
		s.store.state.Channels[channel.ID] = override
	}
	if err := s.store.saveLocked(); err != nil {
		s.store.state.Channels = previous
		return err
	}
	return nil
}

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
