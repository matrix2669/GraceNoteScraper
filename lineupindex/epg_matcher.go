package lineupindex

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/daniel-widrick/GraceNoteScraper/channelcategory"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

const (
	epgBlockHours             = 6
	epgStrongCoverageMinutes  = 288
	epgMinimumOccurrences     = 6
	epgMinimumLongFormTitles  = 2
	epgMinimumLongFormMinutes = 600
	epgMinimumMatchRatio      = 0.80
	weekdayEPGSourcePrefix    = "gracenote-weekday-epg-"
)

const (
	epgPrimeRole     = "prime"
	epgSecondaryRole = "secondary"
)

type epgBlock struct {
	ID       string
	Label    string
	Role     string
	Fallback bool
	Start    time.Time
}

type postalLineupScan struct {
	Comparison bool
	Lineup     *LineupRecord
	Provider   web.Provider
	Grids      map[string]*web.GridResponse
	Facts      []ProviderFact
	Sources    []EvidenceSourceRecord
}

type epgIdentityStation struct {
	StationID     string
	CallSigns     map[string]string
	Affiliates    map[string]string
	ProviderNames map[string]string
	Positions     map[string]map[string]bool
	Categories    []ProviderFact
	LineupKeys    map[string]bool
}

type epgCandidatePair struct {
	LeftID   string
	RightID  string
	Evidence []string
}

type epgPairResult struct {
	Pair                   epgCandidatePair
	Status                 string
	Reason                 string
	Occurrences            int
	Titles                 int
	MatchedMinutes         int
	NeedsPrimeFallback     bool
	NeedsSecondaryFallback bool
}

type epgDerivedFact struct {
	ProviderFact
	LineupKeys []string
}

type epgRunResult struct {
	ConfirmedPairs    int
	QuestionablePairs int
	RejectedPairs     int
	Aliases           int
	Categories        int
	MatchedStations   int
	Facts             []epgDerivedFact
	Source            EvidenceSourceRecord
}

type epgEvent struct {
	ProgramID string
	Title     string
	Start     time.Time
	End       time.Time
}

type epgScheduleComparison struct {
	CoverageLeft   int
	CoverageRight  int
	MatchedMinutes int
	MatchRatio     float64
	Occurrences    map[string]bool
	Titles         map[string]bool
}

var epgPlaceholderProgramIDs = map[string]bool{
	"SH000000010000": true,
	"SH000191120000": true,
	"SH000193310000": true,
	"SH001103200000": true,
	"SH003722930000": true,
}

var epgPlaceholderTitles = map[string]bool{
	"SIGN OFF":          true,
	"LOCAL ORIGINATION": true,
	"PUBLIC EDUCATIONAL AND GOVERNMENT ACCESS": true,
	"PUBLIC EDUCATIONAL GOVERNMENT ACCESS":     true,
	"PAID PROGRAMMING":                         true,
	"PROGRAMA PAGADO":                          true,
	"TO BE ANNOUNCED":                          true,
	"TBA":                                      true,
	"OFF AIR":                                  true,
	"NO PROGRAMMING":                           true,
	"VISIT OPTIMUM NET THEGAME FOR UPCOMING EVENTS": true,
}

func weekdayEPGBlocks(now time.Time, providers []web.Provider, response *web.ProviderResponse) ([]epgBlock, string, error) {
	location, label, err := providerLocation(providers, response)
	if err != nil {
		return nil, "", err
	}
	localNow := now.In(location)
	daysUntilTuesday := (int(time.Tuesday) - int(localNow.Weekday()) + 7) % 7
	tuesday := time.Date(localNow.Year(), localNow.Month(), localNow.Day()+daysUntilTuesday, 20, 0, 0, 0, location)
	if !tuesday.After(localNow) {
		tuesday = tuesday.AddDate(0, 0, 7)
	}
	wednesday := time.Date(tuesday.Year(), tuesday.Month(), tuesday.Day()+1, 14, 0, 0, 0, location)
	return []epgBlock{
		{ID: "primary-prime", Label: "Tuesday prime time", Role: epgPrimeRole, Start: tuesday},
		{ID: "primary-secondary", Label: "Wednesday afternoon", Role: epgSecondaryRole, Start: wednesday},
		{ID: "fallback-prime", Label: "Thursday prime time fallback", Role: epgPrimeRole, Fallback: true, Start: tuesday.AddDate(0, 0, 2)},
		{ID: "fallback-secondary", Label: "Friday afternoon fallback", Role: epgSecondaryRole, Fallback: true, Start: wednesday.AddDate(0, 0, 2)},
	}, label, nil
}

