package dispatcharr

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// Stream is the deliberately limited subset of Dispatcharr stream metadata
// used for matching. Stream URLs, logos, tokens, and statistics are never
// represented here, which keeps them out of browser responses by construction.
type Stream struct {
	ID              int64    `json:"id"`
	Name            string   `json:"name"`
	TVGID           string   `json:"tvg_id,omitempty"`
	M3UAccountID    int64    `json:"m3u_account"`
	ChannelGroupID  *int64   `json:"channel_group,omitempty"`
	StreamChannelNo *float64 `json:"stream_chno,omitempty"`
}

func (s Stream) Key() string {
	return fmt.Sprintf("%d:%d", s.M3UAccountID, s.ID)
}

func (s Stream) Fingerprint() string {
	parts := []string{
		s.Key(), strings.TrimSpace(s.Name), strings.TrimSpace(s.TVGID),
	}
	if s.StreamChannelNo != nil {
		parts = append(parts, strconv.FormatFloat(*s.StreamChannelNo, 'f', -1, 64))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

type MatchChannel struct {
	ID       string
	Number   string
	Name     string
	Category string
	Aliases  []string
	EPGIDs   []string
}

type Decision struct {
	Key             string
	Decision        string
	Source          string
	StreamHash      string
	ChannelID       string
	StreamName      string
	NormalizedAlias string
}

type Candidate struct {
	Key             string   `json:"key"`
	ChannelID       string   `json:"channelId"`
	ChannelNumber   string   `json:"channelNumber"`
	ChannelName     string   `json:"channelName"`
	StreamID        int64    `json:"streamId"`
	StreamKey       string   `json:"-"`
	StreamName      string   `json:"streamName"`
	TVGID           string   `json:"tvgId,omitempty"`
	M3UAccountID    int64    `json:"m3uAccountId"`
	ChannelGroupID  *int64   `json:"channelGroupId,omitempty"`
	StreamChannelNo *float64 `json:"streamChannelNumber,omitempty"`
	StreamHash      string   `json:"-"`
	Source          string   `json:"-"`
	Score           int      `json:"score"`
	Reason          string   `json:"reason"`
	NormalizedAlias string   `json:"-"`
	KnownEPGID      bool     `json:"-"`
}

// CandidateSet retains the current best proposal and every qualifying option
// from the same bounded matching pass. The browser receives only grouped
// summaries, while the server keeps All available for alternate review.
type CandidateSet struct {
	Primary []Candidate
	All     []Candidate
}

type TVGIDEvidence struct {
	ID            string   `json:"id"`
	Known         bool     `json:"known"`
	StreamNames   []string `json:"streamNames,omitempty"`
	M3UAccountIDs []int64  `json:"m3uAccountIds,omitempty"`
}

type CandidateGroup struct {
	Key             string           `json:"key"`
	ChannelID       string           `json:"channelId"`
	ChannelNumber   string           `json:"channelNumber"`
	ChannelName     string           `json:"channelName"`
	Alias           string           `json:"alias"`
	NormalizedAlias string           `json:"normalizedAlias"`
	StreamCount     int              `json:"streamCount"`
	StreamNames     []string         `json:"streamNames"`
	TVGIDs          []string         `json:"tvgIds,omitempty"`
	TVGIDEvidence   []TVGIDEvidence  `json:"tvgIdEvidence,omitempty"`
	M3UAccountIDs   []int64          `json:"m3uAccountIds"`
	MinimumScore    int              `json:"minimumScore"`
	MaximumScore    int              `json:"maximumScore"`
	Tier            string           `json:"tier"`
	Reasons         []string         `json:"reasons"`
	Alternatives    []CandidateGroup `json:"alternatives,omitempty"`
	CandidateKeys   []string         `json:"-"`
}
