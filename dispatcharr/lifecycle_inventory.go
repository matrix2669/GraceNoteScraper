package dispatcharr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The v0.32.0 and v0.33.0 adapters follow the respective official upstream API
// contracts, not a cryptographic attestation: a modified server can misreport
// its version. Two consistent traversals detect churn but are not a transaction.
// Callers must fence the receipt to their source, connection, refresh and state.
const (
	LifecycleCoverageUnavailable  = "unavailable"
	LifecycleCoveragePresenceOnly = "presence-only"
	LifecycleCoverageComplete     = "complete-all-retained"
	lifecycleBatchSize            = 256
)

type LifecycleReference struct{ StreamID, AccountID int64 }
type LifecycleAccount struct {
	ID     int64
	Active bool
	Status string
}

// AccountID==0 means an explicitly null manual account, never an omitted field.
type LifecycleRow struct {
	ID, AccountID int64
	Name          string
}
type LifecycleInventoryReceipt struct {
	connection, version, contract, digest, accountDigest, coverage, reason string
	principal                                                              int64
	permission, pages                                                      int
	started, completed                                                     time.Time
	accounts                                                               []LifecycleAccount
	references                                                             []LifecycleReference
	rows, historical                                                       []LifecycleRow
}

func (r LifecycleInventoryReceipt) ConnectionFingerprint() string { return r.connection }
func (r LifecycleInventoryReceipt) ServerVersion() string         { return r.version }
func (r LifecycleInventoryReceipt) CapabilityContract() string    { return r.contract }
func (r LifecycleInventoryReceipt) InventoryDigest() string       { return r.digest }
func (r LifecycleInventoryReceipt) AccountDigest() string         { return r.accountDigest }
func (r LifecycleInventoryReceipt) Coverage() string {
	if r.coverage == "" {
		return LifecycleCoverageUnavailable
	}
	return r.coverage
}
func (r LifecycleInventoryReceipt) DeferredReason() string { return r.reason }
func (r LifecycleInventoryReceipt) PrincipalID() int64     { return r.principal }
func (r LifecycleInventoryReceipt) PermissionLevel() int   { return r.permission }
func (r LifecycleInventoryReceipt) StartedAt() time.Time   { return r.started }
func (r LifecycleInventoryReceipt) CompletedAt() time.Time { return r.completed }
func (r LifecycleInventoryReceipt) PageCount() int         { return r.pages }
func (r LifecycleInventoryReceipt) Accounts() []LifecycleAccount {
	return append([]LifecycleAccount(nil), r.accounts...)
}
func (r LifecycleInventoryReceipt) Streams() []LifecycleRow {
	return append([]LifecycleRow(nil), r.rows...)
}
func (r LifecycleInventoryReceipt) References() []LifecycleReference {
	return append([]LifecycleReference(nil), r.references...)
}
func (r LifecycleInventoryReceipt) HistoricalRows() []LifecycleRow {
	return append([]LifecycleRow(nil), r.historical...)
}

type lifecycleAccountWire struct {
	ID     *int64  `json:"id"`
	Active *bool   `json:"is_active"`
	Status *string `json:"status"`
}
type lifecycleStreamWire struct {
	ID      *int64          `json:"id"`
	Account json.RawMessage `json:"m3u_account"`
	Name    *string         `json:"name"`
}
type lifecyclePageWire struct {
	Count         *int `json:"count"`
	Next, Results json.RawMessage
}
type lifecyclePrincipal struct {
	ID    *int64 `json:"id"`
	Level *int   `json:"user_level"`
}

type lifecycleVersion struct {
	Version string `json:"version"`
}