func providerLocation(providers []web.Provider, response *web.ProviderResponse) (*time.Location, string, error) {
	aliases := map[string]string{
		"EASTERN": "America/New_York", "EASTERN TIME": "America/New_York", "EASTERN STANDARD TIME": "America/New_York", "ET": "America/New_York", "EST": "America/New_York", "EDT": "America/New_York",
		"CENTRAL": "America/Chicago", "CENTRAL TIME": "America/Chicago", "CENTRAL STANDARD TIME": "America/Chicago", "CT": "America/Chicago", "CST": "America/Chicago", "CDT": "America/Chicago",
		"MOUNTAIN": "America/Denver", "MOUNTAIN TIME": "America/Denver", "MOUNTAIN STANDARD TIME": "America/Denver", "MT": "America/Denver", "MST": "America/Denver", "MDT": "America/Denver",
		"PACIFIC": "America/Los_Angeles", "PACIFIC TIME": "America/Los_Angeles", "PACIFIC STANDARD TIME": "America/Los_Angeles", "PT": "America/Los_Angeles", "PST": "America/Los_Angeles", "PDT": "America/Los_Angeles",
		"ALASKA": "America/Anchorage", "ALASKA TIME": "America/Anchorage", "AKST": "America/Anchorage", "AKDT": "America/Anchorage",
		"HAWAII": "Pacific/Honolulu", "HAWAII TIME": "Pacific/Honolulu", "HST": "Pacific/Honolulu",
	}
	counts := make(map[string]int)
	for _, provider := range providers {
		value := strings.TrimSpace(provider.Timezone)
		if canonical := aliases[strings.ToUpper(value)]; canonical != "" {
			value = canonical
		}
		if value == "" {
			continue
		}
		if _, err := time.LoadLocation(value); err == nil {
			counts[value]++
		}
	}
	if len(counts) > 0 {
		values := make([]string, 0, len(counts))
		for value := range counts {
			values = append(values, value)
		}
		sort.Slice(values, func(i, j int) bool {
			if counts[values[i]] != counts[values[j]] {
				return counts[values[i]] > counts[values[j]]
			}
			return values[i] < values[j]
		})
		location, _ := time.LoadLocation(values[0])
		return location, values[0], nil
	}
	if response != nil {
		if offset, ok := parseUTCOffset(response.StdUTCOffset); ok {
			zones := map[int]string{-10 * 60: "Pacific/Honolulu", -9 * 60: "America/Anchorage", -8 * 60: "America/Los_Angeles", -7 * 60: "America/Denver", -6 * 60: "America/Chicago", -5 * 60: "America/New_York", -4 * 60: "America/Halifax"}
			if zone := zones[offset]; zone != "" {
				location, _ := time.LoadLocation(zone)
				return location, zone, nil
			}
		}
		if offset, ok := parseUTCOffset(response.DSTUTCOffset); ok {
			name := fmt.Sprintf("UTC%+03d:%02d", offset/60, absoluteInt(offset%60))
			return time.FixedZone(name, offset*60), name, nil
		}
	}
	return nil, "", fmt.Errorf("Gracenote did not provide a usable local timezone for the postal code")
}

func parseUTCOffset(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	sign := 1
	if value[0] == '-' {
		sign = -1
		value = value[1:]
	} else if value[0] == '+' {
		value = value[1:]
	}
	if !strings.Contains(value, ":") {
		amount, err := strconv.Atoi(value)
		if err != nil || amount > 14*60 {
			return 0, false
		}
		// Gracenote provider discovery returns offsets as signed minutes
		// (for example, -300 standard and -240 daylight time). Retain the
		// existing whole-hour shorthand for values within the timezone range.
		if amount <= 14 {
			amount *= 60
		}
		return sign * amount, true
	}
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, false
	}
	hours, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, false
	}
	minutes := 0
	minutes, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, false
	}
	if hours > 14 || minutes > 59 {
		return 0, false
	}
	return sign * (hours*60 + minutes), true
}

func absoluteInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func buildEPGCandidates(scans []*postalLineupScan, primaryBlockID string) (map[string]*epgIdentityStation, []epgCandidatePair) {
	stations := make(map[string]*epgIdentityStation)
	identityOwners := make(map[string]map[string]bool)
	for _, scan := range scans {
		grid := scan.Grids[primaryBlockID]
		if grid == nil {
			continue
		}
		for _, channel := range grid.Channels {
			stationID := strings.TrimSpace(channel.ChannelID)
			if stationID == "" {
				continue
			}
			station := ensureEPGIdentityStation(stations, stationID)
			station.LineupKeys[scan.Lineup.Key] = true
			addEPGCallSign(station, channel.CallSign)
			addEPGCallSign(station, channel.AffiliateCallSign)
			addEPGAffiliate(station, channel.AffiliateName)
			for _, event := range channel.Events {
				addEPGCallSign(station, event.CallSign)
			}
		}
		for _, fact := range scan.Facts {
			unsafeNumber := false
			for _, part := range strings.Split(fact.Method, ";") {
				if strings.TrimSpace(part) == "exact provider channel number" {
					unsafeNumber = true
				}
			}
			if unsafeNumber {
				continue
			}
			station := stations[strings.TrimSpace(fact.StationID)]
			if station == nil {
				continue
			}
			switch fact.Kind {
			case FactAlias:
				value := strings.TrimSpace(fact.Value)
				if normalized := normalizeEPGCallSign(value); !ignoredName(normalized) && !isEPGEventFeedName(value) {
					station.ProviderNames[normalized] = value
				}
			case FactCategory:
				station.Categories = append(station.Categories, fact)
			}
		}
	}
	for stationID, station := range stations {
		for key := range station.CallSigns {
			addIdentityOwner(identityOwners, "identity-name:"+key, stationID)
		}
		for key := range station.Affiliates {
			addIdentityOwner(identityOwners, "affiliate:"+key, stationID)
		}
		for key := range station.ProviderNames {
			addIdentityOwner(identityOwners, "identity-name:"+key, stationID)
		}
	}
	pairs := make(map[string]*epgCandidatePair)
	for evidence, owners := range identityOwners {
		if len(owners) < 2 {
			continue
		}
		ids := make([]string, 0, len(owners))
		for stationID := range owners {
			ids = append(ids, stationID)
		}
		sort.Strings(ids)
		for left := 0; left < len(ids); left++ {
			for right := left + 1; right < len(ids); right++ {
				addEPGCandidateEvidence(pairs, ids[left], ids[right], evidence)
			}
		}
	}
	result := make([]epgCandidatePair, 0, len(pairs))
	for _, pair := range pairs {
		sort.Strings(pair.Evidence)
		if !hasStrongEPGIdentityEvidence(pair.Evidence) {
			continue
		}
		result = append(result, *pair)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].LeftID != result[j].LeftID {
			return result[i].LeftID < result[j].LeftID
		}
		return result[i].RightID < result[j].RightID
	})
	return stations, result
}

