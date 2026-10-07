-- W-0021 5F.2, ruling S4 (ADR 0089): the logbook's callsign, part of the
-- logbook identity the cloud receives (ADR 0082 part 8). Display data set by
-- adoption and by identity pushes; never an identity key. Legacy logbooks start
-- with none.
ALTER TABLE logbooks ADD COLUMN callsign TEXT NOT NULL DEFAULT '' CHECK (length(callsign) <= 32);
