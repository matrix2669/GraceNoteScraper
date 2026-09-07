package lineuparr

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// AttachDraftSignatures records browser-safe comparison tokens without adding
// workflow metadata to the generated Lineuparr document.
func AttachDraftSignatures(draft *Draft) error {
	if draft == nil {
		return nil
	}
	draft.CustomizationSignature = customizationSignature(draft.Channels)
	signatures, err := SignExport(ExportFromDraft(draft))
	if err != nil {
		return err
	}
	draft.ExportSignatures = signatures
	return nil
}

func customizationSignature(channels []DraftChannel) string {
	rows := make([]string, 0, len(channels))
	for _, channel := range channels {
		rows = append(rows, fmt.Sprintf("%q\x00%t\x00%q", channel.ID, channel.Included, cleanCategory(channel.Category)))
	}
	sort.Strings(rows)
	return hashSignature(rows)
}

// SignExport compares only the three parts of a saved lineup that the editor
// can make stale. It deliberately ignores generated dates and descriptions.
func SignExport(export any) (ExportSignatures, error) {
	data, err := json.Marshal(export)
	if err != nil {
		return ExportSignatures{}, err
	}
	return SignExportJSON(data)
}

func SignExportJSON(data []byte) (ExportSignatures, error) {
	var document struct {
		Categories map[string][]map[string]json.RawMessage `json:"categories"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return ExportSignatures{}, err
	}
	includedRows := make([]string, 0)
	categoryRows := make([]string, 0)
	aliasRows := make([]string, 0)
	rows := make(map[string]ExportRowSignatures)
	for category, channels := range document.Categories {
		for _, channel := range channels {
			identity := canonicalRaw(channel["number"])
			if identity == "null" || identity == `""` {
				identity = "name:" + canonicalRaw(channel["name"])
			}
			identityKey := hashSignature([]string{identity})
			aliasSignature := hashSignature([]string{
				canonicalRaw(channel["name"]),
				canonicalStringList(channel["aliases"]),
				canonicalStringList(channel["epg_ids"]),
				canonicalStringList(channel["excluded_aliases"]),
			})
			includedRows = append(includedRows, identity)
			categoryRows = append(categoryRows, identity+"\x00"+category)
			aliasRows = append(aliasRows, identity+"\x00"+aliasSignature)
			row := rows[identityKey]
			row.Count++
			row.Aliases = append(row.Aliases, aliasSignature)
			row.Categories = append(row.Categories, category)
			rows[identityKey] = row
		}
	}
	for key, row := range rows {
		sort.Strings(row.Aliases)
		sort.Strings(row.Categories)
		rows[key] = row
	}
	sort.Strings(includedRows)
	sort.Strings(categoryRows)
	sort.Strings(aliasRows)
	return ExportSignatures{
		Included:   hashSignature(includedRows),
		Aliases:    hashSignature(aliasRows),
		Categories: hashSignature(categoryRows),
		Rows:       rows,
	}, nil
}

func canonicalRaw(value json.RawMessage) string {
	if len(value) == 0 {
		return "null"
	}
	var decoded any
	if json.Unmarshal(value, &decoded) != nil {
		return string(value)
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return string(value)
	}
	return string(canonical)
}

func canonicalStringList(value json.RawMessage) string {
	var values []string
	if len(value) > 0 && json.Unmarshal(value, &values) == nil {
		sort.Strings(values)
		return strings.Join(values, "\x00")
	}
	return ""
}

func hashSignature(rows []string) string {
	digest := sha256.Sum256([]byte(strings.Join(rows, "\n")))
	return hex.EncodeToString(digest[:])
}