// encoding/json accepts duplicate keys and matches field names case-insensitively.
// Treat conflicting/ambiguous identity envelopes as malformed, even if one
// occurrence happens to contain a valid value.
func rejectLifecycleDuplicateFields(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return errors.New("invalid lifecycle object")
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return errors.New("invalid lifecycle object")
		}
		key, ok := token.(string)
		if !ok {
			return errors.New("invalid lifecycle object")
		}
		key = strings.ToLower(key)
		if seen[key] {
			return errors.New("duplicate lifecycle field")
		}
		seen[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return errors.New("invalid lifecycle object")
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return errors.New("invalid lifecycle object")
	}
	return nil
}
func (v *lifecycleVersion) UnmarshalJSON(data []byte) error {
	type plain lifecycleVersion
	if err := rejectLifecycleDuplicateFields(data); err != nil {
		return err
	}
	return json.Unmarshal(data, (*plain)(v))
}
func (v *lifecyclePrincipal) UnmarshalJSON(data []byte) error {
	type plain lifecyclePrincipal
	if err := rejectLifecycleDuplicateFields(data); err != nil {
		return err
	}
	return json.Unmarshal(data, (*plain)(v))
}
func (v *lifecycleAccountWire) UnmarshalJSON(data []byte) error {
	type plain lifecycleAccountWire
	if err := rejectLifecycleDuplicateFields(data); err != nil {
		return err
	}
	return json.Unmarshal(data, (*plain)(v))
}
func (v *lifecycleStreamWire) UnmarshalJSON(data []byte) error {
	type plain lifecycleStreamWire
	if err := rejectLifecycleDuplicateFields(data); err != nil {
		return err
	}
	return json.Unmarshal(data, (*plain)(v))
}
func (v *lifecyclePageWire) UnmarshalJSON(data []byte) error {
	type plain lifecyclePageWire
	if err := rejectLifecycleDuplicateFields(data); err != nil {
		return err
	}
	return json.Unmarshal(data, (*plain)(v))
}

type lifecycleCapture struct {
	Version          string
	Principal        int64
	Level            int
	Accounts         []LifecycleAccount
	Rows, Historical []LifecycleRow
	Pages            int
}

