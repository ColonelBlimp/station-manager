package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// W-0021 slice 2B: the queue readout STATES the interim gate so the Forwarding
// card can say why nothing is queued in this archive, and retry is refused by
// name — a re-armed row in a gated archive could never be sent.
func TestForwarderQueues_ReportsTheForwardingGate(t *testing.T) {
	srv := serverWithForwarders(t, forwarderCfg("qrz", "qrz", true, "insert"))
	srv.qso.SetArchive(&types.QsoArchiveConfig{ID: "019fd5c5-efcc-7193-be4f-1fee532ee316", Label: "Contest", Ownership: types.QsoArchiveOwnershipManaged})

	w := getForwarderQueues(t, srv)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"forwarding_gated":true`) || !strings.Contains(body, `"gate_reason":"forwarding is off in this archive`) {
		t.Fatalf("body = %s; want forwarding_gated true with the reason", body)
	}
	rw := retryQueue(t, srv, "qrz", true)
	if rw.Code != http.StatusBadRequest || decodeErrCode(t, rw) != "forwarding_gated" {
		t.Fatalf("retry in a gated archive: status %d code %q; want 400 forwarding_gated", rw.Code, decodeErrCode(t, rw))
	}

	// The adopted archive: not gated, no reason on the wire.
	srv.qso.SetArchive(nil)
	w2 := getForwarderQueues(t, srv)
	if b := w2.Body.String(); !strings.Contains(b, `"forwarding_gated":false`) || strings.Contains(b, "gate_reason") {
		t.Fatalf("body = %s; want forwarding_gated false and no reason", b)
	}
}
