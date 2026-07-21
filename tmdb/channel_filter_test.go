package tmdb

import "testing"

func TestChannelExclusionGroups(t *testing.T) {
	t.Setenv("TMDB_EXCLUDE_CHANNEL_GROUPS", "local-access,shopping,religious")
	t.Setenv("TMDB_EXCLUDE_CHANNEL_PATTERNS", "")

	tests := []struct {
		name      string
		callSign  string
		affiliate string
		number    string
		want      string
	}{
		{name: "peg access", callSign: "PEG024", affiliate: "Public/Educational/Government Access 24", number: "24", want: "local-access"},
		{name: "qvc", callSign: "QVCHD", affiliate: "QVC", number: "650", want: "shopping"},
		{name: "hsn", callSign: "HSNHD", affiliate: "Home Shopping Network", number: "651", want: "shopping"},
		{name: "jewelry tv", callSign: "JTVHD", affiliate: "JEWELRY TV", number: "652", want: "shopping"},
		{name: "shop lc", callSign: "WRNN", affiliate: "RNN / Shop LC", number: "506", want: "shopping"},
		{name: "daystar", callSign: "DYSTRHD", affiliate: "DAYSTAR TELEVISION NETWORK", number: "793", want: "religious"},
		{name: "sonlife", callSign: "SONLFHD", affiliate: "SONLIFE BROADCASTING NETWORK", number: "797", want: "religious"},
		{name: "normal local affiliate", callSign: "WCBSHD", affiliate: "CBS", number: "502", want: ""},
		{name: "normal entertainment", callSign: "FXHD", affiliate: "FX", number: "553", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := channelExclusionReason(tt.callSign, tt.affiliate, tt.number); got != tt.want {
				t.Fatalf("channelExclusionReason() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCustomChannelExclusionPattern(t *testing.T) {
	t.Setenv("TMDB_EXCLUDE_CHANNEL_GROUPS", "")
	t.Setenv("TMDB_EXCLUDE_CHANNEL_PATTERNS", "MYLOCAL, TEST NETWORK")

	if got := channelExclusionReason("MYLOCALHD", "Independent", "999"); got != "custom" {
		t.Fatalf("custom callsign pattern returned %q", got)
	}
	if got := channelExclusionReason("TEST", "The Test Network East", "998"); got != "custom" {
		t.Fatalf("custom affiliate pattern returned %q", got)
	}
}

func TestExcludedChannelSkipsTMDBOnlyWhenAllOccurrencesAreExcluded(t *testing.T) {
	t.Setenv("TMDB_EXCLUDE_CHANNEL_GROUPS", "shopping")
	t.Setenv("TMDB_EXCLUDE_CHANNEL_PATTERNS", "")
	resetChannelEligibilityRegistryForTest()
	defer resetChannelEligibilityRegistryForTest()

	RegisterChannelEligibility("qvc", "QVCHD", "QVC", "650")
	RegisterProgramEligibility("Example Programme", false, "qvc")

	key := cacheKey("Example Programme", false)
	if got := programChannelSkipReason(key); got != "channel-shopping" {
		t.Fatalf("programChannelSkipReason() = %q, want channel-shopping", got)
	}

	// The same title on any normal channel remains eligible. Guide data from
	// both channels is still present; this controls only the external lookup.
	RegisterChannelEligibility("fx", "FXHD", "FX", "553")
	RegisterProgramEligibility("Example Programme", false, "fx")
	if got := programChannelSkipReason(key); got != "" {
		t.Fatalf("eligible occurrence should override exclusion, got %q", got)
	}
}

func TestChannelExclusionAlsoAppliesToMovies(t *testing.T) {
	t.Setenv("TMDB_EXCLUDE_CHANNEL_GROUPS", "religious")
	t.Setenv("TMDB_EXCLUDE_CHANNEL_PATTERNS", "")
	resetChannelEligibilityRegistryForTest()
	defer resetChannelEligibilityRegistryForTest()

	RegisterChannelEligibility("daystar", "DYSTRHD", "DAYSTAR TELEVISION NETWORK", "793")
	RegisterProgramEligibility("The Gospel", true, "daystar")

	if got := tmdbSkipReason(cacheKey("The Gospel", true)); got != "channel-religious" {
		t.Fatalf("movie on excluded channel returned %q", got)
	}
}
