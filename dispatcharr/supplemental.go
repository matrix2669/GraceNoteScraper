package dispatcharr

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const SupplementalRevision = "supplemental-retrieval-v3"

type SupplementalLabel struct{ Value, Evidence string }
type SupplementalTarget struct {
	ID, StationID, PlacementID, Number, Name, CallSign, Country string
	Labels                                                      []SupplementalLabel
	Feed                                                        FeedIdentity
}
type SupplementalCandidate struct {
	Target     SupplementalTarget
	Stream     IdentityStream
	Rank       int
	Reason     string
	Evidence   string
	Descriptor string
}

// Normalization is retrieval-only. Raw names/primary matcher inputs are untouched.
// Numbers inside service brands and subchannel markers are never discarded.
var supplementalPrefix = regexp.MustCompile(`(?i)^(?:(?:US|USA|TV|RK|TUBI|PRIME)\s*[:|]\s*|[0-9]{2,5}(?:\.[0-9]+)?\s+)`)
var supplementalQuality = regexp.MustCompile(`(?i)\s+(HD|SD|FHD|UHD|RAW|4K|8K|60FPS)\s*$`)
var supplementalSubchannel = regexp.MustCompile(`(?i)^([KW][A-Z]{2,3})-?DT[0-9]+$`)
var supplementalEvent = regexp.MustCompile(`(?i)(?:24\s*/\s*7|\bPPV\b|\b(?:VS|VERSUS)\b|\bEVENTS?\s*\d)`)
var supplementalHeader = regexp.MustCompile(`[#=*]{2,}|\|{3,}`)
var supplementalCountry = regexp.MustCompile(`(?i)^(US|USA|CA|CAN|UK|GB|AU|AUS|FR|NL|DE|ES|IT|PT|BR|MX)(?:\s*[:|]\s*|\s+)`)

// Country context is supplemental-specific; do not broaden the existing automatic
// inventory policy. A contradictory marked name or marked group is inadmissible.
func supplementalCountryCompatible(s IdentityStream, country string) bool {
	canonical := func(c string) string {
		switch strings.ToUpper(c) {
		case "USA":
			return "US"
		case "CAN":
			return "CA"
		case "UK":
			return "GB"
		case "AUS":
			return "AU"
		}
		return strings.ToUpper(c)
	}
	for _, raw := range []string{s.Name, s.GroupName} {
		if m := supplementalCountry.FindStringSubmatch(strings.TrimSpace(raw)); len(m) > 0 && canonical(m[1]) != canonical(country) {
			return false
		}
	}
	return true
}

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

// The explicit News12 review exception covers its named local services and Plus,
// not arbitrary words appended to a short network label.
func supplementalNews12Region(text string) bool {
	switch text {
	case "news 12", "news 12 plus", "news 12 new york", "news 12 new jersey", "news 12 long island", "news 12 connecticut", "news 12 brooklyn", "news 12 the bronx", "news 12 westchester", "news 12 hudson valley":
		return true
	}
	return false
}

