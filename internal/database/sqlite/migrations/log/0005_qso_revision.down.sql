-- Revert ADR 0050: restore the 0004 stamp-trigger shape and drop the column.
DROP TRIGGER IF EXISTS trg_qso_set_modified_at;
CREATE TRIGGER IF NOT EXISTS trg_qso_set_modified_at
    AFTER UPDATE
    ON qso
    FOR EACH ROW
    WHEN NEW.modified_at IS NULL OR NEW.modified_at = OLD.modified_at
BEGIN
    UPDATE qso SET modified_at = datetime('now') WHERE id = OLD.id;
END;

ALTER TABLE qso DROP COLUMN revision;
