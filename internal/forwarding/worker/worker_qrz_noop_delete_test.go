package worker

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/status"
	"github.com/ColonelBlimp/station-manager/internal/forwarding/qrz"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// TestWorker_Delete_NoPriorInsert_QrzSettlesUploadedWithoutHTTP is the
// end-to-end guard for W-0010 outcome 9's "QRZ delete without an upstream id is
// a no-op": a delete row for a QSO QRZ never accepted (no prior uploaded insert,
// so no upstream_id) must settle `uploaded` through the REAL qrz forwarder with
// zero requests — not `failed`, which the Forwarding card would count as a live
// backlog. Existing stub-based worker tests prove the worker dispatches an
// empty id; this one proves what QRZ then does with it, through the worker's
// own persist path.
func TestWorker_Delete_NoPriorInsert_QrzSettlesUploadedWithoutHTTP(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, "RESULT=OK&COUNT=1")
	}))
	t.Cleanup(srv.Close)

	creds, err := json.Marshal(map[string]string{"api_key": "ABCD-1234-EFGH-5678"})
	if err != nil {
		t.Fatalf("marshal creds: %v", err)
	}
	fwd, err := qrz.New(types.ForwarderConfig{
		Name: "qrz", Type: qrz.Type, Credentials: creds,
		Endpoints: map[string]string{action.Delete.String(): srv.URL},
	})
	if err != nil {
		t.Fatalf("qrz.New: %v", err)
	}

	h := newHarness(t)
	qsoID := h.seedLogbookAndQso()
	// No insert row at all: FetchPriorUpstreamID comes back empty.
	h.enqueueUpload(qsoID, "qrz", qrz.Type, action.Delete)

	w, err := New(defaultCfg("qrz"), fwd, h.db, h.logger, h.hub)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	row := runUntil(t, w, h, qsoID, func(u types.QsoUpload) bool {
		return u.Status == status.Uploaded.String() || u.Status == status.Failed.String()
	})
	if row.Status != status.Uploaded.String() {
		t.Fatalf("status = %q (last_error=%q), want uploaded — a no-op delete is not a failure", row.Status, row.LastError)
	}
	if row.LastError != "" {
		t.Fatalf("last_error = %q, want empty on a no-op delete", row.LastError)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("QRZ endpoint saw %d requests, want 0", got)
	}
}
