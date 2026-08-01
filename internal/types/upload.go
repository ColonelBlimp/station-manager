package types

import "time"

// QsoUpload mirrors a qso_upload row — one row per (QSO, forwarder, action)
// triple. Shape driven by docs/v2-design/forwarding.md §6.
// ForwarderName is the per-instance config handle; ForwarderType is the plugin
// identifier (e.g. "qrz"). Both are stored on the row so historical entries
// remain interpretable after an operator renames or removes an instance.
type QsoUpload struct {
	ID            int64     `json:"id" boil:"id,bind"`
	CreatedAt     time.Time `json:"created_at" boil:"created_at,bind"`
	ModifiedAt    time.Time `json:"modified_at" boil:"modified_at,bind"`
	QsoID         int64     `json:"qso_id" boil:"qso_id,bind"`
	ForwarderName string    `json:"forwarder_name" boil:"forwarder_name,bind"`
	ForwarderType string    `json:"forwarder_type" boil:"forwarder_type,bind"`
	Action        string    `json:"action" boil:"action,bind"`
	Status        string    `json:"status" boil:"status,bind"`
	Attempts      int64     `json:"attempts" boil:"attempts,bind"`
	LastAttemptAt int64     `json:"last_attempt_at" boil:"last_attempt_at,bind"`
	NextAttemptAt int64     `json:"next_attempt_at" boil:"next_attempt_at,bind"`
	LastError     string    `json:"last_error" boil:"last_error,bind"`
	UpstreamID    string    `json:"upstream_id" boil:"upstream_id,bind"`
	// Origin names what CAUSED this queue entry to exist — live logging, an
	// import, an operator edit, a manual backfill, a stamp-sync mirror, or a
	// reconcile repair; `legacy` for rows carried over by migration 0007.
	// Distinct from Action, which says what MUTATION is being forwarded: a delete
	// enqueued by an operator is action=delete, origin=edit.
	//
	// Deliberately NOT `omitempty`: an absent field and an unknown provenance must
	// not look the same on the wire, the same reasoning bridge.RigStatePayload's
	// SplitOverride uses a *bool for. That also means this field is NOT
	// wire-inert — it exposes "" from the moment it exists — so it must ship in
	// the same commit as its migration and producers, never alone.
	Origin string `json:"origin" boil:"origin,bind"`
}