func hasStrongEPGIdentityEvidence(evidence []string) bool {
	for _, value := range evidence {
		if strings.HasPrefix(value, "identity-name:") {
			return true
		}
	}
	return false
}

func ensureEPGIdentityStation(stations map[string]*epgIdentityStation, stationID string) *epgIdentityStation {
	station := stations[stationID]
	if station == nil {
		station = &epgIdentityStation{
			StationID: stationID, CallSigns: make(map[string]string), Affiliates: make(map[string]string),
			ProviderNames: make(map[string]string), Positions: make(map[string]map[string]bool), LineupKeys: make(map[string]bool),
		}
		stations[stationID] = station
	}
	return station
}

func addEPGCandidateEvidence(pairs map[string]*epgCandidatePair, leftID, rightID, evidence string) {
	if rightID < leftID {
		leftID, rightID = rightID, leftID
	}
	key := leftID + "\x00" + rightID
	pair := pairs[key]
	if pair == nil {
		pair = &epgCandidatePair{LeftID: leftID, RightID: rightID}
		pairs[key] = pair
	}
	pair.Evidence = appendUniqueString(pair.Evidence, evidence)
}

func stringSetsOverlap(left, right map[string]bool) bool {
	for value := range left {
		if right[value] {
			return true
		}
	}
	return false
}

func addEPGCallSign(station *epgIdentityStation, value string) {
	value = strings.TrimSpace(value)
	normalized := normalizeEPGCallSign(value)
	if normalized == "" || ignoredName(normalized) {
		return
	}
	if _, exists := station.CallSigns[normalized]; !exists {
		station.CallSigns[normalized] = value
	}
}

func addEPGAffiliate(station *epgIdentityStation, value string) {
	value = strings.TrimSpace(value)
	normalized := normalizeName(value)
	switch normalized {
	case "", "INDEPENDENT", "LOCAL", "NA", "NONE", "UNKNOWN":
		return
	}
	station.Affiliates[normalized] = value
}

func normalizeEPGCallSign(value string) string {
	compact := normalizeName(value)
	if compact == "" {
		return ""
	}
	digitsStart := len(compact)
	for digitsStart > 0 && compact[digitsStart-1] >= '0' && compact[digitsStart-1] <= '9' {
		digitsStart--
	}
	if digitsStart < len(compact) && strings.HasSuffix(compact[:digitsStart], "DT") {
		return compact
	}
	for _, suffix := range []string{"HD", "SD", "DT"} {
		if strings.HasSuffix(compact, suffix) && len(compact) > len(suffix) {
			return strings.TrimSuffix(compact, suffix)
		}
	}
	return compact
}

func addIdentityOwner(index map[string]map[string]bool, key, stationID string) {
	if strings.HasSuffix(key, ":") || strings.HasSuffix(key, "|") {
		return
	}
	if index[key] == nil {
		index[key] = make(map[string]bool)
	}
	index[key][stationID] = true
}

