package archive

import (
	"context"
	stderr "errors"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// The operations that share one single flight (ADR 0097 §2 step 1): one runs
// at a time per process, and a competitor is refused at once, never queued.
const (
	opDefaultChange = "default_change"
	opActivation    = "activation"
	opLogbookChange = "logbook_change"
)

// The states a default change reports (ADR 0097 §1); the start's states come
// from its report (SetDefaultChangeReport). The first four hold while config
// holds the record; the rest are what it settles into.
const (
	changePendingRestart        = "pending_restart"
	changePendingUnclear        = "pending_unclear"
	changeCleanupPending        = "applied_cleanup_pending"
	changeWithdrawalUnconfirmed = "withdrawal_unconfirmed"
	changeRestartFailed         = "restart_failed"
	changeCancelled             = "cancelled"
	changeWithdrawn             = "withdrawn"
	changeAbandoned             = "not_applied_abandoned"
	changeEffective             = "effective"
)

// settledBy is the state a tombstone's removal settles into.
var settledBy = map[string]string{
	config.RemovedCancelled: changeCancelled,
	config.RemovedWithdrawn: changeWithdrawn,
	config.RemovedAbandoned: changeAbandoned,
	config.RemovedApplied:   changeEffective,
}

// setOutcome records the latest outcome of request rec (lastOutcome).
func (m *Manager) setOutcome(rec config.DefaultLogbookChange, state, code string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastOutcome = types.DefaultChangeView{RequestID: rec.RequestID, LogbookUUID: rec.LogbookUUID, State: state, FailureCode: code}
	m.outcomeVersion++
}

// beginOp takes the single flight for op, or refuses naming the one in flight.
func (m *Manager) beginOp(op string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if m.op != "" {
		return &RequestError{Code: m.op + "_in_progress", Message: "another " + strings.ReplaceAll(m.op, "_", " ") + " is in progress; try again when it has finished"}
	}
	m.op = op
	return nil
}

func (m *Manager) endOp() {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.op = ""
}

// OperationInFlight names the operation holding the single flight a default
// change, an activation and a logbook deletion share ("" when none):
// "default_change", "activation" or "logbook_change".
func (m *Manager) OperationInFlight() string {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	return m.op
}

// WithLogbookChange runs fn (a logbook deletion) under the single flight, so a
// deletion never races a change of the default or an activation.
func (m *Manager) WithLogbookChange(fn func() error) error {
	if err := m.beginOp(opLogbookChange); err != nil {
		return err
	}
	defer m.endOp()
	return fn()
}

// SetLiveEntrySeal wires the live Phone / CW entry seal a default change takes
// after the TX seals (ADR 0097 §2 step 4).
func (m *Manager) SetLiveEntrySeal(s Seal) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.liveSeal = s
}

// StartReport is what this generation's start did with a default change
// (cmd/smd): State while config still holds its record, Settled once it no
// longer does ("" when nothing then remains to report).
type StartReport struct {
	RequestID, LogbookUUID, State, Settled, FailureCode string
}

// SetDefaultChangeReport wires the start's report (cmd/smd), for the status.
func (m *Manager) SetDefaultChangeReport(report func() StartReport) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaultReport = report
}

