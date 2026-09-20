-- Preserve the success order of upstream ids independently of mutable queue
-- state. modified_at advances on re-arm, claim, retry and failure, so it cannot
-- tell a later id-keyed delete which retained id came from the newest accepted
-- insert/update (W-0010 outcome 9; ADR 0081).

ALTER TABLE qso_upload
    ADD COLUMN upstream_id_generation INTEGER
    CHECK (upstream_id_generation IS NULL OR upstream_id_generation > 0);

-- Existing uploaded rows have not transitioned since their successful write,
-- so their modified_at/id order is the best recoverable success order. A
-- non-uploaded row may carry an id retained across a re-arm, but its prior
-- success time has already been lost; leave that generation NULL so it remains
-- a fallback rather than fabricating chronology.
WITH ranked AS (
    SELECT id,
           ROW_NUMBER() OVER (
               PARTITION BY qso_id, forwarder_name
               ORDER BY COALESCE(modified_at, created_at), id
           ) AS generation
    FROM qso_upload
    WHERE status = 'uploaded'
      AND action IN ('insert', 'update')
      AND upstream_id IS NOT NULL
      AND upstream_id <> ''
)
UPDATE qso_upload
SET upstream_id_generation = (
    SELECT generation FROM ranked WHERE ranked.id = qso_upload.id
)
WHERE id IN (SELECT id FROM ranked);
