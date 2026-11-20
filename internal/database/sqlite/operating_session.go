package sqlite

import (
	"context"
	"database/sql"
	stderr "errors"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// The operating session's record (ADR 0100; W-0029 part 1): the transitions of
// the session kept in the archive file, each one transaction, and the step
// that puts a live QSO into the open session inside the QSO's own transaction.
// Plain SQL over the tables of log migration 0020, as contest_serial.go is
// (operator ruling 2026-11-19).
//
// Nothing here knows which desktop app is running. A caller names the app
// lifetime it acts for, and says whether that lifetime is the current one
// where the answer depends on it.

// Close reasons, as stored in operating_session.close_reason. The set is
// checked here, not by the schema, so a new reason needs no table rebuild.
const (
	SessionClosedByExit      = "app_closed"
	SessionClosedByNew       = "new_session"
	SessionClosedByContinue  = "continued_other"
	SessionClosedByDefault   = "logbook_changed"
	SessionClosedBySwitch    = "archive_switched"
	SessionClosedUnconfirmed = "unconfirmed_exit"
)

// SessionOutcome says what a transition did. A refusal is an outcome, not an
// error: an error is a database failure, and then nothing was stored.
type SessionOutcome string

const (
	// SessionApplied: the transition did what it was asked.
	SessionApplied SessionOutcome = "applied"
	// SessionUnchanged: there was nothing to do (the lifetime already holds
	// the open session; no session was open to close).
	SessionUnchanged SessionOutcome = "unchanged"
	// SessionStale: the generation named is not the current one.
	SessionStale SessionOutcome = "stale"
	// SessionRetired: the lifetime's exit is recorded; it never acts again.
	SessionRetired SessionOutcome = "retired"
	// SessionSetupPending: the file has no default logbook to bind a session to.
	SessionSetupPending SessionOutcome = "setup_pending"
	// SessionRecoveryPending: an open session with members is held by another
	// lifetime, and the operator has not yet answered Continue or New.
	SessionRecoveryPending SessionOutcome = "recovery_pending"
	// SessionIneligible: the session named for Continue cannot be continued.
	SessionIneligible SessionOutcome = "ineligible"
	// SessionNotCurrent: an exit reported by a lifetime that is not current
	// and still holds the open session. Terminal for that report.
	SessionNotCurrent SessionOutcome = "not_current"
	// SessionExitAlreadyRecorded: the exit was recorded by an earlier report.
	SessionExitAlreadyRecorded SessionOutcome = "exit_already_recorded"
	// SessionExitRecorded: the exit is recorded and no session changed by it.
	SessionExitRecorded SessionOutcome = "exit_recorded"
)

// OperatingSession is the open session of an archive file.
type OperatingSession struct {
	ID           string
	LogbookUUID  string
	DefaultEpoch int64
	OpenedAt     time.Time
	// IntervalStartedAt and Holder describe the open interval: the current
	// sitting, and the app lifetime that holds it ("" when none does).
	IntervalStartedAt time.Time
	Holder            string
	// HasMembers is true when the session has any member row, a deleted
	// QSO's included: such a session is never discarded.
	HasMembers bool
}

// OperatingSessionState is one snapshot of an archive's session state. Every
// part belongs to Generation.
type OperatingSessionState struct {
	Generation   int64
	DefaultEpoch int64
	// Open is nil when no session is open.
	Open *OperatingSession
	// ExitRecorded is whether the lifetime the read named has a recorded exit.
	ExitRecorded bool
}

// takeSessionWriterPosition is (*sessionTx).takeWriterPosition, as a variable
// so the contention tests can stand where SQLite's busy handler stands: see a
// refused attempt, repeat it when they choose, and hold a control once it has
// the writer's position. Nothing but those tests sets it.
var takeSessionWriterPosition = (*sessionTx).takeWriterPosition

// sessionTx is one transaction over the session's record: a control's own, or
// the QSO's during the logging step.
type sessionTx struct {
	ctx     context.Context
	tx      *sql.Tx
	now     time.Time
	changed bool // session state changed: the generation advances once at the end
}

// sessionHead is the archive-level state a transition starts from.
type sessionHead struct {
	generation, epoch int64
	// defaultUUID is the uuid of the file's live default logbook, "" when the
	// file has none (setup pending).
	defaultUUID string
}

// takeWriterPosition makes the transaction a writer before it reads anything
// it will act on (ADR 0100 §2). A transaction that read first would hold a
// snapshot, and SQLite refuses its later write when another connection has
// committed in between (the reason InsertQsoTx writes first too). The update
// changes nothing; it is here for the lock. No row means no archive identity.
func (c *sessionTx) takeWriterPosition() error {
	res, err := c.tx.ExecContext(c.ctx, `UPDATE archive_metadata SET session_generation = session_generation WHERE singleton = 1`)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return stderr.Join(errors.ErrNotFound, err)
	}
	return nil
}

