package dispatcharr

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// IdentityStream is an immutable value projection of one successfully fetched
// stream. It contains no URLs, credentials, mutable pointers or positive score.
type IdentityStream struct {
	ComparisonName string // Optional captured scoped evidence, never a saved/raw name.
	GroupName      string
	ID             int64
	Name           string
	AccountID      int64
	GroupID        int64
	GroupKnown     bool
	Fingerprint    string
}

func ProjectIdentityInventory(streams []Stream) []IdentityStream {
	result := make([]IdentityStream, len(streams))
	for i, stream := range streams {
		result[i] = IdentityStream{GroupName: stream.GroupName, ID: stream.ID, Name: stream.Name, AccountID: stream.M3UAccountID, Fingerprint: stream.Fingerprint()}
		if stream.ChannelGroupID != nil {
			result[i].GroupID, result[i].GroupKnown = *stream.ChannelGroupID, true
		}
	}
	return result
}

// WithComparisonEvidence returns fresh inventory values bound to this successful
// matcher capture. Callers cannot mutate the original inventory or its identity.
func WithComparisonEvidence(streams []IdentityStream, names map[string]string) []IdentityStream {
	result := append([]IdentityStream(nil), streams...)
	for i := range result {
		result[i].ComparisonName = names[fmt.Sprintf("%d:%d", result[i].AccountID, result[i].ID)]
	}
	return result
}

func (s IdentityStream) comparisonNames() []string {
	if s.ComparisonName != "" && s.ComparisonName != s.Name {
		return []string{s.Name, s.ComparisonName}
	}
	return []string{s.Name}
}

type FeedTarget struct {
	ID, Name, Number, Country string
	Feed                      FeedIdentity
}

// InventoryFeedExclusion is identity evidence, never a scored Candidate or a
// saved Deny. Unknown evidence cannot create a row or an unscored review item.
type InventoryFeedExclusion struct {
	Key, ChannelID, ChannelName, ChannelNumber, StreamName string
	AccountID                                              int64
	Rule, Reason, ManualDecision                           string
	Conflict                                               bool
}

var inventoryCountryPrefix = regexp.MustCompile(`(?i)^(US|USA|CA|CAN|UK|GB|AU|AUS)\s*[:|]\s*`)

func inventoryCountryCompatible(raw, country string) bool {
	m := inventoryCountryPrefix.FindStringSubmatch(strings.TrimSpace(raw))
	if len(m) == 0 {
		return true // Unmarked names still need an exact complete service family.
	}
	canonical := func(value string) string {
		switch strings.ToUpper(value) {
		case "USA", "US":
			return "US"
		case "CAN", "CA":
			return "CA"
		case "UK", "GB":
			return "GB"
		case "AUS", "AU":
			return "AU"
		}
		return strings.ToUpper(value)
	}
	return canonical(m[1]) == canonical(country)
}

// DeriveInventoryFeedExclusions compares independently attributed feed identity
// over the SAME successful inventory supplied to the matcher. It does not call
// the matcher, normalize positive aliases, invent scores or widen unknown holds.
// Rows already represented by scored automatic evidence retain that exact path.
func DeriveInventoryFeedExclusions(source string, streams []IdentityStream, targets []FeedTarget, codes map[string]FeedIdentity, decisions []Decision, scored []AutomaticExclusion) []InventoryFeedExclusion {
	key := func(channel, name string, account int64) string {
		return fmt.Sprintf("%s\x00%s\x00%d", channel, literalReviewName(name), account)
	}
	covered := map[string]bool{}
	for _, row := range scored {
		c := row.Candidate
		covered[key(c.ChannelID, c.StreamName, c.M3UAccountID)] = true
	}
	type parsedStream struct {
		stream IdentityStream
		name   string
		feed   FeedIdentity
	}
	byFamily := map[string][]parsedStream{}
	for _, stream := range streams {
		for _, comparison := range stream.comparisonNames() {
			feed := StreamFeed(comparison, codes)
			if feed.Family != "" && (feed.Region == "east" || feed.Region == "west") {
				byFamily[feed.Family] = append(byFamily[feed.Family], parsedStream{stream, comparison, feed})
			}
			// Index the prospective family for efficiency, but do not admit it until
			// StreamFeedForTarget proves membership against the current target below.
			if internal := internalFeedLabel(comparison, feed); internal.Region != "" && internal.Family != feed.Family {
				byFamily[internal.Family] = append(byFamily[internal.Family], parsedStream{stream, comparison, internal})
			}
		}
	}
	rows := map[string]InventoryFeedExclusion{}
	for _, target := range targets {
		if target.Feed.Family == "" || (target.Feed.Region != "east" && target.Feed.Region != "west") {
			continue
		}
		families := append([]string{target.Feed.Family}, target.Feed.FamilyAliases...)
		for _, family := range families {
			for _, parsed := range byFamily[family] {
				stream := parsed.stream
				id := key(target.ID, stream.Name, stream.AccountID)
				feed := StreamFeedForTarget(parsed.name, codes, target.Feed)
				if covered[id] || feed.Region == "" || !sameFeedFamily(feed.Family, target.Feed) || feed.Region == target.Feed.Region || !inventoryCountryCompatible(stream.Name, target.Country) {
					continue
				}
				decision := explicitStreamDecision(target.ID, source, stream.Fingerprint, stream.Name, decisions)
				reason := "Inventory identity: explicit " + feed.Region + " feed conflicts with attributed " + target.Feed.Region + " target (" + target.Feed.Evidence + "); no match score assigned"
				if decision == "confirmed" {
					reason = "Saved confirmation preserved; review conflict: " + reason
				} else if decision == "denied" {
					reason += "; saved denial preserved separately; automatic identity constraint remains"
				}
				row := InventoryFeedExclusion{Key: fmt.Sprintf("%x", sha256.Sum256([]byte(source+"\x00inventory-feed\x00"+id))), ChannelID: target.ID, ChannelName: target.Name, ChannelNumber: target.Number, StreamName: stream.Name, AccountID: stream.AccountID, Rule: "inventory-opposing-feed-v1", Reason: reason, ManualDecision: decision, Conflict: decision == "confirmed"}
				// Equivalent raw names/account rows have identical export semantics.
				// Keep a deterministic spelling and never lose a confirmed override.
				old, exists := rows[id]
				if !exists || (row.Conflict && !old.Conflict) || (row.Conflict == old.Conflict && row.StreamName < old.StreamName) {
					rows[id] = row
				}
			}
		}
	}
	result := make([]InventoryFeedExclusion, 0, len(rows))
	confirmedNames := map[string]bool{}
	for _, row := range scored {
		if row.Conflict {
			confirmedNames[row.Candidate.ChannelID+"\x00"+literalReviewName(row.Candidate.StreamName)] = true
		}
	}
	for _, row := range rows {
		if row.Conflict {
			confirmedNames[row.ChannelID+"\x00"+literalReviewName(row.StreamName)] = true
		}
	}
	for _, row := range rows {
		// The JSON contract cannot exclude one account's identical literal name
		// while allowing another's. Preserve even a legacy fingerprint-only
		// confirmation instead of reintroducing its name from another account.
		if !row.Conflict && confirmedNames[row.ChannelID+"\x00"+literalReviewName(row.StreamName)] {
			row.Conflict, row.ManualDecision = true, "confirmed"
			row.Reason = "Saved confirmation of this literal name preserved across accounts; review conflict: " + row.Reason
		}
		result = append(result, row)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}
