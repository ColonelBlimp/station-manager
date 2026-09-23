package main

import (
	"context"
	stderr "errors"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
)

// W-0021 slice 3C wiring: through the daemon's real ports an activation seals
// FT8 admission and the rig's keyed paths, persists the pending selector and
// closes the SAME restart channel POST /v1/restart uses — and only under the
// respawn contract.
func TestLifecycle_ArchiveActivationSealsAndRequestsTheRestart(t *testing.T) {
	t.Setenv("SM_SELF_RESTART", "1")
	d, orch := newOrchestratedDaemon(t, nil)
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("start: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := d.archives.Create(ctx, archive.CreateRequest{RequestKey: "k-contest", Label: "Contest", LogbookName: "Contest", LogbookCallsign: "G4ABC"})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	out, err := d.archives.Activate(ctx, res.Entry.ID)
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if out.ID != res.Entry.ID {
		t.Fatalf("activation = %+v", out)
	}
	select {
	case <-d.restartCh:
	default:
		t.Fatal("the restart channel was not closed by the activation")
	}
	if p := d.cfgSvc.Snapshot().PendingQsoArchiveID; p != res.Entry.ID {
		t.Fatalf("pending = %q", p)
	}
	if err := d.ft8.ArmTx(true); !stderr.Is(err, ft8.ErrArchiveSwitchPending) {
		t.Fatalf("ArmTx after activation = %v, want the archive-switch refusal", err)
	}
	if err := d.bridge.SealTx(); err != nil {
		t.Fatalf("the bridge must already be sealed (idempotent re-seal): %v", err)
	}
}

// Without the respawn contract the activation is refused before anything is
// sealed: TX admission stays open.
func TestLifecycle_ArchiveActivationRefusedWithoutRespawnContract(t *testing.T) {
	t.Setenv("SM_SELF_RESTART", "")
	d, orch := newOrchestratedDaemon(t, nil)
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("start: %v", err)
	}
	ctx := context.Background()
	res, err := d.archives.Create(ctx, archive.CreateRequest{RequestKey: "k-contest", Label: "Contest", LogbookName: "Contest", LogbookCallsign: "G4ABC"})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	_, err = d.archives.Activate(ctx, res.Entry.ID)
	var re *archive.RequestError
	if !stderr.As(err, &re) || re.Code != "restart_unavailable" {
		t.Fatalf("activate without the contract = %v, want restart_unavailable", err)
	}
	if p := d.cfgSvc.Snapshot().PendingQsoArchiveID; p != "" {
		t.Fatalf("pending = %q after a refusal", p)
	}
	// Admission is untouched: a profile claim (idle) succeeds.
	if _, err := d.ft8.ClaimProfile("ft4"); err != nil {
		t.Fatalf("FT8 admission was sealed by a refused activation: %v", err)
	}
}
