---
number: 0081
title: Preserve upstream ID success order
status: Accepted
date: 2026-09-20
---

# 0081 — Preserve upstream ID success order

## Context

W-0010 outcome 9 makes a QRZ delete with no prior upstream ID a successful no-op. A post-commit
review found that queue re-arm preserves `upstream_id` but changes the row's status and
`modified_at`. The delete lookup could therefore either miss a real retained ID or choose an older
ID over the most recently accepted insert/update. Queue state is not upstream-success chronology.

## Decision

Store a nullable, monotonically increasing `qso_upload.upstream_id_generation` whenever an
upstream-creating action accepts a non-empty ID. Preserve it with `upstream_id` across re-arms and
order delete lookup by generation; migration-era retained IDs with unknowable order remain NULL
fallbacks.

## Alternatives considered

### Require `status = 'uploaded'`

Rejected because a successful row can be re-armed and later fail locally while its accepted remote
record and retained ID still exist. Ignoring the ID makes a delete falsely settle as a no-op.

### Rank by queue status or `modified_at`

Rejected because re-arm, claim, retry, and failure all change those values. Either ordering fails
one of the two symmetric cases: re-arming the older successful row or re-arming the newest one.

### Copy the newest ID onto every insert/update row

Rejected because it rewrites history on rows that did not produce that ID and still cannot
unambiguously repair already-divergent legacy rows.

### Add a success timestamp

Rejected in favour of a generation: wall-clock rollback and finite timestamp precision can invert
or tie two accepted writes, while the per-destination worker is single-flight and can assign a
strict sequence cheaply.

## Consequences

Log migration 0010 adds the nullable generation and backfills only recoverable order from currently
uploaded rows. A pre-0010 retained ID on a non-uploaded row remains usable as a fallback but is not
given invented chronology. The planned `failure_class` column moves to migration 0011. Success
persistence and generation assignment remain one database statement, so a local write failure
cannot expose half an outcome.

## Triggers to revisit

- If remote identity becomes a dedicated one-row-per-QSO/destination table, move the ID and its
  generation there and remove cross-action lookup.
- If a forwarder legitimately maintains multiple simultaneous remote records for one QSO, replace
  the scalar ID contract with an explicit collection before enabling deletes for it.

## References

- [`W-0010`](../work/W-0010-forwarding-data-and-sync-reliability.md)
- [`forwarding design`](../v2-design/forwarding.md)
