package dispatcharr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	lineuparrmatcher "github.com/daniel-widrick/GraceNoteScraper/lineuparr_matcher"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type lineuparrMatcherResult struct {
	ComparisonNames map[string]string `json:"comparisonNames"`
	Matches         []struct {
		ComparisonName string `json:"comparisonName,omitempty"`
		ExactAlias     string `json:"exactAlias,omitempty"`
		ChannelID      string `json:"channelId"`
		StreamKey      string `json:"streamKey"`
		Score          int    `json:"score"`
		Reason         string `json:"reason"`
	} `json:"matches"`
	MatcherVersion string `json:"matcherVersion"`
}
type matcherChannel struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Number  string   `json:"number"`
	Aliases []string `json:"aliases"`
}
type matcherStream struct {
	Key        string `json:"key"`
	Name       string `json:"name"`
	AccountID  int64  `json:"accountId"`
	GroupID    *int64 `json:"groupId"`
	GroupKnown bool   `json:"groupKnown"`
	GroupName  string `json:"groupName"`
}

// MatchStreamCandidatesWithLineuparr executes the pinned consumer once at 70.
// Score and NameScore both retain its returned score, without a Go score boost.
func MatchStreamCandidatesWithLineuparr(ctx context.Context, source, country string, channels []MatchChannel, streams []Stream, decisions map[string]Decision) (CandidateSet, string, error) {
	request := struct {
		Country  string           `json:"country,omitempty"`
		Channels []matcherChannel `json:"channels"`
		Streams  []matcherStream  `json:"streams"`
	}{Country: country}
	byChannel := make(map[string]MatchChannel)
	byStream := make(map[string]Stream)
	for _, c := range channels {
		if _, exists := byChannel[c.ID]; exists {
			return CandidateSet{}, "", errors.New("duplicate channel ID")
		}
		byChannel[c.ID] = c
		request.Channels = append(request.Channels, matcherChannel{c.ID, c.Name, c.Number, c.Aliases})
	}
	for _, s := range streams {
		if old, exists := byStream[s.Key()]; exists {
			sameGroup := old.ChannelGroupID == nil && s.ChannelGroupID == nil || old.ChannelGroupID != nil && s.ChannelGroupID != nil && *old.ChannelGroupID == *s.ChannelGroupID
			if old.Fingerprint() != s.Fingerprint() || !sameGroup || old.GroupName != s.GroupName {
				return CandidateSet{}, "", errors.New("conflicting records for one stream occurrence")
			}
			continue
		}
		byStream[s.Key()] = s
		request.Streams = append(request.Streams, matcherStream{s.Key(), s.Name, s.M3UAccountID, s.ChannelGroupID, s.ChannelGroupID != nil, s.GroupName})
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return CandidateSet{}, "", err
	}
	directory, err := os.MkdirTemp("", "gracenote-matcher-")
	if err != nil {
		return CandidateSet{}, "", err
	}
	defer os.RemoveAll(directory)
	for _, name := range []string{"runner.py", "fuzzy_matcher.py", "provider_prefixes.py", "matching_core.py", "LICENSE"} {
		data, err := lineuparrmatcher.Files.ReadFile(name)
		if err != nil {
			return CandidateSet{}, "", err
		}
		if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			return CandidateSet{}, "", err
		}
	}
	python := os.Getenv("LINEUPARR_MATCHER_PYTHON")
	if python == "" {
		python = "python3"
	}
	matchCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	command := exec.CommandContext(matchCtx, python, "-I", "-B", filepath.Join(directory, "runner.py"))
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "SYSTEMROOT=" + os.Getenv("SYSTEMROOT"), "LANG=C.UTF-8"}
	command.Stdin = bytes.NewReader(payload)
	output := &limitedMatcherOutput{limit: 64 << 20}
	command.Stdout = output
	command.Stderr = &matcherProgressWriter{notify: matcherProgressCallback(ctx)}
	if err := command.Run(); err != nil {
		if matchCtx.Err() != nil {
			return CandidateSet{}, "", fmt.Errorf("matcher cancelled: %w", matchCtx.Err())
		}
		return CandidateSet{}, "", fmt.Errorf("Lineuparr matcher failed (Python 3 required): %w", err)
	}
	var result lineuparrMatcherResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		return CandidateSet{}, "", fmt.Errorf("decode matcher: %w", err)
	}
	if result.MatcherVersion != lineuparrmatcher.Revision {
		return CandidateSet{}, "", errors.New("unexpected matcher revision")
	}
	set := CandidateSet{ComparisonNames: result.ComparisonNames}
	for key, comparison := range result.ComparisonNames {
		stream, ok := byStream[key]
		if !ok || stream.ChannelGroupID == nil || *stream.ChannelGroupID <= 0 || stream.M3UAccountID <= 0 || comparison == "" || comparison == stream.Name || !strings.HasSuffix(strings.TrimSpace(stream.Name), comparison) {
			return CandidateSet{}, "", errors.New("invalid scoped comparison evidence")
		}
	}
	seen := make(map[string]bool)
	for _, match := range result.Matches {
		channel, okc := byChannel[match.ChannelID]
		stream, oks := byStream[match.StreamKey]
		if !okc || !oks || match.Score < 70 || match.Score > 100 || match.Reason == "" || (match.ComparisonName != "" && result.ComparisonNames[match.StreamKey] != match.ComparisonName) {
			return CandidateSet{}, "", errors.New("invalid matcher result")
		}
		hash := stream.Fingerprint()
		key := candidateKey(source, hash, channel.ID)
		if seen[key] {
			return CandidateSet{}, "", errors.New("duplicate matcher pair")
		}
		seen[key] = true
		known := false
		for _, id := range channel.EPGIDs {
			if strings.TrimSpace(id) != "" && strings.EqualFold(strings.TrimSpace(id), strings.TrimSpace(stream.TVGID)) {
				known = true
			}
		}
		set.All = append(set.All, Candidate{
			MatcherVersion: result.MatcherVersion, Key: key, ChannelID: channel.ID, ChannelNumber: channel.Number, ChannelName: channel.Name,
			StreamID: stream.ID, StreamKey: stream.Key(), StreamName: stream.Name, TVGID: stream.TVGID,
			M3UAccountID: stream.M3UAccountID, ChannelGroupID: stream.ChannelGroupID, StreamChannelNo: stream.StreamChannelNo,
			StreamHash: hash, Source: source, Score: match.Score, NameScore: match.Score,
			Reason: match.Reason, ComparisonName: match.ComparisonName, ExactAlias: match.ExactAlias, NormalizedAlias: NormalizeStreamAlias(stream), KnownEPGID: known,
		})
	}
	included := make(map[string]bool)
	for _, c := range channels {
		included[c.ID] = true
	}
	set.All = FilterSnapshotCandidates(set.All, included, decisions)
	selected := make(map[string]bool)
	for _, c := range set.All {
		if !selected[c.StreamKey] {
			set.Primary = append(set.Primary, c)
			selected[c.StreamKey] = true
		}
	}
	return set, result.MatcherVersion, nil
}

type limitedMatcherOutput struct {
	bytes.Buffer
	limit int
}

func (b *limitedMatcherOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("matcher output limit exceeded")
	}
	return b.Buffer.Write(p)
}
