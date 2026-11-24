-- What the country provider answered for ONE callsign (W-0031, 2026-11-24).
--
-- The country table is matched by prefix: a row there answers for every call that
-- begins with it, and the lookup used to trust the longest row it ALREADY held.
-- A held parent block therefore claimed the separate entities inside it: with
-- only `VK` (Australia) held, VK9X (Christmas Island) read as Australia and the
-- provider was never asked. Which entity a call got depended on what had been
-- looked up before it; sixteen contacts at the station were filed under a parent
-- country.
--
-- This table remembers the provider's answer against the exact callsign it was
-- given for. It is never matched as a prefix. The entity's fields are copied
-- into the row, so it stands alone: the provider's entries for a single
-- callsign, and its one-letter blocks, have no row in the country table at all.
--
-- last_refreshed_at is the freshness stamp the country TTL is read against, as
-- in the country table. callsign is stored trimmed and upper-cased; the CHECK
-- holds every writer to that.

CREATE TABLE IF NOT EXISTS callsign_country
(
    callsign          TEXT     NOT NULL PRIMARY KEY
        CHECK (length(callsign) BETWEEN 1 AND 32 AND callsign = upper(trim(callsign))),
    last_refreshed_at DATETIME NOT NULL,
    name              TEXT     NOT NULL CHECK (length(trim(name)) > 0),
    cq_zone           TEXT     NOT NULL,
    itu_zone          TEXT     NOT NULL,
    continent         TEXT     NOT NULL,
    prefix            TEXT     NOT NULL,
    ccode             TEXT     NOT NULL,
    dxcc_prefix       TEXT     NOT NULL,
    time_offset       TEXT     NOT NULL
) WITHOUT ROWID;
