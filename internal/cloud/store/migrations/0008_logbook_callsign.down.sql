-- Reverses 0008 only while no logbook has a callsign: dropping a recorded one
-- would lose it, and 0007's down guard does not cover an 8 -> 7 step. Refuses,
-- changing nothing, otherwise.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM logbooks WHERE callsign <> '') THEN
        RAISE EXCEPTION 'smcloud 0008 down refused: a logbook callsign is recorded and version 7 cannot hold it';
    END IF;
END $$;

ALTER TABLE logbooks DROP COLUMN callsign;
