package archive

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Seal is a TX admission port (ADR 0071, W-0021 slice 3): the FT8 service's
// SealTxAdmission/ReleaseTxAdmissionSeal and the bridge's SealTx/ReleaseTxSeal,
// injected so this package imports neither subsystem. Seal is a check-and-set
// under the owner's own locks: it refuses, with the busy reason, or holds.
type Seal interface {
	Seal() error
	Release()
}

// SetActivation wires the activation's ports: the two seals (nil = no such
// subsystem) and the restart request (nil = this daemon has no service-manager
// respawn, so activation is refused before anything is sealed or written).
func (m *Manager) SetActivation(tx, rig Seal, restart func() error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.txSeal, m.rigSeal, m.restart = tx, rig, restart
}

// List reports the catalogue as the API serves it, in catalogue order, each
// entry with its state: active (served now), pending (the candidate the next
// start activates) or inactive. A failed candidate is inactive with its
// last_activation_error — never active (ADR 0071 honest state).
func (m *Manager) List() []types.QsoArchiveView {
	snap := m.cfg.Snapshot()
	out := make([]types.QsoArchiveView, 0, len(snap.QsoArchives))
	for _, e := range snap.QsoArchives {
		out = append(out, viewOf(snap, e))
	}
	return out
}

func viewOf(snap config.Config, e types.QsoArchiveConfig) types.QsoArchiveView {
	state := types.QsoArchiveStateInactive
	switch e.ID {
	case snap.ActiveQsoArchiveID:
		state = types.QsoArchiveStateActive
	case snap.PendingQsoArchiveID:
		state = types.QsoArchiveStatePending
	}
	v := types.QsoArchiveView{ID: e.ID, Label: e.Label, Ownership: e.Ownership, State: state, LastActivationError: e.LastActivationError}
	if fi, err := os.Stat(PathFor(snap, e)); err == nil && fi.Mode().IsRegular() {
		v.SizeBytes = fi.Size()
		v.ModifiedAt = fi.ModTime().UTC().Format(time.RFC3339)
	}
	return v
}

// CreateArchive is Create on the wire: the new (or reused) archive as the API
// lists it. Every failure keeps Create's classification (*RequestError for a
// refused request).
func (m *Manager) CreateArchive(ctx context.Context, req types.QsoArchiveCreateRequest) (types.QsoArchiveCreated, error) {
	res, err := m.Create(ctx, req)
	if err != nil {
		return types.QsoArchiveCreated{}, err
	}
	return types.QsoArchiveCreated{Archive: viewOf(m.cfg.Snapshot(), res.Entry), Reused: res.Reused}, nil
}