// RetrieveSupplemental is prepared once per generation. A complete service label
// or attributed abbreviation anchors each proposal; arbitrary token overlap does
// not. Rank is a retrieval ordering tier, never a confidence or primary score.
func RetrieveSupplemental(targets []SupplementalTarget, streams []IdentityStream, primary []Candidate) ([]SupplementalCandidate, bool) {
	prefixes, complete := discoverSupplementalPrefixes(targets, streams)
	scored := map[string]bool{}
	for _, c := range primary {
		scored[c.ChannelID+"\x00"+c.StreamHash] = true
	}
	type indexed struct {
		stream         IdentityStream
		text           string
		spanish        bool
		event          bool
		feed, internal FeedIdentity
		prefix         string
		raw            string
		presentation   string
		plain          string
		explicitEast   bool
		mayWrap        bool
	}
	index := make([]indexed, 0, len(streams))
	for _, s := range streams {
		feed := StreamFeed(s.Name, nil)
		index = append(index, indexed{s, supplementalText(s.Name), strings.Contains(strings.ToLower(s.Name), "cinemáx"), supplementalEvent.MatchString(s.Name) || supplementalHeader.MatchString(s.Name), feed, internalFeedLabel(s.Name, feed), "", s.Name, supplementalPresentationKey(s.Name, ""), supplementalPresentationKeyWithEast(s.Name, "", false), ParseStationFeedLabel(s.Name).Region == "east", strings.Contains(s.Name, "(")})
		if p, ok := prefixes[s.Fingerprint]; ok {
			f := StreamFeed(p.residual, nil)
			index = append(index, indexed{s, supplementalText(p.residual), strings.Contains(strings.ToLower(p.residual), "cinemáx"), supplementalEvent.MatchString(s.Name) || supplementalHeader.MatchString(s.Name), f, internalFeedLabel(p.residual, f), p.label, p.residual, supplementalPresentationKey(p.residual, ""), supplementalPresentationKeyWithEast(p.residual, "", false), ParseStationFeedLabel(p.residual).Region == "east", strings.Contains(p.residual, "(")})
		}
	}
	out := []SupplementalCandidate{}
	seen := map[string]bool{}
	for _, t := range targets {
		type termLabel struct {
			term, value, evidence, presentation, plain string
			spanish                                    bool
			explicitEast                               bool
			feed                                       FeedIdentity
			scheduleReview                             bool
		}
		labels := []termLabel{}
		for _, label := range t.Labels {
			term := supplementalText(label.Value)
			hasLetter := false
			for _, r := range term {
				hasLetter = hasLetter || unicode.IsLetter(r)
			}
			if (len(term) >= 3 || supplementalShortLabelAllowed(label.Value)) && hasLetter {
				labels = append(labels, termLabel{term, label.Value, label.Evidence, supplementalPresentationKey(label.Value, t.CallSign), supplementalPresentationKeyWithEast(label.Value, t.CallSign, false), strings.Contains(strings.ToLower(label.Value), "cinemáx"), ParseStationFeedLabel(label.Value).Region == "east", StreamFeedForTarget(label.Value, nil, t.Feed), false})
			}
		}
		// A captured schedule comparison supports a review-only lead for this
		// exact station/callsign, not an asserted BBC station or feed identity.
		// It is not a primary alias and cannot enter the export without a choice.
		if t.Country == "US" && t.StationID == "89690" && strings.EqualFold(t.CallSign, "BBCNAHD") {
			const value = "BBC News"
			const evidence = "REVIEW LEAD ONLY: reported programme schedule overlap with station 89542 (provider-attributed BBC News), guide observed 2026-11-16; station/feed identity and overlap have not been independently verified"
			labels = append(labels, termLabel{supplementalText(value), value, evidence, supplementalPresentationKey(value, t.CallSign), supplementalPresentationKeyWithEast(value, t.CallSign, false), false, false, FeedIdentity{}, true})
		}
		for _, item := range index {
			s, text := item.stream, item.text
			if scored[t.ID+"\x00"+s.Fingerprint] || text == "" || !supplementalCountryCompatible(s, t.Country) || item.event {
				continue
			}
			streamFeed := targetFeed(item.feed, item.internal, t.Feed)
			rank, reason, evidence, descriptor := 0, "", "", ""
			streamKey, streamPlain := item.presentation, item.plain
			if item.mayWrap {
				streamKey = supplementalPresentationKey(item.raw, t.CallSign)
				streamPlain = supplementalPresentationKeyWithEast(item.raw, t.CallSign, false)
			}
			for _, label := range labels {
				if item.prefix != "" && strings.TrimSpace(label.evidence) == "" {
					continue
				}
				term := label.term
				if label.scheduleReview && text != "bbc news" {
					continue
				}
				if item.spanish != label.spanish {
					continue
				}
				// A bare News12 name deliberately retrieves all captured regions. Plus is
				// shown distinctly for explicit review and never described as an alias.
				if term == "news 12" && supplementalNews12Region(text) {
					rank, reason, evidence, descriptor = 3, "News 12 family; choose the local service explicitly", label.evidence, label.value
					if strings.Contains(text, "plus") {
						reason = "News 12 Plus is a distinct service; review separately"
					}
					continue
				}
				current := 0
				why := ""
				if len(term) >= 3 && text == term {
					current, why = 1, "complete attributed service label"
				} else if strings.TrimSpace(label.evidence) != "" && streamPlain != "" && (streamPlain == label.plain || ((t.Feed.Region == "east" || label.explicitEast || item.explicitEast) && streamKey == label.presentation)) {
					current, why = 1, "complete attributed service label (presentation equivalent)"
				} else if text == "showtime extrem" && term == "showtime extreme" {
					current, why = 3, "truncated attributed service label; verify raw identity"
				}
				// Feed presentation can differ only when the full residual family is
				// independently attributed to this target. Residual service tokens
				// (numbers, subbrands, events, geography) are never discarded.
				if current == 0 && streamFeed.Region != "" && sameFeedFamily(streamFeed.Family, t.Feed) && label.feed.Family == streamFeed.Family {
					current, why = 2, "complete attributed service family with explicit feed"
				}
				// Rewind may be broadcast as WPIX Rewind. Rewind's attributable base term
				// supplies the anchor; the affiliate token is retained and shown for review.
				if current == 0 && strings.HasSuffix(term, " tv") && len(term) > 5 && text == strings.ToLower(supplementalAffiliate(t.CallSign))+" "+strings.TrimSuffix(term, " tv") && supplementalAffiliate(t.CallSign) != "" {
					current, why = 3, "attributed network label without terminal TV; verify affiliate/subchannel"
				}
				if current > 0 && label.scheduleReview {
					current, why = 3, "schedule-corroborated review lead; station and feed identity unverified"
				}
				if current > 0 && (rank == 0 || current < rank) {
					rank, reason, evidence, descriptor = current, why, label.evidence, label.value
				}
			}
			if rank == 0 {
				continue
			}
			if item.prefix != "" {
				reason += "; " + prefixReason(item.prefix)
			}
			key := t.ID + "\x00" + s.Fingerprint
			if seen[key] {
				continue
			}
			seen[key] = true
			if t.Feed.Region == "unknown" {
				reason += "; target feed unresolved"
			} else if t.Feed.Region != "" {
				if streamFeed.Region != "" && streamFeed.Region != t.Feed.Region {
					reason += "; explicit opposing feed"
				} else if streamFeed.Region == "" {
					reason += "; feed unresolved (numeric decoration is not identity)"
				}
			}
			out = append(out, SupplementalCandidate{Target: t, Stream: s, Rank: rank, Reason: reason, Evidence: evidence, Descriptor: descriptor})
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
	return out, complete
}

func supplementalAffiliate(raw string) string {
	if m := supplementalSubchannel.FindStringSubmatch(raw); len(m) > 0 {
		return affiliateIdentity(m[1])
	}
	return affiliateIdentity(raw)
}
