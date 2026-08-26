package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/status"
)

// qsoADIF builds a distinct ADIF record (varying CALL + TIME_ON) so several can
// be submitted into one logbook without tripping duplicate detection.
func qsoADIF(call, timeOn string) string {
	return fmt.Sprintf(
		"<CALL:%d>%s<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<TIME_ON:%d>%s<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>England<EOR>",
		len(call), call, len(timeOn), timeOn)
}

// seedForwarderUpload submits a fresh QSO — which enqueues one pending row to the
// single enabled forwarder `fwd` — and drives that row to `st`, returning the QSO
// id. For any non-pending state it claims exactly one pending row, so a
// forwarder's PENDING rows MUST be seeded LAST or the claim grabs more than one.
func seedForwarderUpload(t *testing.T, srv *Server, lbID int64, fwd, call, tm string, st status.Status) int64 {
	t.Helper()
	qsoID, _ := submitAndGetID(t, srv, lbID, qsoADIF(call, tm))
	if st == status.Pending {
		return qsoID
	}
	claimed, err := srv.db.ClaimPendingUploadsWithContext(context.Background(), fwd, 10)
	if err != nil {
		t.Fatalf("claim %s/%s: %v", fwd, call, err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claim %s/%s: got %d rows, want 1 — seed pending rows LAST", fwd, call, len(claimed))
	}
	switch st {
	case status.InProgress:
		// leave the claimed row in_progress (the in-flight batch)
	case status.Failed:
		if err := srv.db.MarkUploadFailedWithContext(context.Background(), claimed[0].ID, "seed failure"); err != nil {
			t.Fatalf("mark failed: %v", err)
		}
	case status.Uploaded:
		if err := srv.db.MarkUploadSuccessWithContext(context.Background(), claimed[0].ID, "logid-"+call); err != nil {
			t.Fatalf("mark uploaded: %v", err)
		}
	default:
		t.Fatalf("seedForwarderUpload: unsupported state %q", st)
	}
	return qsoID
}

func clearQueue(t *testing.T, srv *Server, name string, setPath bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/forwarder/"+name+"/queue/clear", nil)
	if setPath {
		req.SetPathValue("name", name)
	}
	w := httptest.NewRecorder()
	srv.handleClearForwarderQueue(w, req)
	return w
}

func getForwarderQueues(t *testing.T, srv *Server) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/forwarder-queues", nil)
	w := httptest.NewRecorder()
	srv.handleForwarderQueues(w, req)
	return w
}

// TestClearForwarderQueue_HappyPath: the clear removes only the named forwarder's
// pending+failed backlog and reports the count, while its in-flight batch stays.
func TestClearForwarderQueue_HappyPath(t *testing.T) {
	srv := serverWithForwarders(t, forwarderCfg("qrz", "qrz", true, "insert", "update", "delete"))
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	inflight := seedForwarderUpload(t, srv, lbID, "qrz", "AA1AA", "0900", status.InProgress)
	seedForwarderUpload(t, srv, lbID, "qrz", "BB2BB", "0905", status.Failed)
	seedForwarderUpload(t, srv, lbID, "qrz", "CC3CC", "0910", status.Pending)

	w := clearQueue(t, srv, "qrz", true)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var got clearForwarderQueueResponse
	if err := unmarshalJSON(w.Body.String(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Discarded != 2 {
		t.Fatalf("discarded = %d, want 2 (pending + failed)", got.Discarded)
	}

	// The in-flight row must survive — the clear is scoped to pending+failed.
	rows, err := srv.db.FetchUploadsByQsoIDWithContext(context.Background(), inflight)
	if err != nil {
		t.Fatalf("fetch uploads: %v", err)
	}
	if len(rows) != 1 || rows[0].Status != "in_progress" {
		t.Errorf("in-flight row must survive the clear: got %+v", rows)
	}
}

// TestClearForwarderQueue_Validation: an empty name is a 400 and a name that is
// not a configured forwarder is a 404 — a typo can't silently "clear" nothing.
func TestClearForwarderQueue_Validation(t *testing.T) {
	srv := serverWithForwarders(t,
		forwarderCfg("qrz", "qrz", true, "insert"),
		forwarderCfg("clublog", "clublog", false, "insert"), // disabled
	)

	t.Run("empty name", func(t *testing.T) {
		w := clearQueue(t, srv, "", false) // no path value set → empty name
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
		}
		if code := decodeErrCode(t, w); code != "invalid_forwarder" {
			t.Errorf("code = %q, want invalid_forwarder", code)
		}
	})

	t.Run("unknown forwarder", func(t *testing.T) {
		w := clearQueue(t, srv, "lotw", true)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body = %s", w.Code, w.Body.String())
		}
		if code := decodeErrCode(t, w); code != "unknown_forwarder" {
			t.Errorf("code = %q, want unknown_forwarder", code)
		}
	})

	// A DISABLED forwarder is a valid clear target — clearing is independent of
	// enabled state. It simply has no rows here, so the clear succeeds with 0.
	t.Run("disabled forwarder cleared", func(t *testing.T) {
		w := clearQueue(t, srv, "clublog", true)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
		}
		var got clearForwarderQueueResponse
		if err := unmarshalJSON(w.Body.String(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Discarded != 0 {
			t.Errorf("discarded = %d, want 0 (disabled forwarder, no rows)", got.Discarded)
		}
	})
}

// TestForwarderQueues_ListsAllWithCounts: every CONFIGURED forwarder appears with
// its clearable/in-flight counts, including a disabled one with no rows ({0,0}).
func TestForwarderQueues_ListsAllWithCounts(t *testing.T) {
	srv := serverWithForwarders(t,
		forwarderCfg("qrz", "qrz", true, "insert", "update", "delete"),
		forwarderCfg("clublog", "clublog", false, "insert"), // disabled → no rows
	)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	// qrz: clearable = 1 pending + 1 failed = 2; in_flight = 1.
	seedForwarderUpload(t, srv, lbID, "qrz", "AA1AA", "0900", status.InProgress)
	seedForwarderUpload(t, srv, lbID, "qrz", "BB2BB", "0905", status.Failed)
	seedForwarderUpload(t, srv, lbID, "qrz", "CC3CC", "0910", status.Pending)

	w := getForwarderQueues(t, srv)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var got forwarderQueuesResponse
	if err := unmarshalJSON(w.Body.String(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	by := map[string]forwarderQueueCount{}
	for _, f := range got.Forwarders {
		by[f.Name] = f
	}
	if len(got.Forwarders) != 2 {
		t.Fatalf("forwarders = %d (%v), want 2 (every configured forwarder)", len(got.Forwarders), got.Forwarders)
	}
	// Config order is preserved so the SPA can align counts with its own list.
	if got.Forwarders[0].Name != "qrz" || got.Forwarders[1].Name != "clublog" {
		t.Errorf("order = [%q, %q], want [qrz, clublog] (config order)",
			got.Forwarders[0].Name, got.Forwarders[1].Name)
	}
	if q := by["qrz"]; q.Clearable != 2 || q.InFlight != 1 {
		t.Errorf("qrz = %+v, want {Clearable:2 InFlight:1}", q)
	}
	if c := by["clublog"]; c.Clearable != 0 || c.InFlight != 0 {
		t.Errorf("clublog (disabled, no rows) = %+v, want {0 0}", c)
	}
}
