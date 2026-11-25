package sqlite

import (
	"context"
	"database/sql"
	stderr "errors"
	"time"

	"github.com/aarondl/sqlboiler/v4/queries/qm"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite/adapters"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite/models"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// What the Session panel reads (ADR 0100 §7, §8; W-0029 part 6): the open
// session's contacts, one contact of it, and the closed sessions Continue
// offers. Each answer is ONE snapshot of the file, and carries the session
// generation it was read at: a window draws an answer only for the session and
// generation it is showing, so the three must never be of different moments.
//
// The generation orders changes of the SESSION. It does not order changes to a
// session's contacts: a contact joining, edited or deleted moves no
// generation. What orders two answers about one contact is that contact's own
// edit counter (types.Qso.Revision, ADR 0050).
//
// These read and change nothing. They do not take the writer's position, and
// they know nothing of which app is running: what a session's state is CALLED
// for a caller is the API's to say (GET /v1/operating-session).

// SessionMembers is the open session's contacts.
type SessionMembers struct {
	Generation int64
	// SessionID is the open session, "" when none is open.
	SessionID string
	// Members are its contacts that are not deleted, the newest stored first.
	Members []types.Qso
}

// SessionMember is one contact of the open session.
type SessionMember struct {
	Generation int64
	// SessionID is the open session, "" when none is open.
	SessionID string
	// Member is the contact, nil when it is not a member of the open session
	// that is shown: it is deleted, it is in another session or in none, or the
	// file does not hold it.
	Member *types.Qso
}

// ClosedSession is one line of Continue's list.
type ClosedSession struct {
	ID          string
	LogbookUUID string
	// OpenedAt is the session's first start.
	OpenedAt time.Time
	// LatestSittingStartedAt is the start of its last sitting: what the list is
	// ordered by.
	LatestSittingStartedAt time.Time
	// LatestSittingEndedAt is that sitting's end, nil when the end was not
	// confirmed: no end time is made up for it (ADR 0100 §8).
	LatestSittingEndedAt *time.Time
	// AnEndUnconfirmed is whether the end of ANY of its sittings was not
	// confirmed, the last or an earlier one.
	AnEndUnconfirmed bool
	// ContactCount is how many of its members are contacts not deleted. It can
	// be 0: a session whose only contact was deleted is still a session.
	ContactCount int
}

// ClosedSessions is Continue's list.
type ClosedSessions struct {
	Generation int64
	// DefaultLogbookUUID is the logbook the list is of, "" when the file has no
	// default logbook; then the list is empty.
	DefaultLogbookUUID string
	Sessions           []ClosedSession
	// More is whether closed sessions beyond the limit exist.
	More bool
}

// sessionReadGap is a test-only seam, nil in production: it runs inside each of
// these reads after its first read of the file and before its next, which is
// where a read made of two snapshots would see what was written in between.
var sessionReadGap func()

func betweenSessionReads() {
	if sessionReadGap != nil {
		sessionReadGap()
	}
}

// readSnapshot runs fn in one read transaction: one snapshot of the file.
func (s *Service) readSnapshot(ctx context.Context, op errors.Op, what string, fn func(ctx context.Context, tx *sql.Tx) error) error {
	if err := checkService(op, s); err != nil {
		return err
	}
	h, err := s.getOpenHandle(op)
	if err != nil {
		return err
	}
	ctx, cancel := s.ensureCtxTimeout(ctx)
	defer cancel()
	tx, err := h.BeginTx(ctx, nil)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("begin")
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(ctx, tx); err != nil {
		return errors.New(op).WithErr(err).WithMsg(what)
	}
	return nil
}

// openSessionOf reads the generation and the open session's id ("" for none).
func openSessionOf(ctx context.Context, tx *sql.Tx) (generation int64, sessionID string, err error) {
	var id sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT m.session_generation, (SELECT s.id FROM operating_session s WHERE s.closed_at IS NULL)
		FROM archive_metadata m WHERE m.singleton = 1`).Scan(&generation, &id)
	if stderr.Is(err, sql.ErrNoRows) {
		return 0, "", errors.ErrNotFound
	}
	return generation, id.String, err
}

// memberOf limits a contact query to the members of a session. The models add
// "not deleted" themselves.
func memberOf(sessionID string) qm.QueryMod {
	return qm.Where(`qso.id IN (SELECT qso_id FROM operating_session_member WHERE session_id = ?)`, sessionID)
}

// OperatingSessionMembersWithContext reads the open session's contacts that
// are not deleted, the newest stored first, with the session's id and the
// generation, in one snapshot. With no session open it answers none. A file
// with no archive identity is errors.ErrNotFound.
func (s *Service) OperatingSessionMembersWithContext(ctx context.Context) (SessionMembers, error) {
	const op errors.Op = "sqlite.Service.OperatingSessionMembersWithContext"
	var out SessionMembers
	err := s.readSnapshot(ctx, op, "read the operating session's contacts", func(ctx context.Context, tx *sql.Tx) error {
		var err error
		if out.Generation, out.SessionID, err = openSessionOf(ctx, tx); err != nil || out.SessionID == "" {
			return err
		}
		betweenSessionReads()
		rows, err := models.Qsos(memberOf(out.SessionID), qm.OrderBy("qso.id DESC")).All(ctx, tx)
		if err != nil {
			return err
		}
		out.Members = make([]types.Qso, 0, len(rows))
		for _, row := range rows {
			// A contact that cannot be read fails the answer: a list with a row
			// left out would be drawn as the whole session.
			q, err := adapters.QsoModelToType(row)
			if err != nil {
				return err
			}
			out.Members = append(out.Members, q)
		}
		return nil
	})
	if err != nil {
		return SessionMembers{}, err
	}
	return out, nil
}

// OperatingSessionMemberWithContext reads one contact of the open session by
// its uuid, with the session's id and the generation, in one snapshot. Member
// is nil for a contact that is not a member that is shown.
func (s *Service) OperatingSessionMemberWithContext(ctx context.Context, uuid string) (SessionMember, error) {
	const op errors.Op = "sqlite.Service.OperatingSessionMemberWithContext"
	if uuid == "" {
		return SessionMember{}, errors.New(op).WithMsg("uuid is required")
	}
	var out SessionMember
	err := s.readSnapshot(ctx, op, "read a contact of the operating session", func(ctx context.Context, tx *sql.Tx) error {
		var err error
		if out.Generation, out.SessionID, err = openSessionOf(ctx, tx); err != nil || out.SessionID == "" {
			return err
		}
		betweenSessionReads()
		row, err := models.Qsos(memberOf(out.SessionID), qm.Where(models.QsoColumns.UUID+" = ?", uuid)).One(ctx, tx)
		if stderr.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		q, err := adapters.QsoModelToType(row)
		if err != nil {
			return err
		}
		out.Member = &q
		return nil
	})
	if err != nil {
		return SessionMember{}, err
	}
	return out, nil
}

// closedSessionsSQL is Continue's list (ADR 0100 §8): the closed sessions
// bound to a logbook, by the start of each one's latest sitting, the most
// recent first, the later id first among equal starts. A session always has a
// sitting, so the inner join drops none.
const closedSessionsSQL = `SELECT s.id, s.logbook_uuid, s.opened_at, i.started_at, i.ended_at,
		EXISTS (SELECT 1 FROM operating_session_interval u WHERE u.session_id = s.id AND u.end_state = 'unconfirmed'),
		(SELECT COUNT(*) FROM operating_session_member m JOIN qso q ON q.id = m.qso_id
			WHERE m.session_id = s.id AND q.deleted_at IS NULL)
	FROM operating_session s
	JOIN operating_session_interval i ON i.session_id = s.id
		AND i.seq = (SELECT MAX(seq) FROM operating_session_interval WHERE session_id = s.id)
	WHERE s.closed_at IS NOT NULL AND s.logbook_uuid = ?
	ORDER BY i.started_at DESC, s.id DESC
	LIMIT ?`

// ClosedOperatingSessionsWithContext reads Continue's list: at most limit
// closed sessions of the file's default logbook, with the generation, in one
// snapshot. The open session is never in it. Whether one of them can still be
// continued is decided again, inside the transaction, when it is chosen.
func (s *Service) ClosedOperatingSessionsWithContext(ctx context.Context, limit int) (ClosedSessions, error) {
	const op errors.Op = "sqlite.Service.ClosedOperatingSessionsWithContext"
	if limit < 1 {
		return ClosedSessions{}, errors.New(op).WithMsg("the limit must be at least 1")
	}
	var out ClosedSessions
	err := s.readSnapshot(ctx, op, "read the closed operating sessions", func(ctx context.Context, tx *sql.Tx) error {
		h, err := (&sessionTx{ctx: ctx, tx: tx}).head()
		if err != nil {
			return err
		}
		out.Generation, out.DefaultLogbookUUID = h.generation, h.defaultUUID
		if h.defaultUUID == "" {
			return nil
		}
		betweenSessionReads()
		// One more than asked for, to say whether there are more.
		rows, err := tx.QueryContext(ctx, closedSessionsSQL, h.defaultUUID, limit+1)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var (
				c     ClosedSession
				ended sql.NullTime
			)
			if err := rows.Scan(&c.ID, &c.LogbookUUID, &c.OpenedAt, &c.LatestSittingStartedAt, &ended, &c.AnEndUnconfirmed, &c.ContactCount); err != nil {
				return err
			}
			if ended.Valid {
				t := ended.Time
				c.LatestSittingEndedAt = &t
			}
			out.Sessions = append(out.Sessions, c)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(out.Sessions) > limit {
			out.Sessions, out.More = out.Sessions[:limit], true
		}
		return nil
	})
	if err != nil {
		return ClosedSessions{}, err
	}
	return out, nil
}