// RequestDefaultChange records a change of the default and requests the
// restart that applies it (ADR 0097 §2), in order, each step refusing before
// any later one runs:
//
//  1. the single flight, then any record that survives in config: an
//     activation pending, a default change pending or unconfirmed;
//  2. validation in the daemon: the believed default is the effective one
//     (a stale tab is refused, never retargeted), the target is a live
//     logbook of this archive, not the default, under the default's callsign,
//     and a restart can be requested;
//  3. the FT8 seal, then the rig seal (busy refusals; nothing is ended);
//  4. the live-entry seal: admission closes at once, then the admitted live
//     submissions drain;
//  5. the pending selector, persisted (a definitive failure releases every
//     seal; an uncertain write proceeds, reported);
//  6. the restart. Its failure withdraws the change — durable, uncertain or
//     failed, each with its own code — and releases every seal.
//
// Accepted, the seals stay held until the process exits.
func (m *Manager) RequestDefaultChange(ctx context.Context, req types.DefaultChangeRequest) (types.DefaultChangeAccepted, error) {
	if err := m.beginOp(opDefaultChange); err != nil {
		return types.DefaultChangeAccepted{}, err
	}
	defer m.endOp()
	m.mu.Lock()
	tx, rig, live, restart, db, activated := m.txSeal, m.rigSeal, m.liveSeal, m.restart, m.activeDB, m.activated
	m.mu.Unlock()

	snap := m.cfg.Snapshot()
	if activated || snap.PendingQsoArchiveID != "" {
		return types.DefaultChangeAccepted{}, &RequestError{Code: "activation_pending", Message: "an archive activation is pending; the daemon is restarting"}
	}
	if err := recordInPlay(snap.DefaultLogbookChange); err != nil {
		return types.DefaultChangeAccepted{}, err
	}
	if restart == nil {
		return types.DefaultChangeAccepted{}, &RequestError{Code: "restart_unavailable", Message: "this daemon has no service-manager restart configured; a change of the default needs the attended restart"}
	}
	if db == nil {
		return types.DefaultChangeAccepted{}, &RequestError{Code: "bindings_unavailable", Message: "this daemon has no active archive database wired"}
	}
	logbooks, err := db.FetchAllLogbooksWithContext(ctx)
	if err != nil {
		return types.DefaultChangeAccepted{}, err
	}
	var effective, target types.Logbook
	for _, lb := range logbooks {
		if lb.ID == snap.DefaultLogbookID {
			effective = lb
		}
		if lb.UUID != "" && lb.UUID == req.LogbookUUID {
			target = lb
		}
	}
	switch {
	case effective.UUID == "" || req.BelievedDefaultUUID != effective.UUID:
		return types.DefaultChangeAccepted{}, &RequestError{Code: "default_stale", Message: "the default logbook is not the one this page shows; reload the page"}
	case target.ID == 0:
		return types.DefaultChangeAccepted{}, &RequestError{Code: "default_target_unavailable", Message: "that logbook is not a live logbook of the active archive"}
	case target.ID == effective.ID:
		return types.DefaultChangeAccepted{}, &RequestError{Code: "default_target_active", Message: "that logbook is already the default"}
	case !strings.EqualFold(strings.TrimSpace(target.Callsign), strings.TrimSpace(effective.Callsign)):
		return types.DefaultChangeAccepted{}, &RequestError{Code: "default_target_ineligible", Message: "only a logbook with the default logbook's callsign can be made the default"}
	}

	var held []Seal
	release := func() {
		for i := len(held) - 1; i >= 0; i-- {
			held[i].Release()
		}
	}
	for _, step := range []struct {
		seal Seal
		code string
		who  string
	}{{tx, "tx_busy", "FT8 is busy: "}, {rig, "tx_busy", "the rig is transmitting: "}, {live, "live_entry_busy", "live entry could not be closed: "}} {
		if step.seal == nil {
			continue
		}
		if err := step.seal.Seal(); err != nil {
			release()
			return types.DefaultChangeAccepted{}, &RequestError{Code: step.code, Message: step.who + err.Error()}
		}
		held = append(held, step.seal)
	}

	rec := config.DefaultLogbookChange{RequestID: utils.NewUUIDv7(), ArchiveID: snap.ActiveQsoArchiveID, LogbookUUID: target.UUID}
	dur, err := m.cfg.Update(func(c *config.Config) error {
		if c.DefaultLogbookChange != nil {
			return errRecordChanged
		}
		c.DefaultLogbookChange = &rec
		return nil
	})
	if err != nil {
		release()
		return types.DefaultChangeAccepted{}, &RequestError{Code: "default_change_persist_failed", Message: "the change could not be recorded in config.json: " + err.Error()}
	}
	if err := restart(); err != nil {
		release()
		return types.DefaultChangeAccepted{}, m.withdraw(rec, err)
	}
	m.mu.Lock()
	m.defaultRequested = rec.RequestID
	m.mu.Unlock()
	out := types.DefaultChangeAccepted{RequestID: rec.RequestID, Durability: string(types.QsoArchiveDurabilityDurable)}
	ev := m.logger.InfoWith().Str("request_id", rec.RequestID).Int64("logbook_id", target.ID)
	if dur == config.DurabilityUncertain {
		out.Durability = string(types.QsoArchiveDurabilityUncertain)
		ev = ev.Bool("durability_uncertain", true)
	}
	ev.Msg("default logbook change requested; live entry and transmit admission sealed, restarting")
	return out, nil
}

