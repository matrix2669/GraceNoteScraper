package lineuparr

import "time"

const CurrentStateVersion = 2

// LineupContext describes the active Gracenote provider without coupling this
// package to the application's persisted configuration type.
type LineupContext struct {
	SourceFingerprint string
	Country           string
	PostalCode        string
	ProviderName      string
	LineupID          string
	AdditionalSources []SourceStatus
}

// InputChannel is one provider lineup position. Multiple positions may point
// at the same Gracenote station ID; keeping them separate is what lets the user
// decide whether an SD/HD pair should remain in the export.
type InputChannel struct {
	Key              string
	StationID        string
	PlacementID      string
	Number           string
	CallSign         string
	Affiliate        string
	EventCallSigns   []string
	PreferredName    *AttributedAlias
	CategoryHint     *AttributedCategory
	CategoryConflict bool
	ExternalAliases  []AttributedAlias
}

type AttributedAlias struct {
	Value  string
	Source string
	Method string
}

type AttributedCategory struct {
	Value  string
	Source string
	Label  string
	Method string
}

type AliasEvidence struct {
	Value   string   `json:"value"`
	Sources []string `json:"sources"`
	Methods []string `json:"methods"`
}

type IdentifierEvidence struct {
	Value   string   `json:"value"`
	Sources []string `json:"sources"`
	Methods []string `json:"methods"`
}

type DraftChannel struct {
	ID                      string               `json:"id"`
	StationID               string               `json:"stationId,omitempty"`
	PlacementID             string               `json:"placementId,omitempty"`
	Number                  string               `json:"number"`
	Name                    string               `json:"name"`
	OriginalName            string               `json:"originalName"`
	CallSign                string               `json:"callSign,omitempty"`
	Affiliate               string               `json:"affiliate,omitempty"`
	Category                string               `json:"category"`
	Included                bool                 `json:"included"`
	Aliases                 []string             `json:"aliases,omitempty"`
	AliasEvidence           []AliasEvidence      `json:"aliasEvidence,omitempty"`
	SuppressedAliasEvidence []AliasEvidence      `json:"suppressedAliasEvidence,omitempty"`
	EPGIDs                  []string             `json:"epgIds,omitempty"`
	EPGIDEvidence           []IdentifierEvidence `json:"epgIdEvidence,omitempty"`
	NameSource              string               `json:"nameSource"`
	NameMethod              string               `json:"nameMethod"`
	CategorySource          string               `json:"categorySource"`
	CategoryMethod          string               `json:"categoryMethod,omitempty"`
	MatchedSources          []string             `json:"matchedSources,omitempty"`
	DuplicateOf             string               `json:"duplicateOf,omitempty"`
	DuplicateReason         string               `json:"duplicateReason,omitempty"`
}

type DuplicateSuggestion struct {
	RemoveID     string `json:"removeId"`
	RemoveNumber string `json:"removeNumber"`
	RemoveName   string `json:"removeName"`
	KeepID       string `json:"keepId"`
	KeepNumber   string `json:"keepNumber"`
	KeepName     string `json:"keepName"`
	Reason       string `json:"reason"`
}

type SourceStatus struct {
	ID           string        `json:"id"`
	Label        string        `json:"label"`
	URL          string        `json:"url,omitempty"`
	Status       string        `json:"status"`
	Access       string        `json:"access,omitempty"`
	LocationMode string        `json:"locationMode,omitempty"`
	Matched      int           `json:"matched"`
	Ambiguous    int           `json:"ambiguous,omitempty"`
	Message      string        `json:"message,omitempty"`
	RelatedIDs   []string      `json:"relatedIds,omitempty"`
	Matches      []SourceMatch `json:"matches,omitempty"`
}

type SourceMatch struct {
	ChannelID string   `json:"channelId"`
	Number    string   `json:"number"`
	CallSign  string   `json:"callSign,omitempty"`
	Name      string   `json:"name"`
	Category  string   `json:"category,omitempty"`
	Aliases   []string `json:"aliases,omitempty"`
	EPGIDs    []string `json:"epgIds,omitempty"`
	Methods   []string `json:"methods,omitempty"`
}

type Draft struct {
	GeneratedAt          time.Time             `json:"generatedAt"`
	Package              string                `json:"package"`
	ProviderName         string                `json:"providerName"`
	PostalCode           string                `json:"postalCode"`
	LineupID             string                `json:"lineupId"`
	CountryCode          string                `json:"countryCode"`
	Channels             []DraftChannel        `json:"channels"`
	DuplicateSuggestions []DuplicateSuggestion `json:"duplicateSuggestions"`
	Sources              []SourceStatus        `json:"sources"`
	Categories           []string              `json:"categories"`
	Total                int                   `json:"total"`
	Included             int                   `json:"included"`
	Excluded             int                   `json:"excluded"`
	AliasCount           int                   `json:"aliasCount"`
	Categorized          int                   `json:"categorized"`
	Uncategorized        int                   `json:"uncategorized"`
}

type ChannelUpdate struct {
	Included *bool   `json:"included,omitempty"`
	Category *string `json:"category,omitempty"`
}

type ChannelOverride struct {
	Included          *bool    `json:"included,omitempty"`
	Category          string   `json:"category,omitempty"`
	SuppressedAliases []string `json:"suppressedAliases,omitempty"`
}

type MatchDecision struct {
	Key                    string    `json:"key"`
	Decision               string    `json:"decision"`
	DispatcharrFingerprint string    `json:"dispatcharrFingerprint"`
	StreamFingerprint      string    `json:"streamFingerprint"`
	StreamKey              string    `json:"streamKey"`
	StreamID               int64     `json:"streamId"`
	M3UAccountID           int64     `json:"m3uAccountId"`
	ChannelID              string    `json:"channelId"`
	ChannelNumber          string    `json:"channelNumber"`
	ChannelName            string    `json:"channelName"`
	StreamName             string    `json:"streamName"`
	NormalizedAlias        string    `json:"normalizedAlias,omitempty"`
	TVGID                  string    `json:"tvgId,omitempty"`
	Score                  int       `json:"score"`
	Reason                 string    `json:"reason"`
	UpdatedAt              time.Time `json:"updatedAt"`
}

type State struct {
	Version           int                        `json:"version"`
	SourceFingerprint string                     `json:"sourceFingerprint"`
	Channels          map[string]ChannelOverride `json:"channels,omitempty"`
	MatchDecisions    map[string]MatchDecision   `json:"matchDecisions,omitempty"`
}

type ExportFile struct {
	Package     string                     `json:"package"`
	Date        string                     `json:"date"`
	Description string                     `json:"description"`
	Source      string                     `json:"source"`
	Notes       string                     `json:"notes,omitempty"`
	Categories  map[string][]ExportChannel `json:"categories"`
}

type ExportChannel struct {
	Name    string   `json:"name"`
	Number  any      `json:"number"`
	Aliases []string `json:"aliases,omitempty"`
	EPGIDs  []string `json:"epg_ids,omitempty"`
}
