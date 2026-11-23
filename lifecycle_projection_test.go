package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	builder "github.com/daniel-widrick/GraceNoteScraper/lineuparr"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func lifecycleRetirementServer(t *testing.T) (*dispatcharrServer, string, string, []byte) {
	t.Helper()
	s, f := newDispatcharrTestServer(t, true)
	s.client = &lifecycleFakeAPI{fakeDispatcharrAPI: f, inventory: lifecycleTestClient()}
	cfg, _, _ := s.lineup.store.Get()
	conn, _ := s.config.Get()
	hash := func(v string) string { sum := sha256.Sum256([]byte(v)); return hex.EncodeToString(sum[:]) }
	state := builder.State{Version: builder.CurrentStateVersion, SourceFingerprint: cfg.Fingerprint(), MatchDecisions: map[string]builder.MatchDecision{"obsolete": {Key: "obsolete", Decision: "confirmed", DispatcharrFingerprint: conn.Fingerprint(), StreamFingerprint: strings.Repeat("a", 64), StreamKey: "3:99", StreamID: 99, M3UAccountID: 3, ChannelID: "1001", StreamName: "removed channel", Score: 80, NameScore: 80}}, Channels: map[string]builder.ChannelOverride{}}
	raw, _ := json.Marshal(state)
	var value map[string]any
	_ = json.Unmarshal(raw, &value)
	value["lifecycle"] = map[string]any{"scope": hash(cfg.Fingerprint() + "\x00" + conn.Fingerprint() + "\x00dispatcharr-upstream-v0.33.0"), "processEpoch": s.lifecycleEpoch(), "baseline": map[string]any{"global": []string{hash("us| two hd")}, "accounts": map[string]any{"3": []string{hash("us| two hd")}}}, "bootAt": time.Now().Add(-time.Hour), "bootstrapped": true}
	raw, _ = json.Marshal(value)
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	path := filepath.Join(t.TempDir(), "state.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := builder.LoadStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s.lineup.builder = builder.NewService(store, builder.ServiceOptions{CacheDir: filepath.Join(t.TempDir(), "cache")})
	return s, path, cfg.Fingerprint(), raw
}
func TestLifecycleStagedRefreshFailureNeverRetires(t *testing.T) {
	for _, phase := range []string{"projection", "publication", "source-fence", "cancel-fence"} {
		t.Run(phase, func(t *testing.T) {
			s, path, source, before := lifecycleRetirementServer(t)
			req := httptest.NewRequest("GET", "/review", nil)
			ctx, cancel := context.WithCancel(req.Context())
			defer cancel()
			req = req.WithContext(ctx)
			s.lifecycleStageCheck = func(stage string) error {
				if len(s.lineup.builder.MatchDecisions(source)) != 1 {
					t.Fatal("staging exposed retirement")
				}
				if stage == phase {
					return errors.New("injected")
				}
				if stage == "publication" && phase == "source-fence" {
					cfg, _, _ := s.lineup.store.Get()
					guide := *s.lineup.state.Get()
					s.lineup.state.UpdateForSource(&guide, cfg.Fingerprint())
				}
				if stage == "publication" && phase == "cancel-fence" {
					cancel()
				}
				return nil
			}
			oldSnapshot := snapshotIdentity(s.snapshot)
			w := httptest.NewRecorder()
			if _, ok := s.buildReview(w, req, true); ok {
				t.Fatal("failed stage accepted")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) || len(s.lineup.builder.MatchDecisions(source)) != 1 {
				t.Fatal("failed refresh retired state")
			}
			if snapshotIdentity(s.snapshot) != oldSnapshot {
				t.Fatal("failed refresh published speculative snapshot")
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(path), "lifecycle-backups")); !os.IsNotExist(err) {
				t.Fatal("failed precommit stage wrote backup", err)
			}
		})
	}
}
func TestLifecycleStagedRefreshPublishesPostCommitView(t *testing.T) {
	s, path, source, before := lifecycleRetirementServer(t)
	s.operations = map[string]reviewOperation{"old": {State: "saved", Members: []string{"obsolete"}}}
	phases := []string{}
	s.lifecycleStageCheck = func(phase string) error {
		phases = append(phases, phase)
		if len(s.lineup.builder.MatchDecisions(source)) != 1 {
			t.Fatal("retirement before final publication")
		}
		return nil
	}
	w := httptest.NewRecorder()
	result, ok := s.buildReview(w, httptest.NewRequest("GET", "/review", nil), true)
	if !ok || result.response.Lifecycle.Retired != 1 {
		t.Fatalf("%d %s %+v", w.Code, w.Body.String(), result.response.Lifecycle)
	}
	if len(s.lineup.builder.MatchDecisions(source)) != 0 || len(result.response.Decisions) != 0 || len(result.response.ReviewedItems) != 0 {
		t.Fatal("response uses precommit choices")
	}
	if strings.Join(phases, ",") != "projection,publication" {
		t.Fatal("unexpected staged calls", phases)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state builder.State
	_ = json.Unmarshal(raw, &state)
	if state.Lifecycle == nil || len(state.Lifecycle.Audit) != 1 {
		t.Fatal("no atomic audit")
	}
	backup, err := os.ReadFile(filepath.Join(filepath.Dir(path), "lifecycle-backups", state.Lifecycle.Audit[0].Backup))
	if err != nil || !bytes.Equal(backup, before) {
		t.Fatal("backup differs", err)
	}
	if s.admission.inputs.choices != s.lineup.builder.ReviewInputToken() || s.snapshot.supplementalInputs.choices != s.lineup.builder.ReviewInputToken() {
		t.Fatal("admission refers to precommit state")
	}
	if op, found := s.operations["old"]; found && op.State != "retired" {
		t.Fatal("old operation remains actionable")
	}
	// A restart has the archive and no resurrection, independent of in-memory snapshot.
	recovered, err := builder.LoadStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	next := builder.NewService(recovered, builder.ServiceOptions{})
	if len(next.MatchDecisions(source)) != 0 {
		t.Fatal("restart resurrected decision")
	}
}
