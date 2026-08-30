package worker

import (
	"encoding/json"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// forwardFailedDetail builds the typed, bounded operator-event detail for a
// terminal forward failure (W-0001 / ADR 0076). It records qso_uuid (the canonical
// QSO identifier), qso_id, forwarder, action, and attempts — NEVER the provider Reason
// (the upstream error text that lives in qso_upload.last_error / ForwardFailedPayload.Reason).
//
// qsoUUID is resolved by the caller (markFailed) before the terminal write — including for
// QSO-gone terminals, via a narrow including-deleted read — so it is normally present; it
// is empty only in the pathological unresolved case. qso_id is DEPRECATED and retained
// through v2.0.0-alpha.2 (removed in alpha.3); new consumers key on qso_uuid.
//
// A row.Action outside the known insert/update/delete set is stored as the
// bounded sentinel "unknown" rather than persisted verbatim, so a corrupt action
// can never reach the operator-facing store.
func forwardFailedDetail(row types.QsoUpload, forwarderName, qsoUUID string) []byte {
	act := "unknown"
	if a, err := action.Parse(row.Action); err == nil {
		act = string(a)
	}
	b, _ := json.Marshal(struct {
		QsoUUID   string `json:"qso_uuid"`
		QsoID     int64  `json:"qso_id"` // DEPRECATED (removed v2.0.0-alpha.3); use qso_uuid
		Forwarder string `json:"forwarder"`
		Action    string `json:"action"`
		Attempts  int    `json:"attempts"`
	}{
		QsoUUID:   qsoUUID,
		QsoID:     row.QsoID,
		Forwarder: forwarderName,
		Action:    act,
		Attempts:  int(row.Attempts) + 1,
	})
	return b
}
