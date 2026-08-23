package types

import (
	"encoding/json"
	"time"
)

// OperatorEvent mirrors an operator_event row — the local, categorised
// operator-facing event store (ADR 0076, the W-0001 notification pilot). It is
// the read DTO for the notification-history surface.
//
// Detail is the typed, bounded metadata the producing boundary recorded, as the
// raw JSON stored (json_valid-checked on write). It never carries raw
// third-party/provider error text. It is returned verbatim as json.RawMessage
// so a JSON response embeds it as an object rather than a base64 blob.
type OperatorEvent struct {
	ID         int64           `json:"id"`
	Category   string          `json:"category"`
	Kind       string          `json:"kind"`
	Severity   string          `json:"severity"`
	OccurredAt time.Time       `json:"occurred_at"`
	Build      string          `json:"build"`
	Detail     json.RawMessage `json:"detail"`
}
