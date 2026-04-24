package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	_ "github.com/ColonelBlimp/station-manager/internal/forwarding/stub" // registers the "stub" forwarder
	"github.com/ColonelBlimp/station-manager/internal/forwarding/worker"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// e2eHarness bundles the test server with the worker goroutines it has
// spawned. Caller gets a shutdown closure that cancels ctx and waits
// for every worker's Run to return before the test's own cleanup
// (dbSvc.Close) fires.
type e2eHarness struct {
	srv      *Server
	shutdown func()
}

// startE2E spins up a Server with the given forwarder configs and
// launches one real worker goroutine per enabled entry against the
// same in-memory sqlite. The tick is deliberately short so the
// happy-path converges in tens of milliseconds, not the production
// default of 120 s.
//
// The returned shutdown closure cancels the worker ctx and waits on a
// WaitGroup so the DB handle isn't closed out from under an in-flight
// worker query. t.Cleanup registers it as a backstop — tests usually
// call it explicitly via defer for clarity.
func startE2E(t *testing.T, fwds ...types.ForwarderConfig) *e2eHarness {
	t.Helper()

	srv := serverWithForwarders(t, fwds...)

	workerCtx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	for _, fc := range fwds {
		if !fc.Enabled {
			continue
		}
		fwd, err := forwarding.Build(fc)
		if err != nil {
			cancel()
			t.Fatalf("forwarding.Build(%q): %v", fc.Name, err)
		}
		w, err := worker.New(worker.Config{
			Name:  fc.Name,
			Tick:  50 * time.Millisecond,
			Batch: 5,
			Retry: types.RetryConfig{
				MaxAttempts:       5,
				InitialBackoffSec: 1,
				MaxBackoffSec:     60,
			},
		}, fwd, srv.db, srv.logger, srv.hub)
		if err != nil {
			cancel()
			t.Fatalf("worker.New(%q): %v", fc.Name, err)
		}

		wg.Go(func() {
			w.Run(workerCtx)
		})
	}

	shutdown := func() {
		cancel()
		wg.Wait()
	}
	// Backstop in case the test returns early on t.Fatalf; without this
	// a worker could still be running when dbSvc.Close fires.
	t.Cleanup(shutdown)

	return &e2eHarness{srv: srv, shutdown: shutdown}
}

// fetchUploads hits GET /v1/qso/{id}/uploads and decodes the envelope.
// Fails the test on any non-200 or decode error.
func fetchUploads(t *testing.T, srv *Server, qsoID int64) []types.QsoUpload {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/qso/%d/uploads", qsoID), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", qsoID))
	w := httptest.NewRecorder()
	srv.handleListQsoUploads(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /v1/qso/%d/uploads: status = %d, body = %s", qsoID, w.Code, w.Body.String())
	}
	var env struct {
		Items []types.QsoUpload `json:"items"`
	}
	if err := unmarshalJSON(w.Body.String(), &env); err != nil {
		t.Fatalf("decode uploads envelope: %v", err)
	}
	return env.Items
}

