-- The operating session's record (ADR 0100; W-0029 part 1): the list of
-- contacts made in one sitting, owned by the daemon and kept in the archive
-- file beside the logbooks and QSOs it describes. This migration writes no
-- row: sessions run forward only, and existing contacts are in none.

-- Two counters on the file's one identity row.
--   session_generation advances on every change of session state and is never
--   reset; each operator action names the generation it was taken against.
--   default_epoch counts the moves of default_logbook_id, in the statement
--   that moves it. A session remembers the epoch it opened at, so a change of
--   default is seen even after the pointer has come back to the same logbook.
ALTER TABLE archive_metadata ADD COLUMN session_generation INTEGER NOT NULL DEFAULT 0;
ALTER TABLE archive_metadata ADD COLUMN default_epoch      INTEGER NOT NULL DEFAULT 0;

-- One row for each session. Open while closed_at is NULL, and at most one is
-- open in a file. The close reasons are checked by the code that writes them,
-- not here: a new reason must not need this table rebuilt.
CREATE TABLE operating_session (
    id            TEXT     NOT NULL PRIMARY KEY CHECK (length(id) = 36),
    logbook_uuid  TEXT     NOT NULL,
    default_epoch INTEGER  NOT NULL,
    opened_at     DATETIME NOT NULL,
    closed_at     DATETIME,
    close_reason  TEXT,
    CHECK ((closed_at IS NULL) = (close_reason IS NULL))
);

CREATE UNIQUE INDEX idx_operating_session_one_open
    ON operating_session ((closed_at IS NULL)) WHERE closed_at IS NULL;

-- The sittings of a session: one interval when it opens, one more for each
-- Continue. An end is confirmed, with its time, or unconfirmed, with none: no
-- time is invented for a sitting that a crash ended. app_lifetime is the
-- start of the desktop app that held the interval.
CREATE TABLE operating_session_interval (
    session_id   TEXT     NOT NULL REFERENCES operating_session (id) ON DELETE CASCADE,
    seq          INTEGER  NOT NULL,
    started_at   DATETIME NOT NULL,
    ended_at     DATETIME,
    end_state    TEXT     NOT NULL CHECK (end_state IN ('open', 'confirmed', 'unconfirmed')),
    app_lifetime TEXT,
    PRIMARY KEY (session_id, seq),
    CHECK ((end_state = 'confirmed') = (ended_at IS NOT NULL))
);

CREATE UNIQUE INDEX idx_operating_session_interval_one_open
    ON operating_session_interval (session_id) WHERE end_state = 'open';

-- Membership: a QSO is in at most one session. A deleted QSO keeps its row
-- here (deletion sets qso.deleted_at), so a session with a member cannot be
-- removed.
CREATE TABLE operating_session_member (
    qso_id     INTEGER NOT NULL PRIMARY KEY REFERENCES qso (id),
    session_id TEXT    NOT NULL REFERENCES operating_session (id)
);

CREATE INDEX idx_operating_session_member_session ON operating_session_member (session_id);

-- One row for each start of the desktop app whose exit the daemon has
-- acknowledged. A lifetime found here is never admitted again.
CREATE TABLE operating_app_exit (
    app_lifetime TEXT     NOT NULL PRIMARY KEY,
    exited_at    DATETIME NOT NULL
);
