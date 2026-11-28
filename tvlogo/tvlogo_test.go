package tvlogo

import (
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

type logoTestTransport func(*http.Request) (*http.Response, error)

func (f logoTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestResolveChannelLogoIdentity(t *testing.T) {
	const station = "/tv-logo/tv-logos/main/countries/united-states/"
	for _, tc := range []struct {
		name, call, affiliate, want string
		available                   []string
		notProbed                   []string
	}{
		{"confirmed MILH", "MILH", "", "military-history-us.png", []string{"military-history-us.png"}, nil},
		{"confirmed MILH with HD suffix", "MILHHD", "", "military-history-us.png", []string{"military-history-us.png"}, nil},
		{"numbered subchannel with affiliate", "WCBS-DT2", "Start TV Network", "start-tv-us.png", []string{"wcbs-us.png", "start-tv-us.png"}, []string{"wcbs-us.png"}},
		{"numbered subchannel with HD suffix", "WCBS-DT2-HD", "Start TV Network", "start-tv-us.png", []string{"wcbs-us.png", "start-tv-us.png"}, []string{"wcbs-us.png"}},
		{"space-delimited subchannel", "WCBS DT2", "Start TV Network", "start-tv-us.png", []string{"wcbs-us.png", "start-tv-us.png"}, []string{"wcbs-us.png"}},
		{"spaced hyphen before subchannel", "WCBS -DT2", "", "", []string{"wcbs-us.png"}, []string{"wcbs-us.png"}},
		{"three-letter station subchannel", "WJZ-DT2", "", "", []string{"wjz-us.png"}, []string{"wjz-us.png"}},
		{"subchannel followed by HD2", "WCBS-DT2-HD2", "", "", []string{"wcbs-us.png"}, []string{"wcbs-us.png"}},
		{"compact subchannel followed by HD4", "WCBSDT2HD4", "", "", []string{"wcbs-us.png"}, []string{"wcbs-us.png"}},
		{"subchannel followed by HD4", "WCBS DT2 HD4", "", "", []string{"wcbs-us.png"}, []string{"wcbs-us.png"}},
		{"numbered subchannel without affiliate", "WCBSDT2", "", "", []string{"wcbs-us.png"}, []string{"wcbs-us.png"}},
		{"full branded name before generic fallback", "CARTOON", "Cartoon Network", "cartoon-network-us.png", []string{"cartoon-network-us.png", "cartoon-us.png"}, []string{"cartoon-us.png"}},
		{"exact affiliate before Network fallback", "STRTV", "Start TV Network", "start-tv-network-us.png", []string{"start-tv-network-us.png", "start-tv-us.png"}, []string{"start-tv-us.png"}},
		{"primary station suffix still works", "WCBS-DT", "CBS", "us-local/cbs-2-wcbs-us.png", []string{"us-local/cbs-2-wcbs-us.png"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient("USA", filepath.Join(t.TempDir(), "logo-cache.json"))
			var lookups []string
			c.http.Transport = logoTestTransport(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodHead || req.URL.Host != "raw.githubusercontent.com" {
					t.Fatalf("unexpected request %s %s", req.Method, req.URL)
				}
				path := req.URL.Path
				lookups = append(lookups, path)
				status := http.StatusNotFound
				for _, present := range tc.available {
					if path == station+present {
						status = http.StatusOK
						break
					}
				}
				// Resolve is synchronous in this test: avoid real rate-limit waits.
				c.last = time.Time{}
				return &http.Response{StatusCode: status, Body: http.NoBody}, nil
			})
			want := ""
			if tc.want != "" {
				want = "https://raw.githubusercontent.com" + station + tc.want
			}
			if got := c.Resolve("station", tc.call, tc.affiliate, "2"); got != want {
				t.Fatalf("got %q, want %q; probes: %v", got, want, lookups)
			}
			for _, forbidden := range tc.notProbed {
				if slices.Contains(lookups, station+forbidden) {
					t.Errorf("probed unrelated logo: %s", forbidden)
				}
			}
		})
	}
}

func TestResolveRefreshesOlderAffectedMatchesWithoutFlushingCache(t *testing.T) {
	const prefix = baseRawURL + "/united-states/"
	c := NewClient("USA", filepath.Join(t.TempDir(), "logo-cache.json"))
	for id, logo := range map[string]string{
		"subchannel": "wcbs-us.png", "subchannel-hd": "wcbs-us.png", "three-letter": "wjz-us.png",
		"milh": "history-channel-us.png", "unrelated": "cbs-logo-white-us.png",
	} {
		c.cache.entries["channelId:"+id] = CacheEntry{LogoURL: prefix + logo, FetchedAt: time.Now().Unix(), MatcherVersion: 2}
	}
	var calls int
	c.http.Transport = logoTestTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		c.last = time.Time{}
		status := http.StatusNotFound
		if u := req.URL.String(); u == prefix+"start-tv-us.png" || u == prefix+"military-history-us.png" {
			status = http.StatusOK
		}
		return &http.Response{StatusCode: status, Body: http.NoBody}, nil
	})
	for _, tc := range []struct{ id, call, aff, want string }{
		{"subchannel", "WCBS DT2", "Start TV Network", "start-tv-us.png"},
		{"subchannel-hd", "WCBS -DT2-HD2", "Start TV Network", "start-tv-us.png"},
		{"three-letter", "WJZ-DT2", "Start TV Network", "start-tv-us.png"},
		{"milh", "MILH", "", "military-history-us.png"},
	} {
		if got := c.Resolve(tc.id, tc.call, tc.aff, "2"); got != prefix+tc.want {
			t.Fatalf("%s: got %q; want %q", tc.id, got, prefix+tc.want)
		}
	}
	before := calls
	for _, tc := range []struct{ id, call, aff, want string }{
		{"subchannel", "WCBS DT2", "Start TV Network", "start-tv-us.png"},
		{"subchannel-hd", "WCBS -DT2-HD2", "Start TV Network", "start-tv-us.png"},
		{"three-letter", "WJZ-DT2", "Start TV Network", "start-tv-us.png"},
		{"milh", "MILH", "", "military-history-us.png"},
		{"unrelated", "WCBS-DT", "CBS", "cbs-logo-white-us.png"},
	} {
		if got := c.Resolve(tc.id, tc.call, tc.aff, "2"); got != prefix+tc.want {
			t.Fatalf("%s: got cached %q; want %q", tc.id, got, prefix+tc.want)
		}
	}
	if calls != before {
		t.Errorf("cached resolutions made %d new requests", calls-before)
	}
}