// Activate requests that the next daemon start serve archive `id` (ADR 0071):
// the attended restart is the switch, this process never swaps handles. In
// order, each step refusing with a *RequestError before anything later runs:
// single flight (one activation per process; a second is activation_in_progress);
// the entry exists (archive_not_found) and is not the active one (archive_active);
// a restart port is wired (restart_unavailable); the file holds this archive's
// identity (archive_unavailable — a missing file or another archive's identity
// would only fail at the next start); the FT8 seal, then the bridge seal, each a
// check-and-set under its owner's locks (tx_busy with the owner's reason; an
// FT8 seal already taken is released when the bridge refuses). Only then is
// pending persisted (file first) and the restart requested, the seals held
// until the process exits.
//
// Abort path (review finding 3c): a definitive persist failure releases both
// seals and leaves nothing on disk (activation_persist_failed); an uncertain
// write is a success carrying durability "uncertain" (startup is safe under
// either truth); a failed restart request clears pending and releases both
// seals (restart_failed), and if that clear fails too the seals are still
// released and the diagnostic goes on the entry so the archive lists as
// pending with it (pending_unclear) — the next restart, whenever it comes,
// performs the activation with the ordinary rollback.
func (m *Manager) Activate(ctx context.Context, id string) (types.QsoArchiveActivation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activated {
		return types.QsoArchiveActivation{}, &RequestError{Code: "activation_in_progress", Message: "an activation is already pending; the daemon is restarting"}
	}
	snap := m.cfg.Snapshot()
	entry := snap.QsoArchiveByID(id)
	if entry == nil {
		return types.QsoArchiveActivation{}, &RequestError{Code: "archive_not_found", Message: "no such archive"}
	}
	if id == snap.ActiveQsoArchiveID {
		return types.QsoArchiveActivation{}, &RequestError{Code: "archive_active", Message: "this archive is already active"}
	}
	if m.restart == nil {
		return types.QsoArchiveActivation{}, &RequestError{Code: "restart_unavailable", Message: "this daemon has no service-manager restart configured; an activation needs the attended restart"}
	}
	path := PathFor(snap, *entry)
	identity, found, err := sqlite.PeekArchiveIdentity(path)
	switch {
	case err != nil:
		return types.QsoArchiveActivation{}, &RequestError{Code: "archive_unavailable", Message: fmt.Sprintf("the archive's file %s cannot be read: %v", path, err)}
	case !found:
		return types.QsoArchiveActivation{}, &RequestError{Code: "archive_unavailable", Message: fmt.Sprintf("the archive's file %s is missing or carries no identity", path)}
	case identity.ArchiveUUID != id:
		return types.QsoArchiveActivation{}, &RequestError{Code: "archive_unavailable", Message: fmt.Sprintf("the file at %s holds archive %s, not %s", path, identity.ArchiveUUID, id)}
	}

	if m.txSeal != nil {
		if err := m.txSeal.Seal(); err != nil {
			return types.QsoArchiveActivation{}, &RequestError{Code: "tx_busy", Message: "FT8 is busy: " + err.Error()}
		}
	}
	if m.rigSeal != nil {
		if err := m.rigSeal.Seal(); err != nil {
			m.releaseSeals()
			return types.QsoArchiveActivation{}, &RequestError{Code: "tx_busy", Message: "the rig is transmitting: " + err.Error()}
		}
	}

	dur, err := m.cfg.Update(func(c *config.Config) error {
		c.PendingQsoArchiveID = id
		return nil
	})
	if err != nil {
		m.releaseSeals()
		return types.QsoArchiveActivation{}, &RequestError{Code: "activation_persist_failed", Message: "the activation could not be recorded in config.json: " + err.Error()}
	}
	if err := m.restart(); err != nil {
		return types.QsoArchiveActivation{}, m.abortAfterPersist(id, err)
	}
	m.activated = true
	out := types.QsoArchiveActivation{ID: id, Durability: types.QsoArchiveDurabilityDurable}
	ev := m.logger.InfoWith().Str("archive_id", id).Str("label", entry.Label)
	if dur == config.DurabilityUncertain {
		out.Durability = types.QsoArchiveDurabilityUncertain
		ev = ev.Bool("durability_uncertain", true)
	}
	ev.Msg("archive: activation requested; transmit admission sealed, restarting")
	return out, nil
}

// abortAfterPersist undoes a persisted pending selector whose restart request
// failed: clear it and release the seals; if the clear fails too, record the
// diagnostic on the entry (memory at least) so the pending listing carries it.
func (m *Manager) abortAfterPersist(id string, cause error) error {
	defer m.releaseSeals()
	_, err := m.cfg.Update(func(c *config.Config) error {
		if c.PendingQsoArchiveID == id {
			c.PendingQsoArchiveID = ""
		}
		return nil
	})
	if err == nil {
		m.logger.ErrorWith().Err(cause).Str("archive_id", id).Msg("archive: restart request failed; activation withdrawn")
		return &RequestError{Code: "restart_failed", Message: "the restart could not be requested; the activation was withdrawn: " + cause.Error()}
	}
	diag := fmt.Sprintf("restart request failed (%v) and the pending selector could not be cleared (%v); the next restart activates this archive", cause, err)
	_, _ = m.cfg.UpdateInMemoryThenPersist(func(c *config.Config) error {
		if e := c.QsoArchiveByID(id); e != nil {
			e.LastActivationError = diag
		}
		return nil
	})
	m.logger.ErrorWith().Err(cause).Str("archive_id", id).Str("clear_error", err.Error()).Msg("archive: restart request failed and pending could not be cleared; the next restart activates")
	return &RequestError{Code: "pending_unclear", Message: diag}
}

func (m *Manager) releaseSeals() {
	if m.rigSeal != nil {
		m.rigSeal.Release()
	}
	if m.txSeal != nil {
		m.txSeal.Release()
	}
}
