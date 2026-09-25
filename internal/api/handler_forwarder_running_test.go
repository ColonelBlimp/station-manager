package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ADR 0082 (W-0021 5B): the running-worker set that admits a queue retry is
// the daemon's binding snapshot, not config.json's enabled entries — a name
// with a running worker is admitted whatever config says, a name without one
// is refused, and the queues readout no longer carries a gate.
func TestForwarderQueues_RunningSetComesFromTheBindingSnapshot(t *testing.T) {
	srv := serverWithForwarders(t, forwarderCfg("qrz", "qrz", false, "insert"))
	srv.SetRunningForwarders([]string{"qrz"})
	if w := retryQueue(t, srv, "qrz", true); w.Code != http.StatusOK {
		t.Fatalf("retry with a running worker: status %d body %s; want 200", w.Code, w.Body.String())
	}

	srv.SetRunningForwarders(nil)
	if w := retryQueue(t, srv, "qrz", true); w.Code != http.StatusBadRequest || decodeErrCode(t, w) != "forwarder_disabled" {
		t.Fatalf("retry without a running worker: status %d; want 400 forwarder_disabled", w.Code)
	}

	w := httptest.NewRecorder()
	srv.handleForwarderQueues(w, httptest.NewRequest(http.MethodGet, "/v1/forwarder-queues", nil))
	if b := w.Body.String(); strings.Contains(b, "forwarding_gated") || strings.Contains(b, "gate_reason") {
		t.Fatalf("queues readout still carries the gate: %s", b)
	}
}

// Operator review of 5B(b), P2: the queue endpoints are keyed by the active
// archive's BINDING names, not config names — an additional logbook's
// `<type>.<uuid>` binding is listed and clearable, and a config name that is
// not a binding of this archive is unknown to them.
func TestForwarderQueues_KeyedByBindingNames(t *testing.T) {
	srv := serverWithForwarders(t, forwarderCfg("qrz", "qrz", true, "insert"))
	const extra = "qrz.01920000-0000-7000-8000-00000000000b"
	srv.SetForwarderQueueNames([]string{"qrz", extra})
	srv.SetRunningForwarders([]string{"qrz", extra})

	w := httptest.NewRecorder()
	srv.handleForwarderQueues(w, httptest.NewRequest(http.MethodGet, "/v1/forwarder-queues", nil))
	if b := w.Body.String(); !strings.Contains(b, `"name":"`+extra+`"`) {
		t.Fatalf("readout omits the additional logbook's binding: %s", b)
	}
	if cw := clearQueue(t, srv, extra, true); cw.Code != http.StatusOK {
		t.Fatalf("clear of a binding name: status %d body %s; want 200", cw.Code, cw.Body.String())
	}
	if rw := retryQueue(t, srv, extra, true); rw.Code != http.StatusOK {
		t.Fatalf("retry of a binding name: status %d body %s; want 200", rw.Code, rw.Body.String())
	}

	// Now the archive's bindings no longer include the config name at all.
	srv.SetForwarderQueueNames([]string{extra})
	if cw := clearQueue(t, srv, "qrz", true); cw.Code != http.StatusNotFound || decodeErrCode(t, cw) != "unknown_forwarder" {
		t.Fatalf("clear of a name that is not a binding: status %d; want 404 unknown_forwarder", cw.Code)
	}
	w = httptest.NewRecorder()
	srv.handleForwarderQueues(w, httptest.NewRequest(http.MethodGet, "/v1/forwarder-queues", nil))
	if b := w.Body.String(); strings.Contains(b, `"name":"qrz"`) {
		t.Fatalf("readout still lists a config name that is not a binding: %s", b)
	}
}
