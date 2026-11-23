package dispatcharr

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const SupplementalRevision = "supplemental-retrieval-v1"

type SupplementalLabel struct{ Value, Evidence string }
type SupplementalTarget struct {
	ID, StationID, PlacementID, Number, Name, CallSign, Country string
	Labels                                                      []SupplementalLabel
	Feed                                                        FeedIdentity
}
type SupplementalCandidate struct {
	Target   SupplementalTarget
	Stream   IdentityStream
	Rank     int
	Reason   string
	Evidence string
}

// Normalization is retrieval-only. Raw names/primary matcher inputs are untouched.
// Numbers inside service brands and subchannel markers are never discarded.
var supplementalPrefix = regexp.MustCompile(`(?i)^(?:(?:US|USA|TV|RK|TUBI|PRIME)\s*[:|]\s*|[0-9]{2,5}(?:\.[0-9]+)?\s+)`)
var supplementalQuality = regexp.MustCompile(`(?i)\s+(HD|SD|FHD|UHD|RAW|4K|8K|60FPS)\s*$`)
var supplementalSubchannel = regexp.MustCompile(`(?i)^([KW][A-Z]{2,3})-?DT[0-9]+$`)
var supplementalEvent = regexp.MustCompile(`(?i)(?:24\s*/\s*7|\bPPV\b|\b(?:VS|VERSUS)\b|\bEVENT\s*\d)`)

func supplementalText(value string) string {
	value = streamQualityText(value)
	value = supplementalPrefix.ReplaceAllString(strings.TrimSpace(value), "")
	value = strings.TrimSpace(strings.TrimRight(value, " ("))
	for {
		n := supplementalQuality.ReplaceAllString(value, "")
		if n == value {
			break
		}
		value = n
	}
	value = strings.NewReplacer("á", "a", "Á", "a", "é", "e", "É", "e", "í", "i", "Í", "i", "ó", "o", "Ó", "o", "ú", "u", "Ú", "u", "ü", "u", "Ü", "u", "ñ", "n", "Ñ", "n", "&", " and ").Replace(value)
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '+' {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	value = strings.Join(strings.Fields(b.String()), " ")
	value = strings.ReplaceAll(value, "news12", "news 12")
	return value
}
func supplementalFamily(value string) string {
	value = supplementalText(value)
	for _, s := range []string{" east", " west", " pacific", " east coast", " west coast"} {
		value = strings.TrimSuffix(value, s)
	}
	return strings.TrimSpace(value)
}
func supplementalContains(full, term string) bool {
	return strings.Contains(" "+full+" ", " "+term+" ")
}

// RetrieveSupplemental is prepared once per generation. A complete service label
// or attributed abbreviation anchors each proposal; arbitrary token overlap does
// not. Rank is a retrieval ordering tier, never a confidence or primary score.
func RetrieveSupplemental(targets []SupplementalTarget, streams []IdentityStream, primary []Candidate) ([]SupplementalCandidate, bool) {
	scored := map[string]bool{}
	for _, c := range primary {
		scored[c.ChannelID+"\x00"+c.StreamHash] = true
	}
	type indexed struct {
		stream  IdentityStream
		text    string
		spanish bool
		event   bool
	}
	index := make([]indexed, 0, len(streams))
	for _, s := range streams {
		index = append(index, indexed{s, supplementalText(s.Name), strings.Contains(strings.ToLower(s.Name), "cinemáx"), supplementalEvent.MatchString(s.Name)})
	}
	out := []SupplementalCandidate{}
	for _, t := range targets {
		type termLabel struct {
			term, evidence string
			spanish        bool
		}
		labels := []termLabel{}
		for _, label := range t.Labels {
			term := supplementalText(label.Value)
			hasLetter := false
			for _, r := range term {
				hasLetter = hasLetter || unicode.IsLetter(r)
			}
			if len(term) >= 3 && hasLetter {
				labels = append(labels, termLabel{term, label.Evidence, strings.Contains(strings.ToLower(label.Value), "cinemáx")})
			}
		}
		for _, item := range index {
			s, text := item.stream, item.text
			if scored[t.ID+"\x00"+s.Fingerprint] || text == "" || !inventoryCountryCompatible(s.Name, t.Country) || item.event {
				continue
			}
			rank, reason, evidence := 0, "", ""
			for _, label := range labels {
				term := label.term
				if item.spanish != label.spanish {
					continue
				}
				// A bare News12 name deliberately retrieves all captured regions. Plus is
				// shown distinctly for explicit review and never described as an alias.
				if term == "news 12" && strings.HasPrefix(text, "news 12 ") || term == "news 12" && text == term {
					rank, reason, evidence = 3, "News 12 family; choose the local service explicitly", label.evidence
					if strings.Contains(text, "plus") {
						reason = "News 12 Plus is a distinct service; review separately"
					}
					continue
				}
				current := 0
				why := ""
				if text == term {
					current, why = 1, "complete attributed service label"
				} else if supplementalContains(text, term) {
					current, why = 2, "attributed service label with additional raw descriptors"
				} else if len(term) >= 8 && strings.HasPrefix(text, strings.TrimSuffix(term, "e")) && len(text)+1 == len(term) {
					current, why = 3, "truncated attributed service label; verify raw identity"
				}
				// Rewind may be broadcast as WPIX Rewind. Rewind's attributable base term
				// supplies the anchor; the affiliate token is retained and shown for review.
				if current == 0 && strings.HasSuffix(term, " tv") && len(term) > 5 && text == strings.ToLower(supplementalAffiliate(t.CallSign))+" "+strings.TrimSuffix(term, " tv") && supplementalAffiliate(t.CallSign) != "" {
					current, why = 3, "attributed network label without terminal TV; verify affiliate/subchannel"
				}
				if current > 0 && (rank == 0 || current < rank) {
					rank, reason, evidence = current, why, label.evidence
				}
			}
			if rank == 0 {
				continue
			}
			streamFeed := StreamFeed(s.Name, nil)
			if t.Feed.Region != "" && t.Feed.Region != "unknown" {
				if streamFeed.Region != "" && streamFeed.Region != t.Feed.Region {
					reason += "; explicit opposing feed"
				} else if streamFeed.Region == "" {
					reason += "; feed unresolved (numeric decoration is not identity)"
				}
			}
			out = append(out, SupplementalCandidate{Target: t, Stream: s, Rank: rank, Reason: reason, Evidence: evidence})
			if len(out) >= 250000 {
				return out, false
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Rank != b.Rank {
			return a.Rank < b.Rank
		}
		if a.Target.ID != b.Target.ID {
			return a.Target.ID < b.Target.ID
		}
		if a.Stream.Name != b.Stream.Name {
			return a.Stream.Name < b.Stream.Name
		}
		if a.Stream.AccountID != b.Stream.AccountID {
			return a.Stream.AccountID < b.Stream.AccountID
		}
		return a.Stream.ID < b.Stream.ID
	})
	return out, true
}

func supplementalAffiliate(raw string) string {
	if m := supplementalSubchannel.FindStringSubmatch(raw); len(m) > 0 {
		return affiliateIdentity(m[1])
	}
	return affiliateIdentity(raw)
}
