package lineuparr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func lifeNames(names ...string) lifecycleNames {
	rows := []dispatcharr.LifecycleRow{}
	for i, n := range names {
		rows = append(rows, dispatcharr.LifecycleRow{ID: int64(i + 1), AccountID: 1, Name: n})
	}
	return lifecycleInventoryNames(rows)
}
func TestLifecycleCandidateIdentityAndProtection(t *testing.T) {
	target := DraftChannel{ID: "target", StationID: "station", PlacementID: "place", CallSign: "RAW", Included: true}
	d := MatchDecision{Key: "p", Decision: "confirmed", DispatcharrFingerprint: "conn", StreamFingerprint: strings.Repeat("a", 64), StreamKey: "1:99", StreamID: 99, M3UAccountID: 1, ChannelID: "target", ChannelName: "Old display", StreamName: "US: FX HD"}
	state := State{SourceFingerprint: "source", MatchDecisions: map[string]MatchDecision{"p": d}}
	refs := []dispatcharr.LifecycleReference{{StreamID: 99, AccountID: 1}}
	if got := lifecycleCandidates(state, "source", "conn", []DraftChannel{target}, nil, nil, refs); len(got) != 1 {
		t.Fatalf("valid historical target rejected %v", got)
	}
	for _, rows := range [][]dispatcharr.LifecycleRow{{{ID: 99, AccountID: 4, Name: "renamed"}}, {{ID: 1, AccountID: 7, Name: "  us:   fx hd "}}} {
		if got := lifecycleCandidates(state, "source", "conn", []DraftChannel{target}, rows, nil, refs); len(got) != 0 {
			t.Fatal("existing ID or literal replacement not retained")
		}
	}
	for _, raw := range []string{"TV: FX HD", "US: FX SD", "US: FX WEST HD"} {
		if got := lifecycleCandidates(state, "source", "conn", []DraftChannel{target}, []dispatcharr.LifecycleRow{{ID: 1, Name: raw}}, nil, refs); len(got) != 1 {
			t.Fatalf("distinct name collapsed %s", raw)
		}
	}
	if got := lifecycleCandidates(state, "source", "conn", []DraftChannel{target, target}, nil, nil, refs); len(got) != 0 {
		t.Fatal("ambiguous target admitted")
	}
	if got := lifecycleCandidates(state, "source", "conn", []DraftChannel{target}, nil, nil, nil); len(got) != 0 {
		t.Fatal("uncovered ID admitted")
	}
	denial := d
	denial.Key = "d"
	denial.Decision = "denied"
	state.MatchDecisions["d"] = denial
	if got := lifecycleCandidates(state, "source", "conn", []DraftChannel{target}, nil, nil, refs); len(got) != 0 {
		t.Fatal("opposing decision discarded")
	}
}
func TestLifecycleBootstrapAndCatastrophicHysteresis(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := []lifecycleCandidate{{key: "p"}}
	base := lifeNames("a", "b", "c", "d")
	s, ret, _ := lifecyclePlan(LifecycleState{}, "scope", "one", now, base, c)
	if len(ret) != 0 {
		t.Fatal("bootstrap retired")
	}
	s, ret, _ = lifecyclePlan(s, "scope", "two", now.Add(time.Minute), base, c)
	if len(ret) != 1 {
		t.Fatal("stable bootstrap did not settle")
	}
	// 50% distinct-name loss defers three observations for at least five minutes.
	s, ret, _ = lifecyclePlan(s, "scope", "three", now.Add(2*time.Minute), lifeNames("a", "b"), c)
	if len(ret) != 0 {
		t.Fatal("catastrophic loss retired")
	}
	s, ret, _ = lifecyclePlan(s, "scope", "four", now.Add(3*time.Minute), lifeNames("a", "b"), c)
	if len(ret) != 0 {
		t.Fatal("catastrophic hold too short")
	}
	_, ret, _ = lifecyclePlan(s, "scope", "five", now.Add(7*time.Minute), lifeNames("a", "b"), c)
	if len(ret) != 1 {
		t.Fatal("stable loss did not settle")
	}
}

