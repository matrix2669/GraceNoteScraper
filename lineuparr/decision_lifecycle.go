package lineuparr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
)

// LifecycleState is private persisted reconciliation evidence and append-only
// audit. Literal names are hashed in baseline evidence, not rewritten in choices.
type LifecycleState struct {
	Scope        string                       `json:"scope,omitempty"`
	ProcessEpoch string                       `json:"processEpoch,omitempty"`
	Continuity   []LifecycleContinuityArchive `json:"continuity,omitempty"`
	Baseline     lifecycleNames               `json:"baseline,omitempty"`
	BootAt       time.Time                    `json:"bootAt,omitempty"`
	Bootstrapped bool                         `json:"bootstrapped,omitempty"`
	LastReceipt  string                       `json:"lastReceipt,omitempty"`
	HoldLost     []string                     `json:"holdLost,omitempty"`
	HoldEpoch    uint64                       `json:"holdEpoch,omitempty"`
	Pending      map[string]lifecyclePending  `json:"pending,omitempty"`
	Audit        []LifecycleAudit             `json:"audit,omitempty"`
}
type LifecycleContinuityArchive struct {
	Reason, ProcessEpoch, Scope string
	At                          time.Time
	Pending                     map[string]lifecyclePending `json:"pending,omitempty"`
	HoldLost                    []string                    `json:"holdLost,omitempty"`
	HoldEpoch                   uint64                      `json:"holdEpoch,omitempty"`
}
type lifecycleNames struct {
	Global   []string            `json:"global,omitempty"`
	Accounts map[string][]string `json:"accounts,omitempty"`
}
type lifecyclePending struct {
	First        time.Time `json:"first"`
	Count        int       `json:"count"`
	LastReceipt  string    `json:"lastReceipt"`
	Catastrophic bool      `json:"catastrophic"`
	Epoch        uint64    `json:"epoch,omitempty"`
}
type LifecycleAudit struct {
	BatchID, Source, Connection, Contract, ServerVersion, InventoryDigest, AccountDigest, ReferenceDigest, HistoricalDigest, Backup, PreStateHash, Reason string
	PrincipalID                                                                                                                                           int64
	PermissionLevel                                                                                                                                       int
	CapturedFrom, CapturedUntil                                                                                                                           time.Time
	At                                                                                                                                                    time.Time
	Primary                                                                                                                                               map[string]MatchDecision        `json:"primary,omitempty"`
	Supplemental                                                                                                                                          map[string]SupplementalDecision `json:"supplemental,omitempty"`
}
type LifecycleResult struct {
	Status          string    `json:"status"`
	Reason          string    `json:"reason,omitempty"`
	Retired         int       `json:"retired"`
	Retained        int       `json:"retained"`
	Deferred        int       `json:"deferred"`
	LastObservation time.Time `json:"lastObservation,omitempty"`
}

type lifecycleCandidate struct {
	key          string
	primary      *MatchDecision
	supplemental *SupplementalDecision
}

func lifecycleValidFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func lifecycleName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}
func lifecycleHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func lifecycleInventoryNames(rows []dispatcharr.LifecycleRow) lifecycleNames {
	global := map[string]bool{}
	accounts := map[string]map[string]bool{}
	for _, r := range rows {
		name := lifecycleName(r.Name)
		if name == "" {
			continue
		}
		h := lifecycleHash(name)
		global[h] = true
		a := fmt.Sprint(r.AccountID)
		if accounts[a] == nil {
			accounts[a] = map[string]bool{}
		}
		accounts[a][h] = true
	}
	result := lifecycleNames{Accounts: map[string][]string{}}
	for h := range global {
		result.Global = append(result.Global, h)
	}
	sort.Strings(result.Global)
	for a, set := range accounts {
		for h := range set {
			result.Accounts[a] = append(result.Accounts[a], h)
		}
		sort.Strings(result.Accounts[a])
	}
	return result
}
func lifecycleLoss(base, current lifecycleNames) ([]string, bool) {
	present := map[string]bool{}
	for _, h := range current.Global {
		present[h] = true
	}
	lost := []string{}
	count := 0
	for _, h := range base.Global {
		if !present[h] {
			lost = append(lost, "g:"+h)
			count++
		}
	}
	catastrophic := len(base.Global) > 0 && count*2 >= len(base.Global)
	for account, names := range base.Accounts {
		p := map[string]bool{}
		for _, h := range current.Accounts[account] {
			p[h] = true
		}
		n := 0
		for _, h := range names {
			if !p[h] {
				lost = append(lost, "a:"+account+":"+h)
				n++
			}
		}
		if len(names) > 0 && n*2 >= len(names) {
			catastrophic = true
		}
	}
	sort.Strings(lost)
	return lost, catastrophic
}
func lifecycleWorsened(previous, current []string) bool {
	old := map[string]bool{}
	for _, v := range previous {
		old[v] = true
	}
	for _, v := range current {
		if !old[v] {
			return true
		}
	}
	return false
}
func lifecycleScope(source, connection, contract string) string {
	return lifecycleHash(source + "\x00" + connection + "\x00" + contract)
}