// waitForUploads polls GET /v1/qso/{id}/uploads at 25 ms intervals
// until match returns true for the current row set, or the deadline
// is reached. On timeout it fails the test with the last observed
// rows so diagnostics point at "what state did the system settle on?"
// rather than "we gave up."
func waitForUploads(t *testing.T, srv *Server, qsoID int64, match func([]types.QsoUpload) bool) []types.QsoUpload {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last []types.QsoUpload
	for time.Now().Before(deadline) {
		last = fetchUploads(t, srv, qsoID)
		if match(last) {
			return last
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("waitForUploads: condition not reached within deadline; last rows = %+v", last)
	return nil
}

// allUploaded reports whether every row in rows has status="uploaded".
// The zero-rows case returns false — an empty set doesn't satisfy
// "everything finished" (it just means ingest hasn't committed yet).
func allUploaded(rows []types.QsoUpload) bool {
	if len(rows) == 0 {
		return false
	}
	for _, r := range rows {
		if r.Status != "uploaded" {
			return false
		}
	}
	return true
}

// hasActionUploaded reports whether some row for action `act` exists
// and has status="uploaded". Used to assert a specific lifecycle
// action has completed without caring about the state of the others.
func hasActionUploaded(rows []types.QsoUpload, act string) bool {
	for _, r := range rows {
		if r.Action == act && r.Status == "uploaded" {
			return true
		}
	}
	return false
}

// =============================================================================
// Scenario 1: POST /v1/qso reaches 'uploaded' end-to-end
// =============================================================================

func TestE2E_InsertReachesUploaded(t *testing.T) {
	h := startE2E(t,
		forwarderCfg("qrz", "stub", true, "insert", "update", "delete"),
		forwarderCfg("clublog", "stub", true, "insert"),
	)
	defer h.shutdown()

	lbID := createTestLogbook(t, h.srv, "My Log", "G4ABC")
	qsoID := submitAndGetID(t, h.srv, lbID, testQsoADIF)

	rows := waitForUploads(t, h.srv, qsoID, allUploaded)

	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 (one per enabled forwarder); rows = %+v", len(rows), rows)
	}
	for _, r := range rows {
		if r.Action != "insert" {
			t.Fatalf("row %+v: action = %q, want insert", r, r.Action)
		}
		if r.Attempts != 1 {
			t.Fatalf("row %+v: attempts = %d, want 1 (single submit)", r, r.Attempts)
		}
		if r.UpstreamID != "stub-ok" {
			t.Fatalf("row %+v: upstream_id = %q, want stub-ok", r, r.UpstreamID)
		}
		if r.LastError != "" {
			t.Fatalf("row %+v: last_error = %q, want empty on success", r, r.LastError)
		}
	}
}

// =============================================================================
// Scenario 2: PATCH enqueues an update row that reaches 'uploaded'
// =============================================================================

func TestE2E_UpdateReachesUploaded(t *testing.T) {
	h := startE2E(t,
		forwarderCfg("qrz", "stub", true, "insert", "update"),
	)
	defer h.shutdown()

	lbID := createTestLogbook(t, h.srv, "My Log", "G4ABC")
	qsoID := submitAndGetID(t, h.srv, lbID, testQsoADIF)

	// Settle the insert row first — isolates the post-PATCH assertion
	// from any lingering insert-row transition.
	waitForUploads(t, h.srv, qsoID, allUploaded)

	// PATCH — the update handler must enqueue a new qso_upload row with
	// action=update, which the worker then picks up and uploads.
	patchW := patchQso(t, h.srv, qsoID, `{"comment":"test edit"}`)
	if patchW.Code != http.StatusOK {
		t.Fatalf("PATCH: status = %d; body = %s", patchW.Code, patchW.Body.String())
	}

	rows := waitForUploads(t, h.srv, qsoID, func(rs []types.QsoUpload) bool {
		return hasActionUploaded(rs, "update")
	})

	// We expect two rows total now: insert (uploaded) + update (uploaded).
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 (insert + update); rows = %+v", len(rows), rows)
	}
	var sawInsert, sawUpdate bool
	for _, r := range rows {
		if r.Status != "uploaded" {
			t.Fatalf("row %+v: status = %q, want uploaded", r, r.Status)
		}
		switch r.Action {
		case "insert":
			sawInsert = true
		case "update":
			sawUpdate = true
		}
	}
	if !sawInsert || !sawUpdate {
		t.Fatalf("missing insert or update row: %+v", rows)
	}
}

// =============================================================================
// Scenario 3: DELETE enqueues a delete row that reaches 'uploaded',
// and the worker handles the soft-deleted QSO via the IncludingDeleted
// fallback per forwarding.md §4.
// =============================================================================

func TestE2E_DeleteReachesUploaded(t *testing.T) {
	h := startE2E(t,
		forwarderCfg("qrz", "stub", true, "insert", "update", "delete"),
	)
	defer h.shutdown()

	lbID := createTestLogbook(t, h.srv, "My Log", "G4ABC")
	qsoID := submitAndGetID(t, h.srv, lbID, testQsoADIF)

	// Settle the insert first.
	waitForUploads(t, h.srv, qsoID, allUploaded)

	// DELETE: qsoservice.Delete soft-deletes the QSO AND enqueues a
	// delete-action qso_upload row in one tx.
	delW := deleteQso(t, h.srv, qsoID)
	if delW.Code != http.StatusNoContent {
		t.Fatalf("DELETE: status = %d; body = %s", delW.Code, delW.Body.String())
	}

	rows := waitForUploads(t, h.srv, qsoID, func(rs []types.QsoUpload) bool {
		return hasActionUploaded(rs, "delete")
	})

	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 (insert + delete); rows = %+v", len(rows), rows)
	}
	var sawInsert, sawDelete bool
	for _, r := range rows {
		if r.Status != "uploaded" {
			t.Fatalf("row %+v: status = %q, want uploaded", r, r.Status)
		}
		switch r.Action {
		case "insert":
			sawInsert = true
		case "delete":
			sawDelete = true
		}
	}
	if !sawInsert || !sawDelete {
		t.Fatalf("missing insert or delete row: %+v", rows)
	}

	// The QSO itself should be soft-deleted — the canonical GET
	// handler must 404. The uploads endpoint is the only path that
	// still exposes the id while the delete-forward completes.
	getReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/qso/%d", qsoID), nil)
	getReq.SetPathValue("id", fmt.Sprintf("%d", qsoID))
	getW := httptest.NewRecorder()
	h.srv.handleGetQso(getW, getReq)
	if getW.Code != http.StatusNotFound {
		t.Fatalf("GET /v1/qso/%d after delete: status = %d, want 404", qsoID, getW.Code)
	}
}
