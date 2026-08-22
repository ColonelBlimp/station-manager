---
number: 0075
title: Migrate retired keys before unknown-key rejection
status: Accepted (operator-ratified 2026-08-22)
date: 2026-08-22
---

# 0075 — Migrate retired keys before unknown-key rejection

## Context

Implementing [ADR 0074](0074-reject-unknown-config-keys-before-any-write.md) surfaced a
conflict with [ADR 0067](0067-ft8-one-rule-run-model.md). ADR 0074 refuses a
supported-version `config.json` that carries any key the schema does not recognise. ADR
0067 retired `ft8.tx.auto_work_callers` and pinned the opposite guarantee — *an upgrade
must not refuse to boot over it* — and that key is carried at the **current** version, not
an older one, so no migration removed it. The strict gate would therefore make an upgraded
install refuse to boot over a harmless leftover key: the exact regression ADR 0067
prohibits.

An audit of JSON tags removed between the version-2 introduction commit and HEAD found four
such retired version-2 paths still writable by older installs:

- `ft8.tx.auto_work_callers` (ADR 0067),
- `ft8.meter.alc_red` (ADR 0064),
- `rigs[].audio.device` (split into `audio.rx` / `audio.tx`; the v1→v2 step can still
  synthesize it), and
- `psk_reporter.antenna` (superseded by the canonical `logging_station.my_antenna`).

A separate retired field, per-provider `lookup.*.useragent`, retired before config
versioning (commit `8c4e9c8b`) and is not part of the version-2 set; the global
`Config.UserAgent` is authoritative. Stray `useragent` entries in fixtures were removed as
the no-op keys they always were.

## Decision

Reconcile the two ADRs by **consuming retired keys in a migration, then rejecting whatever
remains unknown** (Option B). Concretely:

- Bump the schema version to `3` and register a `2 -> 3` raw-document migration.
- The migration deletes `ft8.tx.auto_work_callers` and `ft8.meter.alc_red`; folds each
  `rigs[].audio.device` into `audio.rx` and `audio.tx` **only when absent** (an operator's
  split values are never overwritten), then deletes `device`; and moves
  `psk_reporter.antenna` into `logging_station.my_antenna` **only when the canonical field
  is absent** — otherwise the canonical value wins — then deletes the retired key.
- The migration is idempotent and consumes an `audio.device` synthesized by the `1 -> 2`
  step (the historical `1 -> 2` migration is not rewritten).
- The ADR 0074 gate runs **after** migration on the migrated document, so only keys still
  unknown are rejected. The migrated shape is persisted exactly once, under an explicit
  `schema_version` reason that names no value.

This keeps both invariants true: a version-2 document with any retired path migrates and
boots (ADR 0067), and a version-3 document carrying those same paths is rejected as unknown
(ADR 0074).

## Consequences

- **Both ADRs hold without exception.** Upgraded installs boot; genuine typos at the
  current version are still refused. No permanent tolerance list dilutes the gate.
- **Every future key removal has a defined path.** A field removed from the structs but
  still written by older installs must be consumed by the next ordered migration, not merely
  ignored — recorded in `config.md` §13.4.
- **A below-current file is rewritten once** to persist the migrated shape (retired keys
  gone from disk); the next boot reads a current file and writes nothing.
- **Diagnostics stay distinct** — malformed JSON, a newer-than-supported version, and an
  unknown key remain three separate messages, none naming a value.

## Alternatives considered

- **(A) A permanent retired-key tolerance list** the gate skips. Rejected: it weakens ADR
  0074 indefinitely, growing one entry per retirement, and leaves retired keys on disk
  forever rather than cleaning them up.
- **(C) Reject retired keys too**, superseding ADR 0067's promise and requiring operators to
  hand-remove the key before an upgraded daemon boots. Rejected: it recreates precisely the
  boot-refusal regression ADR 0067 was written to prevent.

## Relationship to other work

- **Reconciles** [ADR 0067](0067-ft8-one-rule-run-model.md) and
  [ADR 0074](0074-reject-unknown-config-keys-before-any-write.md). ADR 0067's historical
  record is left unchanged.
- **Work item:** [`W-0006`](../archive/work/W-0006-reject-unknown-config-keys.md) — the migration,
  the reject gate, the preflight, the `config.md` §5.4/§13 reflection, and the tests land
  together.
