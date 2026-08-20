# Internal error-handling audit

**Status:** Fixed — implementation complete; internal test suite verified
**Re-verified 2026-08-20 (code-authoritative audit reconciliation):** EH-1..EH-7 all confirmed resolved against current source; no residual finding entered the backlog. This closes the cross-dependencies the persistence/config audits cited — EH-3 (config-migration), EH-5 (`checkedRowsAffected`), EH-6 (`txutil.Rollback`) are all fixed, so those PT/CC dependencies are stale.
**Reviewed:** 2026-08-14  
**Scope:** production Go under `internal/`; generated SQLBoiler models and tests excluded  
**Code changes:** all seven action themes implemented; this document remains the
review and resolution record

## Executive summary

A strict ignored-error pass produced **120 `errcheck` diagnostics**. That was a
backlog count, not a defect count: most are deliberate read-side cleanup,
best-effort diagnostic reads, HTTP response writes, or assertions made safe by a
preceding validator. The diagnostics reduced to **seven action themes**, all now
addressed.

The highest-risk work was concentrated in `internal/evidence`. Retention health's
fail-closed Stage 2 wiring was already present at implementation start; the
remaining work makes iterator errors abort mutations, status read failures explicit,
and transaction cleanup observable.

Two independent P1 findings were new:

1. the v1→v2 config migration can delete a malformed legacy field, stamp the
   document current, and let `Load` succeed; and
2. evidence profile activation can commit a decision made from an incomplete SQL
   iterator because `Rows.Err` is never checked.

Production source and regression tests now cover every listed action.

## Findings at a glance

| ID | Priority | Area | Disposition |
|---|---:|---|---|
| EH-1 | P0 | Evidence retention/capacity failures become measurements or silence | Fixed by existing retention-health Stage 2 |
| EH-2 | P1 | Evidence iterator errors can still lead to profile, purge, or sync work | Fixed |
| EH-3 | P1 | Config migration accepts and removes malformed versioned data | Fixed |
| EH-4 | P1 | Evidence status can return valid-looking zero or partial counts | Fixed |
| EH-5 | P2 | Six SQLite methods ignore `RowsAffected` errors | Fixed |
| EH-6 | P2 | Fourteen transaction owners discard rollback failures | Fixed |
| EH-7 | P3 | A small set of stateful close/release outcomes remain unobservable | Fixed |

Priority meanings follow the existing internal logging review: P0 is release-gate
work, P1 should be closed before a serious release, P2 is important correctness or
operability work, and P3 is useful hardening.

## EH-1 — evidence retention and capacity errors are still swallowed (P0)