var errRecordChanged = stderr.New("the default logbook change record changed")

// recordInPlay refuses while config holds a default-change record: pending, or
// a removal not yet confirmed durable.
func recordInPlay(rec *config.DefaultLogbookChange) error {
	switch {
	case rec == nil:
		return nil
	case rec.Removed == "":
		return &RequestError{Code: "default_change_pending", Message: "a change of the default logbook is pending; it must be applied or cancelled first"}
	}
	return &RequestError{Code: "default_change_unconfirmed", Message: "the removal of a default logbook change is not yet confirmed on disk; confirm it first"}
}

// withdraw replaces the pending selector with a "withdrawn" tombstone after
// the restart could not be requested. Durable, the withdrawal is confirmed;
// uncertain, it stays unconfirmed; failed, the selector stays (pending
// unclear: the next start applies it unless it is cancelled).
func (m *Manager) withdraw(rec config.DefaultLogbookChange, cause error) error {
	tomb := rec
	tomb.Removed, tomb.FailureCode = config.RemovedWithdrawn, changeRestartFailed
	dur, err := m.cfg.Update(func(c *config.Config) error {
		if c.DefaultLogbookChange == nil || *c.DefaultLogbookChange != rec {
			return errRecordChanged
		}
		c.DefaultLogbookChange = &tomb
		return nil
	})
	switch {
	case err != nil:
		m.setOutcome(rec, changeRestartFailed, "default_change_pending_unclear")
		m.logger.ErrorWith().Err(cause).Str("request_id", rec.RequestID).Str("clear_error", err.Error()).
			Msg("default logbook change: the restart could not be requested and the change could not be withdrawn; the next restart applies it")
		return &RequestError{Code: "default_change_pending_unclear", Message: "the restart could not be requested and the change could not be withdrawn; it applies at the next restart unless cancelled"}
	case dur != config.Durable:
		m.setOutcome(rec, changeRestartFailed, "restart_failed_withdrawal_unconfirmed")
		m.logger.ErrorWith().Err(cause).Str("request_id", rec.RequestID).
			Msg("default logbook change: the restart could not be requested; the withdrawal is not yet confirmed on disk")
		return &RequestError{Code: "restart_failed_withdrawal_unconfirmed", Message: "the restart could not be requested; the change was withdrawn, but not yet confirmed on disk: " + cause.Error()}
	}
	m.setOutcome(rec, changeRestartFailed, "restart_failed")
	m.logger.ErrorWith().Err(cause).Str("request_id", rec.RequestID).Msg("default logbook change: the restart could not be requested; the change was withdrawn")
	return &RequestError{Code: "restart_failed", Message: "the restart could not be requested; the change was withdrawn: " + cause.Error()}
}

// actionTarget checks request id against the record in play for an action
// (ADR 0097 §1): already resolved when there is none, superseded when another
// request holds it, and refused while this one's restart is requested.
func (m *Manager) actionTarget(id string) (config.DefaultLogbookChange, error) {
	rec := m.cfg.Snapshot().DefaultLogbookChange
	switch {
	case rec == nil:
		return config.DefaultLogbookChange{}, &RequestError{Code: "already_resolved", Message: "that default logbook change is already resolved"}
	case rec.RequestID != id:
		return config.DefaultLogbookChange{}, &RequestError{Code: "pending_changed", Message: "another default logbook change is now in play; nothing was done"}
	}
	m.mu.Lock()
	requested := m.defaultRequested
	m.mu.Unlock()
	if requested == id {
		return config.DefaultLogbookChange{}, &RequestError{Code: opDefaultChange + "_in_progress", Message: "the change is applying; the daemon is restarting"}
	}
	return *rec, nil
}

var errNotPermitted = &RequestError{Code: "not_permitted", Message: "that action does not apply to the default logbook change in its current state"}