func (c *sessionTx) head() (sessionHead, error) {
	var (
		h    sessionHead
		id   sql.NullInt64
		uuid sql.NullString
	)
	err := c.tx.QueryRowContext(c.ctx, `SELECT m.session_generation, m.default_epoch, l.id, l.uuid
		FROM archive_metadata m LEFT JOIN logbook l ON l.id = m.default_logbook_id AND l.deleted_at IS NULL
		WHERE m.singleton = 1`).Scan(&h.generation, &h.epoch, &id, &uuid)
	if stderr.Is(err, sql.ErrNoRows) {
		return h, errors.ErrNotFound
	}
	if err != nil {
		return h, err
	}
	if id.Valid && uuid.String == "" {
		return h, stderr.New("the default logbook has no uuid")
	}
	h.defaultUUID = uuid.String
	return h, nil
}

// open reads the open session, nil when there is none.
func (c *sessionTx) open() (*OperatingSession, error) {
	var (
		o       OperatingSession
		started sql.NullTime
	)
	err := c.tx.QueryRowContext(c.ctx, `SELECT s.id, s.logbook_uuid, s.default_epoch, s.opened_at,
			i.started_at, COALESCE(i.app_lifetime, ''),
			EXISTS (SELECT 1 FROM operating_session_member m WHERE m.session_id = s.id)
		FROM operating_session s
		LEFT JOIN operating_session_interval i ON i.session_id = s.id AND i.end_state = 'open'
		WHERE s.closed_at IS NULL`).
		Scan(&o.ID, &o.LogbookUUID, &o.DefaultEpoch, &o.OpenedAt, &started, &o.Holder, &o.HasMembers)
	if stderr.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !started.Valid {
		return nil, stderr.New("the open session has no open interval")
	}
	o.IntervalStartedAt = started.Time
	return &o, nil
}

// start reads what every transition acts on, and runs the default-epoch test
// (ADR 0100 §4): an open session of an earlier default is closed with
// logbook_changed, whoever holds it. The session returned is the one still
// open afterwards.
func (c *sessionTx) start() (sessionHead, *OperatingSession, error) {
	h, err := c.head()
	if err != nil {
		return h, nil, err
	}
	o, err := c.open()
	if err != nil || o == nil || o.DefaultEpoch == h.epoch {
		return h, o, err
	}
	return h, nil, c.close(o, SessionClosedByDefault, true)
}

// stale is the stale check of a control (ADR 0100 §5), made after start. A
// close by the default-epoch test has moved the generation, so the one the
// caller saw cannot be current.
func (c *sessionTx) stale(h sessionHead, generation int64) bool {
	return c.changed || generation != h.generation
}

// one runs a statement that must change exactly one row.
func (c *sessionTx) one(query string, args ...any) error {
	res, err := c.tx.ExecContext(c.ctx, query, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return stderr.New("the session record is not as it was read")
	}
	return nil
}

// close ends the open session. One with no member row is removed with its
// intervals instead of kept (ADR 0100 ruling 4); a deleted QSO's row counts as
// a member. A confirmed end is at this transaction's instant; an unconfirmed
// one has no time, since none is known.
func (c *sessionTx) close(o *OperatingSession, reason string, confirmed bool) error {
	c.changed = true
	if !o.HasMembers {
		return c.one(`DELETE FROM operating_session WHERE id = ? AND closed_at IS NULL`, o.ID)
	}
	var err error
	if confirmed {
		err = c.one(`UPDATE operating_session_interval SET end_state = 'confirmed', ended_at = ?
			WHERE session_id = ? AND end_state = 'open'`, c.now, o.ID)
	} else {
		err = c.endUnconfirmed(o.ID)
	}
	if err != nil {
		return err
	}
	return c.one(`UPDATE operating_session SET closed_at = ?, close_reason = ? WHERE id = ? AND closed_at IS NULL`, c.now, reason, o.ID)
}

func (c *sessionTx) endUnconfirmed(sessionID string) error {
	return c.one(`UPDATE operating_session_interval SET end_state = 'unconfirmed'
		WHERE session_id = ? AND end_state = 'open'`, sessionID)
}

