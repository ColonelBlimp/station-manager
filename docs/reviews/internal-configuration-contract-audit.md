# Internal configuration-contract audit

**Status:** review complete; actions open
**Reviewed:** 2026-08-14
**Scope:** configuration defaults, migration, normalization, validation,
persistence, warnings, secret handling, and startup/runtime consumption under
`internal/`, with `cmd/smd` as the composition boundary
**Code changes:** none; this document is the review deliverable

## Executive summary

The configuration design has strong foundations: first-run defaults distinguish
explicit false/zero values where required, configuration writes use an atomic
replacement and owner-only permissions, API updates are presence-aware and validate
their merged candidate under the config lock, and secret-bearing diagnostics fail
closed. Focused tests for config, API, SQLite, logging, and `cmd/smd` all pass.

The review found **five action themes**. The P1 is a current data-loss path: every
daemon start performs a typed rewrite before unknown keys are inspected. That rewrite
deletes every unrecognised field, and the later warning pass reads the rewritten file,
so the deletion is both irreversible and silent. An overlay-only probe reproduced this
with a no-op `Service.Update`.

Two P2 findings weaken the documented single validation pipeline. `Config.Validate`
does not cover datastore or logging rules that their consumers later enforce, and the
exported persistence primitives rely on every caller remembering to normalize and
validate. A probe confirmed that `Load` accepts consumer-invalid values and that
`Service.Update` can persist a value rejected by `Config.Validate`.

The remaining work is an unknown-key coverage gap inside arrays and persistence
hardening around the fixed temp filename. The malformed-migration data loss already
recorded as EH-3 remains a P1 dependency for this work; it is referenced here rather
than duplicated.

No production source was changed during this audit.

## Findings at a glance

| ID | Priority | Area | Disposition |
|---|---:|---|---|
| CC-1 | P1 | Startup erases unknown fields before it can warn about them | New |
| CC-2 | P2 | Unknown-key checks stop at slices of structs | New |
| CC-3 | P2 | The authoritative validator omits datastore and logging | New |
| CC-4 | P2 | Persistence primitives do not enforce normalize/validate | New preventive boundary |
| CC-5 | P3 | A stale fixed-name temp file can block later config writes | New hardening |

Priority meanings follow the other internal reviews: P0 is release-gate work, P1
should be closed before a serious release, P2 is important correctness or operability
work, and P3 is useful hardening.

## CC-1 — startup erases unknown fields before warning (P1)

`Load` intentionally performs lenient typed JSON decoding. `UnknownKeys` documents the
reason as forward compatibility and promises an advisory warning for likely typos at
[`internal/config/config.go:1298`](../../internal/config/config.go). The startup order
breaks both parts of that contract:

1. `cmd/smd` loads the file and assigns its path to the config service.
2. Before a logger exists, it calls `persistResolvedConfig` at
   [`cmd/smd/main.go:238`](../../cmd/smd/main.go).
3. `persistResolvedConfig` always calls `Service.Update`, even when `UserAgent` and the
   ClubLog credential blob do not change, at
   [`cmd/smd/main.go:1284`](../../cmd/smd/main.go).
4. `Service.Update` serializes the typed `Config` and replaces the file at
   [`internal/config/config.go:1897`](../../internal/config/config.go). Unknown JSON
   fields cannot survive that typed round trip.
5. Only after the logger starts does the daemon re-read the now-replaced file and call
   `UnknownKeys`, at [`cmd/smd/main.go:368`](../../cmd/smd/main.go).

The source comment already notes that this update moves the config mtime on every boot,
but the consequence is more serious than mtime noise. A typo such as an unknown
top-level block, an additive field written by another compatible build, or an opaque
extension is deleted before the operator can be told it existed. Because
`startupChanges` diffs the already-loaded typed value before and after only the
User-Agent/key mutation, the raw-document deletion also produces no `config saved`
record.

This is not limited to startup. Every later typed `Service.Update` also drops unknown
fields. Startup makes the loss unconditional and suppresses the intended warning.

### Reproduction

An overlay-only test wrote a normal current-version config plus
`"future_block":{"enabled":true}`. `UnknownKeys` reported `future_block` before the
update. `Load` succeeded, and a no-op `Service.Update` then removed the block; running
`UnknownKeys` on the resulting file returned no findings. The probe passed because it
asserted the current unsafe behavior; no probe file was added to the repository.

### Action

First choose and encode one coherent policy:

- **Preserve and warn** is consistent with the current documented forward-compatible
  behavior. Keep the original raw document (or an unknown-field sidecar) in the
  service and merge typed known-field updates without dropping unknown members.
- If preservation is deliberately rejected, fail the load on unknown keys before any
  write. Warning and then deleting before the warning can be emitted is not a valid
  third policy.

Independently, split startup persistence into explicit reasons. Capture unknown keys
from the original bytes before any rewrite and retain them until the logger exists.
Do not invoke `Update` for User-Agent/key resolution when the typed delta is empty.
Migration/default materialization should have its own raw-document delta and save
record rather than being an invisible side effect of a nominally unrelated update.
Keep the current legacy-mode tightening behavior through an explicit permission check;
skipping a content no-op must not leave a secret-bearing legacy `0644` file unchanged.