func evaluateEPGPairs(scans []*postalLineupScan, pairs []epgCandidatePair, blocks []epgBlock) []epgPairResult {
	results := make([]epgPairResult, 0, len(pairs))
	hasFallback := false
	for _, block := range blocks {
		hasFallback = hasFallback || block.Fallback
	}
	for _, pair := range pairs {
		result := epgPairResult{Pair: pair}
		if hasFallback && epgPairIsPlaceholderOnly(scans, pair, blocks) {
			result.Status = "rejected"
			result.Reason = "placeholder-only"
			results = append(results, result)
			continue
		}
		prime, primeOK := chooseEPGBlock(scans, pair, blocks, epgPrimeRole)
		secondary, secondaryOK := chooseEPGBlock(scans, pair, blocks, epgSecondaryRole)
		if !primeOK || !secondaryOK {
			result.Status = "questionable"
			result.Reason = "weak-weekday-block-after-fallback"
			result.NeedsPrimeFallback = !primeOK
			result.NeedsSecondaryFallback = !secondaryOK
			results = append(results, result)
			continue
		}
		if prime.MatchRatio < epgMinimumMatchRatio || secondary.MatchRatio < epgMinimumMatchRatio {
			result.Status = "rejected"
			result.Reason = "weekday-schedules-diverged"
			results = append(results, result)
			continue
		}
		occurrences := make(map[string]bool)
		titles := make(map[string]bool)
		for key := range prime.Occurrences {
			occurrences[key] = true
		}
		for key := range secondary.Occurrences {
			occurrences[key] = true
		}
		for key := range prime.Titles {
			titles[key] = true
		}
		for key := range secondary.Titles {
			titles[key] = true
		}
		result.Occurrences = len(occurrences)
		result.Titles = len(titles)
		result.MatchedMinutes = prime.MatchedMinutes + secondary.MatchedMinutes
		if result.Occurrences < epgMinimumOccurrences && (result.Titles < epgMinimumLongFormTitles || result.MatchedMinutes < epgMinimumLongFormMinutes) {
			result.Status = "questionable"
			result.Reason = "insufficient-meaningful-programmes"
			results = append(results, result)
			continue
		}
		result.Status = "confirmed"
		if result.Occurrences < epgMinimumOccurrences {
			result.Reason = "long-form-coverage"
		} else {
			result.Reason = "weekday-programmes"
		}
		results = append(results, result)
	}
	return results
}

func epgPairIsPlaceholderOnly(scans []*postalLineupScan, pair epgCandidatePair, blocks []epgBlock) bool {
	available := 0
	for _, block := range blocks {
		left, leftOK := resolveEPGSchedule(scans, pair.LeftID, block)
		right, rightOK := resolveEPGSchedule(scans, pair.RightID, block)
		if !leftOK || !rightOK {
			continue
		}
		available++
		comparison := compareEPGSchedules(left, right, block)
		if comparison.CoverageLeft > 0 && comparison.CoverageRight > 0 {
			return false
		}
	}
	return available > 0
}

func chooseEPGBlock(scans []*postalLineupScan, pair epgCandidatePair, blocks []epgBlock, role string) (epgScheduleComparison, bool) {
	var primary *epgScheduleComparison
	var fallback *epgScheduleComparison
	for _, block := range blocks {
		if block.Role != role {
			continue
		}
		left, leftOK := resolveEPGSchedule(scans, pair.LeftID, block)
		right, rightOK := resolveEPGSchedule(scans, pair.RightID, block)
		if !leftOK || !rightOK {
			continue
		}
		comparison := compareEPGSchedules(left, right, block)
		if comparison.CoverageLeft < epgStrongCoverageMinutes || comparison.CoverageRight < epgStrongCoverageMinutes {
			continue
		}
		if block.Fallback {
			copy := comparison
			fallback = &copy
		} else {
			copy := comparison
			primary = &copy
		}
	}
	if primary != nil {
		return *primary, true
	}
	if fallback != nil {
		return *fallback, true
	}
	return epgScheduleComparison{}, false
}

func resolveEPGSchedule(scans []*postalLineupScan, stationID string, block epgBlock) ([]epgEvent, bool) {
	type variant struct {
		events []epgEvent
		count  int
	}
	variants := make(map[string]*variant)
	for _, scan := range scans {
		grid := scan.Grids[block.ID]
		if grid == nil {
			continue
		}
		var selected []epgEvent
		found := false
		for _, channel := range grid.Channels {
			if strings.TrimSpace(channel.ChannelID) != stationID {
				continue
			}
			selected = append(selected, normalizeEPGEvents(channel.Events)...)
			found = true
		}
		if !found {
			continue
		}
		sort.Slice(selected, func(i, j int) bool {
			if !selected[i].Start.Equal(selected[j].Start) {
				return selected[i].Start.Before(selected[j].Start)
			}
			if !selected[i].End.Equal(selected[j].End) {
				return selected[i].End.Before(selected[j].End)
			}
			return selected[i].ProgramID < selected[j].ProgramID
		})
		fingerprint := epgScheduleFingerprint(selected)
		if variants[fingerprint] == nil {
			variants[fingerprint] = &variant{events: selected}
		}
		variants[fingerprint].count++
	}
	if len(variants) == 0 {
		return nil, false
	}
	ranked := make([]*variant, 0, len(variants))
	for _, value := range variants {
		ranked = append(ranked, value)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].count != ranked[j].count {
			return ranked[i].count > ranked[j].count
		}
		return len(ranked[i].events) > len(ranked[j].events)
	})
	if len(ranked) > 1 && ranked[0].count == ranked[1].count {
		return nil, false
	}
	return ranked[0].events, true
}

