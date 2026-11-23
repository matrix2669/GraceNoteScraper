package lineuparr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/daniel-widrick/GraceNoteScraper/dispatcharr"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// ReconcileLifecycle is retained for isolated persistence tests. Server refresh
// uses PrepareLifecycle, stages every derived result, then CommitLifecycle.
func (s *Service) ReconcileLifecycle(ctx context.Context, source, connection, token, operation, epoch string, started, now time.Time, targets []DraftChannel, receipt dispatcharr.LifecycleInventoryReceipt) (LifecycleResult, []string, error) {
	p, err := s.PrepareLifecycle(ctx, source, connection, token, operation, epoch, started, now, targets, receipt)
	if err != nil {
		return LifecycleResult{Status: "deferred", Reason: "Lifecycle preparation failed; choices preserved"}, nil, err
	}
	if err = s.CommitLifecycle(ctx, p, nil); err != nil {
		return LifecycleResult{Status: "deferred", Reason: "Lifecycle save failed; choices preserved"}, nil, err
	}
	return p.Result, p.RetiredKeys(), nil
}

func preparedLifecycleFixture(t *testing.T) (*Service, *LifecycleProjection, []byte) {
	t.Helper()
	svc := newTestService(t, "", "")
	receipt, cfg, started := lifecycleReceiptFixture(t, `{"count":1,"next":null,"results":[{"id":1,"m3u_account":1,"name":"kept"}]}`, `[]`)
	d := MatchDecision{Key: "p", Decision: "confirmed", DispatcharrFingerprint: cfg.Fingerprint(), StreamFingerprint: strings.Repeat("a", 64), StreamKey: "1:99", StreamID: 99, M3UAccountID: 1, ChannelID: "target", StreamName: "removed", Score: 80, NameScore: 80}
	if err := svc.SetMatchDecision("source", d); err != nil {
		t.Fatal(err)
	}
	svc.store.state.Lifecycle = &LifecycleState{Scope: lifecycleScope("source", cfg.Fingerprint(), receipt.CapabilityContract()), Baseline: lifeNames("kept"), BootAt: time.Now().Add(-time.Hour), Bootstrapped: true, ProcessEpoch: "process-one"}
	if err := svc.store.saveLocked(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(svc.store.path)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := svc.PrepareLifecycle(context.Background(), "source", cfg.Fingerprint(), svc.ReviewStateToken(), "operation", "process-one", started, time.Now().UTC(), []DraftChannel{{ID: "target", StationID: "station", Included: true}}, receipt)
	if err != nil || plan.Result.Retired != 1 {
		t.Fatalf("plan %+v %v", plan, err)
	}
	return svc, plan, before
}
func TestLifecycleProjectionIsolatedUntilFinalCommit(t *testing.T) {
	svc, plan, before := preparedLifecycleFixture(t)
	if len(svc.MatchDecisions("source")) != 1 || len(plan.MatchDecisions("source")) != 0 {
		t.Fatal("speculative state escaped")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(svc.store.path), "lifecycle-backups")); !os.IsNotExist(err) {
		t.Fatal("prepare wrote backup", err)
	}
	inputs := []InputChannel{{Key: "target", StationID: "station", CallSign: "TEST", Number: "1"}}
	capture := captureSources(nil, nil)
	staged, err := plan.BuildCaptured(context.Background(), LineupContext{SourceFingerprint: "source", Country: "US"}, inputs, capture)
	if err != nil {
		t.Fatal(err)
	}
	live, err := svc.BuildCaptured(context.Background(), LineupContext{SourceFingerprint: "source", Country: "US"}, inputs, capture)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(live.Channels[0].Aliases, staged.Channels[0].Aliases) {
		t.Fatal("staged draft did not remove retired alias")
	}
	if _, err := plan.BuildCaptured(context.Background(), LineupContext{SourceFingerprint: "source"}, inputs, nil); err == nil {
		t.Fatal("nil capture acquired sources")
	}
	err = svc.CommitLifecycle(context.Background(), plan, func(write func() error) error { return ErrStaleReview })
	after, _ := os.ReadFile(svc.store.path)
	if !errors.Is(err, ErrStaleReview) || !bytes.Equal(before, after) || len(svc.MatchDecisions("source")) != 1 {
		t.Fatal("failed final fence changed state", err)
	}
	if err = svc.CommitLifecycle(context.Background(), plan, nil); err != nil {
		t.Fatal(err)
	}
	reopened, err := LoadStateStore(svc.store.path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.state.MatchDecisions) != 0 || len(reopened.state.Lifecycle.Audit) != 1 {
		t.Fatal("commit not atomic across restart")
	}
	next, err := svc.BuildCaptured(context.Background(), LineupContext{SourceFingerprint: "source", Country: "US"}, inputs, capture)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(staged.Channels, next.Channels) {
		t.Fatal("staged projection differed from committed build")
	}
	if err = svc.CommitLifecycle(context.Background(), plan, nil); !errors.Is(err, ErrStaleReview) {
		t.Fatal("old plan replayed", err)
	}
}
func TestLifecycleProjectionConcurrentChoiceAndAfterCommitCancellation(t *testing.T) {
	t.Run("concurrent-choice", func(t *testing.T) {
		svc, plan, _ := preparedLifecycleFixture(t)
		if err := svc.SetAliasSuppressed("source", "target", "extra", true); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(svc.store.path)
		if err := svc.CommitLifecycle(context.Background(), plan, nil); !errors.Is(err, ErrStaleReview) {
			t.Fatal(err)
		}
		after, _ := os.ReadFile(svc.store.path)
		if !bytes.Equal(before, after) || len(svc.MatchDecisions("source")) != 1 {
			t.Fatal("stale plan changed newer choice")
		}
	})
	t.Run("after-commit-cancellation", func(t *testing.T) {
		svc, plan, _ := preparedLifecycleFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		published := false
		err := svc.CommitLifecycle(ctx, plan, func(write func() error) error {
			if e := write(); e != nil {
				return e
			}
			cancel()
			published = true
			return nil
		})
		if err != nil || !published || len(svc.MatchDecisions("source")) != 0 {
			t.Fatal("committed operation reported failure", err)
		}
		reopened, e := LoadStateStore(svc.store.path)
		if e != nil || len(reopened.state.Lifecycle.Audit) != 1 {
			t.Fatal("durable commit lost", e)
		}
	})
}

