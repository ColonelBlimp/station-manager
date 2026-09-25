-- Reverse of 0014. Queue rows keyed by a UUID-derived binding name
-- (`<destination>.<logbook uuid>`, minted for a logbook other than the file's
-- default) are first collapsed to the destination's ONE pre-binding name — the
-- default logbook's binding name when it has one, otherwise the
-- lexicographically first binding name of that destination — so an older build,
-- whose worker set comes from config.json, can drain them under the name that
-- config carries (ADR 0082 part 4). QSO, logbook and other queue rows are
-- untouched. Then the table and the seed marker are dropped.

CREATE TEMP TABLE destination_collapse AS
SELECT d.destination AS destination,
       COALESCE((SELECT d2.forwarder_name
                 FROM logbook_destination d2
                          JOIN archive_metadata am ON am.singleton = 1 AND d2.logbook_id = am.default_logbook_id
                 WHERE d2.destination = d.destination),
                MIN(d.forwarder_name)) AS target
FROM logbook_destination d
GROUP BY d.destination;

UPDATE qso_upload
SET forwarder_name = (SELECT c.target
                      FROM logbook_destination d
                               JOIN destination_collapse c ON c.destination = d.destination
                      WHERE d.forwarder_name = qso_upload.forwarder_name)
WHERE forwarder_name IN (SELECT d.forwarder_name
                         FROM logbook_destination d
                                  JOIN destination_collapse c ON c.destination = d.destination
                         WHERE d.forwarder_name <> c.target);

DROP TABLE destination_collapse;
DROP TABLE IF EXISTS logbook_destination;
ALTER TABLE archive_metadata DROP COLUMN destination_bindings_seeded_at;