// lifecycleCandidates is deliberately conservative. Primary identity is the
// source-scoped ChannelID contract; supplemental identity has the stronger
// stored station/placement/callsign tuple. Existing physical IDs and any same
// literal name across accounts win over absence; denials are never candidates.
func lifecycleCandidates(state State, source, connection string, targets []DraftChannel, rows, historical []dispatcharr.LifecycleRow, refs []dispatcharr.LifecycleReference) []lifecycleCandidate {
	if state.SourceFingerprint != source {
		return nil
	}
	targetMap := map[string]DraftChannel{}
	duplicate := map[string]bool{}
	for _, t := range targets {
		if t.ID == "" {
			continue
		}
		if _, ok := targetMap[t.ID]; ok {
			duplicate[t.ID] = true
		}
		targetMap[t.ID] = t
	}
	existsID := map[int64]bool{}
	names := map[string]bool{}
	for _, r := range rows {
		existsID[r.ID] = true
		names[lifecycleName(r.Name)] = true
	}
	for _, r := range historical {
		existsID[r.ID] = true
	}
	covered := map[string]bool{}
	for _, r := range refs {
		covered[fmt.Sprintf("%d:%d", r.AccountID, r.StreamID)] = true
	}
	denied := map[string]bool{}
	for _, d := range state.MatchDecisions {
		if d.Decision == "denied" && d.DispatcharrFingerprint == connection && lifecycleName(d.StreamName) != "" {
			denied[d.ChannelID+"\x00"+lifecycleName(d.StreamName)] = true
		}
	}
	for _, d := range state.SupplementalDecisions {
		if d.Decision == "denied" && d.DispatcharrFingerprint == connection && d.SourceFingerprint == source && lifecycleName(d.StreamName) != "" {
			denied[d.ChannelID+"\x00"+lifecycleName(d.StreamName)] = true
		}
	}
	valid := func(channel, name string, id, account int64) bool {
		t, ok := targetMap[channel]
		return ok && !duplicate[channel] && t.Included && channel != "" && strings.TrimSpace(t.StationID) != "" && id > 0 && account >= 0 && lifecycleName(name) != "" && covered[fmt.Sprintf("%d:%d", account, id)] && !existsID[id] && !names[lifecycleName(name)] && !denied[channel+"\x00"+lifecycleName(name)]
	}
	out := []lifecycleCandidate{}
	for key, d := range state.MatchDecisions {
		if d.Key != key || d.Decision != "confirmed" || d.DispatcharrFingerprint != connection || d.StreamKey != fmt.Sprintf("%d:%d", d.M3UAccountID, d.StreamID) || !lifecycleValidFingerprint(d.StreamFingerprint) || !valid(d.ChannelID, d.StreamName, d.StreamID, d.M3UAccountID) {
			continue
		}
		copy := d
		out = append(out, lifecycleCandidate{key: "primary:" + key, primary: &copy})
	}
	for key, d := range state.SupplementalDecisions {
		t, ok := targetMap[d.ChannelID]
		if !ok || d.Key != key || d.Key != SupplementalKey(d) || d.Origin != SupplementalOrigin || d.GroupKey == "" || d.OperationID == "" || d.AdmittedSnapshot == "" || d.RetrievalVersion == "" || d.Decision != "confirmed" || d.SourceFingerprint != source || d.DispatcharrFingerprint != connection || d.StationID == "" || d.PlacementID == "" || d.RawCallSign == "" || t.StationID != d.StationID || t.PlacementID != d.PlacementID || t.CallSign != d.RawCallSign || !lifecycleValidFingerprint(d.StreamFingerprint) || !valid(d.ChannelID, d.StreamName, d.StreamID, d.M3UAccountID) {
			continue
		}
		copy := d
		out = append(out, lifecycleCandidate{key: "supplemental:" + key, supplemental: &copy})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// lifecyclePlan computes a new evidence state without mutating choices. The
// caller persists it under the full state fence with any archive/removal.
func lifecyclePlan(old LifecycleState, scope, receiptID string, now time.Time, names lifecycleNames, candidates []lifecycleCandidate) (LifecycleState, []lifecycleCandidate, LifecycleResult) {
	next := old
	result := LifecycleResult{Status: "deferred", LastObservation: now}
	if scope == "" || receiptID == "" || len(names.Global) == 0 {
		result.Reason = "Inventory evidence is incomplete or empty"
		return old, nil, result
	}
	if old.Scope == scope && old.Bootstrapped && len(old.Pending) == 0 && len(candidates) == 0 && lifecycleStateHash(old.Baseline) == lifecycleStateHash(names) {
		result.Status = "current"
		result.Reason = "No lifecycle changes"
		return old, nil, result
	}
	if old.Scope != scope {
		next = LifecycleState{Scope: scope, Baseline: names, BootAt: now, Pending: map[string]lifecyclePending{}, Audit: old.Audit, Continuity: old.Continuity}
	}
	if next.LastReceipt == receiptID {
		result.Reason = "This observation was already processed"
		return old, nil, result
	}
	if next.Pending == nil {
		next.Pending = map[string]lifecyclePending{}
	} else {
		copy := make(map[string]lifecyclePending, len(next.Pending))
		for k, v := range next.Pending {
			copy[k] = v
		}
		next.Pending = copy
	}
	lost, catastrophic := lifecycleLoss(next.Baseline, names)
	if old.Scope == scope && old.Bootstrapped && catastrophic && len(old.Pending) == 0 && len(candidates) == 0 {
		result.Reason = "Inventory drop remains on hold; no eligible choices"
		return old, nil, result
	}
	hasPriorHold := false
	for _, pending := range next.Pending {
		hasPriorHold = hasPriorHold || pending.Catastrophic
	}
	if catastrophic || hasPriorHold {
		if next.HoldEpoch == 0 || lifecycleWorsened(next.HoldLost, lost) {
			next.HoldEpoch++
		}
		next.HoldLost = append([]string(nil), lost...)
	}
	active := map[string]bool{}
	retire := []lifecycleCandidate{}
	for _, c := range candidates {
		active[c.key] = true
		p, exists := next.Pending[c.key]
		if catastrophic || (exists && p.Catastrophic) {
			if !exists || !p.Catastrophic || p.Epoch != next.HoldEpoch {
				p = lifecyclePending{First: now, Count: 1, LastReceipt: receiptID, Catastrophic: true, Epoch: next.HoldEpoch}
			} else if p.LastReceipt != receiptID {
				if p.Count < 3 {
					p.Count++
				}
				p.LastReceipt = receiptID
			}
			next.Pending[c.key] = p
			if p.Count >= 3 && now.Sub(p.First) >= 5*time.Minute {
				retire = append(retire, c)
			}
			continue
		}
		if !next.Bootstrapped || exists {
			if !exists || p.Catastrophic {
				p = lifecyclePending{First: now, Count: 1, LastReceipt: receiptID}
			} else if p.LastReceipt != receiptID {
				if p.Count < 2 {
					p.Count++
				}
				p.LastReceipt = receiptID
			}
			next.Pending[c.key] = p
			if p.Count >= 2 && now.Sub(p.First) >= time.Minute {
				retire = append(retire, c)
			}
			continue
		}
		retire = append(retire, c)
	}
	for k := range next.Pending {
		if !active[k] {
			delete(next.Pending, k)
		}
	}
	if !catastrophic && !next.Bootstrapped && now.Sub(next.BootAt) >= time.Minute {
		next.Bootstrapped = true
	}
	for _, c := range retire {
		delete(next.Pending, c.key)
	}
	held := len(next.Pending) > 0
	heldCatastrophic := false
	for _, pending := range next.Pending {
		heldCatastrophic = heldCatastrophic || pending.Catastrophic
	}
	if !heldCatastrophic && !catastrophic {
		next.HoldLost = nil
		next.HoldEpoch = 0
	}
	if !catastrophic && !held && next.Bootstrapped {
		next.Baseline = names
	}
	next.LastReceipt = receiptID
	result.Deferred = len(candidates) - len(retire)
	result.Retired = len(retire)
	result.Status = "current"
	if result.Deferred > 0 {
		result.Status = "deferred"
		result.Reason = "Awaiting additional complete observations"
	}
	return next, retire, result
}

func lifecycleStateHash(v any) string { b, _ := json.Marshal(v); return lifecycleHash(string(b)) }

// LifecycleReferences snapshots candidate identities. The caller must fence the
// matching state token across network capture and final reconciliation.
func (s *Service) LifecycleReferences(source string) []dispatcharr.LifecycleReference {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	if s.store.state.SourceFingerprint != source {
		return nil
	}
	refs := []dispatcharr.LifecycleReference{}
	for _, d := range s.store.state.MatchDecisions {
		if d.Decision == "confirmed" && d.StreamID > 0 && d.M3UAccountID >= 0 {
			refs = append(refs, dispatcharr.LifecycleReference{StreamID: d.StreamID, AccountID: d.M3UAccountID})
		}
	}
	for _, d := range s.store.state.SupplementalDecisions {
		if d.Decision == "confirmed" && d.StreamID > 0 && d.M3UAccountID >= 0 {
			refs = append(refs, dispatcharr.LifecycleReference{StreamID: d.StreamID, AccountID: d.M3UAccountID})
		}
	}
	return refs
}

// reconcileLifecycle computes proposed state on an isolated projection store.
// It performs no filesystem writes; CommitLifecycle owns durable publication.
func (s *Service) reconcileLifecycle(ctx context.Context, source, connection, token, operation, processEpoch string, started, timeNow time.Time, targets []DraftChannel, receipt dispatcharr.LifecycleInventoryReceipt) (LifecycleResult, []string, error) {
	result := LifecycleResult{Status: "deferred", Reason: "Inventory is incomplete; choices preserved"}
	if err := ctx.Err(); err != nil {
		return result, nil, err
	}
	if receipt.Coverage() != dispatcharr.LifecycleCoverageComplete || receipt.ConnectionFingerprint() != connection || receipt.CapabilityContract() == "" || receipt.PrincipalID() <= 0 || receipt.PermissionLevel() < 10 || receipt.StartedAt().Before(started) || receipt.CompletedAt().Before(receipt.StartedAt()) || receipt.CompletedAt().After(timeNow) || timeNow.Sub(receipt.CompletedAt()) > time.Minute || operation == "" || processEpoch == "" {
		return result, nil, nil
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if s.store.reviewTokenLocked() != token {
		return result, nil, ErrStaleReview
	}
	unbound := s.store.state.SourceFingerprint == "" && len(s.store.state.MatchDecisions) == 0 && len(s.store.state.SupplementalDecisions) == 0 && len(s.store.state.Channels) == 0
	if s.store.state.SourceFingerprint != source && !unbound {
		return result, nil, ErrStaleReview
	}
	previous := s.store.state
	old := LifecycleState{}
	if previous.Lifecycle != nil {
		data, _ := json.Marshal(previous.Lifecycle)
		_ = json.Unmarshal(data, &old)
	}
	if !validLifecycleState(old) {
		return LifecycleResult{Status: "deferred", Reason: "Stored lifecycle evidence is invalid; choices preserved"}, nil, errors.New("invalid lifecycle evidence")
	}
	scope := lifecycleScope(source, connection, receipt.CapabilityContract())
	if old.Scope != "" && (old.Scope != scope || old.ProcessEpoch != processEpoch) {
		archiveLifecyclePending(&old, "scope or process continuity changed", timeNow)
		if old.Scope == scope {
			old.BootAt = timeNow
			old.Bootstrapped = false
			old.LastReceipt = ""
		}
	}
	candidates := lifecycleCandidates(previous, source, connection, targets, receipt.Streams(), receipt.HistoricalRows(), receipt.References())
	names := lifecycleInventoryNames(receipt.Streams())
	receiptID := lifecycleStateHash(struct {
		Connection, Contract, Inventory, Accounts string
		Started, Completed                        time.Time
		References                                []dispatcharr.LifecycleReference
		Historical                                []dispatcharr.LifecycleRow
	}{connection, receipt.CapabilityContract(), receipt.InventoryDigest(), receipt.AccountDigest(), receipt.StartedAt(), receipt.CompletedAt(), receipt.References(), receipt.HistoricalRows()})
	next, retire, result := lifecyclePlan(old, scope, receiptID, timeNow, names, candidates)
	next.ProcessEpoch = processEpoch
	candidateKeys := map[string]bool{}
	for _, candidate := range candidates {
		candidateKeys[candidate.key] = true
	}
	presentIDs, presentNames := map[int64]bool{}, map[string]bool{}
	for _, row := range receipt.Streams() {
		presentIDs[row.ID] = true
		presentNames[lifecycleName(row.Name)] = true
	}
	for _, row := range receipt.HistoricalRows() {
		presentIDs[row.ID] = true
	}
	count := func(key, choice, name string, id int64) {
		if choice != "confirmed" {
			return
		}
		if presentIDs[id] || (lifecycleName(name) != "" && presentNames[lifecycleName(name)]) {
			result.Retained++
		} else if !candidateKeys[key] {
			result.Deferred++
		}
	}
	for key, decision := range previous.MatchDecisions {
		count("primary:"+key, decision.Decision, decision.StreamName, decision.StreamID)
	}
	for key, decision := range previous.SupplementalDecisions {
		count("supplemental:"+key, decision.Decision, decision.StreamName, decision.StreamID)
	}
	if result.Deferred > 0 && result.Status == "current" {
		result.Status = "deferred"
		result.Reason = "Some choices have unresolved scope or require further observations"
	}
	if lifecycleStateHash(next) == lifecycleStateHash(old) {
		return result, nil, nil
	}
	var backup, preHash string
	if len(retire) > 0 {
		var err error
		backup, preHash, err = lifecycleBackupIdentity(previous)
		if err != nil {
			return LifecycleResult{Status: "deferred", Reason: "Backup failed; choices preserved"}, nil, err
		}
	}
	nextState := cloneReviewState(previous)
	if unbound {
		nextState.SourceFingerprint = source
	}
	keys := []string{}
	if len(retire) > 0 {
		audit := LifecycleAudit{BatchID: operation, Source: source, Connection: connection, Contract: receipt.CapabilityContract(), ServerVersion: receipt.ServerVersion(), PrincipalID: receipt.PrincipalID(), PermissionLevel: receipt.PermissionLevel(), InventoryDigest: receipt.InventoryDigest(), AccountDigest: receipt.AccountDigest(), ReferenceDigest: lifecycleStateHash(receipt.References()), HistoricalDigest: lifecycleStateHash(receipt.HistoricalRows()), Backup: backup, PreStateHash: preHash, Reason: "Original physical stream and matching literal name absent from validated retained inventory", CapturedFrom: receipt.StartedAt(), CapturedUntil: receipt.CompletedAt(), At: timeNow, Primary: map[string]MatchDecision{}, Supplemental: map[string]SupplementalDecision{}}
		for _, c := range retire {
			if c.primary != nil {
				k := strings.TrimPrefix(c.key, "primary:")
				audit.Primary[k] = *c.primary
				delete(nextState.MatchDecisions, k)
				keys = append(keys, k)
			} else {
				k := strings.TrimPrefix(c.key, "supplemental:")
				audit.Supplemental[k] = *c.supplemental
				delete(nextState.SupplementalDecisions, k)
				keys = append(keys, k)
			}
		}
		next.Audit = append(next.Audit, audit)
	}
	nextState.Lifecycle = &next
	if err := ctx.Err(); err != nil {
		return LifecycleResult{Status: "deferred", Reason: "Refresh cancelled; choices preserved"}, nil, err
	}
	s.store.state = nextState
	return result, keys, nil
}

func (s *StateStore) writeLifecycleBackup(state State) (string, string, error) {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return "", "", err
	}
	data = append(data, '\n')
	current, err := os.ReadFile(s.path)
	if err != nil || !bytes.Equal(current, data) {
		return "", "", errors.New("current state differs from backup input")
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	name := "lifecycle-" + digest + ".json"
	dir := filepath.Join(filepath.Dir(s.path), "lifecycle-backups")
	if err = s.lifecycleBackupStage("create"); err != nil {
		return "", "", err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", "", err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return "", "", errors.New("insecure lifecycle backup directory")
	}
	path := filepath.Join(dir, name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			return "", "", errors.New("insecure lifecycle backup")
		}
		if e := s.lifecycleBackupStage("verify"); e != nil {
			return "", "", e
		}
		existing, e := os.ReadFile(path)
		if e != nil || !bytes.Equal(existing, data) {
			return "", "", errors.New("lifecycle backup conflict")
		}
		existingFile, e := os.Open(path)
		if e != nil {
			return "", "", e
		}
		if e = existingFile.Sync(); e != nil {
			_ = existingFile.Close()
			return "", "", e
		}
		if e = existingFile.Close(); e != nil {
			return "", "", e
		}
	} else if err != nil {
		return "", "", err
	} else {
		e := s.lifecycleBackupStage("write")
		n := 0
		if e == nil {
			n, e = file.Write(data)
		}
		if e == nil && n != len(data) {
			e = io.ErrShortWrite
		}
		if e == nil {
			e = s.lifecycleBackupStage("sync")
		}
		if e == nil {
			e = file.Sync()
		}
		closeErr := file.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			_ = os.Remove(path)
			return "", "", e
		}
		if e = s.lifecycleBackupStage("verify"); e != nil {
			return "", "", e
		}
		check, e := os.ReadFile(path)
		if e != nil || !bytes.Equal(check, data) {
			return "", "", errors.New("lifecycle backup verification failed")
		}
	}
	directory, err := os.Open(dir)
	if err != nil {
		return "", "", err
	}
	if err = directory.Sync(); err != nil {
		_ = directory.Close()
		return "", "", err
	}
	if err = directory.Close(); err != nil {
		return "", "", err
	}
	return name, digest, nil
}

// Invalid or corrupted persisted evidence must never turn into authority.
func validLifecycleState(state LifecycleState) bool {
	if state.Scope == "" {
		return !state.Bootstrapped && state.BootAt.IsZero() && state.LastReceipt == "" && len(state.Pending) == 0 && len(state.Baseline.Global) == 0 && len(state.Baseline.Accounts) == 0
	}
	if !lifecycleValidFingerprint(state.Scope) || state.BootAt.IsZero() || len(state.Baseline.Global) == 0 || len(state.Baseline.Global) > 100000 || len(state.Baseline.Accounts) > 100000 || len(state.Pending) > 100000 || len(state.HoldLost) > 200000 {
		return false
	}
	if state.LastReceipt != "" && !lifecycleValidFingerprint(state.LastReceipt) {
		return false
	}
	validSet := func(values []string) bool {
		if len(values) > 100000 {
			return false
		}
		previous := ""
		for _, v := range values {
			if !lifecycleValidFingerprint(v) || v <= previous {
				return false
			}
			previous = v
		}
		return true
	}
	if !validSet(state.Baseline.Global) {
		return false
	}
	union := map[string]bool{}
	for account, values := range state.Baseline.Accounts {
		id, err := strconv.ParseInt(account, 10, 64)
		if err != nil || id < 0 || strconv.FormatInt(id, 10) != account || !validSet(values) {
			return false
		}
		for _, value := range values {
			union[value] = true
		}
	}
	if len(union) != len(state.Baseline.Global) {
		return false
	}
	for _, value := range state.Baseline.Global {
		if !union[value] {
			return false
		}
	}
	previousLoss := ""
	for _, value := range state.HoldLost {
		if value <= previousLoss {
			return false
		}
		previousLoss = value
		parts := strings.Split(value, ":")
		if len(parts) == 2 && parts[0] == "g" {
			if !lifecycleValidFingerprint(parts[1]) {
				return false
			}
			continue
		}
		if len(parts) != 3 || parts[0] != "a" || !lifecycleValidFingerprint(parts[2]) {
			return false
		}
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || id < 0 || strconv.FormatInt(id, 10) != parts[1] {
			return false
		}
	}
	for key, p := range state.Pending {
		if key == "" || p.First.IsZero() || p.Count < 1 || p.Count > 3 || !lifecycleValidFingerprint(p.LastReceipt) || (p.Catastrophic && (p.Epoch == 0 || p.Epoch > state.HoldEpoch)) {
			return false
		}
	}
	return true
}

// InvalidateLifecycleContinuity discards only pending hold evidence after a
// failed explicit refresh. Accepted baseline and all user decisions/audit remain.
func (s *Service) InvalidateLifecycleContinuity(source, token string) (bool, error) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if s.store.reviewTokenLocked() != token {
		return false, ErrStaleReview
	}
	if s.store.state.SourceFingerprint != source {
		if s.store.state.SourceFingerprint == "" && s.store.state.Lifecycle == nil {
			return false, nil
		}
		return false, ErrStaleReview
	}
	old := s.store.state.Lifecycle
	if old == nil || old.Scope == "" {
		return false, nil
	}
	if !validLifecycleState(*old) {
		return false, errors.New("invalid lifecycle evidence")
	}
	previous := s.store.state
	next := cloneReviewState(previous)
	resetAt := time.Now().UTC()
	archiveLifecyclePending(next.Lifecycle, "failed explicit refresh", resetAt)
	next.Lifecycle.Bootstrapped = false
	next.Lifecycle.BootAt = resetAt
	s.store.state = next
	if err := s.store.saveLocked(); err != nil {
		s.store.state = previous
		return false, err
	}
	return true, nil
}

func archiveLifecyclePending(state *LifecycleState, reason string, at time.Time) {
	if len(state.Pending) > 0 || len(state.HoldLost) > 0 {
		state.Continuity = append(state.Continuity, LifecycleContinuityArchive{Reason: reason, ProcessEpoch: state.ProcessEpoch, Scope: state.Scope, At: at, Pending: state.Pending, HoldLost: state.HoldLost, HoldEpoch: state.HoldEpoch})
	}
	state.Pending = nil
	state.HoldLost = nil
	state.HoldEpoch = 0
}

func (s *StateStore) lifecycleBackupStage(stage string) error {
	if s.beforeLifecycleBackup != nil {
		return s.beforeLifecycleBackup(stage)
	}
	return nil
}
