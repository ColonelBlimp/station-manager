---
number: 0084
title: Archive summaries live in a derived sidecar, not config.json
status: Accepted
date: 2026-09-27
---

# 0084 — Archive summaries live in a derived sidecar, not config.json

## Context

Settings → Archives cannot say what an archive holds: `GET /v1/qso-archives`
returns identity, ownership, state, size and modified time, and an inactive
archive's file stays closed by design (ADR 0071). The operator ruled (fresh-install
test, 2026-09-26) that the daemon keeps a per-archive logbook summary — logbook
name, callsign, QSO count — so each archive row can list its logbooks without
opening a closed file.

The first design put the summary in the archive catalogue, which is
`config.json`. Review found that this changes the config shape: the loader
refuses a supported-version file with an unknown key
(`internal/config/config.go:618`), so without a config-version bump and a down
step an alpha.3 rollback would refuse the file and not start. A summary is also
derived data, rewritten as QSOs change, which config.json — the operator's
settings, saved from Settings — is not.

## Decision

The daemon keeps archive summaries in one station-global sidecar,
`<data_dir>/db/qso-archive-summaries.json` (under `archive.GlobalDir`), treated
as discardable derived state: losing or rejecting it costs a summary, never a
start or a QSO.

## Alternatives considered

### Summaries in config.json, inside 5C's config v6

Carry the summary on each catalogue entry and ship it with W-0021 5C's config
v6, with a down migration that strips the summary fields. Rejected: it makes
derived, frequently rewritten data part of the operator's configuration, so every
count change is a durable config rewrite competing with Settings saves; it ties the
feature to 5C's schedule; and it spends a config version and a rollback step on
data that can simply be rebuilt.

### A summary file beside each archive

Write `<archive>.summary.json` next to the database file. Rejected: an external
archive's directory belongs to the operator (backups, sync folders, removable
media), and the daemon must not scatter files there; a replaced or copied archive
would also carry a stale summary with it.

### Open closed archives read-only at list time

Rejected in the 2026-09-26 ruling: it breaks the closed-file rule, must read
older-schema files, and adds failure modes to a list call.

## Consequences

- The sidecar has its own format version and is keyed by archive UUID. A missing,
  corrupt or unsupported-version file is a cache miss — the row shows no summary —
  never a startup failure. It is written atomically with owner-only permissions
  (0600).
- The active archive's summary lives in memory, rebuilt whenever the active archive
  is opened (at startup as well as on a switch). It is persisted after a clean
  checkpoint and close, and after an offline import or restore.
- Summary updates never sit on the QSO response path. QSO and logbook changes
  (submit, delete, batch import, restore, logbook create, rename and delete)
  signal a bounded, coalescing post-commit observer; a test with a blocked summary
  writer proves the QSO response is not delayed (invariants: nothing blocks
  logging).
- A persisted summary carries a change signature of the finalised file —
  device ID, inode number and ctime, with size and mtime kept for diagnostics —
  taken after `wal_checkpoint(TRUNCATE)` leaves no WAL. Size and mtime alone
  cannot serve: under WAL, QSO writes leave the main file's mtime unchanged until
  checkpoint or close (observed in the W-0021 drill F). Ordinary writes and
  metadata changes, including restoring an mtime, update ctime, and inode numbers
  are unique only within a filesystem, hence the device ID (inode(7)). A
  mismatch shows the summary as stale. This is a change signature, not an
  integrity proof: inode reuse and privileged or storage-level manipulation remain
  possible; it covers non-adversarial restore and replacement. A test pins a
  same-size replacement with a preserved mtime.
- config.json and its version are unchanged by this feature, so a rollback is
  unaffected: an older build ignores the sidecar.

## Triggers to revisit

- A second consumer needs archive contents durably (not as a cache) — then the
  data may belong in a real store rather than a discardable file.
- Archives move to a filesystem where ctime or inode numbers are not meaningful
  (a network or FUSE mount) — the change signature needs a different basis.
- Summaries grow beyond a small per-archive record (for example per-band counts),
  making a single rewritten JSON file too costly.

## References

- ADR 0071 — first-class QSO archives (the closed-file rule).
- ADR 0082 — per-logbook destination bindings (the logbook-first layout this
  feeds).
- `docs/dogfood-inbox.md` — the archive-contents item, its REVIEW and REVIEW 2
  rulings.
- `docs/work/W-0021-qso-archives.md` — drill F, the WAL mtime observation.
- inode(7): https://man7.org/linux/man-pages/man7/inode.7.html
- `internal/archive/paths.go` (`GlobalDir`), `internal/config/config.go`
  (unknown-key refusal).