func normalizeEPGEvents(events []web.JSONEvent) []epgEvent {
	result := make([]epgEvent, 0, len(events))
	for _, event := range events {
		start, startErr := time.Parse(time.RFC3339, strings.TrimSpace(event.StartTime))
		end, endErr := time.Parse(time.RFC3339, strings.TrimSpace(event.EndTime))
		title := strings.TrimSpace(event.Program.Title)
		programID := strings.TrimSpace(event.Program.ID)
		if startErr != nil || endErr != nil || !end.After(start) || (programID == "" && title == "") {
			continue
		}
		result = append(result, epgEvent{ProgramID: programID, Title: title, Start: start, End: end})
	}
	return result
}

func epgScheduleFingerprint(events []epgEvent) string {
	var builder strings.Builder
	for _, event := range events {
		builder.WriteString(event.ProgramID)
		builder.WriteByte('|')
		builder.WriteString(normalizeEPGTitle(event.Title))
		builder.WriteByte('|')
		builder.WriteString(event.Start.UTC().Format(time.RFC3339))
		builder.WriteByte('|')
		builder.WriteString(event.End.UTC().Format(time.RFC3339))
		builder.WriteByte(';')
	}
	return builder.String()
}

func compareEPGSchedules(left, right []epgEvent, block epgBlock) epgScheduleComparison {
	meaningfulLeft := meaningfulEPGEvents(left, block)
	meaningfulRight := meaningfulEPGEvents(right, block)
	comparison := epgScheduleComparison{
		CoverageLeft: epgCoverageMinutes(meaningfulLeft, block), CoverageRight: epgCoverageMinutes(meaningfulRight, block),
		Occurrences: make(map[string]bool), Titles: make(map[string]bool),
	}
	matchedRight := make(map[int]bool)
	intervals := make([][2]time.Time, 0)
	for _, leftEvent := range meaningfulLeft {
		for index, rightEvent := range meaningfulRight {
			if matchedRight[index] || !sameEPGProgrammeSlot(leftEvent, rightEvent) {
				continue
			}
			matchedRight[index] = true
			if interval, ok := clipEPGInterval(leftEvent, block); ok {
				intervals = append(intervals, interval)
			}
			semantic := normalizeEPGTitle(leftEvent.Title)
			if semantic == "" {
				semantic = leftEvent.ProgramID
			}
			comparison.Occurrences[semantic+"|"+leftEvent.Start.UTC().Format(time.RFC3339)+"|"+leftEvent.End.UTC().Format(time.RFC3339)] = true
			comparison.Titles[semantic] = true
			break
		}
	}
	comparison.MatchedMinutes = unionEPGMinutes(intervals)
	denominator := minInt(comparison.CoverageLeft, comparison.CoverageRight)
	if denominator > 0 {
		comparison.MatchRatio = float64(comparison.MatchedMinutes) / float64(denominator)
	}
	return comparison
}

func meaningfulEPGEvents(events []epgEvent, block epgBlock) []epgEvent {
	result := make([]epgEvent, 0, len(events))
	for _, event := range events {
		if isEPGPlaceholder(event) {
			continue
		}
		if _, ok := clipEPGInterval(event, block); ok {
			result = append(result, event)
		}
	}
	return result
}

func isEPGPlaceholder(event epgEvent) bool {
	return epgPlaceholderProgramIDs[event.ProgramID] || epgPlaceholderTitles[normalizeEPGTitle(event.Title)]
}

func normalizeEPGTitle(value string) string {
	var builder strings.Builder
	space := false
	for _, character := range strings.ToUpper(strings.TrimSpace(value)) {
		if (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') {
			if space && builder.Len() > 0 {
				builder.WriteByte(' ')
			}
			builder.WriteRune(character)
			space = false
		} else {
			space = true
		}
	}
	return builder.String()
}

func sameEPGProgrammeSlot(left, right epgEvent) bool {
	if !left.Start.Equal(right.Start) || !left.End.Equal(right.End) {
		return false
	}
	if left.ProgramID != "" && left.ProgramID == right.ProgramID {
		return true
	}
	leftTitle := normalizeEPGTitle(left.Title)
	return leftTitle != "" && leftTitle == normalizeEPGTitle(right.Title)
}

func clipEPGInterval(event epgEvent, block epgBlock) ([2]time.Time, bool) {
	start := block.Start
	end := block.Start.Add(epgBlockHours * time.Hour)
	eventStart := event.Start
	eventEnd := event.End
	if eventStart.Before(start) {
		eventStart = start
	}
	if eventEnd.After(end) {
		eventEnd = end
	}
	return [2]time.Time{eventStart, eventEnd}, eventEnd.After(eventStart)
}

func epgCoverageMinutes(events []epgEvent, block epgBlock) int {
	intervals := make([][2]time.Time, 0, len(events))
	for _, event := range events {
		if interval, ok := clipEPGInterval(event, block); ok {
			intervals = append(intervals, interval)
		}
	}
	return unionEPGMinutes(intervals)
}

