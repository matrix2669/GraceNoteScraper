package dispatcharr

import (
	"regexp"
	"strings"
	"unicode"
)

var supplementalCallsign = regexp.MustCompile(`(?i)^([KW][A-Z]{2,3})(?:-?DT([0-9]*))?$`)

var supplementalWrapper = regexp.MustCompile(`(?i)^(.*?)\s*\(([KW][A-Z]{2,3}(?:-?DT\d*)?)\)\s*$`)

// supplementalPresentationKey is retrieval-only, not a saved alias or a
// consumer normalization. It compares a whole attributed label with a whole
// stream label and never removes a subbrand or an opposing feed.
func supplementalPresentationKey(raw, callsign string) string {
	return supplementalPresentationKeyWithEast(raw, callsign, true)
}

func supplementalPresentationKeyWithEast(raw, callsign string, stripEast bool) string {
	raw = streamQualityText(raw)
	raw = supplementalPrefix.ReplaceAllString(strings.TrimSpace(raw), "")
	raw = feedQuality.ReplaceAllString(raw, "")
	if m := supplementalWrapper.FindStringSubmatch(raw); len(m) == 3 && supplementalWrapperMatches(m[2], callsign) {
		raw = strings.TrimSpace(m[1])
	}
	// East is the default presentation only when it is the sole feed token;
	// do not reinterpret mixed labels or regional words embedded in a brand.
	if stripEast {
		parsed := StreamFeed(raw, nil)
		if parsed.Region == "east" && parsed.Evidence == "explicit stream feed label" {
			if m := feedSuffix.FindStringSubmatch(raw); len(m) > 0 && strings.EqualFold(m[2], "east") {
				raw = strings.TrimSpace(m[1])
			}
		}
	}
	// E! is a distinct whole service label. Never equate it with plain E.
	if strings.EqualFold(strings.TrimSpace(raw), "E!") {
		return "special:e!"
	}
	raw = strings.NewReplacer("&", " and ", "á", "a", "Á", "a", "é", "e", "É", "e", "í", "i", "Í", "i", "ó", "o", "Ó", "o", "ú", "u", "Ú", "u", "ü", "u", "Ü", "u", "ñ", "n", "Ñ", "n").Replace(strings.ToLower(raw))
	var b strings.Builder
	for _, r := range raw {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '+' {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	words := strings.Fields(b.String())
	for i, word := range words {
		if word == "jr" {
			words[i] = "junior"
		}
	}
	for len(words) > 1 && (words[len(words)-1] == "tv" || words[len(words)-1] == "network") {
		words = words[:len(words)-1]
	}
	joined := strings.Join(words, "")
	// Common station presentations concatenate TV/Network (MeTV, UPTV).
	// The entire remaining service label must still match its attributed peer.
	for _, suffix := range []string{"network", "tv"} {
		if strings.HasSuffix(joined, suffix) && len(joined)-len(suffix) >= 2 {
			joined = strings.TrimSuffix(joined, suffix)
		}
	}
	return joined
}

func supplementalShortLabelAllowed(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "fx", "up", "e!":
		return true
	}
	return false
}

// A bare affiliate in a wrapper is compatible with its verified numbered
// subchannel. A numbered wrapper requires the same explicit target subchannel.
func supplementalWrapperMatches(wrapper, target string) bool {
	w := supplementalCallsign.FindStringSubmatch(strings.TrimSpace(wrapper))
	t := supplementalCallsign.FindStringSubmatch(strings.TrimSpace(target))
	if len(w) != 3 || len(t) != 3 || !strings.EqualFold(w[1], t[1]) {
		return false
	}
	return w[2] == "" || (t[2] != "" && w[2] == t[2])
}