### Required tests

Add end-to-end startup tests for:

- an unknown top-level key surviving startup and producing exactly one warning;
- an unknown nested key surviving an unrelated API PUT;
- a semantic no-op startup leaving file content and mtime unchanged;
- an actual User-Agent or ClubLog scrub writing once and recording only a safe delta;
- an old schema rewrite being named as a migration rather than an operator save; and
- a legacy wide file mode still tightening when no content changes.

## CC-2 — unknown-key checks stop at slices of structs (P2)

`collectUnknownKeys` recurses only when the reflected field is a struct. Its explicit
leaf branch at [`internal/config/config.go:1340`](../../internal/config/config.go)
treats slices and maps alike as opaque. Maps should be opaque because their keys are
operator data, but slices whose element type is a config struct contain schema fields.

Consequently typos inside large hand-edited surfaces are never reported, including
elements of `rigs`, `forwarders`, `operators`, `lookup.chain`, and
`evidence.antennas`. The current `TestUnknownKeys` covers top-level and nested object
fields only, so it does not exercise these shapes.

An overlay-only probe passed `{"rigs":[{"typo_inside_rig":true}]}` to
`UnknownKeys`; the result was empty, confirming the gap without changing the tree.

### Action

When a field is a slice or array whose dereferenced element is a struct, decode its raw
value as a list and recurse into every object element. Continue treating map keys as
data. Report paths with an index (`rigs[0].typo`) or, where a stable identity is
available, the same identity form used by config diffs (`forwarders[qrz].typo`).

This should land with CC-1 so the newly detected warning is captured before any typed
rewrite and the offending field is not destroyed.

### Required tests

Table-test every struct-valued config slice, a slice with a non-object element, nested
struct pointers within an element, and arbitrary map keys. Assert both the exact path
and the absence of false positives on a fully populated default config.

## CC-3 — the authoritative validator omits datastore and logging (P2)

`Config.Validate` calls the config-owned validators listed at
[`internal/config/validate.go:62`](../../internal/config/validate.go), but it never
validates `Config.Datastore` or `Config.Logging`. Both canonical types carry explicit
constraints:

- datastore driver, path, pool bounds, and timeouts at
  [`internal/types/datastore.go:9`](../../internal/types/datastore.go); and
- logging level, relative log directory, rotation bounds, and shutdown timeout at
  [`internal/types/logging.go:3`](../../internal/types/logging.go).

Those constraints are enforced only after `Load` succeeds, by SQLite initialization
at [`internal/database/sqlite/validation.go:14`](../../internal/database/sqlite/validation.go)
and logging initialization at
[`internal/logging/validation.go:21`](../../internal/logging/validation.go). Logging
also adds semantic path and caller-skip checks not expressible in its struct tags.

This contradicts the design contract that `Validate` is the one source of truth and
that hand-edited malformed values fail at the config boundary. In particular, an
invalid logging block can prevent the structured logger itself from starting, leaving
the operator with a later dependency-injection error rather than a field-specific
`invalid config (...)` message.

### Reproduction

An overlay-only test serialized `DefaultConfig` with datastore driver `postgres` and
logging level `verbose`. `Load` returned success and retained both values even though
the downstream validators reject them. No probe file was added to the repository.

### Action

Add pure `validateDatastore` and `validateLogging` checks to `Config.Validate`,
including logging's relative-path and skip-frame semantics. Keep the consumer
validators as defensive boundaries, but remove rule drift by sharing a small
standard-library-only rule function or by adding explicit parity tests. Do not make
`config` import concrete SQLite or logging services.

Filesystem viability (directory existence, permissions, actual database open) remains
a consumer/startup concern; this finding is about deterministic structural and
semantic rules that can be checked without I/O.

### Required tests

For every datastore/logging rule, assert that `Validate` returns a field-specific code,
`Load` fails with that code, and the defensive consumer validator agrees. Include valid
boundary values and a default config as positive controls.

## CC-4 — persistence does not enforce the authoritative pipeline (P2)

The documented write pipeline is `normalize → validate → persist`, and the API follows
it carefully inside the update closure at
[`internal/api/handler_config.go:759`](../../internal/api/handler_config.go). The
exported service boundary does not. `Service.Update` and
`UpdateInMemoryThenPersist` deep-clone and commit whatever their callback returns; they
do not call either `Normalize` or `Validate` at
[`internal/config/config.go:1897`](../../internal/config/config.go) and
[`internal/config/config.go:1930`](../../internal/config/config.go).

Current production callers are disciplined: the API validates, and the two startup
mutations are narrowly controlled. The contract is nevertheless caller-dependent.
Any new package using the advertised update primitive can commit a shape that cannot
survive the next `Load`, leaving memory and disk internally inconsistent until restart.

An overlay-only probe set `server.max_concurrent_requests=-1` in a successful
`Service.Update`. The file and live service accepted it; immediately passing the
snapshot to `Config.Validate` produced a blocking finding.

