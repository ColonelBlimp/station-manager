package types

import "time"

// QsoHistory mirrors a qso_history row — the append-only audit trail
// for QSO mutations (ADR 0016 prep #2). One row is appended every
// time a QSO is updated or deleted; INSERT-time provenance is
// already covered by Qso.AdditionalData per ADR 0014 prep #4.
//
// BeforeImage carries the pre-mutation snapshot as the raw JSON the
// daemon stored. Operator-facing endpoints typically deserialize it
// into a Qso for display; we keep the raw bytes here to avoid a
// double round-trip when the consumer just wants to forward the
// snapshot (e.g. SM Cloud sync).
type QsoHistory struct {
	ID          int64     `json:"id"`
	QsoUUID     string    `json:"qso_uuid"`
	Op          string    `json:"op"`
	At          time.Time `json:"at"`
	Source      string    `json:"source"`
	BeforeImage []byte    `json:"before_image"`
}
