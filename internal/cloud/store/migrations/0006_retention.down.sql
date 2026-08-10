DROP TABLE IF EXISTS evidence_tombstones;
-- Restore the four-kind CHECK — guarded, because the test harnesses run
-- this down on a possibly-bare database before rebuilding.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'evidence_records') THEN
        -- Rows of the kind this migration enabled cannot survive its down.
        DELETE FROM evidence_records WHERE kind = 'retention';
        ALTER TABLE evidence_records DROP CONSTRAINT IF EXISTS evidence_records_kind_check;
        ALTER TABLE evidence_records ADD CONSTRAINT evidence_records_kind_check
            CHECK (kind IN ('observation', 'coverage', 'loss_interval', 'profile'));
    END IF;
END $$;
