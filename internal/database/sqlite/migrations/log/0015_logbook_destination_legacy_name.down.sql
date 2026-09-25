-- Reverse of 0015. Before the column goes, queue rows keyed by a binding name
-- are collapsed to the destination's ONE pre-binding name so an older build,
-- whose worker set comes from config.json, drains them under the name config
-- carries (ADR 0082 part 4): the legacy_name recorded on that destination's
-- rows when any row carries one; otherwise — bindings that never derived from
-- config — the default logbook's binding name when it has one, else the
-- lexicographically first binding name. This runs here, at schema 15, because
-- 0014's down (which drops the table) has no legacy_name to consult. QSO,
-- logbook and other queue rows are untouched.

CREATE TEMP TABLE destination_collapse AS
SELECT d.destination AS destination,
       COALESCE((SELECT d0.legacy_name
                 FROM logbook_destination d0
                 WHERE d0.destination = d.destination AND d0.legacy_name IS NOT NULL
                 ORDER BY d0.id
                 LIMIT 1),
                (SELECT d2.forwarder_name
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
ALTER TABLE logbook_destination DROP COLUMN legacy_name;