// openNew opens a session bound to the default logbook and the current epoch,
// with its first interval.
func (c *sessionTx) openNew(h sessionHead, holder string) (string, error) {
	id := utils.NewUUIDv7At(c.now)
	if err := c.one(`INSERT INTO operating_session (id, logbook_uuid, default_epoch, opened_at) VALUES (?, ?, ?, ?)`,
		id, h.defaultUUID, h.epoch, c.now); err != nil {
		return "", err
	}
	return id, c.addInterval(id, holder)
}

// addInterval starts a sitting of a session, held by an app lifetime or, when
// the logging step opened the session with no app current, by none.
func (c *sessionTx) addInterval(sessionID, holder string) error {
	c.changed = true
	return c.one(`INSERT INTO operating_session_interval (session_id, seq, started_at, end_state, app_lifetime)
		SELECT ?1, COALESCE(MAX(seq), 0) + 1, ?2, 'open', NULLIF(?3, '')
		FROM operating_session_interval WHERE session_id = ?1`, sessionID, c.now, holder)
}

func (c *sessionTx) exitRecorded(lifetime string) (bool, error) {
	var n int
	err := c.tx.QueryRowContext(c.ctx, `SELECT COUNT(*) FROM operating_app_exit WHERE app_lifetime = ?`, lifetime).Scan(&n)
	return n == 1, err
}

// retired is the first question of every control a lifetime asks for: a
// lifetime whose exit is recorded acts no more, and touches no session. It
// comes before the default-epoch test and the stale check. A New or a Continue
// sent before the exit and arriving after it can name a generation that is
// still current, since the exit of a lifetime holding no session moves none.
func (c *sessionTx) retired(lifetime string) (bool, error) {
	return c.exitRecorded(lifetime)
}

func (c *sessionTx) recordExit(lifetime string) error {
	return c.one(`INSERT INTO operating_app_exit (app_lifetime, exited_at) VALUES (?, ?)`, lifetime, c.now)
}

// finish advances the generation once if session state changed, however many
// steps the transaction took (W-0029 part 1, C4).
func (c *sessionTx) finish() error {
	if !c.changed {
		return nil
	}
	return c.one(`UPDATE archive_metadata SET session_generation = session_generation + 1 WHERE singleton = 1`)
}

// state reads the whole state inside this transaction: one snapshot.
func (c *sessionTx) state(lifetime string) (OperatingSessionState, error) {
	h, err := c.head()
	if err != nil {
		return OperatingSessionState{}, err
	}
	st := OperatingSessionState{Generation: h.generation, DefaultEpoch: h.epoch}
	if st.Open, err = c.open(); err != nil {
		return OperatingSessionState{}, err
	}
	st.ExitRecorded, err = c.exitRecorded(lifetime)
	return st, err
}

