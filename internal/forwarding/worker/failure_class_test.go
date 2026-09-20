package worker

import (
	"fmt"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/failure"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/status"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// W-0010 outcome 9: the forwarder's terminal Class reaches the row unchanged,
// and a terminal result without one leaves the row unclassified — the boot
// re-arm must see exactly the credential failures and nothing else.
func TestWorker_TerminalClass_ReachesRow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		class forwarding.FailureClass
		want  string
	}{
		{"auth", failure.Auth, "auth"},
		{"unclassified", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			qsoID := h.seedLogbookAndQso()
			h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

			fwd := &recordingForwarder{
				typeName: stub.Type,
				result: forwarding.Result{
					Outcome: forwarding.OutcomeTerminal,
					Err:     fmt.Errorf("rejected"),
					Class:   tc.class,
				},
			}
			w, err := New(defaultCfg("stub"), fwd, h.db, h.logger, h.hub)
			if err != nil {
				t.Fatalf("new worker: %v", err)
			}

			row := runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
				return u.Status == status.Failed.String()
			})
			if row.FailureClass != tc.want {
				t.Fatalf("failure_class = %q, want %q", row.FailureClass, tc.want)
			}
		})
	}
}

// Exhausted retries are the worker's decision, not the upstream's verdict on
// the credential: the row fails unclassified even though the destination kept
// answering.
func TestWorker_ExhaustedTransient_IsUnclassified(t *testing.T) {
	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	h.enqueueUpload(qsoID, "stub", stub.Type, action.Insert)

	fwd := &recordingForwarder{
		typeName: stub.Type,
		result:   forwarding.Result{Outcome: forwarding.OutcomeTransient, Err: fmt.Errorf("503")},
	}
	cfg := defaultCfg("stub")
	cfg.Retry.MaxAttempts = 1
	w, err := New(cfg, fwd, h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	row := runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Status == status.Failed.String()
	})
	if row.FailureClass != "" {
		t.Fatalf("failure_class = %q on exhausted retries, want unclassified", row.FailureClass)
	}
}