// CancelDefaultChange withdraws pending request id while the daemon runs on
// the old default: a "cancelled" tombstone, confirmed when durable.
func (m *Manager) CancelDefaultChange(ctx context.Context, id string) (types.DefaultChangeStatus, error) {
	return m.recoveryAction(ctx, id, func(rec config.DefaultLogbookChange, effective types.Logbook) (*config.DefaultLogbookChange, bool) {
		if rec.Removed != "" || rec.LogbookUUID == effective.UUID {
			return nil, false // a tombstone, or a change already applied
		}
		tomb := rec
		tomb.Removed = config.RemovedCancelled
		return &tomb, true
	})
}

// ConfirmDefaultChange confirms request id's unconfirmed withdrawal by a
// durable rewrite that keeps its tombstone; it never alters a selector.
func (m *Manager) ConfirmDefaultChange(ctx context.Context, id string) (types.DefaultChangeStatus, error) {
	return m.recoveryAction(ctx, id, func(rec config.DefaultLogbookChange, _ types.Logbook) (*config.DefaultLogbookChange, bool) {
		if rec.Removed == "" || rec.Removed == config.RemovedApplied {
			return nil, false
		}
		return &rec, true
	})
}

// RetryDefaultCleanup finishes request id's applied cleanup: a selector naming
// the effective default becomes its "applied" tombstone, and an "applied"
// tombstone is rewritten durably.
func (m *Manager) RetryDefaultCleanup(ctx context.Context, id string) (types.DefaultChangeStatus, error) {
	return m.recoveryAction(ctx, id, func(rec config.DefaultLogbookChange, effective types.Logbook) (*config.DefaultLogbookChange, bool) {
		switch {
		case rec.Removed == "" && rec.LogbookUUID == effective.UUID:
			tomb := rec
			tomb.Removed = config.RemovedApplied
			return &tomb, true
		case rec.Removed == config.RemovedApplied:
			return &rec, true
		}
		return nil, false
	})
}

// recoveryAction runs one action under the single flight: the request id is
// checked against the record in play, decide says what the record becomes
// (or that the action does not apply), and the write is a forced rewrite that
// rechecks the record under config's lock, so a delayed action never touches
// a later request. The config service confirms a tombstone the write held
// durably.
func (m *Manager) recoveryAction(ctx context.Context, id string, decide func(rec config.DefaultLogbookChange, effective types.Logbook) (*config.DefaultLogbookChange, bool)) (types.DefaultChangeStatus, error) {
	if err := m.beginOp(opDefaultChange); err != nil {
		return types.DefaultChangeStatus{}, err
	}
	defer m.endOp()
	rec, err := m.actionTarget(id)
	if err != nil {
		return types.DefaultChangeStatus{}, err
	}
	effective, err := m.effectiveDefault(ctx, m.cfg.Snapshot().DefaultLogbookID)
	if err != nil {
		return types.DefaultChangeStatus{}, err
	}
	next, ok := decide(rec, effective)
	if !ok {
		return types.DefaultChangeStatus{}, errNotPermitted
	}
	_, _, err = m.cfg.UpdateIfChanged(true, func(c *config.Config) error {
		if c.DefaultLogbookChange == nil || *c.DefaultLogbookChange != rec {
			return errRecordChanged
		}
		c.DefaultLogbookChange = next
		return nil
	})
	switch {
	case stderr.Is(err, errRecordChanged) && m.cfg.Snapshot().DefaultLogbookChange == nil:
		// A durable save confirmed it between the check and this write.
		return types.DefaultChangeStatus{}, &RequestError{Code: "already_resolved", Message: "that default logbook change was resolved meanwhile; nothing was done"}
	case stderr.Is(err, errRecordChanged):
		return types.DefaultChangeStatus{}, &RequestError{Code: "pending_changed", Message: "the default logbook change changed meanwhile; nothing was done"}
	case err != nil:
		return types.DefaultChangeStatus{}, &RequestError{Code: "default_change_write_failed", Message: "config.json could not be written: " + err.Error()}
	}
	m.mu.Lock()
	last := m.lastOutcome
	m.mu.Unlock()
	// A Confirm keeps the outcome this process already holds for the request
	// (a restart failure stays a restart failure); otherwise the removal's own,
	// with its reason (an abandonment's code).
	if last.RequestID != rec.RequestID || next.Removed != rec.Removed {
		m.setOutcome(rec, settledBy[next.Removed], next.FailureCode)
	}
	return m.DefaultChangeStatus(ctx)
}

