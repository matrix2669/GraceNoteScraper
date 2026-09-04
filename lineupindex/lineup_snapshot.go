package lineupindex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/daniel-widrick/GraceNoteScraper/channelcategory"
	"github.com/daniel-widrick/GraceNoteScraper/web"
)

const CurrentLineupSnapshotVersion = 1

// LineupSnapshot is the reusable, runtime-generated identity and category file
// for one Gracenote lineup in one configured postal-code scan. Programme events,
// provider credentials, service addresses, and stream URLs are intentionally
// excluded.
type LineupSnapshot struct {
	SchemaVersion           int                     `json:"schemaVersion"`
	CategoryTaxonomyVersion int                     `json:"categoryTaxonomyVersion"`
	CapturedAt              string                  `json:"capturedAt"`
	Country                 string                  `json:"country"`
	PostalCode              string                  `json:"postalCode"`
	Lineup                  LineupRecord            `json:"lineup"`
	Sources                 []EvidenceSourceRecord  `json:"sources,omitempty"`
	Channels                []LineupSnapshotChannel `json:"channels"`
}

type LineupSnapshotChannel struct {
	PositionID        string               `json:"positionId,omitempty"`
	StationID         string               `json:"stationId"`
	Number            string               `json:"number,omitempty"`
	CallSign          string               `json:"callSign,omitempty"`
	AffiliateName     string               `json:"affiliateName,omitempty"`
	AffiliateCallSign string               `json:"affiliateCallSign,omitempty"`
	EventCallSigns    []string             `json:"eventCallSigns,omitempty"`
	Aliases           []LineupSnapshotFact `json:"aliases,omitempty"`
	Category          string               `json:"category,omitempty"`
	CategoryConflict  bool                 `json:"categoryConflict,omitempty"`
	CategoryEvidence  []LineupSnapshotFact `json:"categoryEvidence,omitempty"`
}

type LineupSnapshotFact struct {
	Value           string  `json:"value"`
	RawValue        string  `json:"rawValue,omitempty"`
	SourceID        string  `json:"sourceId"`
	SourceLabel     string  `json:"sourceLabel"`
	SourceURL       string  `json:"sourceUrl,omitempty"`
	Method          string  `json:"method"`
	MatchMethod     string  `json:"matchMethod,omitempty"`
	MatchConfidence float64 `json:"matchConfidence,omitempty"`
}

