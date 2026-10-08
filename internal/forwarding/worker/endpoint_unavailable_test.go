package worker

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
)

// T5 sequence: outage -> seven endpoint-unavailable replies -> success. Real
// SQLite claims/completions preserve attempts across retries. Moving only the
// due timestamp avoids sleeps and leaves the persisted backoff to be asserted.
func TestWorker_EndpointUnavailableRetriesPastBudgetThenSucceeds(t *testing.T) {
	h, buf := captureHarness(t)
	row := seedClaimedRow(t, h)
	fwd := &recordingForwarder{typeName: stub.Type}
	cfg := defaultCfg("stub")
	cfg.Retry.MaxAttempts = 5
	cfg.Retry.InitialBackoffSec = 60
	cfg.Retry.MaxBackoffSec = 1800
	w, err := New(cfg, fwd, h.db, h.logger, h.hub)
	if err != nil {
		t.Fatal(err)
	}
	// A prior outage must recover on the 404, even though this QSO stays queued.
	w.reach.unreachable(stderrors.New("connection refused"))
	const diagnostic = "smcloud identity endpoint is unavailable (HTTP 404)"
	fwd.result = forwarding.Result{Outcome: forwarding.Outcome("endpoint_unavailable"), Err: stderrors.New(diagnostic)}
	for i, baseSec := range []int64{60, 120, 240, 480, 960, 1800, 1800} {
		before := time.Now().Unix()
		w.processRow(context.Background(), row)
		after := time.Now().Unix()
		got := h.fetchUpload(row.QsoID)
		if got.Status != "pending" || got.Attempts != int64(i+1) {
			t.Fatalf("attempt %d: status=%q attempts=%d; endpoint unavailable must stay pending beyond five tries", i+1, got.Status, got.Attempts)
		}
		if got.NextAttemptAt < before+baseSec || got.NextAttemptAt > after+baseSec+baseSec/5 {
			t.Fatalf("attempt %d: next_attempt_at=%d, want capped exponential backoff in [%d,%d]", i+1, got.NextAttemptAt, before+baseSec, after+baseSec+baseSec/5)
		}
		if got.LastError != diagnostic || got.FailureClass != "" || got.UpstreamID != "" {
			t.Fatalf("attempt %d: wrong pending diagnostic/completion: %+v", i+1, got)
		}
		// A future retry must not be claimable before its due time.
		claimed, err := h.db.ClaimPendingUploadsWithContext(context.Background(), "stub", 1)
		if err != nil || len(claimed) != 0 {
			t.Fatalf("retry claimed before due: rows=%d err=%v", len(claimed), err)
		}
		tx, cancel, err := h.db.BeginTxContext(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.ExecContext(context.Background(), "UPDATE qso_upload SET next_attempt_at = 0 WHERE id = ?", row.ID)
		if err != nil {
			_ = tx.Rollback()
			cancel()
			t.Fatal(err)
		}
		err = tx.Commit()
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		row = claimOne(t, h)
	}
	attempts := withMessage(t, buf, msgAttempt)
	if len(attempts) != 7 {
		t.Fatalf("attempt records=%d, want 7", len(attempts))
	}
	for _, rec := range attempts {
		if rec["outcome"] != "endpoint_unavailable" || rec["disposition"] != wantPersisted || rec["level"] != "info" || rec["error"] != diagnostic {
			t.Errorf("missing distinct, default-visible endpoint diagnostic: %+v", rec)
		}
	}
	if got := withMessage(t, buf, msgDestUnreachable); len(got) != 1 {
		t.Errorf("404 reported an outage: unreachable transitions=%d, want only the initial outage", len(got))
	}
	if got := withMessage(t, buf, msgDestRecovered); len(got) != 1 {
		t.Errorf("404 did not recover reachability: transitions=%d", len(got))
	}
	fwd.result = forwarding.Result{Outcome: forwarding.OutcomeSuccess, UpstreamID: "cloud-qso"}
	w.processRow(context.Background(), row)
	if got := h.fetchUpload(row.QsoID); got.Status != "uploaded" || got.UpstreamID != "cloud-qso" || got.LastError != "" {
		t.Fatalf("endpoint returned but upload did not complete: %+v", got)
	}
}