func unionEPGMinutes(intervals [][2]time.Time) int {
	if len(intervals) == 0 {
		return 0
	}
	sort.Slice(intervals, func(i, j int) bool {
		if !intervals[i][0].Equal(intervals[j][0]) {
			return intervals[i][0].Before(intervals[j][0])
		}
		return intervals[i][1].Before(intervals[j][1])
	})
	start := intervals[0][0]
	end := intervals[0][1]
	total := time.Duration(0)
	for _, interval := range intervals[1:] {
		if !interval[0].After(end) {
			if interval[1].After(end) {
				end = interval[1]
			}
			continue
		}
		total += end.Sub(start)
		start, end = interval[0], interval[1]
	}
	total += end.Sub(start)
	return int(total.Round(time.Minute) / time.Minute)
}

func buildEPGDerivedFacts(stations map[string]*epgIdentityStation, results []epgPairResult, sourceID, timezone string) []epgDerivedFact {
	byKey := make(map[string]epgDerivedFact)
	for _, result := range results {
		if result.Status != "confirmed" {
			continue
		}
		left := stations[result.Pair.LeftID]
		right := stations[result.Pair.RightID]
		if left == nil || right == nil {
			continue
		}
		method := fmt.Sprintf("pair-level identity (%s); two non-concurrent weekday blocks in %s matched at least 80%% with %d meaningful programmes and %d matched minutes", strings.Join(result.Pair.Evidence, ", "), timezone, result.Occurrences, result.MatchedMinutes)
		method += "; identity-policy-v2"
		appendEPGPeerFacts(byKey, left, right, sourceID, method)
		appendEPGPeerFacts(byKey, right, left, sourceID, method)
	}
	result := make([]epgDerivedFact, 0, len(byKey))
	for _, fact := range byKey {
		result = append(result, fact)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].StationID != result[j].StationID {
			return result[i].StationID < result[j].StationID
		}
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		return result[i].Value < result[j].Value
	})
	return result
}

func appendEPGPeerFacts(byKey map[string]epgDerivedFact, target, peer *epgIdentityStation, sourceID, method string) {
	lineups := unionStringKeys(target.LineupKeys, peer.LineupKeys)
	aliases := make(map[string]string)
	for normalized, value := range peer.CallSigns {
		aliases[normalizeName(value)] = value
		if normalized != normalizeName(value) {
			aliases[normalized] = normalized
		}
	}
	for normalized, value := range peer.ProviderNames {
		aliases[normalized] = value
	}
	for normalized, value := range aliases {
		if ignoredName(normalized) || isEPGEventFeedName(value) {
			continue
		}
		key := target.StationID + "\x00" + FactAlias + "\x00" + normalized
		byKey[key] = epgDerivedFact{ProviderFact: ProviderFact{
			StationID: target.StationID, Kind: FactAlias, Value: value,
			SourceID: sourceID, SourceLabel: "Gracenote weekday EPG confirmation", Method: method,
		}, LineupKeys: lineups}
	}
	for _, category := range peer.Categories {
		normalized := normalizeName(category.Value)
		if ignoredName(normalized) {
			continue
		}
		key := target.StationID + "\x00" + FactCategory + "\x00" + normalized
		categoryMethod := method + "; category carried from " + strings.TrimSpace(category.SourceLabel)
		// Identity confirmation must retain, not upgrade, category provenance.
		categoryMethod += "; " + category.Method
		if strings.Contains(category.Method, "identity-policy-v2") {
			categoryMethod += "; identity-policy-v2"
		}
		byKey[key] = epgDerivedFact{ProviderFact: ProviderFact{
			StationID: target.StationID, Kind: FactCategory, Value: category.Value, RawValue: category.RawValue,
			SourceID: sourceID, SourceLabel: "Gracenote weekday EPG confirmation",
			Method: categoryMethod,
		}, LineupKeys: lineups}
	}
}