func (s *Service) writeLineupSnapshot(lineup LineupRecord, grid *web.GridResponse, evidence ProviderEvidenceResult) error {
	if strings.TrimSpace(s.snapshotDir) == "" || grid == nil {
		return nil
	}
	factsByStation := make(map[string][]ProviderFact)
	for _, fact := range evidence.Facts {
		stationID := strings.TrimSpace(fact.StationID)
		if stationID != "" {
			factsByStation[stationID] = append(factsByStation[stationID], fact)
		}
	}
	capturedAt := s.now().UTC().Format("2006-01-02T15:04:05Z07:00")
	lineup.Status = StatusComplete
	lineup.ChannelCount = len(grid.Channels)
	lineup.ScannedAt = capturedAt
	lineup.LastError = ""
	snapshot := LineupSnapshot{
		SchemaVersion:           CurrentLineupSnapshotVersion,
		CategoryTaxonomyVersion: channelcategory.CurrentVersion,
		CapturedAt:              capturedAt,
		Country:                 lineup.Country,
		PostalCode:              lineup.PostalCode,
		Lineup:                  lineup,
		Sources:                 append([]EvidenceSourceRecord(nil), evidence.Sources...),
		Channels:                make([]LineupSnapshotChannel, 0, len(grid.Channels)),
	}
	for _, channel := range grid.Channels {
		stationID := strings.TrimSpace(channel.ChannelID)
		if stationID == "" {
			continue
		}
		item := LineupSnapshotChannel{
			PositionID: strings.TrimSpace(channel.ID), StationID: stationID,
			Number: strings.TrimSpace(channel.ChannelNo), CallSign: strings.TrimSpace(channel.CallSign),
			AffiliateName: strings.TrimSpace(channel.AffiliateName), AffiliateCallSign: strings.TrimSpace(channel.AffiliateCallSign),
			EventCallSigns: snapshotEventCallSigns(channel.Events),
		}
		categories := make(map[string]bool)
		for _, fact := range factsByStation[stationID] {
			snapshotFact := LineupSnapshotFact{
				Value: strings.TrimSpace(fact.Value), RawValue: strings.TrimSpace(fact.RawValue),
				SourceID: strings.TrimSpace(fact.SourceID), SourceLabel: strings.TrimSpace(fact.SourceLabel),
				SourceURL: strings.TrimSpace(fact.SourceURL), Method: strings.TrimSpace(fact.Method),
				MatchMethod: strings.TrimSpace(fact.MatchMethod), MatchConfidence: fact.MatchConfidence,
			}
			switch fact.Kind {
			case FactAlias:
				item.Aliases = append(item.Aliases, snapshotFact)
			case FactCategory:
				match, ok := channelcategory.Resolve(fact.Value)
				if !ok {
					continue
				}
				snapshotFact.Value = match.Category
				categories[match.Category] = true
				item.CategoryEvidence = append(item.CategoryEvidence, snapshotFact)
			}
		}
		if len(categories) == 1 {
			for category := range categories {
				item.Category = category
			}
		} else if len(categories) > 1 {
			item.CategoryConflict = true
		}
		sort.Slice(item.Aliases, func(i, j int) bool { return item.Aliases[i].Value < item.Aliases[j].Value })
		sort.Slice(item.CategoryEvidence, func(i, j int) bool {
			if item.CategoryEvidence[i].Value != item.CategoryEvidence[j].Value {
				return item.CategoryEvidence[i].Value < item.CategoryEvidence[j].Value
			}
			return item.CategoryEvidence[i].SourceID < item.CategoryEvidence[j].SourceID
		})
		snapshot.Channels = append(snapshot.Channels, item)
	}
	sort.Slice(snapshot.Channels, func(i, j int) bool {
		if snapshot.Channels[i].Number != snapshot.Channels[j].Number {
			return lineupNumberLess(snapshot.Channels[i].Number, snapshot.Channels[j].Number)
		}
		return snapshot.Channels[i].StationID < snapshot.Channels[j].StationID
	})
	return writeLineupSnapshotFile(lineupSnapshotPath(s.snapshotDir, lineup.Country, lineup.PostalCode, lineup.Key), snapshot)
}

func snapshotEventCallSigns(events []web.JSONEvent) []string {
	seen := make(map[string]bool)
	result := make([]string, 0)
	for _, event := range events {
		value := strings.TrimSpace(event.CallSign)
		key := strings.ToUpper(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func lineupSnapshotPath(root, country, postalCode, lineupKey string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(lineupKey)))
	filename := hex.EncodeToString(digest[:12]) + ".json"
	return filepath.Join(root, pathSegment(country, "country"), pathSegment(postalCode, "postal"), filename)
}

func pathSegment(value, fallback string) string {
	var builder strings.Builder
	for _, character := range strings.ToUpper(strings.TrimSpace(value)) {
		if (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-' {
			builder.WriteRune(character)
		}
	}
	if builder.Len() == 0 {
		return fallback
	}
	return builder.String()
}

func writeLineupSnapshotFile(path string, snapshot LineupSnapshot) error {
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding lineup snapshot: %w", err)
	}
	data = append(data, '\n')
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return fmt.Errorf("creating lineup snapshot directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".gracenote-lineup-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temporary lineup snapshot: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("setting lineup snapshot permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("writing lineup snapshot: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("syncing lineup snapshot: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("closing lineup snapshot: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replacing lineup snapshot: %w", err)
	}
	removeTemporary = false
	return nil
}

func lineupNumberLess(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == right {
		return false
	}
	leftParts := strings.Split(left, ".")
	rightParts := strings.Split(right, ".")
	for index := 0; index < len(leftParts) || index < len(rightParts); index++ {
		leftPart := ""
		rightPart := ""
		if index < len(leftParts) {
			leftPart = strings.TrimLeft(leftParts[index], "0")
		}
		if index < len(rightParts) {
			rightPart = strings.TrimLeft(rightParts[index], "0")
		}
		if len(leftPart) != len(rightPart) {
			return len(leftPart) < len(rightPart)
		}
		if leftPart != rightPart {
			return leftPart < rightPart
		}
	}
	return left < right
}
