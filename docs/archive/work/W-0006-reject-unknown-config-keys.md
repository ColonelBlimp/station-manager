# W-0006 — Reject unknown config keys before any write

**Status:** Completed 2026-08-22 — [ADR 0074](../../decisions/0074-reject-unknown-config-keys-before-any-write.md) and [ADR 0075](../../decisions/0075-migrate-retired-keys-before-unknown-key-rejection.md) implemented and verified
**Selected:** 2026-08-20
**Outcome:** A supported-version `config.json` containing an unrecognised schema key is
refused at startup — naming every offending path and no value — before the daemon writes,
migrates-in-place, or tightens permissions, so an operator's typo can never be silently
dropped while startup reports success.

`W-0006` is an immutable identity. Its status may change; priority and ranked position live
only in [`docs/backlog.md`](../../backlog.md).

## Verified pre-implementation state (code-authoritative, 2026-08-20)

- `config.Load` (`internal/config/config.go:566`) migrates the raw document
  (`migrateDocument`, `config.go:582`) then decodes **leniently** — unknown members are
  ignored (`json.Unmarshal`, `config.go:592`).
- The advisory `UnknownKeys` walker exists (`config.go:1425`) and already reports top-level
  and nested-struct paths, running `migrateDocument` first so a migration-renamed key is not
  falsely flagged. It treats scalars, **slices, and maps** as leaves (`config.go:1454`).
- **CC-1 defect:** startup rewrites `config.json` through a typed round-trip
  (`persistResolvedConfig` → `Service.Update`, `config.go:1897`) that drops unknown members,
  and only *then* re-reads the scrubbed file and calls `UnknownKeys`
  (`cmd/smd/lifecycle_adapters.go`), so the warning reports nothing and the field is gone.
- **CC-2 defect:** because the walker stops at slices, typos inside `rigs[]`, `forwarders[]`,
  `lookup.chain[]`, `operators[]`, and `evidence.antennas[]` are never detected.
- **Opaque containers already preserved:** forwarder `Credentials` (`json.RawMessage`),
  forwarder `Endpoints` (map), and other map/frequency/mapping tables are leaves in the
  walker; their internal keys are operator data and must stay untouched.

## Preserved prerequisites — NOT open scope

- **EH-3 is fixed** (`internal-error-handling-audit.md`): the migration rejects malformed
  present fields/versions and preserves the original bytes on failure. The reject gate
  builds on those guarantees; do not re-open or re-implement them.
- The **newer-than-supported version** downgrade guard (`migrations.go:213`) and the
  **malformed-JSON** diagnosis (`describeJSONError`) already work for correctly typed input;
  this item must keep both diagnostics distinct from unknown-key rejection.
- `WriteJSON`'s `0600` write, legacy-wide-mode tightening, and atomic replacement are
  correct and out of scope except as noted in the permission-hardening criterion.

## Scope

Owns, as one raw-document safety change:

- the unknown-key **reject gate** in `config.Load`, after migrations and before any write
  (CC-1 core);
- extending the walker to **struct-slice** elements (CC-2);
- CC-1's **no-op-write** and **explicit persistence-reason** handling: startup must not call
  `Service.Update` for User-Agent/key resolution when the typed delta is empty, and
  migration/default materialisation must persist under its own named reason rather than as a
  side effect of an unrelated update.

Does **not** own: EH-3 (done), CC-3/CC-4 (validator coverage / normalize-validate
enforcement), CC-5 (`WriteJSON` hardening), or any change to the opaque-container data
model. Permission hardening of a legacy wide-mode file stays an explicit independent action.

## Operator-observable acceptance criteria

1. A supported-version `config.json` with an **unknown top-level key** refuses startup; the
   daemon does not start, does not rewrite the file, and does not change its mtime or mode.
   The nearest confusable outcome — starting while silently dropping the key — must fail the
   test.
2. An unknown **nested** key and an unknown key inside a **struct slice**
   (`rigs[]`/`forwarders[]`/`lookup.chain[]`/`operators[]`/`evidence.antennas[]`) are each
   detected and each refuse startup. Indexed paths (`rigs[0].typo`) are required; identity
   paths (`forwarders[qrz].typo`) are optional polish.
3. The refusal reports **every** offending path and **no value** — not the member's value,
   not the surrounding bytes, not a credential.
4. Recognised **raw migrations run first**, so a key a migration renames or removes is not
   reported as unknown.
5. Arbitrary keys inside **maps and `json.RawMessage`** (forwarder credentials/endpoints,
   frequency/mapping tables) are accepted as data and never reported.
6. **Malformed JSON**, a **newer-than-supported version**, and **unknown keys** produce three
   distinct diagnostics; none is reported as another.
7. A **valid semantic no-op** startup leaves `config.json` content and mtime unchanged; a
   User-Agent/ClubLog-key scrub or a migration writes exactly once and records only a safe,
   named delta (never a value). A legacy wide-mode file still tightens to `0600` even when no
   content changes, as an explicit permission action.
8. **Deployment acceptance:** a read-only preflight can evaluate the live `config.json`
   against the current schema and report the unknown key paths (values omitted) without
   starting the daemon, so a would-be startup refusal is diagnosable ahead of deploy.

## Verification standard

TDD, RED first. Write failing end-to-end startup assertions for criteria 1–7 before the
gate exists; prove the current behaviour (silent drop, useless post-scrub warning) fails the
reject assertion via a reversion proof, and confirm the reversion reached its intended
assertion. Fixtures must make the confusable states differ: a clean config that starts, and
an unknown-key config that refuses, in the same test. Assert on observable startup outcome
and file state (content + mtime + mode), not on walker internals. Focused loop:
`go test ./internal/config ./cmd/smd`. No RF, audio, CAT, live-credential, or destructive-DB
action is involved.

## Completion evidence (2026-08-22)

- `config.Load` now rejects every unknown top-level, nested, and struct-slice path after
  migration and before typed decode or any write. The refusal and `smd config-check`
  preflight list paths only; file content, mtime, and mode remain unchanged.
- The version-3 migration consumes all four retired version-2 paths, preserves canonical
  split-audio and antenna values, rejects malformed retired values without rewriting the
  source, and persists a migrated document once under a path-only `schema_version` reason.
- Semantic no-op startup writes nothing; the independent permission action still narrows a
  legacy wide mode to `0600`. Focused `internal/config` and `cmd/smd` suites are green, with
  reversion proofs reaching the intended reject, migration, precedence, persistence, and
  no-value assertions.
- The final repository-wide `task ci:local` gate passed completely, including the short
  race suite and `internal/evidence`. An earlier reported
  `TestReceipt_DialContextRecordedAndSeparated` `SQLITE_BUSY` race-load flake passed three
  isolated retries and did not recur in two final full-gate runs. No RF, CAT, audio,
  hardware, live-credential, or destructive-database action was used.

## References

- [ADR 0074](../../decisions/0074-reject-unknown-config-keys-before-any-write.md) — the ruling.
- [ADR 0075](../../decisions/0075-migrate-retired-keys-before-unknown-key-rejection.md) — retired-key migration and persistence reconciliation.
- [`internal-configuration-contract-audit.md`](../../reviews/internal-configuration-contract-audit.md)
  — CC-1 (P1) and CC-2 (P2).
- [`internal-error-handling-audit.md`](../../reviews/internal-error-handling-audit.md) — EH-3
  (fixed prerequisite).
- [`docs/v2-design/config.md`](../../v2-design/config.md) — canonical config reference; §5/§13
  update lands with the implementation, not this dossier.
- [`docs/backlog.md`](../../backlog.md) — authoritative ranking.