### Action

Make the ordinary service update boundary enforce `Normalize` followed by
`Validate`, returning a typed validation error with the blocking `Finding`. Apply the
same rule before the in-memory commit in `UpdateInMemoryThenPersist`. If a raw repair
operation genuinely must bypass normal validation, keep it private and name the
exception explicitly rather than making bypass the default public primitive.

API-only checks that require external constructor probes, database state, or request
semantics remain in the API. It may retain a dry-run validation before side effects,
but the final in-lock service commit should still be authoritative.

While changing this boundary, avoid an unconditional disk write when the normalized
candidate is identical. Coordinate that no-op behavior with CC-1's raw-field
preservation and explicit file-permission hardening.

### Required tests

Assert that both update methods reject an invalid callback with disk and memory
unchanged, normalize a valid callback before commit, and preserve the existing
deep-clone rollback behavior. Add a concurrency test showing that validation applies
to the fresh under-lock candidate, not a stale pre-lock snapshot.

## CC-5 — stale fixed-name temp files block later writes (P3)

`WriteJSON` always uses `config.json.tmp` at
[`internal/config/config.go:679`](../../internal/config/config.go). It also preserves
an operator-tightened `0400` target mode. A process interruption after creating that
temp file but before rename leaves a `0400` fixed-name temp behind. The next
`os.WriteFile` tries to truncate the same non-writable file and fails with permission
denied, so every later config PUT remains blocked until the operator manually removes
the temp file.

An overlay-only probe created a valid `0400` target and a crash-like `0400`
`config.json.tmp`; `WriteJSON` failed at the temp write exactly as described.

The replacement is atomic against readers, but it is not crash-durable: neither the
temp file nor the parent directory is synced before success is reported. That is a
hardening gap rather than evidence of ordinary runtime data loss, so P3 is appropriate.

### Action

Use a unique temp file in the target directory, created writable through an open file
descriptor; write, set the final mode, sync and close it, then rename. Clean up only
the temp path owned by that invocation. Define the post-rename directory-sync outcome
carefully so `Service.Update` cannot report memory-old/disk-new if durability reporting
fails after the visible commit.

### Required tests

Simulate a stale temp belonging to an interrupted prior write and show that a new
write succeeds without touching it. Retain the current mode and atomicity tests, add
cleanup checks for failures at each pre-rename stage, and fault-inject sync/rename
errors to pin the disk-versus-memory commit contract.

## Existing P1 dependency — malformed migrations (EH-3)

The earlier error-handling audit already records that v1→v2 migration uses unchecked
raw-map assertions and can delete malformed `bridge.mode_mappings`, while a present
non-numeric version is treated as absent v1. See
[`internal-error-handling-audit.md` EH-3](internal-error-handling-audit.md#eh-3--config-migration-can-erase-malformed-versioned-data-p1).

Action EH-3 together with CC-1. Both need a presence-aware raw-document model, exact
shape validation before deletion, and tests proving the original bytes survive every
failure. Implementing them separately risks replacing one silent-loss path with
another.

## Positive controls and non-findings

The following reviewed areas do not require action from Item 4:

- Defaults that are true on first run but allow explicit false are deliberately set in
  `DefaultConfig`, while pointer fields distinguish absent from explicit zero/false.
  Lookup TTL and SMTP normalization tests cover the important cases.
- API config overlays are presence-aware, merge masked passwords/credentials without
  exposing or accidentally clearing them, validate the fresh under-lock candidate,
  and serialize the one live FT8 setting after commit.
- `WriteJSON` writes owner-only `0600`, tightens legacy wide permissions, and preserves
  a stricter target mode. Encryption at rest is an explicitly documented rejected
  design, not an omission in this implementation.
- Config-load snippets redact values with an allowlist and preserve only diagnostic
  structure. Config save diffs also fail closed: known secret leaves report presence
  only, unrecognised paths are redacted, and URL-shaped values are reduced to origins.
- Runtime consumers use snapshots read-only in the reviewed production call sites.
  The snapshot is documented as shallow; no caller was found mutating an aliased slice,
  map, or pointer.
- The numeric newer-version downgrade guard works for correctly typed versions. The
  malformed-version case remains EH-3.

## Recommended action order

1. Close **CC-1 + EH-3** as one raw-document safety change; add CC-2 while that walker
   is being changed.
2. Complete **CC-3**, then make **CC-4** enforce the now-complete authoritative
   validator at every config commit.
3. Harden `WriteJSON` under **CC-5** without changing its visible atomic replacement
   and file-mode guarantees.

## Verification performed

Focused package tests passed:

```text
go test ./internal/config ./internal/api ./internal/database/sqlite ./internal/logging ./cmd/smd
```

Three temporary overlay test sets also confirmed the current unsafe behaviors for
CC-1/CC-2, CC-3/CC-4, and CC-5. They asserted the observed defects, were run only as
review probes, and were removed afterwards. `git status` showed no production source
change from this audit; the only new deliverable is this report alongside the earlier
untracked review reports.