// effectiveDefault is logbook id, the default this generation logs to.
func (m *Manager) effectiveDefault(ctx context.Context, id int64) (types.Logbook, error) {
	m.mu.Lock()
	db := m.activeDB
	m.mu.Unlock()
	if db == nil {
		return types.Logbook{}, &RequestError{Code: "bindings_unavailable", Message: "this daemon has no active archive database wired"}
	}
	logbooks, err := db.FetchAllLogbooksWithContext(ctx)
	if err != nil {
		return types.Logbook{}, err
	}
	for _, lb := range logbooks {
		if lb.ID == id {
			return lb, nil
		}
	}
	return types.Logbook{ID: id}, nil
}

// DefaultChangeStatus reports the effective default and the change in play,
// derived from ONE config snapshot so a save landing meanwhile cannot mix two
// states: while that snapshot holds the record (pending, or a removal not yet
// confirmed durable) the record decides; otherwise the latest outcome this
// process acted on, else what this start settled into. A state that holds
// only while the record does is never reported without it.
func (m *Manager) DefaultChangeStatus(ctx context.Context) (types.DefaultChangeStatus, error) {
	for {
		out, stable, err := m.defaultChangeStatusOnce(ctx)
		if err != nil || stable {
			return out, err
		}
	}
}

// defaultChangeStatusOnce builds the status from one config snapshot and the
// runtime outcome as it stood then. An outcome is recorded only after its
// request's config write, so when outcomeVersion is unchanged across the
// snapshot, every outcome read was recorded before it and the snapshot already
// reflects that write: "no record" then means the removal was confirmed, never
// a write the snapshot simply predates. stable is false when it moved; the
// caller reads again.
func (m *Manager) defaultChangeStatusOnce(ctx context.Context) (types.DefaultChangeStatus, bool, error) {
	m.mu.Lock()
	before, report := m.outcomeVersion, m.defaultReport
	m.mu.Unlock()
	snap := m.cfg.Snapshot()
	var start StartReport
	if report != nil {
		start = report()
	}
	effective, err := m.effectiveDefault(ctx, snap.DefaultLogbookID)
	if err != nil {
		return types.DefaultChangeStatus{}, true, err
	}
	m.mu.Lock()
	after, requested, last := m.outcomeVersion, m.defaultRequested, m.lastOutcome
	m.mu.Unlock()
	if after != before {
		return types.DefaultChangeStatus{}, false, nil
	}
	out := types.DefaultChangeStatus{Effective: effective}
	if rec := snap.DefaultLogbookChange; rec != nil {
		v := types.DefaultChangeView{RequestID: rec.RequestID, LogbookUUID: rec.LogbookUUID, Removed: rec.Removed,
			FailureCode: rec.FailureCode, Unconfirmed: true}
		switch {
		case last.RequestID == rec.RequestID && last.FailureCode != "":
			v.FailureCode = last.FailureCode
		case v.FailureCode == "" && start.RequestID == rec.RequestID:
			v.FailureCode = start.FailureCode
		}
		switch {
		case rec.Removed == "" && requested == rec.RequestID:
			v.State = changePendingRestart
		case rec.Removed == "" && rec.LogbookUUID == effective.UUID, rec.Removed == config.RemovedApplied:
			v.State = changeCleanupPending
		case rec.Removed == "" && last.RequestID != rec.RequestID && start.RequestID == rec.RequestID && start.State != "":
			v.State = start.State
		case rec.Removed == "":
			v.State = changePendingUnclear
		default:
			v.State = changeWithdrawalUnconfirmed
		}
		out.Change = &v
		return out, true, nil
	}
	switch {
	case last.RequestID != "":
		// The record is gone, so a withdrawal is confirmed now, whatever was
		// known when it was written.
		if last.FailureCode == "restart_failed_withdrawal_unconfirmed" {
			last.FailureCode = changeRestartFailed
		}
		out.Change = &last
	case start.Settled != "":
		out.Change = &types.DefaultChangeView{RequestID: start.RequestID, LogbookUUID: start.LogbookUUID, State: start.Settled, FailureCode: start.FailureCode}
	}
	return out, true, nil
}
