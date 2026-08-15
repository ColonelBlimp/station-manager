package worker

// L7 acceptance — a post-commit hook panic must not masquerade as a pre-submit
// processing failure, and must not retry an already-committed upload.
//
// persistOutcome commits the upload (markSuccess stamps the QSO), logs the attempt,
// THEN invokes the fallible OnQsoStamped mirror-notify hook. If that hook panics, the
// outer processRowSafely recovery catches it and logs "panic processing row;
// resetting to retry" + re-arms the row through the transient path — but the upload
// already committed, so re-arming re-forwards a completed upload (a duplicate) under a
// false pre-submit narrative.
//
// Confusable states (the finding's own): a pre-submit row-processing failure (which
// SHOULD retry) vs a post-commit mirror-notify failure (which must NOT); upload not
// committed vs upload safely committed.
//
// Criterion (observable: the log narrative + the queue row): when OnQsoStamped panics,
// the worker logs ONE post-commit line (phase=post_commit, hook=on_qso_stamped,
// upload_committed=true, with upload_id + qso_id), the committed upload is NOT re-armed
// (stays Uploaded, attempts=1, the hook is not re-invoked), and it is distinguishable
// from a pre-submit panic — no "panic processing row" line.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/status"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// stampingSuccessForwarder succeeds AND has a non-empty AdifPrefix, so markSuccess
// stamps the QSO (bumps its revision) and thus invokes OnQsoStamped — unlike the
// stub, whose AdifPrefix is "" (no stamp, no hook). Type() matches the enqueued
// row's forwarder_type.
type stampingSuccessForwarder struct{}

func (stampingSuccessForwarder) Type() string       { return stub.Type }
func (stampingSuccessForwarder) AdifPrefix() string { return "TESTFWD" }
func (stampingSuccessForwarder) Submit(context.Context, types.Qso, forwarding.Action, string) forwarding.Result {
	return forwarding.Result{Outcome: forwarding.OutcomeSuccess, UpstreamID: "stamp-ok"}
}

func TestWorker_PostCommitHookPanic_UploadStaysCommitted(t *testing.T) {
	h, buf := captureHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	var hookCalls int32
	hookFired := make(chan struct{}, 1)
	cfg := defaultCfg("stub")
	cfg.OnQsoStamped = func(_ context.Context, _ int64) {
		atomic.AddInt32(&hookCalls, 1)
		select {
		case hookFired <- struct{}{}:
		default:
		}
		panic("boom: mirror-notify hook exploded")
	}

	w, err := New(cfg, stampingSuccessForwarder{}, h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()

	select {
	case <-hookFired:
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		t.Fatalf("OnQsoStamped never fired; log:\n%s", buf.String())
	}
	// Post-commit the fix contains the panic and stops. In the buggy path the reset
	// ran synchronously with the panic and the loop keeps re-forwarding — either way,
	// stop and inspect the durable evidence.
	cancel()
	<-done

	// The upload must remain committed and NOT be re-armed/re-forwarded.
	row := h.fetchUpload(qsoID)
	if row.Status != status.Uploaded.String() {
		t.Errorf("status = %q, want %q (a committed upload must not be re-armed by a hook panic)",
			row.Status, status.Uploaded.String())
	}
	if row.Attempts != 1 {
		t.Errorf("attempts = %d, want 1 (no re-forward of a committed upload)", row.Attempts)
	}
	if got := atomic.LoadInt32(&hookCalls); got != 1 {
		t.Errorf("hook calls = %d, want 1 (contained, not re-invoked via a retry)", got)
	}

	// Correctly labelled as the post-commit hook failure it is.
	recs := withMessage(t, buf, "forwarder: post-commit hook panicked; upload already committed, not retried")
	if len(recs) != 1 {
		t.Fatalf("post-commit hook-panic lines = %d, want 1; log:\n%s", len(recs), buf.String())
	}
	r := recs[0]
	if r["phase"] != "post_commit" {
		t.Errorf("phase = %v, want post_commit", r["phase"])
	}
	if r["hook"] != "on_qso_stamped" {
		t.Errorf("hook = %v, want on_qso_stamped", r["hook"])
	}
	if r["upload_committed"] != true {
		t.Errorf("upload_committed = %v, want true", r["upload_committed"])
	}
	if _, ok := r["upload_id"]; !ok {
		t.Errorf("post-commit line missing upload_id: %v", r)
	}
	if _, ok := r["qso_id"]; !ok {
		t.Errorf("post-commit line missing qso_id: %v", r)
	}

	// And NOT the pre-submit narrative.
	if pre := withMessage(t, buf, "forwarder: panic processing row; resetting to retry"); len(pre) != 0 {
		t.Errorf("a committed-upload hook panic was mislabelled as pre-submit processing (%d lines)", len(pre))
	}
}
