package qsoservice

import (
	"context"
	"database/sql"
	"time"
)

// SessionMembership connects live logging to the archive's operating session
// (ADR 0100 §2; W-0029 part 5): with it, a contact submitted through
// SubmitLive joins the open session in the contact's own transaction. Whoever
// composes the daemon supplies it; this package knows nothing of app
// lifetimes or of the events a session change is published as.
type SessionMembership struct {
	// Holder is the app lifetime that holds a session a contact has to open
	// because none is open: "" when no app holds one. It is called once for a
	// contact, inside the contact's transaction, after the QSO's insert has
	// taken the writer's position, and must not block on anything a logging
	// transaction could be waited for by. Nil is "".
	Holder func() string
	// Advanced is told, after the contact's commit and after the contact's own
	// notices, that the contact's transaction advanced the session generation
	// (it opened a session, or closed one of an earlier default), with the
	// generation that transaction produced. A contact that joins the session
	// already open advances nothing and is not told of. It must not block.
	// Nil tells nobody.
	Advanced func(generation int64)
	// Now is the clock a session opened or closed by a contact is timed by.
	// Nil is time.Now.
	Now func() time.Time
}

// SetSessionMembership connects the membership, or with nil disconnects it.
// Until it is connected SubmitLive stores as Submit does and no contact joins
// a session.
func (s *Service) SetSessionMembership(m *SessionMembership) { s.membership.Store(m) }

// joinSession is the logging step of a submit (ADR 0100 §2): for a contact
// made while operating, with the membership connected, it puts the QSO just
// written in tx into the open session. The step itself leaves a QSO outside
// the file's default logbook in no session. It returns what to call once the
// contact has committed and been announced: the telling of a generation the
// step advanced, and otherwise nothing.
//
// An error is a database failure. The caller rolls the contact back with it.
func (s *Service) joinSession(ctx context.Context, tx *sql.Tx, qsoID int64, mode submitMode) (committed func(), err error) {
	nothing := func() {}
	m := s.membership.Load()
	if mode != submitOperating || m == nil {
		return nothing, nil
	}
	holder, now := "", time.Now
	if m.Holder != nil {
		holder = m.Holder()
	}
	if m.Now != nil {
		now = m.Now
	}
	joined, err := s.DB.JoinOperatingSessionTx(ctx, tx, qsoID, holder, now())
	if err != nil {
		return nothing, err
	}
	if !joined.Advanced || m.Advanced == nil {
		return nothing, nil
	}
	return func() { m.Advanced(joined.Generation) }, nil
}