// CaptureLifecycleInventory never touches matching, decisions, or persisted
// state. Supported API/version and administrator identity are checked twice;
// inactive accounts permit positive presence evidence, not global absence.
func (c *Client) CaptureLifecycleInventory(ctx context.Context, cfg Config, refs []LifecycleReference) (LifecycleInventoryReceipt, error) {
	r := LifecycleInventoryReceipt{started: time.Now().UTC(), coverage: LifecycleCoverageUnavailable}
	fail := func(err error) (LifecycleInventoryReceipt, error) {
		r.completed = time.Now().UTC()
		r.reason = "Inventory unavailable; choices preserved"
		return r, err
	}
	config, err := cfg.Normalized()
	if err != nil {
		return fail(errors.New("invalid connection configuration"))
	}
	r.connection = config.Fingerprint()
	ids := map[int64]bool{}
	if len(refs) > maxStreamCount {
		return fail(errors.New("too many historical references"))
	}
	for _, ref := range refs {
		if ref.StreamID <= 0 || ref.AccountID < 0 {
			return fail(errors.New("invalid historical reference"))
		}
		ids[ref.StreamID] = true
	}
	r.references = append([]LifecycleReference(nil), refs...)
	sort.Slice(r.references, func(i, j int) bool {
		if r.references[i].StreamID != r.references[j].StreamID {
			return r.references[i].StreamID < r.references[j].StreamID
		}
		return r.references[i].AccountID < r.references[j].AccountID
	})
	ordered := make([]int64, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	a, err := c.captureLifecyclePass(ctx, config, ordered)
	if err != nil {
		return fail(err)
	}
	b, err := c.captureLifecyclePass(ctx, config, ordered)
	if err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	if !bytes.Equal(aa, bb) {
		return fail(errors.New("inventory, principal or account manifest changed between observations"))
	}
	r.version = a.Version
	r.contract = "dispatcharr-upstream-v" + a.Version
	r.principal = a.Principal
	r.permission = a.Level
	r.pages = a.Pages
	r.accounts = a.Accounts
	r.rows = a.Rows
	r.historical = a.Historical
	digest := func(v any) string {
		data, _ := json.Marshal(v)
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:])
	}
	r.digest = digest(a.Rows)
	r.accountDigest = digest(a.Accounts)
	r.coverage = LifecycleCoverageComplete
	for _, account := range a.Accounts {
		if !account.Active || (account.Status != "idle" && account.Status != "success") {
			r.coverage = LifecycleCoveragePresenceOnly
			r.reason = "Inactive or unsettled account scope prevents proving global absence"
			break
		}
	}
	// A completely empty observed inventory can be a transient or catastrophic
	// loss. Preserve choices until a separate policy decides how to handle it.
	if len(a.Rows) == 0 {
		r.coverage = LifecycleCoveragePresenceOnly
		r.reason = "Empty inventory is ambiguous; global absence is unproven"
	}
	r.completed = time.Now().UTC()
	return r, nil
}
func (c *Client) lifecycleIdentity(ctx context.Context, cfg Config) (string, int64, int, error) {
	var v lifecycleVersion
	if err := c.getAuthorizedJSON(ctx, cfg, cfg.BaseURL+"/api/core/version/", &v); err != nil {
		return "", 0, 0, err
	}
	if v.Version != "0.32.0" && v.Version != "0.33.0" {
		return "", 0, 0, errors.New("unsupported Dispatcharr lifecycle API version")
	}
	var p lifecyclePrincipal
	if err := c.getAuthorizedJSON(ctx, cfg, cfg.BaseURL+"/api/accounts/users/me/", &p); err != nil {
		return "", 0, 0, err
	}
	if p.ID == nil || *p.ID <= 0 || p.Level == nil || *p.Level < 10 {
		return "", 0, 0, errors.New("administrator identity not verified")
	}
	return v.Version, *p.ID, *p.Level, nil
}
func (c *Client) captureLifecyclePass(ctx context.Context, cfg Config, ids []int64) (lifecycleCapture, error) {
	var out lifecycleCapture
	v, id, level, err := c.lifecycleIdentity(ctx, cfg)
	if err != nil {
		return out, err
	}
	out.Version, out.Principal, out.Level = v, id, level
	accounts, err := c.lifecycleAccounts(ctx, cfg)
	if err != nil {
		return out, err
	}
	out.Accounts = accounts
	known, active := map[int64]bool{}, map[int64]bool{}
	for _, a := range accounts {
		known[a.ID] = true
		active[a.ID] = a.Active
	}
	seen := map[int64]bool{}
	expected := -1
	ended := false
	for page := 1; page <= maxStreamPages; page++ {
		q := url.Values{"page": {strconv.Itoa(page)}, "page_size": {strconv.Itoa(streamPageSize)}, "hide_stale": {"false"}, "ordering": {"name"}}
		endpoint := cfg.BaseURL + "/api/channels/streams/?" + q.Encode()
		var p lifecyclePageWire
		if err = c.getAuthorizedJSON(ctx, cfg, endpoint, &p); err != nil {
			return lifecycleCapture{}, err
		}
		if p.Count == nil || *p.Count < 0 || *p.Count > maxStreamCount || len(p.Next) == 0 || len(p.Results) == 0 || string(p.Results) == "null" {
			return lifecycleCapture{}, errors.New("incomplete stream page metadata")
		}
		if expected < 0 {
			expected = *p.Count
		} else if expected != *p.Count {
			return lifecycleCapture{}, errors.New("stream count changed between pages")
		}
		var wire []lifecycleStreamWire
		if !utf8.Valid(p.Results) || json.Unmarshal(p.Results, &wire) != nil || len(wire) > streamPageSize {
			return lifecycleCapture{}, errors.New("malformed stream page")
		}
		for _, w := range wire {
			row, e := parseLifecycleRow(w)
			if e != nil || seen[row.ID] || (row.AccountID != 0 && (!known[row.AccountID] || !active[row.AccountID])) {
				return lifecycleCapture{}, errors.New("invalid, duplicate or out-of-scope stream")
			}
			seen[row.ID] = true
			out.Rows = append(out.Rows, row)
			if len(out.Rows) > maxStreamCount {
				return lifecycleCapture{}, errors.New("too many streams")
			}
		}
		out.Pages = page
		if string(p.Next) == "null" {
			if len(out.Rows) != expected {
				return lifecycleCapture{}, errors.New("premature stream pagination termination")
			}
			ended = true
			break
		}
		var next string
		if json.Unmarshal(p.Next, &next) != nil || next == "" || len(wire) == 0 || len(out.Rows) >= expected {
			return lifecycleCapture{}, errors.New("inconsistent stream pagination")
		}
		base, _ := url.Parse(cfg.BaseURL + "/api/channels/streams/")
		parsed, e := url.Parse(next)
		if e != nil {
			return lifecycleCapture{}, errors.New("invalid next page")
		}
		parsed = base.ResolveReference(parsed)
		if parsed.Scheme != base.Scheme || parsed.Host != base.Host || parsed.Path != base.Path || parsed.User != nil || parsed.Fragment != "" || parsed.Query().Get("page") != strconv.Itoa(page+1) {
			return lifecycleCapture{}, errors.New("next page outside expected scope")
		}
	}
	if !ended {
		return lifecycleCapture{}, fmt.Errorf("stream pagination exceeded %d pages", maxStreamPages)
	}
	sort.Slice(out.Rows, func(i, j int) bool { return out.Rows[i].ID < out.Rows[j].ID })
	for start := 0; start < len(ids); start += lifecycleBatchSize {
		end := start + lifecycleBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		rows, e := c.lifecycleByIDs(ctx, cfg, ids[start:end])
		if e != nil {
			return lifecycleCapture{}, e
		}
		out.Historical = append(out.Historical, rows...)
	}
	sort.Slice(out.Historical, func(i, j int) bool { return out.Historical[i].ID < out.Historical[j].ID })
	visible := make(map[int64]LifecycleRow, len(out.Rows))
	for _, row := range out.Rows {
		visible[row.ID] = row
	}
	historical := make(map[int64]bool, len(out.Historical))
	for _, row := range out.Historical {
		historical[row.ID] = true
	}
	for _, id := range ids {
		if _, exists := visible[id]; exists && !historical[id] {
			return lifecycleCapture{}, errors.New("historical lookup omitted a visible stream")
		}
	}
	for _, row := range out.Historical {
		if row.AccountID != 0 && !known[row.AccountID] {
			return lifecycleCapture{}, errors.New("historical stream account outside manifest")
		}
		if row.AccountID == 0 || active[row.AccountID] {
			if listed, ok := visible[row.ID]; !ok || listed != row {
				return lifecycleCapture{}, errors.New("historical lookup conflicts with visible inventory")
			}
		}
	}
	endAccounts, err := c.lifecycleAccounts(ctx, cfg)
	if err != nil {
		return lifecycleCapture{}, err
	}
	beforeAccounts, _ := json.Marshal(out.Accounts)
	afterAccounts, _ := json.Marshal(endAccounts)
	if !bytes.Equal(beforeAccounts, afterAccounts) {
		return lifecycleCapture{}, errors.New("account manifest changed during capture")
	}
	endVersion, endID, endLevel, err := c.lifecycleIdentity(ctx, cfg)
	if err != nil {
		return lifecycleCapture{}, err
	}
	if endVersion != v || endID != id || endLevel != level {
		return lifecycleCapture{}, errors.New("principal or version changed during capture")
	}
	return out, nil
}
func (c *Client) lifecycleAccounts(ctx context.Context, cfg Config) ([]LifecycleAccount, error) {
	var raw json.RawMessage
	if err := c.getAuthorizedJSON(ctx, cfg, cfg.BaseURL+"/api/m3u/accounts/", &raw); err != nil {
		return nil, err
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '[' {
		return nil, errors.New("account manifest shape not verified")
	}
	var wire []lifecycleAccountWire
	if !utf8.Valid(raw) || json.Unmarshal(raw, &wire) != nil || len(wire) > maxStreamCount {
		return nil, errors.New("malformed or oversized account manifest")
	}
	seen := map[int64]bool{}
	accounts := make([]LifecycleAccount, 0, len(wire))
	for _, a := range wire {
		if a.ID == nil || *a.ID <= 0 || a.Active == nil || a.Status == nil || !validLifecycleStatus(*a.Status) || seen[*a.ID] {
			return nil, errors.New("invalid or duplicate account identity")
		}
		seen[*a.ID] = true
		accounts = append(accounts, LifecycleAccount{*a.ID, *a.Active, *a.Status})
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })
	return accounts, nil
}
func parseLifecycleRow(w lifecycleStreamWire) (LifecycleRow, error) {
	if w.ID == nil || *w.ID <= 0 || w.Name == nil || strings.TrimSpace(*w.Name) == "" || len(*w.Name) > maxStreamNameSize || len(w.Account) == 0 {
		return LifecycleRow{}, errors.New("invalid safe stream identity")
	}
	row := LifecycleRow{ID: *w.ID, Name: *w.Name}
	if string(w.Account) != "null" {
		if json.Unmarshal(w.Account, &row.AccountID) != nil || row.AccountID <= 0 {
			return LifecycleRow{}, errors.New("invalid account identity")
		}
	}
	return row, nil
}