func TestResolveTransientHEADDoesNotCacheMissingLogo(t *testing.T) {
	const prefix = baseRawURL + "/united-states/"
	for _, status := range []int{0, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		name := http.StatusText(status)
		if status == 0 {
			name = "Network failure"
		}
		t.Run(name, func(t *testing.T) {
			c := NewClient("USA", filepath.Join(t.TempDir(), "logo-cache.json"))
			c.cache.entries["channelId:station"] = CacheEntry{LogoURL: prefix + "wcbs-us.png", FetchedAt: time.Now().Unix(), MatcherVersion: 2}
			unavailable := true
			c.http.Transport = logoTestTransport(func(req *http.Request) (*http.Response, error) {
				c.last = time.Time{}
				if req.URL.String() == prefix+"wcbs-us.png" {
					t.Fatal("probed parent logo during subchannel repair")
				}
				if req.URL.String() != prefix+"start-tv-us.png" {
					return &http.Response{StatusCode: http.StatusGone, Body: http.NoBody}, nil
				}
				if unavailable {
					if status == 0 {
						return nil, errors.New("transient network error")
					}
					return &http.Response{StatusCode: status, Body: http.NoBody}, nil
				}
				return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
			})
			if got := c.Resolve("station", "WCBS -DT2", "Start TV Network", "2.2"); got != "" {
				t.Fatalf("got stale logo %q during outage", got)
			}
			unavailable = false
			if got := c.Resolve("station", "WCBS -DT2", "Start TV Network", "2.2"); got != prefix+"start-tv-us.png" {
				t.Fatalf("got %q after recovery; expected a retry", got)
			}
		})
	}
}

func TestResolveTransientHigherPriorityDoesNotCacheFallback(t *testing.T) {
	const prefix = baseRawURL + "/united-states/"
	c := NewClient("USA", filepath.Join(t.TempDir(), "logo-cache.json"))
	exactUnavailable := true
	c.http.Transport = logoTestTransport(func(req *http.Request) (*http.Response, error) {
		c.last = time.Time{}
		status := http.StatusNotFound
		switch req.URL.String() {
		case prefix + "start-tv-network-us.png":
			status = http.StatusOK
			if exactUnavailable {
				status = http.StatusServiceUnavailable
			}
		case prefix + "start-tv-us.png":
			status = http.StatusOK
		}
		return &http.Response{StatusCode: status, Body: http.NoBody}, nil
	})
	if got := c.Resolve("station", "WCBS DT2", "Start TV Network", "2.2"); got != prefix+"start-tv-us.png" {
		t.Fatalf("got %q during outage; want verified provisional fallback", got)
	}
	exactUnavailable = false
	if got := c.Resolve("station", "WCBS DT2", "Start TV Network", "2.2"); got != prefix+"start-tv-network-us.png" {
		t.Fatalf("got %q after recovery; want higher-priority exact logo", got)
	}
}