func TestLifecycleCrashAfterDurableCommit(t *testing.T) {
	if path := os.Getenv("LIFECYCLE_CRASH_STATE"); path != "" {
		svc, plan, before := preparedLifecycleFixture(t)
		svc.store.path = path
		if err := os.WriteFile(path, before, 0600); err != nil {
			t.Fatal(err)
		}
		_ = svc.CommitLifecycle(context.Background(), plan, func(write func() error) error {
			if err := write(); err != nil {
				return err
			}
			os.Exit(23) // abrupt process loss before any in-memory snapshot publication
			return nil
		})
		t.Fatal("crash point not reached")
	}
	path := filepath.Join(t.TempDir(), "state.json")
	cmd := exec.Command(os.Args[0], "-test.run=^TestLifecycleCrashAfterDurableCommit$")
	cmd.Env = append(os.Environ(), "LIFECYCLE_CRASH_STATE="+path)
	output, err := cmd.CombinedOutput()
	if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 23 {
		t.Fatalf("child: %v %s", err, output)
	}
	store, err := LoadStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.state.MatchDecisions) != 0 || store.state.Lifecycle == nil || len(store.state.Lifecycle.Audit) != 1 {
		t.Fatal("crash left partial transaction")
	}
	a := store.state.Lifecycle.Audit[0]
	data, err := os.ReadFile(filepath.Join(filepath.Dir(path), "lifecycle-backups", a.Backup))
	if err != nil {
		t.Fatal(err)
	}
	var backup State
	if json.Unmarshal(data, &backup) != nil || backup.MatchDecisions["p"].Decision != "confirmed" {
		t.Fatal("crash backup missing original")
	}
}
