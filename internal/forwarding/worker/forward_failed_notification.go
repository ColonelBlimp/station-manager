package worker

import (
	"encoding/json"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// forwardFailedDetail builds the typed, bounded operator-event detail for a
// terminal forward failure (W-0001 / ADR 0076). It records only qso_id,
// forwarder, action, and attempts — NEVER the provider Reason (the upstream
// error text that lives in qso_upload.last_error / ForwardFailedPayload.Reason).
//
// A row.Action outside the known insert/update/delete set is stored as the
// bounded sentinel "unknown" rather than persisted verbatim, so a corrupt action
// can never reach the operator-facing store. qso_id is the integer id available
// at the producing boundary (there is no UUID here); it stays usable even for
// QSO-gone terminals where a UUID lookup could fail.
func forwardFailedDetail(row types.QsoUpload, forwarderName string) []byte {
	act := "unknown"
	if a, err := action.Parse(row.Action); err == nil {
		act = string(a)
	}
	b, _ := json.Marshal(struct {
		QsoID     int64  `json:"qso_id"`
		Forwarder string `json:"forwarder"`
		Action    string `json:"action"`
		Attempts  int    `json:"attempts"`
	}{
		QsoID:     row.QsoID,
		Forwarder: forwarderName,
		Action:    act,
		Attempts:  int(row.Attempts) + 1,
	})
	return b
}
