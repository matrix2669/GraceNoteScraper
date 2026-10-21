package dispatcharr

import "strings"

// AutomaticExclusion is a derived current-snapshot assertion. It is never a
// saved manual Deny, and is recomputed after every input or decision change.
type AutomaticExclusion struct {
	Candidate      Candidate
	Rule           string
	Reason         string
	PendingReview  bool
	ManualDecision string
	Conflict       bool
}
type AutomaticResult struct {
	Rows   []AutomaticExclusion
	Hidden map[string]bool
}

func explicitCandidateDecision(c Candidate, decisions []Decision) string {
	// Only a retained constituent is an explicit override. A lossy group alias
	// must not extend a confirmation to a newly seen opposing-feed raw name.
	// Existing explicit confirmations remain conflicts even after authentication
	// changes; the opaque prior fingerprint cannot prove a different endpoint.
	// This preserves choices without reusing prior automatic evidence.
	denied := false
	for _, d := range decisions {
		if d.ChannelID == c.ChannelID &&
			((d.StreamHash != "" && d.StreamHash == c.StreamHash) || (d.StreamName != "" && equalRawStream(d.StreamName, c.StreamName))) {
			if d.Decision == "confirmed" {
				return "confirmed"
			}
			if d.Decision == "denied" && d.Source == c.Source {
				denied = true
			}
		}
	}
	if denied {
		return "denied"
	}
	return ""
}

func DeriveAutomaticExclusions(candidates []Candidate, targets map[string]AffiliateTarget, feeds map[string]FeedIdentity, codes map[string]FeedIdentity, decisions []Decision) AutomaticResult {
	result := AutomaticResult{Hidden: map[string]bool{}}
	byKey := map[string]AutomaticExclusion{}
	affiliate := planAutomaticAffiliateExclusions(candidates, targets)
	for _, suggestion := range affiliate.Suggestions {
		c := suggestion.Candidate
		byKey[c.Key] = AutomaticExclusion{Candidate: c, Rule: "affiliate-callsign-v1", Reason: "Explicit stream callsign " + suggestion.StreamCallSign + " differs from current station " + suggestion.TargetCallSign}
	}
	for _, c := range candidates {
		target := feeds[c.ChannelID]
		if target.Region == "" {
			continue
		}
		stream := StreamFeed(c.StreamName, codes)
		row := AutomaticExclusion{Candidate: c, Rule: "feed-identity-v1"}
		if target.Region == "unknown" {
			row.PendingReview = true
			row.Reason = "Target feed identity unresolved; manual review required (" + target.Evidence + ")"
		} else if stream.Region != "" && stream.Family == target.Family && stream.Region != target.Region {
			row.Reason = "Explicit " + stream.Region + " feed conflicts with attributed " + target.Region + " target (" + target.Evidence + ")"
		} else if stream.Region == "" || stream.Family != target.Family {
			row.PendingReview = true
			row.Reason = "Feed identity unresolved; " + target.Region + " target requires explicit matching feed and service-family evidence (" + target.Evidence + ")"
		} else {
			continue
		}
		byKey[c.Key] = row
	}
	for _, c := range candidates {
		row, ok := byKey[c.Key]
		if !ok {
			continue
		}
		row.ManualDecision = explicitCandidateDecision(c, decisions)
		switch row.ManualDecision {
		case "confirmed":
			row.Conflict = true
			row.Reason = "Saved confirmation preserved; review conflict: " + row.Reason
		case "denied":
			row.Reason += "; saved denial preserved separately; automatic identity constraint remains"
		}
		// Automatic constraints use independently extracted identity, not the
		// confidence of the positive candidate. No manual Deny threshold or
		// sibling/EPG boost is promoted into identity evidence here.
		result.Rows = append(result.Rows, row)
	}
	// Existing manual review groups remain whole when any constituent is pending
	// or explicitly confirmed. Definite exclusions are still exported per name.
	manualGroups := map[string]bool{}
	for _, row := range result.Rows {
		if row.PendingReview || row.Conflict {
			manualGroups[row.Candidate.ChannelID+"\x00"+ReviewIdentity(row.Candidate.StreamName, row.Candidate.NormalizedAlias)] = true
		}
	}
	for _, row := range result.Rows {
		if !row.PendingReview && !row.Conflict && !manualGroups[row.Candidate.ChannelID+"\x00"+ReviewIdentity(row.Candidate.StreamName, row.Candidate.NormalizedAlias)] {
			result.Hidden[row.Candidate.Key] = true
		}
	}
	return result
}

func equalRawStream(a, b string) bool {
	return strings.EqualFold(strings.Join(strings.Fields(a), " "), strings.Join(strings.Fields(b), " "))
}
