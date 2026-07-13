-- No-op: 0002 deleted cache rows that cannot (and need not) be restored — the
-- country table is a hamnut cache, so any missing row is re-fetched on the
-- next cold miss. SELECT keeps the file a valid statement for the runner.
SELECT 1;
