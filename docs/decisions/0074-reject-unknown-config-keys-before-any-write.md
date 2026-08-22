---
number: 0074
title: Reject unknown config keys before any write
status: Accepted (operator-ratified 2026-08-20)
date: 2026-08-20
---

# 0074 — Reject unknown config keys before any write

## Context

`config.Load` decodes `config.json` leniently: any JSON member the `Config` schema does
not recognise is silently ignored. `UnknownKeys` (`internal/config/config.go:1412`)
documents the rationale, citing *review 2026-06-19 L1*: forward-compatibility, so *"an old
daemon must still load a config a newer one wrote,"* backed by an advisory startup warning
so a hand-editing typo is *"visible without making one typo fatal."* That was the
**warning-only ruling** of 2026-06-19.

Two things have since removed its premise:

- **The forward-compatibility justification is dead.** Schema versioning now makes a
  newer-than-supported document *fatal* at load — `migrateDocument`
  (`internal/config/migrations.go:213`) refuses it as a downgrade guard, and
  `docs/v2-design/config.md` §13 states it plainly: *"A file from a newer schema version is
  rejected."* An old daemon is not expected to load a newer config, so tolerating unknown
  members in a **supported-version** document no longer buys forward compatibility. In a
  supported version an unknown key is a typo or an unsupported setting — and starting while
  ignoring it falsely suggests the requested setting took effect.

- **The advisory is not even delivered.** The audited startup order
  (`internal-configuration-contract-audit.md` CC-1) rewrites `config.json` through a typed
  round-trip (`Service.Update`, `internal/config/config.go:1897`) that drops unknown members
  *before* `UnknownKeys` is ever consulted, so the warning fires against an
  already-scrubbed file and reports nothing. "Warn, then delete before the warning can be
  emitted" is not a coherent policy.

Legitimate open-ended configuration is already carried by explicit opaque containers whose
internal keys are operator **data**, not schema: forwarder `Credentials`
(`json.RawMessage`), forwarder `Endpoints` (a map), frequency and mapping tables. The
unknown-key walker already treats maps and `json.RawMessage` as leaves
(`internal/config/config.go:1454`), so those values are unaffected either way.

## Decision

Reject unknown supported-schema keys. Concretely:

- During `config.Load`, **after** the recognised raw migrations run and **before** any
  write or permission change, detect unknown keys in the migrated raw document. If any
  remain, refuse startup.
- Detection covers **top-level, nested, and struct-slice paths** — the last folds in
  audit finding **CC-2**, whose walker stops at slices of structs today, so typos inside
  `rigs[]`, `forwarders[]`, `lookup.chain[]`, `operators[]`, and `evidence.antennas[]` go
  unseen. Report indexed paths (`rigs[0].typo`) as the required form; stable-identity
  paths (`forwarders[qrz].typo`) are optional polish.
- Report **all** offending paths and **never their values** — the failing bytes may be
  operator data or credentials (`config.json` is `0600`, logs are `0644`).
- Continue treating arbitrary map keys and `json.RawMessage` contents as valid data.
- Preserve distinct diagnostics for **malformed JSON** and a **newer-than-supported
  schema version**; unknown-key rejection is a third, separate diagnosis.
- A valid semantic no-op must not rewrite `config.json` or move its mtime; permission
  hardening of a legacy wide-mode file remains an explicit, independent action, not a
  side effect of a content no-op.

This reverses the 2026-06-19 warning-only ruling, whose premise the versioned-config
contract removed.

## Consequences

- **Breaking for a currently-tolerated stray key.** A supported-version `config.json`
  carrying an unknown member will now refuse startup instead of silently dropping it. The
  refusal is self-diagnosing (it names the paths), and recognised migrations run first, so
  a renamed or removed field is handled before detection. A read-only preflight against the
  live `config.json` is the deployment safeguard (see W-0006).
- **Simpler than preservation.** Rejecting avoids the ambiguous merge rules preserve-and-warn
  would need for nested arrays, removed forwarders, and whole-block replacements.
- **Extensibility is unaffected** — the opaque containers above still accept arbitrary keys
  as data.

## Alternatives considered

- **Preserve-and-warn** (keep the original raw document or an unknown-field sidecar and
  merge typed known-field updates without dropping unknowns). Rejected: it re-legitimises
  forward compatibility that the downgrade guard already denies for supported versions, and
  it requires exactly the ambiguous nested-array / removed-forwarder / whole-block merge
  semantics that reject avoids — while the legitimate open-ended surfaces are already
  covered by explicit opaque containers.

## Relationship to other work

- **Implements** audit finding CC-1 and folds in CC-2
  (`docs/reviews/internal-configuration-contract-audit.md`).
- **Builds on** EH-3 (`docs/reviews/internal-error-handling-audit.md`), already fixed: the
  migration rejects malformed present fields/versions and preserves the original bytes on
  failure. Those guarantees are a **preserved prerequisite** of this decision, not open
  scope.
- **Work item:** [`W-0006`](../archive/work/W-0006-reject-unknown-config-keys.md). The
  `docs/v2-design/config.md` reflection, the code, its TDD/reversion proof, and the audit
  closure land with the implementation, not this record.
