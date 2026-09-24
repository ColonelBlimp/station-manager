// Package stationevents is the vocabulary of the operator-facing Station Events
// store (W-0020, ADR 0076 shape): the closed (category, kind) pairs the schema
// head enforces, the severity set, and the typed FACTS the daemon's producing
// boundaries hand to the assembly-owned recorder.
//
// It imports only the standard library so every producer-side package can name
// a category or kind without reaching the store, and so the store's tests can
// enumerate the pairs the schema must accept. Producers never build rows: the
// bridge and FT8 seams take primitives, the recorder (cmd/smd) turns them into
// these facts, and the facts into rows — one conversion table, one place.
package stationevents

import "time"

// Categories. The schema CHECK admits only these two, each with its own kind
// set — a kind is legal only under ITS category.
const (
	CategoryNotification = "notification"
	CategoryAlarm        = "alarm"
)

// Kinds. The notification kinds (ADR 0076's two, plus the archive switch
// outcomes of ADR 0071 — recorded by the daemon at the lifecycle boundary) and
// the alarm family (W-0020 rulings 2026-09-14).
const (
	KindExportAdifFailed = "export.adif_failed"
	KindForwardFailed    = "forward.failed"
	// KindArchiveActivated: a pending archive was promoted to active after the
	// restart proved it (recorded only once the promotion write succeeded, in
	// the newly active archive's own file).
	KindArchiveActivated = "archive.activated"
	// KindArchiveActivationFailed: a candidate did not come up and the daemon
	// fell back to the last-known-good archive (recorded in that archive's
	// file, after the fallback reached it). Expected refusals — busy, already
	// active, cancelled — are never events.
	KindArchiveActivationFailed = "archive.activation_failed"
	KindTxAlarmRaised           = "tx_alarm.raised"
	KindTxAlarmCleared          = "tx_alarm.cleared"
	KindDriveAlarmRaised        = "drive_alarm.raised"
	KindDriveAlarmCleared       = "drive_alarm.cleared"
	KindTxDisarmed              = "tx.disarmed"
	KindSessionTerminated       = "session.terminated"
)

// Severities — the closed set the store's CHECK admits (ADR 0008 toast levels).
const (
	SeverityInfo  = "info"
	SeverityWarn  = "warn"
	SeverityError = "error"
)

// PartnerCallMaxLen bounds session.terminated's partner_call (ruling 5): still
// untrusted decoded input, normalised upper-case and trimmed by the recorder.
const PartnerCallMaxLen = 32

// ArchiveLabelMaxLen bounds an archive label on an archive.* row: operator
// text, trimmed to printable runes by the recorder (the catalogue admits up to
// 80; the row never carries more).
const ArchiveLabelMaxLen = 80

// KindsByCategory is the closed pair table: every (category, kind) the store
// accepts, and nothing else. The schema CHECK is written from this table and
// the migration tests enumerate it in both directions, so the two cannot
// drift apart unnoticed. Returned fresh so a caller cannot mutate it.
func KindsByCategory() map[string][]string {
	return map[string][]string{
		CategoryNotification: {
			KindExportAdifFailed, KindForwardFailed,
			KindArchiveActivated, KindArchiveActivationFailed,
		},
		CategoryAlarm: {
			KindTxAlarmRaised, KindTxAlarmCleared,
			KindDriveAlarmRaised, KindDriveAlarmCleared,
			KindTxDisarmed, KindSessionTerminated,
		},
	}
}

// Fact is one typed occurrence a producing boundary reports. The set is sealed:
// the recorder's conversion table is total over it, so a new fact is a
// deliberate act that also adds a kind, a severity and a detail shape (ADR 0076
// §2 — never an arbitrary category/kind/detail envelope).
type Fact interface{ fact() }

// TxAlarmRaised — the TX alarm latched (ADR 0051 codes such as tx_unconfirmed,
// tx_still_keyed). At is the occurrence time captured BEFORE enqueue.
type TxAlarmRaised struct {
	Code string
	At   time.Time
}

// TxAlarmCleared — the standing TX alarm cleared on positive RX evidence. Code
// and RaisedAt are the identity of the alarm that stood, retained by the bridge
// from raise to clear so the row can say how long it stood.
type TxAlarmCleared struct {
	Code     string
	RaisedAt time.Time
	At       time.Time
}

// DriveAlarmRaised — the TX-drive alarm (drive_no_output) raised.
type DriveAlarmRaised struct {
	Code string
	At   time.Time
}

// DriveAlarmRecovered — output confirmed normal after a drive alarm.
type DriveAlarmRecovered struct {
	Code string
	At   time.Time
}

// TxDisarmed — an automatic disarm the operator did not ask for while NO
// partner exchange was in progress (cat_lost, dial_moved). The routine
// unattended linger disarm is lifecycle and is never reported (ruling 2).
type TxDisarmed struct {
	Cause string
	At    time.Time
}

// ExchangeTerminated — a partner exchange ended by the daemon, not the operator
// (unattended, cat_lost, dial_moved, dial_unknown, tx_not_armed,
// tx_bad_message). PartnerCall is raw decoded input here; the recorder bounds
// and normalises it before it reaches a row.
type ExchangeTerminated struct {
	Cause       string
	PartnerCall string
	Rung        string
	At          time.Time
}

// ArchiveActivated — the pending candidate became the active archive.
type ArchiveActivated struct {
	ArchiveID string
	Label     string
	At        time.Time
}

// ArchiveActivationFailed — the candidate did not come up; Code is the stable
// archive failure code (archive.Fail*), never the error text.
type ArchiveActivationFailed struct {
	ArchiveID string
	Label     string
	Code      string
	At        time.Time
}

func (ArchiveActivated) fact()        {}
func (ArchiveActivationFailed) fact() {}
func (TxAlarmRaised) fact()           {}
func (TxAlarmCleared) fact()          {}
func (DriveAlarmRaised) fact()        {}
func (DriveAlarmRecovered) fact()     {}
func (TxDisarmed) fact()              {}
func (ExchangeTerminated) fact()      {}
