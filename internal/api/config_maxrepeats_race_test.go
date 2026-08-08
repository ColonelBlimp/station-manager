package api

import (
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// The repeat-cap live-apply must not leave the sequencer behind persisted
// config (codex 2026-08-08 P2). The PUT handler persists under the config
// service's lock but snapshots-and-applies AFTER releasing it, so two
// interleaved saves can commit A-then-B yet apply B-then-A: config and both
// responses report B's cap while the rig keeps calling with A's until the
// next save or restart. The confusable this pins apart: "the last save won
// everywhere" vs "the last save won on disk only".
//
// The interleaving is forced deterministically via the server's one-shot
// ft8ApplyTestGap, which fires inside save A's snapshot→apply window: save B
// (commit 5 + apply) runs there in full. Against the unserialized code B
// finishes inside the window and A then applies its stale 3 — red. With the
// apply serialized around a FRESH snapshot, B blocks until A's apply is done
// and then applies the latest committed value — green on every interleaving.
// The 200ms select is the deadlock escape for the fixed path only; on the
// unserialized path nothing blocks B, so the timing carries no verdict.
func TestConfigPut_MaxRepeatsLiveApplyMatchesCommitted(t *testing.T) {
	ft8Svc := ft8.NewService(types.Ft8Config{Enabled: true}, logging.Noop(), t.TempDir())
	srv := testServerWithFt8(t, nil, ft8Svc)

	update := func(v int) {
		t.Helper()
		if err := srv.cfg.Update(func(cfg *config.Config) error {
			if cfg.Ft8.TX == nil {
				cfg.Ft8.TX = &types.Ft8TXConfig{}
			}
			cfg.Ft8.TX.MaxRepeats = v
			return nil
		}); err != nil {
			t.Fatalf("update: %v", err)
		}
	}

	bDone := make(chan struct{})
	srv.ft8ApplyTestGap = func() {
		go func() {
			defer close(bDone)
			update(5)
			srv.applyCommittedFt8MaxRepeats()
		}()
		select {
		case <-bDone: // unserialized: B completes inside A's window
		case <-time.After(200 * time.Millisecond): // serialized: B is blocked on A
		}
	}

	update(3)
	srv.applyCommittedFt8MaxRepeats()

	select {
	case <-bDone:
	case <-time.After(2 * time.Second):
		t.Fatal("save B never completed")
	}

	committed := types.ResolveFt8MaxRepeats(srv.cfg.Snapshot().Ft8.TX)
	if committed != 5 {
		t.Fatalf("fixture: committed cap = %d, want save B's 5", committed)
	}
	if live := ft8Svc.MaxRepeats(); live != committed {
		t.Errorf("sequencer cap = %d, persisted config = %d — the live apply lost the race and the rig runs a cap no response ever reported", live, committed)
	}
}