This is the existing
[`internal-codebase-logging-gaps.md` L2](internal-codebase-logging-gaps.md#l2-evidence-retention-and-capacity-decisions-silently-swallow-sqlfilesystem-failures),
not a duplicate finding. The ignored-error audit independently confirmed every
listed path and found that the planned wiring is not present:

- `freelistBytes` converts either PRAGMA failure to zero at
  [`internal/evidence/retention.go:124`](../../internal/evidence/retention.go);
- `metadataBytes` discards both aggregate-query errors at
  [`internal/evidence/retention.go:138`](../../internal/evidence/retention.go);
- `physicalUsage` ignores every non-`nil` `Stat` error and undercounts at
  [`internal/evidence/service.go:867`](../../internal/evidence/service.go);
- `compactKind` silently returns on query, scan, marshal, begin, insert and delete
  failures and discards its commit error at
  [`internal/evidence/retention.go:459`](../../internal/evidence/retention.go);
- `maybeCompact` treats a count-query error as “no compaction required” at
  [`internal/evidence/retention.go:424`](../../internal/evidence/retention.go); and
- the drop-path and post-purge WAL checkpoints suppress scan errors at
  [`internal/evidence/service.go:706`](../../internal/evidence/service.go) and
  [`internal/evidence/retention.go:184`](../../internal/evidence/retention.go).

These values authorize writes, purges and drop-new behavior. A physical-usage error
can therefore fail open, while a freelist error fails closed but is reported as
capacity exhaustion. Compaction failure is indistinguishable from “nothing to
compact.”

The operator decisions already recorded in L2 should remain authoritative:

- fail closed when a measurement needed to authorize a write is unknown;
- classify the loss as `measurement_error`, not `cap` or `writer_error`;
- keep decoding and retain the loss accumulator in memory until recovery;
- represent unknown usage/metadata as `null`, never zero; and
- log a degraded edge, a five-minute write-driven heartbeat, and recovery.

### Current implementation checkpoint

Stage 1 landed in `468a9ad1`, with a heartbeat-count correction in `71088525`.
Stage 2 is now wired into the production `Service`; the current logging review
records the completed fail-closed measurement policy and tests.

### Action

Complete the six Stage 2 items already specified in L2, adding both WAL checkpoint
paths and `maybeCompact` to the error-returning surface. Update the stale Stage 1
checkpoint in the logging review when this work is actioned.

**Resolution (2026-08-15):** already complete in the reconciled tree; the
logging review now records the production wiring and fault-injection coverage.

### Required tests

Fault-inject main-db `Stat`, optional WAL/SHM `Stat`, PRAGMA scan, aggregate scan,
checkpoint and compaction commit failures. Assert:

- no slot write is authorized by an unknown measurement;
- the loss reason is `measurement_error`;
- status is unknown rather than zero;
- the first transition and bounded heartbeat are distinct; and
- the first complete successful measurement emits recovery and resumes writes.

## EH-2 — SQL iteration errors can still drive evidence mutations (P1)

`database/sql.Rows.Next()` returning false means either end-of-data or an iterator
failure. Several evidence paths close the rows but never call `Rows.Err()`:

- profile activation reads the prior active UUID set at
  [`internal/evidence/profiles.go:193`](../../internal/evidence/profiles.go);
- `scanPurgeRows` returns `Rows.Close()` as if it represented iteration success at
  [`internal/evidence/retention.go:218`](../../internal/evidence/retention.go);
- unsynced purge selection has the same omission at
  [`internal/evidence/retention.go:346`](../../internal/evidence/retention.go);
- sync selection treats a partial UUID list as a successful batch at
  [`internal/evidence/sync.go:304`](../../internal/evidence/sync.go);
- compaction omits the check at
  [`internal/evidence/retention.go:460`](../../internal/evidence/retention.go); and
- the status profile grouping omits it at
  [`internal/evidence/profiles.go:310`](../../internal/evidence/profiles.go).

The last two belong operationally to EH-1 and EH-4, but all six need the same
mechanical correction.

The profile path is the most consequential. An incomplete `activeUUIDs` map makes
`!activeUUIDs[f.uuid]` true, so the transaction can mint a new profile version,
replace `profile_active`, and commit even though reading the prior mapping failed.

The purge paths receipt exactly the rows they did scan, so this is not an
unreceipted-delete finding. It is still wrong to commit a retention decision after
its selection query failed. The sync path is eventually recoverable, but currently
hides the database fault and may report a successful partial cycle.

### Action

Check `Rows.Err()` after every loop and before treating the result set as complete;
then check `Close` where its result is meaningful. Wrap the iterator error with the
operation being performed. Existing callers already provide the right behavior once
an error is returned:

- startup profile reconciliation enters `ProfilesDegraded`;
- purge logs a bounded chunk failure and performs no commit; and
- sync enters its existing transition-based backoff.

**Resolution (2026-08-15):** all six loops now check `Rows.Err()` before a
successful result is used; meaningful closes are checked. Regression tests inject
an error after the first row and verify the profile and purge transactions do not
mutate, while sync enters backoff without offering a row.

### Required tests

Use a faulting SQL driver/test seam whose first row scans successfully and whose next
step fails. Assert that profile activation mints no version and changes no active
mapping, purge deletes nothing and writes no receipt, and sync enters backoff without
marking any row offered.

## EH-3 — config migration can erase malformed versioned data (P1)

The raw v1→v2 migration uses unchecked assertions in
[`internal/config/migrations.go:41`](../../internal/config/migrations.go). The
highest-risk branch is:

```go
byDriver, _ := bridge["mode_mappings"].(map[string]any)
if len(byDriver) == 0 {
    delete(bridge, "mode_mappings")
    return nil
}
```

If `bridge.mode_mappings` is present but is not an object, the assertion produces a
nil map, the migration deletes the field, stamps version 2, and `Load` succeeds.
Malformed per-driver entries are similarly skipped before the entire legacy block is
deleted. This field has been removed from the typed `BridgeConfig`, so typed JSON
unmarshal cannot catch the bad legacy shape after migration removed it.

`documentVersion` at
[`internal/config/migrations.go:157`](../../internal/config/migrations.go) also treats
every present non-number as an absent v1 version. For example, `"version":"3"` is
silently migrated and stamped as version 2 rather than being rejected as malformed
or newer-than-supported.

`Load` does not immediately rewrite the source file, so the original bytes initially
remain recoverable. The in-memory configuration has already lost the field, however,
and the next normal config update writes the typed v2 shape without it.

### Reproduction

Overlay-only probes run during this review confirmed both current behaviors:

- a v1 document with `"bridge":{"mode_mappings":"not-an-object"}` is accepted by
  `Load` after the migration deletes the field; and
- a document with `"version":"2"` is treated as v1 and silently stamped with the
  numeric current version.

The probes passed because they asserted the current unsafe behavior; no probe file
was added to the repository.

### Action

Make migration parsing presence-aware:

- an absent version means v1; a present version must be an integer JSON number;
- a present removed field must have the exact legacy shape or migration fails;
- `rigs`, each rig, its `model`, the driver mapping, and each mapping value must be
  validated when present;
- do not delete `bridge.mode_mappings` until the complete source block has been
  validated and every applicable mapping has a valid destination; and
- preserve the source file unchanged on every migration error.

**Resolution (2026-08-15):** migration now validates presence and shape before
any deletion, rejects malformed present versions, and table-tests path-specific
errors with byte-for-byte source preservation.

### Required tests

Table-test wrong version types, wrong `mode_mappings` type, wrong per-driver value,
wrong `rigs` type, non-object rig entries and non-string models. Each should return a
path-specific migration error and leave the on-disk bytes byte-for-byte unchanged.

## EH-4 — the evidence honesty endpoint can return plausible partial data (P1)

`GET /v1/evidence/status` is explicitly documented as the local “honesty surface,”
but its database-derived fields do not have a consistent unknown state:

- observation and unprofiled counts discard scan errors at
  [`internal/evidence/service.go:431`](../../internal/evidence/service.go);
- retention totals discard their aggregate scan error at
  [`internal/evidence/retention.go:562`](../../internal/evidence/retention.go);
- profile totals and grouped unprofiled counts return silently on query/scan failure
  at [`internal/evidence/profiles.go:301`](../../internal/evidence/profiles.go); and
- sync initializes an empty map, then includes only the per-kind queries that
  succeed, while `Quarantined` sums only successful queries at
  [`internal/evidence/sync.go:657`](../../internal/evidence/sync.go).

The resulting JSON can say zero observations, omit one unsynced kind, or understate
quarantine while otherwise looking complete. `ProfilesStatus.Lineages` and
`Versions` already use pointers, demonstrating the correct unavailable-vs-zero
distinction, but the convention is not carried through the rest of the payload.

### Action

Keep the useful in-memory state available, but make every database-derived group
all-or-nothing and explicitly available/degraded. Recommended shape:

- pointer/nullable counts for unknown values, including the L2 usage and metadata
  decisions already made;
- a top-level degraded/status-error indicator safe for operator display; and
- no per-request warning flood—log only degraded and recovered transitions.

A 200 response with explicit unknowns is preferable to replacing the entire payload
with a 503, because capture state, dropped-slot count and the last sync error remain
useful when aggregate reads fail.

**Resolution (2026-08-15):** database-derived status groups are now atomic:
their counts/maps are nullable as a group, top-level `degraded`/`status_error`
identify a failed read, and a transition tracker emits one degraded and one
recovered record.

### Required tests

Force each aggregate and grouped query to fail independently. Assert no failed group
serializes as zero or a partial map, healthy groups remain available, and repeated
status polling does not produce repeated Warn records.

## EH-5 — six `RowsAffected` errors are interpreted as row count zero (P2)

Six methods in
[`internal/database/sqlite/api_context.go`](../../internal/database/sqlite/api_context.go)
use `n, _ := res.RowsAffected()`:

- `MarkUploadSuccessWithContext`;
- both updates in `MarkUploadSuccessWithAdifStampWithContext`;
- `MarkSessionEmailedWithContext`;
- `MarkUploadTransientRetryWithContext`; and
- `MarkUploadFailedWithContext`.

The service already has the correct pattern in the logbook delete path at line 1796:
wrap a `RowsAffected` error before interpreting the count.

The current SQLite driver supports `RowsAffected`, so this is a latent result-contract
failure rather than a known normal-path failure. If the result does fail, however,
the consequences are misleading:

- completion methods run the zero-row classifier and can report a concurrent
  re-arm even though the update executed;
- the stamped transaction can roll back under the wrong classification; and
- session email stamping returns `(0, nil)`, producing a count-mismatch warning
  instead of the post-send stamp-error path.

### Action

Use one package-local helper that returns a wrapped count or error and replace all
six call sites. Keep the existing zero-row semantics only after the count was
successfully obtained.

**Resolution (2026-08-15):** `checkedRowsAffected` owns all six sites; injected
result failures preserve the caller error and cannot invoke zero-row handling or
commit the stamped transaction.

### Required tests

Inject an `sql.Result` whose `RowsAffected` returns an error. Assert the original
error is preserved with the caller operation, no zero-row classifier runs, the
transaction does not commit, and session email returns a non-nil error rather than
`(0, nil)`.

## EH-6 — rollback failures are handled in one transaction owner only (P2)

There are **14** unconditional deferred rollbacks whose errors are discarded:

- ten in `internal/evidence`;
- three in `internal/cloud/store`; and
- one in `internal/database/sqlite`.

The common `defer tx.Rollback()` pattern is correct for ensuring an attempted
rollback and harmless after a successful commit, but it suppresses the only signal
that rollback itself failed on an error path. In contrast, `internal/qsoservice`
already routes failure branches through `rollbackTx`, warns on rollback failure, and
has regression tests for clean and failed rollback.

A rollback error is secondary—the primary query/mutation error must not be replaced—but
it leaves the transaction disposition uncertain and is operationally relevant for
the evidence archive and SM Cloud store.

### Action

Adopt the `qsoservice` policy consistently:

- on failure paths, attempt rollback and join the rollback error to the primary
  returned error, or log it where the API cannot return it;
- ignore only `sql.ErrTxDone` from the post-commit deferred guard; and
- keep the read-only `ExportSnapshot` cleanup lower severity, but make its policy
  explicit rather than silently sharing the mutation-path idiom.

Do not emit one false warning per successful commit: an unconditional helper must
recognize `sql.ErrTxDone`, or rollback should be called explicitly only on error
branches.

**Resolution (2026-08-15):** the shared `txutil.Rollback` guard covers all 14
owners. It joins real rollback failures to a primary error and ignores only the
expected post-commit `sql.ErrTxDone`; the read-only export path documents and uses
the same explicit policy.

## EH-7 — selective lifecycle close/release hardening (P3)

Most close errors in the 120 diagnostics are correctly subordinate to a primary
failure or occur on read-only inputs. Four stateful boundaries deserve explicit
handling:

- `audio.WriteWAV` flushes the buffer but returns success before checking the final
  output-file close at
  [`internal/audio/wav.go:257`](../../internal/audio/wav.go). A late writeback error
  can therefore make a probe report a successful WAV.
- evidence `Stop` discards `db.Close` and has no completion result or record at
  [`internal/evidence/service.go:321`](../../internal/evidence/service.go). This is
  already H2 in the logging review.
- FT8 capture startup discards `Capture.Close` after `Start` fails at
  [`internal/ft8/source_cgo.go:56`](../../internal/ft8/source_cgo.go). `Close` owns
  the initialized malgo context and can itself fail, leaving cleanup invisible.
- idle-inhibition releases cannot report either logind FD-close or ScreenSaver
  `UnInhibit` failure because the internal surface contract is `func()` at
  [`internal/inhibit/inhibit.go:48`](../../internal/inhibit/inhibit.go). Timeout is
  observable, but an immediate returned error is not.

Recommended handling is narrow: return a close error at successful output boundaries,
join it to a primary error on failed initialization, and use `func() error` plus a
bounded warning for inhibition releases. Do not turn ordinary input-file close or
HTTP response-body cleanup into warning noise.

**Resolution (2026-08-15):** WAV writes check the final close, evidence shutdown
records a failed archive close, FT8 startup joins a cleanup failure, and inhibition
release is `func() error` with a bounded warning at the FT8 boundary.

The pre-existing worktree changes in `internal/pskreporter/identity.go` also contain
best-effort temp close/remove ignores. A failed remove can leave a mode-0600
`.pskid-*` file, but it does not change the selected identifier or publication
outcome; this is optional P3 cleanup and was not edited by this review.

## Accepted ignored results

The following classes were reviewed and should not be converted mechanically into
errors or warnings:

- read-only file closes after a complete read;
- HTTP response-body closes after bounded/full consumption;
- response-writer writes after headers are committed, where the client connection is
  the only useful error consumer;
- connection/temporary-file cleanup while an already more useful primary error is
  being returned, unless the cleanup owns a stateful subsystem as in EH-7;
- validated conversions (`Atoi`, date/frequency parsing) whose lexical validator is
  the immediately preceding invariant;
- fixed-shape JSON marshal operations that cannot encounter an unsupported value;
- context type assertions with an explicit safe fallback;
- intentionally consumed `recover()` values in panic-containment code;
- buffered FT8 decode-log `WriteString` errors, because `bufio.Writer` retains the
  sticky error and the checked flush/close path reports it; and
- SM Cloud payload parse errors deliberately converted into a permanent per-row
  rejection rather than a batch-level error.

`errorlint` reported five sites. They are intentional current-frame inspection or
comparisons on errors returned directly by `database/sql`/`io`, not wrapped values.
`sqlclosecheck` reported four explicit early-close patterns; those need `Rows.Err`
corrections, not a mechanical conversion to `defer`.

## Static-analysis coverage and proposed ratchet

The repository's `.golangci.yml` intentionally enables maintainability metrics only.
The audit used a temporary production-only configuration with generated models
excluded and all issue suppression disabled.

The raw `errcheck` concentration was: `evidence` 29, `database/sqlite` 19, `ft8`
11, `cloud/store` 10, `config` 8, `forwarding/smcloud` 6, and 37 across the other
16 packages. That distribution is why evidence owns four of the seven action
themes even after deliberate ignores are removed.

| Check | Result | Triage |
|---|---:|---|
| `errcheck` with blank assignments and assertions | 120 | Seven action themes plus accepted cases above |
| `rowserrcheck` | 5 | All real; manual review found one additional helper omission |
| `bodyclose` | 0 | Ready to gate |
| `nilnesserr` | 0 | Ready to gate |
| `nilerr` | 2 | Both intentional wire-level rejection conversions |
| `errorlint` | 5 | Intentional/direct-error cases |
| `sqlclosecheck` | 4 | Style false positives around explicit closes |

`wrapcheck` was also sampled and produced 118 diagnostics, dominated by adapters,
callbacks, interface pass-throughs and sentinel-preserving returns. It should not be
enabled unconfigured: a separate operation-tag consistency review is needed to decide
which package boundaries must add context and which must preserve the exact callback
error.

Recommended rollout:

1. Enable `bodyclose` and `nilnesserr` immediately in a separate correctness-linter
   config or clearly separated section.
2. Fix all six iterator-completion omissions, then enable `rowserrcheck`.
3. Close EH-1 through EH-6 and annotate every intentional `errcheck` discard with a
   specific reason; do not globally exclude `Close`, `Rollback`, `RowsAffected` or
   type assertions, because those broad exclusions would hide the findings in this
   report.
4. Enable strict `errcheck` (`check-blank: true`) once the accepted cases are
   explicit. Decide separately whether checked type assertions belong in the same
   gate or a second ratchet.

## Action order

- [x] EH-1: complete evidence retention-health Stage 2 and update the stale L2 checkpoint.
- [x] EH-2: make every evidence iterator fail before state changes on `Rows.Err`.
- [x] EH-3: make config migration reject malformed present fields and versions.
- [x] EH-4: make evidence status groups explicitly unknown/degraded on query failure.
- [x] EH-5: centralize checked `RowsAffected` handling.
- [x] EH-6: adopt a consistent rollback-failure policy outside `qsoservice`.
- [x] EH-7: harden the four stateful close/release boundaries.
- [ ] Add the staged correctness-linter gate after its corresponding debt is closed.

## Verification performed

- strict production-only `errcheck` with blank assignments and type assertions;
- `rowserrcheck`, `bodyclose`, `nilerr`, `nilnesserr`, `errorlint` and
  `sqlclosecheck` over `./internal/...`;
- manual review of every production `Rows.Next` loop;
- source-level transaction, close, response-body and invariant triage;
- overlay-only config regression probes proving the malformed-field deletion and
  non-numeric-version upgrade behavior; and
- comparison against the existing logging and package-level review decisions.

The overlay probes passed. No full test suite was required because the repository
change is documentation only; implementation fixes should run the package tests named
under each finding and the normal full CI gate.