func lifecycleReceiptFixture(t *testing.T, streamBody string, historical string) (dispatcharr.LifecycleInventoryReceipt, dispatcharr.Config, time.Time) {
	t.Helper()
	started := time.Now().UTC()
	cfg := dispatcharr.Config{BaseURL: "https://dispatcharr.test", AuthMethod: dispatcharr.AuthAPIKey, APIKey: "secret"}
	client := dispatcharr.NewClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.URL.Path {
		case "/api/core/version/":
			body = `{"version":"0.33.0"}`
		case "/api/accounts/users/me/":
			body = `{"id":1,"user_level":10}`
		case "/api/m3u/accounts/":
			body = `[{"id":1,"is_active":true,"status":"success"}]`
		case "/api/channels/streams/":
			body = streamBody
		case "/api/channels/streams/by-ids/":
			body = historical
		default:
			t.Fatalf("unexpected %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})})
	receipt, err := client.CaptureLifecycleInventory(context.Background(), cfg, []dispatcharr.LifecycleReference{{StreamID: 99, AccountID: 1}})
	if err != nil || receipt.Coverage() != dispatcharr.LifecycleCoverageComplete {
		t.Fatalf("receipt %s %v", receipt.Coverage(), err)
	}
	return receipt, cfg, started
}
func TestLifecycleAtomicArchiveAndRollback(t *testing.T) {
	for _, failure := range []string{"", "encode", "write", "sync", "rename", "backup:create", "backup:write", "backup:sync", "backup:verify"} {
		t.Run(failure, func(t *testing.T) {
			svc := newTestService(t, "", "")
			receipt, cfg, started := lifecycleReceiptFixture(t, `{"count":1,"next":null,"results":[{"id":1,"m3u_account":1,"name":"kept"}]}`, `[]`)
			d := MatchDecision{Key: "p", Decision: "confirmed", DispatcharrFingerprint: cfg.Fingerprint(), StreamFingerprint: strings.Repeat("a", 64), StreamKey: "1:99", StreamID: 99, M3UAccountID: 1, ChannelID: "target", ChannelName: "Old", StreamName: "removed", Score: 100, NameScore: 100}
			if err := svc.SetMatchDecision("source", d); err != nil {
				t.Fatal(err)
			}
			svc.store.mu.Lock()
			svc.store.state.Lifecycle = &LifecycleState{Scope: lifecycleScope("source", cfg.Fingerprint(), receipt.CapabilityContract()), Baseline: lifeNames("kept"), BootAt: time.Now().Add(-time.Hour), Bootstrapped: true, ProcessEpoch: "process-one"}
			if err := svc.store.saveLocked(); err != nil {
				t.Fatal(err)
			}
			svc.store.mu.Unlock()
			before, err := os.ReadFile(svc.store.path)
			if err != nil {
				t.Fatal(err)
			}
			token := svc.ReviewStateToken()
			if strings.HasPrefix(failure, "backup:") {
				svc.store.beforeLifecycleBackup = func(stage string) error {
					if "backup:"+stage == failure {
						return errors.New("injected")
					}
					return nil
				}
			} else if failure != "" {
				svc.store.beforeSave = func(stage string) error {
					if stage == failure {
						return errors.New("injected")
					}
					return nil
				}
			}
			result, keys, err := svc.ReconcileLifecycle(context.Background(), "source", cfg.Fingerprint(), token, "operation", "process-one", started, time.Now().UTC(), []DraftChannel{{ID: "target", StationID: "station", Included: true}}, receipt)
			if failure != "" {
				after, _ := os.ReadFile(svc.store.path)
				if err == nil || !bytes.Equal(before, after) || len(svc.MatchDecisions("source")) != 1 || len(keys) != 0 {
					t.Fatalf("failed save changed state: %v %v", result, err)
				}
				return
			}
			if err != nil || result.Retired != 1 || len(keys) != 1 || len(svc.MatchDecisions("source")) != 0 {
				t.Fatalf("retire failed %+v %v", result, err)
			}
			reopened, err := LoadStateStore(svc.store.path)
			if err != nil {
				t.Fatal(err)
			}
			if reopened.state.Lifecycle == nil || len(reopened.state.Lifecycle.Audit) != 1 || reopened.state.Lifecycle.Audit[0].Primary["p"].StreamName != "removed" {
				t.Fatal("audit missing after restart")
			}
			backup := reopened.state.Lifecycle.Audit[0].Backup
			data, err := os.ReadFile(filepath.Join(filepath.Dir(svc.store.path), "lifecycle-backups", backup))
			if err != nil || !bytes.Equal(data, before) {
				t.Fatalf("backup mismatch %v", err)
			}
		})
	}
}

func TestLifecycleLossThresholdAndAccountIsolation(t *testing.T) {
	rows := []dispatcharr.LifecycleRow{}
	for i := 0; i < 100; i++ {
		rows = append(rows, dispatcharr.LifecycleRow{ID: int64(i + 1), AccountID: 1, Name: fmt.Sprintf("name-%d", i)})
	}
	base := lifecycleInventoryNames(rows)
	if _, cat := lifecycleLoss(base, lifecycleInventoryNames(rows[:51])); cat {
		t.Fatal("49 percent loss marked catastrophic")
	}
	if _, cat := lifecycleLoss(base, lifecycleInventoryNames(rows[:50])); !cat {
		t.Fatal("50 percent loss missed")
	}
	// Per-account loss cannot be masked by a large healthy account.
	rows = append(rows, dispatcharr.LifecycleRow{ID: 101, AccountID: 2, Name: "other-one"}, dispatcharr.LifecycleRow{ID: 102, AccountID: 2, Name: "other-two"})
	base = lifecycleInventoryNames(rows)
	if _, cat := lifecycleLoss(base, lifecycleInventoryNames(rows[:100])); !cat {
		t.Fatal("lost account hidden by global totals")
	}
}
func TestLifecycleHoldRestorationAndNewLoss(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := lifeNames("a", "b", "c", "d")
	s := LifecycleState{Scope: "scope", Baseline: base, Bootstrapped: true, ProcessEpoch: "process-one"}
	candidate := []lifecycleCandidate{{key: "p"}}
	s, _, _ = lifecyclePlan(s, "scope", "one", now, lifeNames("a", "b"), candidate)
	s, _, _ = lifecyclePlan(s, "scope", "two", now.Add(time.Minute), lifeNames("a", "b"), candidate)
	// Restoring the candidate cancels its hold.
	restored, _, _ := lifecyclePlan(s, "scope", "three", now.Add(2*time.Minute), lifeNames("a", "b", "c"), nil)
	if len(restored.Pending) != 0 {
		t.Fatal("restored candidate still pending")
	}
	// A newly lost name restarts the hold despite three observations and elapsed time.
	s, ret, _ := lifecyclePlan(s, "scope", "four", now.Add(6*time.Minute), lifeNames("a"), candidate)
	if len(ret) != 0 || s.Pending["p"].Count != 1 {
		t.Fatal("worsening loss advanced stale hold")
	}
	// Extra unrelated names do not reset unchanged loss evidence.
	s, ret, _ = lifecyclePlan(s, "scope", "five", now.Add(7*time.Minute), lifeNames("a", "new"), candidate)
	if len(ret) != 0 || s.Pending["p"].Count != 2 {
		t.Fatal("unrelated addition reset hold")
	}
}
func TestLifecycleBackupFailureLeavesChoices(t *testing.T) {
	svc := newTestService(t, "", "")
	receipt, cfg, started := lifecycleReceiptFixture(t, `{"count":1,"next":null,"results":[{"id":1,"m3u_account":1,"name":"kept"}]}`, `[]`)
	d := MatchDecision{Key: "p", Decision: "confirmed", DispatcharrFingerprint: cfg.Fingerprint(), StreamFingerprint: strings.Repeat("a", 64), StreamKey: "1:99", StreamID: 99, M3UAccountID: 1, ChannelID: "target", StreamName: "removed", Score: 100, NameScore: 100}
	if err := svc.SetMatchDecision("source", d); err != nil {
		t.Fatal(err)
	}
	svc.store.mu.Lock()
	svc.store.state.Lifecycle = &LifecycleState{Scope: lifecycleScope("source", cfg.Fingerprint(), receipt.CapabilityContract()), Baseline: lifeNames("kept"), BootAt: time.Now().Add(-time.Hour), Bootstrapped: true, ProcessEpoch: "process-one"}
	if err := svc.store.saveLocked(); err != nil {
		t.Fatal(err)
	}
	svc.store.mu.Unlock()
	before, _ := os.ReadFile(svc.store.path)
	if err := os.WriteFile(filepath.Join(filepath.Dir(svc.store.path), "lifecycle-backups"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	_, keys, err := svc.ReconcileLifecycle(context.Background(), "source", cfg.Fingerprint(), svc.ReviewStateToken(), "operation", "process-one", started, time.Now().UTC(), []DraftChannel{{ID: "target", StationID: "station", Included: true}}, receipt)
	after, _ := os.ReadFile(svc.store.path)
	if err == nil || len(keys) != 0 || !bytes.Equal(before, after) || len(svc.MatchDecisions("source")) != 1 {
		t.Fatal("backup failure changed choices")
	}
}

func TestLifecycleSupplementalTupleAndDenials(t *testing.T) {
	target := DraftChannel{ID: "target", StationID: "station", PlacementID: "place", CallSign: "RAW", Included: true}
	d := SupplementalDecision{Origin: SupplementalOrigin, SourceFingerprint: "source", DispatcharrFingerprint: "conn", ChannelID: target.ID, StationID: target.StationID, PlacementID: target.PlacementID, RawCallSign: target.CallSign, StreamID: 99, M3UAccountID: 1, StreamName: "US: FX HD", StreamFingerprint: strings.Repeat("b", 64), GroupKey: "group", OperationID: "op", AdmittedSnapshot: "snapshot", RetrievalVersion: "v1", Decision: "confirmed"}
	d.Key = SupplementalKey(d)
	state := State{SourceFingerprint: "source", SupplementalDecisions: map[string]SupplementalDecision{d.Key: d}}
	refs := []dispatcharr.LifecycleReference{{StreamID: 99, AccountID: 1}}
	if got := lifecycleCandidates(state, "source", "conn", []DraftChannel{target}, nil, nil, refs); len(got) != 1 {
		t.Fatalf("valid supplemental missed %v", got)
	}
	for _, change := range []string{"station", "placement", "callsign", "excluded"} {
		other := target
		switch change {
		case "station":
			other.StationID = "other"
		case "placement":
			other.PlacementID = "other"
		case "callsign":
			other.CallSign = "OTHER"
		case "excluded":
			other.Included = false
		}
		if got := lifecycleCandidates(state, "source", "conn", []DraftChannel{other}, nil, nil, refs); len(got) != 0 {
			t.Fatalf("%s mismatch admitted", change)
		}
	}
	d.Decision = "denied"
	state.SupplementalDecisions[d.Key] = d
	if got := lifecycleCandidates(state, "source", "conn", []DraftChannel{target}, nil, nil, refs); len(got) != 0 {
		t.Fatal("denial eligible")
	}
}
func TestLifecycleSameReceiptCannotAdvanceBootstrap(t *testing.T) {
	svc := newTestService(t, "", "")
	receipt, cfg, started := lifecycleReceiptFixture(t, `{"count":1,"next":null,"results":[{"id":1,"m3u_account":1,"name":"kept"}]}`, `[]`)
	d := MatchDecision{Key: "p", Decision: "confirmed", DispatcharrFingerprint: cfg.Fingerprint(), StreamFingerprint: strings.Repeat("a", 64), StreamKey: "1:99", StreamID: 99, M3UAccountID: 1, ChannelID: "target", StreamName: "removed", Score: 100, NameScore: 100}
	if err := svc.SetMatchDecision("source", d); err != nil {
		t.Fatal(err)
	}
	target := []DraftChannel{{ID: "target", StationID: "station", Included: true}}
	first := time.Now().UTC()
	r, _, err := svc.ReconcileLifecycle(context.Background(), "source", cfg.Fingerprint(), svc.ReviewStateToken(), "operation-one", "process-one", started, first, target, receipt)
	if err != nil || r.Retired != 0 {
		t.Fatal("first observation retired")
	}
	token := svc.ReviewStateToken()
	r, _, err = svc.ReconcileLifecycle(context.Background(), "source", cfg.Fingerprint(), token, "operation-two", "process-one", started, first.Add(59*time.Second), target, receipt)
	if err != nil || r.Retired != 0 || svc.ReviewStateToken() != token {
		t.Fatal("same receipt advanced hold")
	}
}
func TestLifecycleAuditSurvivesResetAndSourceChange(t *testing.T) {
	svc := newTestService(t, "", "")
	svc.store.mu.Lock()
	svc.store.state.SourceFingerprint = "source"
	svc.store.state.Lifecycle = &LifecycleState{Scope: "scope", Audit: []LifecycleAudit{{BatchID: "batch"}}}
	if err := svc.store.saveLocked(); err != nil {
		t.Fatal(err)
	}
	svc.store.mu.Unlock()
	backup, err := svc.ResetBuilder("source", svc.ReviewStateToken(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResetBuilder("source", svc.ReviewStateToken(), backup, false); err != nil {
		t.Fatal(err)
	}
	svc.store.mu.RLock()
	state := svc.store.state
	svc.store.mu.RUnlock()
	if state.Lifecycle == nil || len(state.Lifecycle.Audit) != 1 || state.Lifecycle.Scope != "" {
		t.Fatal("reset lost audit or retained pending evidence")
	}
	svc.store.mu.Lock()
	svc.store.ensureSourceLocked("other")
	state = svc.store.state
	svc.store.mu.Unlock()
	if state.Lifecycle == nil || len(state.Lifecycle.Audit) != 1 {
		t.Fatal("source change lost audit")
	}
}

func TestLifecycleCatastrophicHoldSurvivesPartialRecovery(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := lifeNames("a", "b", "c", "d")
	s := LifecycleState{Scope: "scope", Baseline: base, BootAt: now.Add(-time.Hour), Bootstrapped: true, ProcessEpoch: "process-one"}
	candidate := []lifecycleCandidate{{key: "p"}}
	s, _, _ = lifecyclePlan(s, "scope", "one", now, lifeNames("a", "b"), candidate)
	s, ret, _ := lifecyclePlan(s, "scope", "two", now.Add(time.Minute), lifeNames("a", "b", "c"), candidate)
	if len(ret) != 0 || !s.Pending["p"].Catastrophic || len(s.Baseline.Global) != 4 {
		t.Fatal("partial recovery bypassed catastrophic hold")
	}
	_, ret, _ = lifecyclePlan(s, "scope", "three", now.Add(5*time.Minute), lifeNames("a", "b", "c"), candidate)
	if len(ret) != 1 {
		t.Fatal("stable partial recovery did not settle after full hold")
	}
}

func TestLifecycleCandidateFirstSeenAtBootstrapBoundaryStillWaits(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	base := lifeNames("a", "b", "c")
	s, _, _ := lifecyclePlan(LifecycleState{}, "scope", "one", now, base, nil)
	candidate := []lifecycleCandidate{{key: "new"}}
	s, ret, _ := lifecyclePlan(s, "scope", "two", now.Add(time.Minute), base, candidate)
	if len(ret) != 0 {
		t.Fatal("first candidate observation retired")
	}
	s, ret, _ = lifecyclePlan(s, "scope", "three", now.Add(61*time.Second), base, candidate)
	if len(ret) != 0 {
		t.Fatal("candidate bypassed its own 60-second wait")
	}
	_, ret, _ = lifecyclePlan(s, "scope", "four", now.Add(2*time.Minute), base, candidate)
	if len(ret) != 1 {
		t.Fatal("candidate did not settle")
	}
}

func TestLifecycleCatastrophicEvidenceIsSharedNotPerChoice(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	names := make([]string, 1000)
	for i := range names {
		names[i] = fmt.Sprintf("service-%d", i)
	}
	base := lifeNames(names...)
	s := LifecycleState{Scope: "scope", Baseline: base, BootAt: now.Add(-time.Hour), Bootstrapped: true, ProcessEpoch: "process-one"}
	candidates := make([]lifecycleCandidate, 500)
	for i := range candidates {
		candidates[i] = lifecycleCandidate{key: fmt.Sprintf("candidate-%d", i)}
	}
	s, _, _ = lifecyclePlan(s, "scope", "one", now, lifeNames(names[:400]...), candidates)
	encoded, err := json.Marshal(s)
	if err != nil || len(encoded) > 500000 {
		t.Fatalf("hold evidence expanded per choice: bytes=%d error=%v", len(encoded), err)
	}
}

func TestLifecycleFencesRejectStaleOrIncompleteEvidence(t *testing.T) {
	svc := newTestService(t, "", "")
	receipt, cfg, started := lifecycleReceiptFixture(t, `{"count":1,"next":null,"results":[{"id":1,"m3u_account":1,"name":"kept"}]}`, `[]`)
	d := MatchDecision{Key: "p", Decision: "confirmed", DispatcharrFingerprint: cfg.Fingerprint(), StreamFingerprint: strings.Repeat("a", 64), StreamKey: "1:99", StreamID: 99, M3UAccountID: 1, ChannelID: "target", StreamName: "removed", Score: 100, NameScore: 100}
	if err := svc.SetMatchDecision("source", d); err != nil {
		t.Fatal(err)
	}
	before := svc.ReviewStateToken()
	targets := []DraftChannel{{ID: "target", StationID: "station", Included: true}}
	for _, tc := range []struct {
		source, conn, token string
		start, now          time.Time
		receipt             dispatcharr.LifecycleInventoryReceipt
	}{
		{"source", cfg.Fingerprint(), before, started, time.Now().UTC(), dispatcharr.LifecycleInventoryReceipt{}},
		{"source", "other", before, started, time.Now().UTC(), receipt},
		{"source", cfg.Fingerprint(), before, receipt.StartedAt().Add(time.Second), time.Now().UTC(), receipt},
		{"source", cfg.Fingerprint(), before, started, receipt.CompletedAt().Add(61 * time.Second), receipt},
	} {
		_, keys, _ := svc.ReconcileLifecycle(context.Background(), tc.source, tc.conn, tc.token, "op", "process-one", tc.start, tc.now, targets, tc.receipt)
		if len(keys) != 0 || svc.ReviewStateToken() != before {
			t.Fatal("invalid receipt changed state")
		}
	}
	_, keys, err := svc.ReconcileLifecycle(context.Background(), "source", cfg.Fingerprint(), "stale-token", "op", "process-one", started, time.Now().UTC(), targets, receipt)
	if !errors.Is(err, ErrStaleReview) || len(keys) != 0 || svc.ReviewStateToken() != before {
		t.Fatal("stale state fence accepted")
	}
}

func TestLifecycleRestartArchivesPendingAndRestartsEvidence(t *testing.T) {
	svc := newTestService(t, "", "")
	receipt, cfg, started := lifecycleReceiptFixture(t, `{"count":1,"next":null,"results":[{"id":1,"m3u_account":1,"name":"kept"}]}`, `[]`)
	d := MatchDecision{Key: "p", Decision: "confirmed", DispatcharrFingerprint: cfg.Fingerprint(), StreamFingerprint: strings.Repeat("a", 64), StreamKey: "1:99", StreamID: 99, M3UAccountID: 1, ChannelID: "target", StreamName: "removed", Score: 100, NameScore: 100}
	if err := svc.SetMatchDecision("source", d); err != nil {
		t.Fatal(err)
	}
	targets := []DraftChannel{{ID: "target", StationID: "station", Included: true}}
	now := time.Now().UTC()
	if _, _, err := svc.ReconcileLifecycle(context.Background(), "source", cfg.Fingerprint(), svc.ReviewStateToken(), "one", "process-one", started, now, targets, receipt); err != nil {
		t.Fatal(err)
	}
	result, keys, err := svc.ReconcileLifecycle(context.Background(), "source", cfg.Fingerprint(), svc.ReviewStateToken(), "two", "process-two", started, now.Add(time.Second), targets, receipt)
	if err != nil || result.Retired != 0 || len(keys) != 0 {
		t.Fatalf("restart retired choice %+v %v", result, err)
	}
	svc.store.mu.RLock()
	state := svc.store.state.Lifecycle
	svc.store.mu.RUnlock()
	if len(state.Continuity) != 1 || len(state.Continuity[0].Pending) != 1 || state.Pending["primary:p"].Count != 1 {
		t.Fatalf("restart evidence not archived/reset: %+v", state)
	}
}
func TestLifecycleFailedInvalidationPreservesChoicesAndCanRetry(t *testing.T) {
	svc := newTestService(t, "", "")
	receipt, cfg, started := lifecycleReceiptFixture(t, `{"count":1,"next":null,"results":[{"id":1,"m3u_account":1,"name":"kept"}]}`, `[]`)
	d := MatchDecision{Key: "p", Decision: "confirmed", DispatcharrFingerprint: cfg.Fingerprint(), StreamFingerprint: strings.Repeat("a", 64), StreamKey: "1:99", StreamID: 99, M3UAccountID: 1, ChannelID: "target", StreamName: "removed", Score: 100, NameScore: 100}
	if err := svc.SetMatchDecision("source", d); err != nil {
		t.Fatal(err)
	}
	targets := []DraftChannel{{ID: "target", StationID: "station", Included: true}}
	if _, _, err := svc.ReconcileLifecycle(context.Background(), "source", cfg.Fingerprint(), svc.ReviewStateToken(), "one", "process-one", started, time.Now().UTC(), targets, receipt); err != nil {
		t.Fatal(err)
	}
	before := svc.ReviewStateToken()
	svc.store.beforeSave = func(string) error { return errors.New("injected") }
	if changed, err := svc.InvalidateLifecycleContinuity("source", before); err == nil || changed || svc.ReviewStateToken() != before {
		t.Fatal("failed invalidation changed state")
	}
	svc.store.beforeSave = nil
	changed, err := svc.InvalidateLifecycleContinuity("source", before)
	if err != nil || !changed || len(svc.MatchDecisions("source")) != 1 {
		t.Fatal("invalidation retry lost decisions")
	}
	svc.store.mu.RLock()
	state := svc.store.state.Lifecycle
	svc.store.mu.RUnlock()
	if len(state.Pending) != 0 || len(state.Continuity) != 1 {
		t.Fatal("invalidation did not archive/reset evidence")
	}
}

func TestLifecycleCancellationAfterBackupPreservesChoices(t *testing.T) {
	svc := newTestService(t, "", "")
	receipt, cfg, started := lifecycleReceiptFixture(t, `{"count":1,"next":null,"results":[{"id":1,"m3u_account":1,"name":"kept"}]}`, `[]`)
	d := MatchDecision{Key: "p", Decision: "confirmed", DispatcharrFingerprint: cfg.Fingerprint(), StreamFingerprint: strings.Repeat("a", 64), StreamKey: "1:99", StreamID: 99, M3UAccountID: 1, ChannelID: "target", StreamName: "removed", Score: 100, NameScore: 100}
	if err := svc.SetMatchDecision("source", d); err != nil {
		t.Fatal(err)
	}
	svc.store.mu.Lock()
	svc.store.state.Lifecycle = &LifecycleState{Scope: lifecycleScope("source", cfg.Fingerprint(), receipt.CapabilityContract()), Baseline: lifeNames("kept"), BootAt: time.Now().Add(-time.Hour), Bootstrapped: true, ProcessEpoch: "process-one"}
	if err := svc.store.saveLocked(); err != nil {
		t.Fatal(err)
	}
	svc.store.mu.Unlock()
	before, _ := os.ReadFile(svc.store.path)
	ctx, cancel := context.WithCancel(context.Background())
	svc.store.beforeLifecycleBackup = func(stage string) error {
		if stage == "verify" {
			cancel()
		}
		return nil
	}
	_, keys, err := svc.ReconcileLifecycle(ctx, "source", cfg.Fingerprint(), svc.ReviewStateToken(), "op", "process-one", started, time.Now().UTC(), []DraftChannel{{ID: "target", StationID: "station", Included: true}}, receipt)
	after, _ := os.ReadFile(svc.store.path)
	if !errors.Is(err, context.Canceled) || len(keys) != 0 || !bytes.Equal(before, after) || len(svc.MatchDecisions("source")) != 1 {
		t.Fatal("cancelled lifecycle changed state")
	}
}