func isEPGEventFeedName(value string) bool {
	normalized := normalizeName(value)
	if normalized == "" {
		return false
	}
	for _, marker := range []string{
		"PAYPERVIEW", "PPV", "SPECIALEVENT", "EVENTCHANNEL", "OVERFLOW", "ALTERNATE", "ALTFEED",
		"LEAGUEPASS", "SUNDAYTICKET", "EXTRAINNINGS", "CENTERICE",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	last := normalized[len(normalized)-1]
	if last < '0' || last > '9' {
		return false
	}
	for _, league := range []string{"WNBA", "NBA", "NFL", "NHL", "MLB", "MLS", "NCAA", "UFC", "BOXING", "WWE"} {
		for _, relation := range []string{"ON", "AT", "VS"} {
			if strings.Contains(normalized, league+relation) {
				return true
			}
		}
	}
	return false
}

func unionStringKeys(values ...map[string]bool) []string {
	seen := make(map[string]bool)
	for _, set := range values {
		for value := range set {
			seen[value] = true
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func lineupsForPairs(scans []*postalLineupScan, pairs []epgCandidatePair, predicate func(epgCandidatePair) bool) map[int]bool {
	stationIDs := make(map[string]bool)
	for _, pair := range pairs {
		if predicate != nil && !predicate(pair) {
			continue
		}
		stationIDs[pair.LeftID] = true
		stationIDs[pair.RightID] = true
	}
	result := make(map[int]bool)
	for index, scan := range scans {
		for _, grid := range scan.Grids {
			for _, channel := range grid.Channels {
				if stationIDs[strings.TrimSpace(channel.ChannelID)] {
					result[index] = true
					break
				}
			}
			if result[index] {
				break
			}
		}
	}
	return result
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func weekdayEPGSourceID(country, postalCode string) string {
	return weekdayEPGSourcePrefix + strings.ToLower(pathSegment(country, "country")) + "-" + strings.ToLower(pathSegment(postalCode, "postal"))
}

func (s *Service) runPostalEPG(ctx context.Context, postalKey, country, postalCode, timezone string, scans []*postalLineupScan, blocks []epgBlock, lastGridRequest *time.Time) (epgRunResult, error) {
	sourceID := weekdayEPGSourceID(country, postalCode)
	if strings.HasPrefix(postalKey, "market:") {
		sourceID += "-" + strings.ReplaceAll(postalKey, ":", "-")
	}
	result := epgRunResult{Source: EvidenceSourceRecord{
		ID: sourceID, Label: "Gracenote weekday EPG confirmation", Status: StatusComplete,
	}}
	stations, pairs := buildEPGCandidates(scans, blocks[0].ID)
	if len(pairs) == 0 {
		aliases, categories, matched, err := s.replaceEPGFacts(sourceID, nil)
		result.Aliases, result.Categories, result.MatchedStations = aliases, categories, matched
		result.Source.Message = "No cross-station candidates had independent identity evidence; no schedule-only aliases were created."
		return result, err
	}

	needed := lineupsForPairs(scans, pairs, nil)
	s.extendEPGJob(len(needed))
	if err := s.fetchPostalEPGBlock(ctx, postalKey, scans, needed, blocks[1], lastGridRequest); err != nil {
		result.Source.Status = StatusError
		result.Source.Message = "The second weekday block could not be completed; prior confirmed EPG aliases were left unchanged: " + err.Error()
		return result, err
	}
	primaryResults := evaluateEPGPairs(scans, pairs, blocks[:2])
	primeNeeds := make(map[string]bool)
	secondaryNeeds := make(map[string]bool)
	for _, pairResult := range primaryResults {
		key := pairResult.Pair.LeftID + "\x00" + pairResult.Pair.RightID
		if pairResult.NeedsPrimeFallback {
			primeNeeds[key] = true
		}
		if pairResult.NeedsSecondaryFallback {
			secondaryNeeds[key] = true
		}
	}
	for _, request := range []struct {
		block epgBlock
		needs map[string]bool
	}{{block: blocks[2], needs: primeNeeds}, {block: blocks[3], needs: secondaryNeeds}} {
		if len(request.needs) == 0 {
			continue
		}
		fallbackPairs := make([]epgCandidatePair, 0, len(request.needs))
		for _, pair := range pairs {
			if request.needs[pair.LeftID+"\x00"+pair.RightID] {
				fallbackPairs = append(fallbackPairs, pair)
			}
		}
		fallbackLineups := lineupsForPairs(scans, fallbackPairs, nil)
		s.extendEPGJob(len(fallbackLineups))
		if err := s.fetchPostalEPGBlock(ctx, postalKey, scans, fallbackLineups, request.block, lastGridRequest); err != nil {
			result.Source.Status = StatusError
			result.Source.Message = "A fallback weekday block could not be completed; prior confirmed EPG aliases were left unchanged: " + err.Error()
			return result, err
		}
	}

	finalResults := evaluateEPGPairs(scans, pairs, blocks)
	for _, pairResult := range finalResults {
		switch pairResult.Status {
		case "confirmed":
			result.ConfirmedPairs++
		case "questionable":
			result.QuestionablePairs++
		case "rejected":
			result.RejectedPairs++
		}
	}
	result.Facts = buildEPGDerivedFacts(stations, finalResults, sourceID, timezone)
	aliases, categories, matched, err := s.replaceEPGFacts(sourceID, result.Facts)
	if err != nil {
		result.Source.Status = StatusError
		result.Source.Message = "Confirmed EPG evidence could not be saved: " + err.Error()
		return result, err
	}
	result.Aliases, result.Categories, result.MatchedStations = aliases, categories, matched
	result.Source.Matched = matched
	result.Source.Aliases = aliases
	result.Source.Categories = categories
	result.Source.Message = fmt.Sprintf(
		"%d pair-level matches confirmed, %d questionable, and %d rejected using two non-concurrent weekday blocks in %s; %d aliases and %d categories were retained without programme payloads.",
		result.ConfirmedPairs, result.QuestionablePairs, result.RejectedPairs, timezone, aliases, categories,
	)
	if err := s.rewriteEPGLineupSnapshots(scans, result.Facts, result.Source, blocks[0].ID); err != nil {
		result.Source.Status = StatusError
		result.Source.Message = "Confirmed EPG evidence was saved, but lineup snapshots could not be refreshed: " + err.Error()
		return result, err
	}
	return result, nil
}

func (s *Service) rewriteEPGLineupSnapshots(scans []*postalLineupScan, facts []epgDerivedFact, source EvidenceSourceRecord, primaryBlockID string) error {
	for _, scan := range scans {
		grid := scan.Grids[primaryBlockID]
		if grid == nil {
			continue
		}
		stationIDs := make(map[string]bool)
		for _, channel := range grid.Channels {
			stationIDs[strings.TrimSpace(channel.ChannelID)] = true
		}
		evidence := ProviderEvidenceResult{
			Facts: append([]ProviderFact(nil), scan.Facts...), Sources: append([]EvidenceSourceRecord(nil), scan.Sources...),
		}
		derivedCount := 0
		for _, fact := range facts {
			if !stationIDs[strings.TrimSpace(fact.StationID)] {
				continue
			}
			evidence.Facts = append(evidence.Facts, fact.ProviderFact)
			derivedCount++
		}
		if derivedCount > 0 {
			evidence.Sources = mergeEvidenceSources(evidence.Sources, []EvidenceSourceRecord{source})
		}
		if err := s.writeLineupSnapshot(*scan.Lineup, grid, evidence); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) fetchPostalEPGBlock(ctx context.Context, postalKey string, scans []*postalLineupScan, indexes map[int]bool, block epgBlock, lastGridRequest *time.Time) error {
	ordered := make([]int, 0, len(indexes))
	for index := range indexes {
		ordered = append(ordered, index)
	}
	sort.Ints(ordered)
	errorsByProvider := make([]string, 0)
	for _, index := range ordered {
		if err := ctx.Err(); err != nil {
			return err
		}
		scan := scans[index]
		s.mu.Lock()
		s.job.CurrentProvider = strings.TrimSpace(scan.Provider.Name) + " · " + block.Label
		s.mu.Unlock()
		if err := waitBetween(ctx, *lastGridRequest, s.gridDelay, s.now); err != nil {
			return err
		}
		*lastGridRequest = s.now()
		grid, err := s.grids.FetchGrid(ctx, lineupPreferences(scan.Lineup), block.Start.Unix())
		s.updatePostalJob(postalKey, func(record *PostalScanRecord) { record.GridRequests++ })
		s.mu.Lock()
		s.job.CompletedCount++
		s.mu.Unlock()
		if err != nil || grid == nil {
			if err == nil {
				err = fmt.Errorf("grid lookup returned no response")
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			errorsByProvider = append(errorsByProvider, strings.TrimSpace(scan.Provider.Name)+": "+err.Error())
			continue
		}
		scan.Grids[block.ID] = grid
	}
	if len(errorsByProvider) > 0 {
		return fmt.Errorf("%s", strings.Join(errorsByProvider, "; "))
	}
	return nil
}

func (s *Service) extendEPGJob(requests int) {
	if requests <= 0 {
		return
	}
	s.mu.Lock()
	s.job.TotalCount += requests
	s.mu.Unlock()
}

func (s *Service) replaceEPGFacts(sourceID string, facts []epgDerivedFact) (int, int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, station := range s.index.Stations {
		retained := station.Facts[:0]
		for _, fact := range station.Facts {
			if fact.SourceID != sourceID {
				retained = append(retained, fact)
			}
		}
		station.Facts = retained
	}
	aliases := 0
	categories := 0
	matchedStations := make(map[string]bool)
	for _, derived := range facts {
		fact := derived.ProviderFact
		stationID := strings.TrimSpace(fact.StationID)
		value := strings.TrimSpace(fact.Value)
		station := s.index.Stations[stationID]
		if station == nil || value == "" || (fact.Kind != FactAlias && fact.Kind != FactCategory) {
			continue
		}
		if fact.Kind == FactCategory {
			match, ok := channelcategory.Resolve(value)
			if !ok {
				continue
			}
			value = match.Category
		}
		normalized := normalizeName(value)
		if ignoredName(normalized) {
			continue
		}
		duplicate := false
		for _, existing := range station.Facts {
			if existing.SourceID == sourceID && existing.Kind == fact.Kind && existing.Normalized == normalized {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		station.Facts = append(station.Facts, StationFact{
			Kind: fact.Kind, Value: value, Normalized: normalized, RawValue: strings.TrimSpace(fact.RawValue),
			SourceID: sourceID, SourceLabel: "Gracenote weekday EPG confirmation", Method: strings.TrimSpace(fact.Method),
			LineupKeys: append([]string(nil), derived.LineupKeys...),
		})
		matchedStations[stationID] = true
		if fact.Kind == FactAlias {
			aliases++
		} else {
			categories++
		}
	}
	for _, station := range s.index.Stations {
		sort.SliceStable(station.Facts, func(i, j int) bool {
			if station.Facts[i].Kind != station.Facts[j].Kind {
				return station.Facts[i].Kind < station.Facts[j].Kind
			}
			if station.Facts[i].Normalized != station.Facts[j].Normalized {
				return station.Facts[i].Normalized < station.Facts[j].Normalized
			}
			return station.Facts[i].SourceID < station.Facts[j].SourceID
		})
	}
	s.index.UpdatedAt = s.now().UTC().Format(time.RFC3339)
	if err := writeIndex(s.path, s.index); err != nil {
		return 0, 0, 0, err
	}
	return aliases, categories, len(matchedStations), nil
}