// by-ids is a read-only POST action in both supported upstream versions. It
// bypasses the active-account queryset but can prove only presence of known IDs.
func (c *Client) lifecycleByIDs(ctx context.Context, cfg Config, ids []int64) ([]LifecycleRow, error) {
	payload, _ := json.Marshal(map[string][]int64{"ids": ids})
	endpoint := cfg.BaseURL + "/api/channels/streams/by-ids/"
	var status int
	var data []byte
	var err error
	if cfg.AuthMethod == AuthAPIKey {
		status, data, err = c.requestWithAuth(ctx, http.MethodPost, endpoint, "", cfg.APIKey, payload, maxStreamResponseSize)
	} else {
		var token string
		token, err = c.accessToken(ctx, cfg)
		if err == nil {
			status, data, err = c.request(ctx, http.MethodPost, endpoint, token, payload, maxStreamResponseSize)
		}
		if err == nil && status == http.StatusUnauthorized {
			token, err = c.renewToken(ctx, cfg)
			if err == nil {
				status, data, err = c.request(ctx, http.MethodPost, endpoint, token, payload, maxStreamResponseSize)
			}
		}
	}
	if err != nil || status < 200 || status >= 300 {
		return nil, errors.New("historical stream lookup unavailable")
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '[' {
		return nil, errors.New("historical stream response malformed")
	}
	var wire []lifecycleStreamWire
	if !utf8.Valid(data) || json.Unmarshal(data, &wire) != nil || len(wire) > len(ids) {
		return nil, errors.New("historical stream response malformed")
	}
	requested := map[int64]bool{}
	for _, id := range ids {
		requested[id] = true
	}
	seen := map[int64]bool{}
	rows := make([]LifecycleRow, 0, len(wire))
	for _, w := range wire {
		row, e := parseLifecycleRow(w)
		if e != nil || !requested[row.ID] || seen[row.ID] {
			return nil, errors.New("historical stream identity invalid or unexpected")
		}
		seen[row.ID] = true
		rows = append(rows, row)
	}
	return rows, nil
}

func validLifecycleStatus(s string) bool {
	switch s {
	case "idle", "fetching", "parsing", "error", "success", "pending_setup", "disabled":
		return true
	}
	return false
}