// OperatingSessionStateWithContext reads the archive's session state in one
// snapshot: the generation, the default epoch, the open session if any, and
// whether the named lifetime's exit is recorded. A file with no archive
// identity is errors.ErrNotFound.
func (s *Service) OperatingSessionStateWithContext(ctx context.Context, lifetime string) (OperatingSessionState, error) {
	const op errors.Op = "sqlite.Service.OperatingSessionStateWithContext"
	if err := checkService(op, s); err != nil {
		return OperatingSessionState{}, err
	}
	h, err := s.getOpenHandle(op)
	if err != nil {
		return OperatingSessionState{}, err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()
	tx, err := h.BeginTx(ctx, nil)
	if err != nil {
		return OperatingSessionState{}, errors.New(op).WithErr(err).WithMsg("begin")
	}
	defer func() { _ = tx.Rollback() }()
	st, err := (&sessionTx{ctx: ctx, tx: tx}).state(lifetime)
	if err != nil {
		return OperatingSessionState{}, errors.New(op).WithErr(err).WithMsg("read the operating session state")
	}
	return st, nil
}

// sessionControl runs one control: a durable transaction (operator ruling
// 2026-11-20: a control survives a power cut once reported) that takes the
// writer's position before it reads. It returns the state as committed, for a
// refusal too. An error means nothing was stored.
func (s *Service) sessionControl(ctx context.Context, op errors.Op, what, lifetime string, now time.Time,
	control func(*sessionTx) (SessionOutcome, error),
) (OperatingSessionState, SessionOutcome, error) {
	if err := checkService(op, s); err != nil {
		return OperatingSessionState{}, "", err
	}
	var (
		st      OperatingSessionState
		outcome SessionOutcome
	)
	err := s.durableTx(ctx, op, what, func(ctx context.Context, tx *sql.Tx) error {
		c := &sessionTx{ctx: ctx, tx: tx, now: now.UTC()}
		if err := takeSessionWriterPosition(c); err != nil {
			return err
		}
		var err error
		if outcome, err = control(c); err != nil {
			return err
		}
		if err := c.finish(); err != nil {
			return err
		}
		st, err = c.state(lifetime)
		return err
	})
	if err != nil {
		return OperatingSessionState{}, "", errors.New(op).WithErr(err).WithMsg(what)
	}
	return st, outcome, nil
}

// OpenOperatingSessionWithContext is the start step for an app lifetime
// becoming current (W-0029, proposal A), decided from the file alone:
//
//   - the lifetime's exit is recorded: SessionRetired, before anything else,
//     so a retired caller touches no session;
//   - no default logbook: SessionSetupPending;
//   - the lifetime holds the open session: SessionUnchanged;
//   - another lifetime holds it, and it has a member: SessionRecoveryPending;
//   - another lifetime holds it, and it is empty: it is discarded and a new
//     one is opened, SessionApplied;
//   - none is open: one is opened, SessionApplied.
//
// The default-epoch test runs before the table for a lifetime that is not
// retired. Running the step twice leaves what running it once leaves.
func (s *Service) OpenOperatingSessionWithContext(ctx context.Context, lifetime string, now time.Time) (OperatingSessionState, SessionOutcome, error) {
	const op errors.Op = "sqlite.Service.OpenOperatingSessionWithContext"
	if lifetime == "" {
		return OperatingSessionState{}, "", errors.New(op).WithMsg("the app lifetime is required")
	}
	return s.sessionControl(ctx, op, "open the operating session", lifetime, now, func(c *sessionTx) (SessionOutcome, error) {
		if retired, err := c.retired(lifetime); err != nil || retired {
			return SessionRetired, err
		}
		h, o, err := c.start()
		switch {
		case err != nil:
			return "", err
		case h.defaultUUID == "":
			return SessionSetupPending, nil
		case o != nil && o.Holder == lifetime:
			return SessionUnchanged, nil
		case o != nil && o.HasMembers:
			return SessionRecoveryPending, nil
		case o != nil:
			if err := c.close(o, SessionClosedUnconfirmed, false); err != nil { // empty: removed
				return "", err
			}
		}
		_, err = c.openNew(h, lifetime)
		return SessionApplied, err
	})
}

// NewOperatingSessionWithContext closes the open session and opens a new one
// held by the lifetime (ADR 0100 §3, §8). Closing its own session is
// new_session with a confirmed end; closing another lifetime's is the recovery
// answer New, unconfirmed_exit with an unconfirmed end. With none open it
// opens one. A generation that is not current is SessionStale. A lifetime
// whose exit is recorded is SessionRetired, before anything else.
//
// Whether the lifetime is the CURRENT one is the caller's to establish, under
// its own admission order: this refuses only what the file itself records.
func (s *Service) NewOperatingSessionWithContext(ctx context.Context, lifetime string, generation int64, now time.Time) (OperatingSessionState, SessionOutcome, error) {
	const op errors.Op = "sqlite.Service.NewOperatingSessionWithContext"
	if lifetime == "" {
		return OperatingSessionState{}, "", errors.New(op).WithMsg("the app lifetime is required")
	}
	return s.sessionControl(ctx, op, "start a new operating session", lifetime, now, func(c *sessionTx) (SessionOutcome, error) {
		if retired, err := c.retired(lifetime); err != nil || retired {
			return SessionRetired, err
		}
		h, o, err := c.start()
		switch {
		case err != nil:
			return "", err
		case c.stale(h, generation):
			return SessionStale, nil
		case h.defaultUUID == "":
			return SessionSetupPending, nil
		case o != nil && o.Holder == lifetime:
			err = c.close(o, SessionClosedByNew, true)
		case o != nil:
			err = c.close(o, SessionClosedUnconfirmed, false)
		}
		if err != nil {
			return "", err
		}
		_, err = c.openNew(h, lifetime)
		return SessionApplied, err
	})
}

// ContinueOperatingSessionWithContext continues the named session (ADR 0100
// §3, §8).
//
//   - It is the open session, held by another lifetime: the recovery answer
//     Continue. Its identity and members are kept, the interrupted interval
//     ends unconfirmed, and a new interval is held by this lifetime.
//   - It is closed, in this file, and bound to the default logbook: the
//     current session is closed with continued_other, and the named one is
//     open again with a new interval and the file's current epoch.
//   - Anything else is SessionIneligible; a closed session named while another
//     lifetime's session awaits its answer is SessionRecoveryPending.
//
// A lifetime whose exit is recorded is SessionRetired, before anything else.
// As for New, whether the lifetime is the current one is the caller's to
// establish.
func (s *Service) ContinueOperatingSessionWithContext(ctx context.Context, lifetime string, generation int64, sessionID string, now time.Time) (OperatingSessionState, SessionOutcome, error) {
	const op errors.Op = "sqlite.Service.ContinueOperatingSessionWithContext"
	if lifetime == "" || sessionID == "" {
		return OperatingSessionState{}, "", errors.New(op).WithMsg("the app lifetime and the session are required")
	}
	return s.sessionControl(ctx, op, "continue an operating session", lifetime, now, func(c *sessionTx) (SessionOutcome, error) {
		if retired, err := c.retired(lifetime); err != nil || retired {
			return SessionRetired, err
		}
		h, o, err := c.start()
		switch {
		case err != nil:
			return "", err
		case c.stale(h, generation):
			return SessionStale, nil
		case o != nil && o.ID == sessionID && o.Holder == lifetime:
			return SessionIneligible, nil
		case o != nil && o.ID == sessionID:
			if err := c.endUnconfirmed(o.ID); err != nil {
				return "", err
			}
			return SessionApplied, c.addInterval(o.ID, lifetime)
		case o != nil && o.Holder != lifetime:
			return SessionRecoveryPending, nil
		}
		return c.continueClosed(h, o, sessionID, lifetime)
	})
}

// continueClosed reopens a closed session of the default logbook. Eligibility
// is checked here, inside the transaction, before the current session closes.
func (c *sessionTx) continueClosed(h sessionHead, current *OperatingSession, sessionID, lifetime string) (SessionOutcome, error) {
	var logbook string
	err := c.tx.QueryRowContext(c.ctx, `SELECT logbook_uuid FROM operating_session WHERE id = ? AND closed_at IS NOT NULL`, sessionID).Scan(&logbook)
	if stderr.Is(err, sql.ErrNoRows) || (err == nil && (h.defaultUUID == "" || logbook != h.defaultUUID)) {
		return SessionIneligible, nil
	}
	if err != nil {
		return "", err
	}
	if current != nil {
		if err := c.close(current, SessionClosedByContinue, true); err != nil {
			return "", err
		}
	}
	// Rebound to the file's current epoch, or the default-epoch test would
	// close it at the next contact after an X to Y to X change (ADR 0100 §8).
	if err := c.one(`UPDATE operating_session SET closed_at = NULL, close_reason = NULL, default_epoch = ? WHERE id = ?`, h.epoch, sessionID); err != nil {
		return "", err
	}
	return SessionApplied, c.addInterval(sessionID, lifetime)
}

// CloseOperatingSessionForSwitchWithContext closes the open session, whoever
// holds it, with archive_switched (ADR 0100 §4). At the switch request its
// interval ends confirmed; at an arrival, where an open session is what a
// crash left, unconfirmed (operator ruling 2026-11-20). With none open it is
// SessionUnchanged.
func (s *Service) CloseOperatingSessionForSwitchWithContext(ctx context.Context, arrival bool, now time.Time) (OperatingSessionState, SessionOutcome, error) {
	const op errors.Op = "sqlite.Service.CloseOperatingSessionForSwitchWithContext"
	return s.sessionControl(ctx, op, "close the operating session for an archive switch", "", now, func(c *sessionTx) (SessionOutcome, error) {
		o, err := c.open()
		if err != nil || o == nil {
			return SessionUnchanged, err
		}
		return SessionApplied, c.close(o, SessionClosedBySwitch, !arrival)
	})
}

// RecordOperatingAppExitWithContext records the confirmed exit of an app
// lifetime (W-0029, proposals C and D). The caller says whether the lifetime
// is the current one; this decides the rest from the file:
//
//   - its exit is already recorded: SessionExitAlreadyRecorded, nothing
//     changes, whatever generation is named;
//   - it is not current and holds the open session: SessionNotCurrent, nothing
//     changes;
//   - it is not current and holds none: the exit is recorded,
//     SessionExitRecorded, and no session is touched;
//   - it is current: the default-epoch test, then the stale check; then the
//     exit is recorded and, if it holds the open session, that is closed with
//     app_closed in the same commit (SessionApplied).
func (s *Service) RecordOperatingAppExitWithContext(ctx context.Context, lifetime string, generation int64, current bool, now time.Time) (OperatingSessionState, SessionOutcome, error) {
	const op errors.Op = "sqlite.Service.RecordOperatingAppExitWithContext"
	if lifetime == "" {
		return OperatingSessionState{}, "", errors.New(op).WithMsg("the app lifetime is required")
	}
	return s.sessionControl(ctx, op, "record the app's exit", lifetime, now, func(c *sessionTx) (SessionOutcome, error) {
		if recorded, err := c.exitRecorded(lifetime); err != nil || recorded {
			return SessionExitAlreadyRecorded, err
		}
		if !current {
			return c.exitNotCurrent(lifetime)
		}
		h, o, err := c.start()
		switch {
		case err != nil:
			return "", err
		case c.stale(h, generation):
			return SessionStale, nil
		}
		if err := c.recordExit(lifetime); err != nil {
			return "", err
		}
		if o == nil || o.Holder != lifetime {
			return SessionExitRecorded, nil
		}
		return SessionApplied, c.close(o, SessionClosedByExit, true)
	})
}

// exitNotCurrent is the exit of a lifetime this process has not admitted. It
// changes no session, and runs no default-epoch test: another lifetime's
// session is not this caller's to close.
func (c *sessionTx) exitNotCurrent(lifetime string) (SessionOutcome, error) {
	o, err := c.open()
	switch {
	case err != nil:
		return "", err
	case o != nil && o.Holder == lifetime:
		return SessionNotCurrent, nil
	}
	return SessionExitRecorded, c.recordExit(lifetime)
}

// JoinOperatingSessionTx is the logging step (ADR 0100 §2): it puts a QSO just
// written in tx into the open session, in the QSO's own transaction, so the
// two commit or roll back together. tx already holds the writer's position,
// because the QSO row was written first.
//
// In order: the QSO's logbook must be the file's default, or nothing is done
// and no session is touched; the default-epoch test; if no session is open, a
// new one, held by holder ("" for none, and none if the holder's exit is
// recorded); the member row. It returns the
// session joined, "" when the QSO joins none. No session policy refuses it. An
// error is a database failure, and the caller rolls the QSO back with it.
func (s *Service) JoinOperatingSessionTx(ctx context.Context, tx *sql.Tx, qsoID int64, holder string, now time.Time) (string, error) {
	const op errors.Op = "sqlite.Service.JoinOperatingSessionTx"
	if err := checkService(op, s); err != nil {
		return "", err
	}
	if tx == nil {
		return "", errors.New(op).WithMsg("the QSO's transaction is required")
	}
	c := &sessionTx{ctx: ctx, tx: tx, now: now.UTC()}
	id, err := c.join(qsoID, holder)
	if err != nil {
		return "", errors.New(op).WithErr(err).WithMsg("put the QSO into the operating session")
	}
	return id, nil
}

// openForContact is the logging step's safety net: the session a contact opens
// when none is open. A holder whose exit is recorded holds nothing, so the
// session is then opened with no holder: no exit report could ever close an
// interval given to a lifetime that has gone. The contact is stored either
// way; this refuses nothing.
func (c *sessionTx) openForContact(h sessionHead, holder string) (string, error) {
	if holder != "" {
		gone, err := c.retired(holder)
		if err != nil {
			return "", err
		}
		if gone {
			holder = ""
		}
	}
	return c.openNew(h, holder)
}

func (c *sessionTx) join(qsoID int64, holder string) (string, error) {
	var isDefault bool
	err := c.tx.QueryRowContext(c.ctx, `SELECT q.logbook_id IS (SELECT default_logbook_id FROM archive_metadata WHERE singleton = 1)
		FROM qso q WHERE q.id = ?`, qsoID).Scan(&isDefault)
	if err != nil {
		return "", err // no such QSO included: the step is for a QSO written in this transaction
	}
	if !isDefault {
		return "", nil
	}
	h, o, err := c.start()
	if err != nil || h.defaultUUID == "" {
		return "", err
	}
	id := ""
	if o != nil {
		id = o.ID // an open session keeps the holder it has
	} else if id, err = c.openForContact(h, holder); err != nil {
		return "", err
	}
	if err := c.one(`INSERT INTO operating_session_member (qso_id, session_id) VALUES (?, ?)`, qsoID, id); err != nil {
		return "", err
	}
	return id, c.finish()
}
