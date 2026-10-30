-- contest_serial_allocation is the durable record of sent serials handed out
-- for contest logging (W-0023 weekend slice, 2026-10-30): one row per serial,
-- per (logbook, CONTEST_ID, occurrence). The occurrence is the event's start
-- date (YYYYMMDD): an annual event repeats its CONTEST_ID, and each edition —
-- or a dry run under another date — is its own sequence. A serial is consumed
-- when a contest QSO starts, whether or not it is ever logged, and is never
-- handed out again; gaps are expected. Each draft keeps the serial it was
-- given: a retry for that draft (a lost response) gets the same number back,
-- whatever other drafts took in between. The next serial is the highest so
-- far plus one.

CREATE TABLE contest_serial_allocation (
    logbook_id   INTEGER  NOT NULL REFERENCES logbook (id),
    contest_id   TEXT     NOT NULL,
    occurrence   TEXT     NOT NULL,
    serial       INTEGER  NOT NULL,
    draft        TEXT     NOT NULL,
    allocated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (logbook_id, contest_id, occurrence, serial),
    UNIQUE (logbook_id, contest_id, occurrence, draft)
);
