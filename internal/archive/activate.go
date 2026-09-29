package archive

import (
	"context"
	"os"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
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
	sums, _ := ReadSummaries(SummariesPath(snap))
	active := m.liveSummary()
	out := make([]types.QsoArchiveView, 0, len(snap.QsoArchives))
	for _, e := range snap.QsoArchives {
		v := viewOf(snap, e)
		v.Logbooks, v.ContentsStatus = contentsOf(snap, e, sums, active)
		out = append(out, v)
	}
	return out
}

// ActiveSummaryView is the live summary of the active archive (ActiveSummary).
type ActiveSummaryView interface {
	Snapshot() (archiveID string, logbooks []LogbookSummary, status string)
}

// SetActiveSummary wires the active archive's live summary (ADR 0084 slice 2b).
func (m *Manager) SetActiveSummary(a ActiveSummaryView) {
	m.summaryMu.Lock()
	defer m.summaryMu.Unlock()
	m.activeSummary = a
}

func (m *Manager) liveSummary() ActiveSummaryView {
	m.summaryMu.Lock()
	defer m.summaryMu.Unlock()
	return m.activeSummary
}

// contentsOf is what an archive holds and whether that can be trusted: the
// active archive's live summary when the tracker is for it; otherwise the
// sidecar's, current only while the file still matches the signature it was
// taken against and no committed changes remain in its WAL. Never nil logbooks.
func contentsOf(snap config.Config, e types.QsoArchiveConfig, sums Summaries, active ActiveSummaryView) ([]types.QsoArchiveLogbook, string) {
	if active != nil && e.ID == snap.ActiveQsoArchiveID {
		if id, lbs, status := active.Snapshot(); id == e.ID {
			if lbs == nil {
				lbs = []types.QsoArchiveLogbook{}
			}
			return lbs, status
		}
	}
	s, ok := sums[e.ID]
	if !ok {
		return []types.QsoArchiveLogbook{}, ContentsUnknown
	}
	lbs := s.Logbooks
	if lbs == nil {
		lbs = []types.QsoArchiveLogbook{}
	}
	path := PathFor(snap, e)
	if now, err := SignatureOf(path); err == nil && s.Current(now) && walEmptyOrMissing(path) {
		return lbs, ContentsCurrent
	}
	return lbs, ContentsStale
}

// walEmptyOrMissing reports whether the main-file signature covers the whole
// archive. SQLite may commit into a nonempty WAL without changing that signature;
// any other stat failure also leaves freshness unproven.
func walEmptyOrMissing(dbPath string) bool {
	fi, err := os.Stat(dbPath + "-wal")
	if err != nil {
		return os.IsNotExist(err)
	}
	return fi.Size() == 0
}

func viewOf(snap config.Config, e types.QsoArchiveConfig) types.QsoArchiveView {
	state := types.QsoArchiveStateInactive
	switch e.ID {
	case snap.ActiveQsoArchiveID:
		state = types.QsoArchiveStateActive
	case snap.PendingQsoArchiveID:
		state = types.QsoArchiveStatePending
	}
	code := NormalizeFailureCode(e.LastActivationError)
	v := types.QsoArchiveView{ID: e.ID, Label: e.Label, Ownership: e.Ownership, State: state, LastActivationCode: code, LastActivationError: FailureMessage(code)}
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
	snap := m.cfg.Snapshot()
	v := viewOf(snap, res.Entry)
	sums, _ := ReadSummaries(SummariesPath(snap))
	v.Logbooks, v.ContentsStatus = contentsOf(snap, res.Entry, sums, m.liveSummary())
	return types.QsoArchiveCreated{Archive: v, Reused: res.Reused}, nil
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
	// Preflight with the one classifier the start uses: the refusal carries the
	// stable code and the operator's wording; the path and the detail go to the
	// log, not the response. An expected refusal is not a durable failure — it
	// is neither recorded on the entry nor as a Station Event.
	paths, err := Resolve(snap, id)
	if err != nil {
		return types.QsoArchiveActivation{}, &RequestError{Code: "archive_not_found", Message: "no such archive"}
	}
	if err := VerifyIdentity(paths); err != nil {
		code := FailureCode(err)
		m.logger.WarnWith().Err(err).Str("archive_id", id).Str("code", code).Msg("archive: activation refused at preflight")
		return types.QsoArchiveActivation{}, &RequestError{Code: code, Message: FailureMessage(code)}
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
	// The entry carries the CODE only; the diagnostic goes to the log.
	_, _ = m.cfg.UpdateInMemoryThenPersist(func(c *config.Config) error {
		if e := c.QsoArchiveByID(id); e != nil {
			e.LastActivationError = FailPendingUnclear
		}
		return nil
	})
	m.logger.ErrorWith().Err(cause).Str("archive_id", id).Str("clear_error", err.Error()).Msg("archive: restart request failed and pending could not be cleared; the next restart activates")
	return &RequestError{Code: FailPendingUnclear, Message: FailureMessage(FailPendingUnclear)}
}

func (m *Manager) releaseSeals() {
	if m.rigSeal != nil {
		m.rigSeal.Release()
	}
	if m.txSeal != nil {
		m.txSeal.Release()
	}
}
