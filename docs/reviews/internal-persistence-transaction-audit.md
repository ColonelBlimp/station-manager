# Internal persistence and transaction-boundary audit

**Status:** review complete; actions open  
**Reviewed:** 2026-08-14  
**Scope:** production persistence under `internal/`: SQLite log/reference stores,
evidence SQLite, SM Cloud Postgres, configuration/filesystem state, forwarder
queueing, restore, and the session-email side-effect boundary; generated models
and test-only storage excluded  
**Code changes:** none; this document is the review deliverable

## Executive summary

The main storage architecture is substantially sound. QSO creation and editing
group the authoritative row, upload queue, and audit history in one SQLite
transaction; QSO edits use revision-based compare-and-swap; worker claims and
completions are conditional; cloud QSO batches are transactional; evidence
ingest uses `SERIALIZABLE` with bounded retries; and cloud export reads one
repeatable-read snapshot. The reference database is explicitly best-effort, so
its separation from the log transaction is not an accidental consistency gap.

The review found **six action themes**. There is no new P0 issue. The two P1s
concern records intended to be trustworthy after the fact:

1. SM Cloud accepts a different QSO state carrying the same version tuple as an
   “idempotent” replay, and the reconcile protocol cannot see the resulting
   content divergence.
2. QSO delete is not revision-guarded and appends the handler's stale snapshot,
   rather than the state actually deleted, to the append-only audit history.

The P2 work closes two further stale-read/write races, prevents import fallback
after a rollback whose outcome is explicitly unknown, and makes successful
config writes durable across host power loss rather than merely atomic to
ordinary readers.

Three overlay-only probes reproduced the stale-delete history, logbook lost
update, and session-email revision race. They asserted current unsafe behavior
and were removed after the run. No production source was changed.

## Findings at a glance

| ID | Priority | Area | Disposition |
|---|---:|---|---|
| PT-1 | P1 | Equal-version SM Cloud writes can silently change backup content | New |
| PT-2 | P1 | Stale QSO delete corrupts the pre-delete audit sequence | New |
| PT-3 | P2 | Session email can stamp content that was not sent and over-report durable stamps | New |
| PT-4 | P2 | Concurrent partial logbook updates lose unrelated changes | New |
| PT-5 | P2 | Import fallback proceeds after rollback reports unverified atomicity | New extension of EH-6 |
| PT-6 | P2 | Config replacement is atomic but not crash-durable | New; coordinate with CC-5 |

Priority meanings follow the other internal reviews: P0 is release-gate work,
P1 should be closed before a serious release, P2 is important correctness or
operability work, and P3 is useful hardening.

## PT-1 — equal-version cloud writes can silently change backup content (P1)

The SM Cloud upsert order is `(revision, modified_at)`. A strictly newer tuple
wins and an older tuple is correctly refused. On an exact tuple tie, however,
the SQL uses `modified_at >=` and unconditionally replaces `logbook_id`,
`deleted_at`, and `payload` at
[`internal/cloud/store/store.go:194`](../../internal/cloud/store/store.go).
The comment calls this an identical, idempotent replay, but equality of the
version fields does not prove equality of the state.

The existing integration tests pin the discrepancy directly:

- `TestUpsert_StaleGuard` writes `{"v":1}`, then sends `{"v":3}` at the same
  timestamp and expects it to be applied at
  [`internal/cloud/store/store_test.go:177`](../../internal/cloud/store/store_test.go);
- `TestUpsert_RevisionGuard` likewise sends a different `{"v":5}` payload at
  the same revision and timestamp and expects `applied=1` at
  [`internal/cloud/store/store_test.go:336`](../../internal/cloud/store/store_test.go).

This is not just a last-writer policy that reconcile can repair. The summary
hash covers only `(uuid, modified_at, revision)` at
[`internal/cloud/reconcile/reconcile.go:36`](../../internal/cloud/reconcile/reconcile.go),
and the full manifest adds only the deletion boolean. `runOnce` returns
`InSync=true` before fetching that manifest when the hashes match at
[`internal/forwarding/smcloud/reconcile.go:234`](../../internal/forwarding/smcloud/reconcile.go).
Two different live payloads with the same version are therefore permanently
indistinguishable to routine reconciliation. A live/tombstone or tombstone
metadata tie is arrival-ordered in the same way.

ADR 0050 introduced revisions specifically because equal-version, divergent
payloads make the backup silently untrustworthy. A correct current single
writer should never emit two different states at one revision, which makes a
tie-divergence a protocol invariant violation—not a state the store should
quietly resolve by arrival order. Legacy revision-zero data makes defensive
detection more useful, not less.

### Action

Split exact-version handling into two outcomes:

- equal version **and equal authoritative state** is an idempotent no-op;
- equal version with different payload or tombstone metadata is an explicit
  `version_conflict`, with the UUID named and no arrival-order overwrite.

Do not change `>=` to `>` without the equality check; ADR 0050 correctly notes
that this would pin a legitimate later legacy write as stale. Postgres `jsonb`
equality can compare logical payload content without depending on input key
order or whitespace. Treat cloud-logbook routing separately if moving an
otherwise identical row between configured backup logbooks is intended.

The current response has only batch counts. Prefer rolling the whole QSO batch
back on a version conflict and returning a bounded conflict response rather
than reporting `applied=0`, which the forwarder deliberately treats as backup
success for an ordinarily stale push. Add a diagnostic full-export comparison
or other explicit integrity check if already-stored equal-version divergence
must be found; the existing manifest cannot discover it.

### Required tests

Against real Postgres, cover exact replay, reordered-but-logically-equal JSON,
different payload, live/tombstone disagreement, different tombstone metadata,
and a conflict in the middle of a multi-row batch. Exact replay must preserve
the row without error; every divergent tie must preserve the prior row, report
the UUID as a conflict, and leave all batch-mates uncommitted. Prove the daemon
does not classify that conflict as an uploaded backup.

## PT-2 — stale QSO delete writes the wrong audit preimage (P1)

The delete handler fetches a live QSO and passes that snapshot into the service
at [`internal/api/handler_qso.go:38`](../../internal/api/handler_qso.go).
`qsoservice.Delete` later starts a transaction and deletes by integer ID at
[`internal/qsoservice/delete.go:38`](../../internal/qsoservice/delete.go), but
`DeleteQsoByIDTx` has neither an expected-revision predicate nor an
authoritative preimage return at
[`internal/database/sqlite/api_context.go:2804`](../../internal/database/sqlite/api_context.go).
The service then marshals the earlier handler snapshot into `qso_history`.

The deterministic race is:

1. DELETE fetches revision N.
2. PATCH fetches N and commits revision N+1.
3. DELETE starts its transaction, finds the current row, and deletes it without
   comparing revisions.
4. The append-only delete history stores the revision-N content even though the
   database state immediately before deletion was N+1.

The tombstone row itself retains the concurrent edit, and a later cloud-delete
worker fetches that current tombstone. The permanent damage is narrower but
still important: the user-visible delete silently wins over a concurrent edit,
and the audit trail's documented “pre-delete snapshot” omits the actual last
live state. Because `qso_history` is append-only, that false sequence cannot be
repaired in place.

The update path already has the correct pattern. It includes revision in the
mutation predicate, maps a stale snapshot to `edit_conflict`, and writes the
same guarded snapshot to history at
[`internal/database/sqlite/api_context.go:518`](../../internal/database/sqlite/api_context.go)
and [`internal/qsoservice/update.go:283`](../../internal/qsoservice/update.go).

### Reproduction

An overlay-only test fetched one QSO, committed a comment edit from that
snapshot, then called `Delete` with the now-stale copy. Delete returned success.
The stored tombstone carried the new comment, while the delete history's
`before_image` did not. The probe passed because it asserted the current unsafe
behavior and was removed after the run.

### Action

Make delete one revision-guarded persistence operation. Pass the expected
revision into the transaction, delete only the matching active row, and obtain
the authoritative pre-delete image under the same transaction. A still-live
row at another revision should become a caller-facing `409 delete_conflict`,
parallel to QSO edit conflict; a missing/tombstoned row remains 404.

The alternative policy—delete the latest state regardless of what the request
saw—still requires fetching and serializing that latest state inside the
transaction. It would keep history truthful, but it would be inconsistent with
the established optimistic-concurrency policy for QSO mutation. Revision CAS
is the more coherent default.

### Required tests

Fetch the same QSO twice, update through one snapshot, then attempt delete
through the other. Assert 409, the edited row remains live, and no delete queue
or history row is written. A fresh refetch followed by delete must succeed, and
its `before_image` must match the latest content. Also race PATCH and DELETE
under `-race` repeatedly to verify that every outcome has one serial ordering.

## PT-3 — session email stamps a different revision from the one sent (P2)

The session-email handler fetches QSO snapshots, composes their ADIF, and sends
that immutable body at
[`internal/api/handler_session_email.go:147`](../../internal/api/handler_session_email.go).
After SMTP succeeds, it stamps the rows in a set-based update using integer IDs
only at
[`internal/api/handler_session_email.go:245`](../../internal/api/handler_session_email.go)
and
[`internal/database/sqlite/api_context.go:2280`](../../internal/database/sqlite/api_context.go).

Two avoidable inconsistencies follow from the gap:

- a QSO edited after ADIF composition is stamped on its newer revision even
  though that content was never in the attachment; and
- a QSO deleted after composition matches no stamp row, but when the affected
  count is short the handler deliberately leaves the full attempted UUID set in
  `emailed` at
  [`internal/api/handler_session_email.go:261`](../../internal/api/handler_session_email.go).

The latter contradicts `SessionEmailResponse`'s contract that `emailed` lists
UUIDs whose durable fields were written. The former can prevent a corrected QSO
from being resent because its current row now says it was forwarded.

This is distinct from the unavoidable SMTP/database split. The existing
failure policy after an accepted email is honest: a stamp error is logged and
the response returns an empty durable set. The defect is that the successful
stamp path does not prove it stamped the version that was sent.

### Reproduction

An overlay-only probe fetched revision N (the attachment snapshot), committed a
comment edit to N+1, then called the production stamp method with the fetched
row ID. It returned one affected row and marked the N+1 content as emailed. The
probe was removed after the run.

### Action

Carry `(id, revision, uuid)` from composition to the post-send stamp. Stamp only
rows still at the expected revision and return the identifiers actually changed
using `UPDATE ... RETURNING` or an equivalent transaction. The HTTP response
must list that exact set; expose changed/missing rows as needing review or
resend rather than optimistically claiming they were stamped.

Do not describe this as exactly-once email. SMTP can accept a message before a
network failure or local stamp failure becomes visible. The useful invariant
is narrower: every durable “emailed” mark certifies that the marked QSO content
was the content composed for that send.

### Required tests

Pause a fake mailer after the handler composes the attachment. While paused,
edit one QSO and delete another, then release the send. Assert the attachment
contains the old snapshots, neither changed row is stamped or returned in
`emailed`, and an unchanged batch-mate is stamped and returned. Retain the
existing test that a post-accept stamp failure returns an empty set.

## PT-4 — partial logbook PATCHes can overwrite each other (P2)

`PATCH /v1/logbook/{id}` is presence-aware at the JSON boundary, but the handler
implements it by fetching the whole row, modifying the provided fields in that
copy, and passing the full value to persistence at
[`internal/api/handler_logbook.go:96`](../../internal/api/handler_logbook.go).
`UpdateLogbookWithContext` writes name, callsign, and description from the copy
without a revision guard at
[`internal/database/sqlite/api_context.go:1825`](../../internal/database/sqlite/api_context.go).

Two requests that both read `{name:A, description:X}` can therefore commit as:

1. request one changes only the name and writes `{B, X}`;
2. request two changes only the description but writes its stale name too,
   producing `{A, Y}`.

Both return 200, yet the first committed change is silently lost. The active-row
predicate correctly prevents resurrection after a concurrent delete; it does
not prevent lost updates between two live patches.

### Reproduction

An overlay-only probe fetched two copies, renamed through the first, then changed
the description through the second. Both updates succeeded and the final name
was restored to its original value. The probe was removed after the run.

### Action

Preserve PATCH semantics at the mutation boundary. The smallest fix is an
atomic field-level update that writes only request members that were present,
with duplicate-name and active-row classification retained. If logbook audit or
general edit conflict reporting is planned, add a revision and CAS instead.
Do not pass a stale full-row DTO to a partial-update persistence method.

### Required tests

Deterministically interleave disjoint name and description patches and assert
both survive. Cover concurrent rename collisions, same-field last-writer policy,
and PATCH versus DELETE. The response should reflect the values committed by
that request rather than the stale pre-update object.

## PT-5 — import fallback runs after an unverified rollback (P2)

Batch import intentionally falls back to per-record inserts when one batched
QSO or upload-row insert fails. Both failure branches call `rollbackTx` and then
immediately start the fallback at
[`internal/qsoservice/submit_batch.go:195`](../../internal/qsoservice/submit_batch.go).

`rollbackTx` returns no result. On rollback failure it logs that “write
atomicity is unverified” at
[`internal/qsoservice/rollback.go:9`](../../internal/qsoservice/rollback.go),
but the caller proceeds to replay the complete batch anyway. If the original
transaction's disposition is genuinely unknown, replay can duplicate or
misclassify rows, and the fallback's counts no longer have a proven basis.

This extends EH-6 rather than replacing it. EH-6 covers suppressed rollback
errors across evidence, cloud, and SQLite. `qsoservice` is better because it
records the failure, but import is the one reviewed caller that takes another
mutation path after explicitly declaring the first mutation uncertain.

### Action

Make rollback return a classified result. Treat `sql.ErrTxDone` from a known
automatic rollback as benign where that can be established; on any other
rollback error, join/preserve the original insert error, abort the import, and
do not enter per-record fallback. Fallback is safe only after rollback has been
confirmed.

Coordinate the helper with EH-6 so all transaction owners get one policy for
primary error, rollback error, post-commit `ErrTxDone`, and logging. Do not let
the cleanup error replace the mutation error that triggered it.

### Required tests

Use a controllable SQL driver or transaction seam to make an insert fail and
rollback fail independently. Assert no fallback insert is attempted after an
uncertain rollback, both errors remain diagnosable, and a clean rollback still
enters the current per-record recovery path with correct counts.

## PT-6 — config replacement is not crash-durable (P2)

`config.WriteJSON` provides reader atomicity: it writes a temp path and renames
it over `config.json` at
[`internal/config/config.go:653`](../../internal/config/config.go). It does not
`fsync` the temp file before rename or the containing directory afterwards.
`Service.Update` then treats the return as the disk commit point and publishes
the new in-memory value at
[`internal/config/config.go:1897`](../../internal/config/config.go).

This protects against process interruption during JSON generation or write, but
not host crash or power loss after a successful API response. POSIX rename makes
the namespace change atomic to readers; without syncing file data and the
directory entry, the rebooted system may see the old config, no replacement, or
an incompletely persisted new inode depending on filesystem behavior. The
affected state includes safety settings, credentials, and datastore paths.

SQLite's `synchronous=NORMAL` makes a similar last-transaction power-loss trade
explicit and documents why it is acceptable. Config persistence currently
claims an atomic on-disk rewrite without recording or testing a durability
policy. PT-6 asks for that policy to be made deliberate.

### Action

Coordinate this with CC-5's fixed-temp-name hardening:

1. create a unique temp file in the target directory;
2. write and set the final mode;
3. sync and close the temp file;
4. rename it over the target; and
5. sync the parent directory before reporting durable success.

The rename-before-directory-sync interval needs an explicit result model. If
directory sync fails, the new file is visible in this process but its survival
across reboot is uncertain. Preserve in-memory/disk coherence and report an
“applied, durability uncertain” outcome rather than returning a generic failure
while leaving callers to assume nothing changed.

If losing the most recent config update on host failure is intentionally
accepted, document that as clearly as the SQLite `synchronous=NORMAL` choice
and narrow the method's “on-disk” claim. Given that config contains operational
and safety state, durable replacement is the recommended policy.

### Required tests

Add a file-operation seam to inject write, file-sync, close, rename, and
directory-sync errors. Assert the old file remains intact before publication,
temp files are cleaned, permissions stay owner-only, and the returned commit
state tells `Service.Update` whether to publish the in-memory value. A
subprocess/crash harness on Linux should verify that the success path performs
both sync barriers in the documented order.

## Existing dependencies, not duplicate findings

Several already-open review items are persistence prerequisites and should be
scheduled with this report rather than refiled:

- **EH-1 / EH-2 / EH-4:** evidence capacity, iterator, and status faults can
  authorize mutations or publish plausible partial state.
- **EH-5:** six SQLite state transitions ignore `RowsAffected` errors,
  including the session-email stamp count used by PT-3.
- **EH-6:** rollback outcomes are discarded by evidence, cloud, and one SQLite
  transaction owner; PT-5 adds the unsafe-replay consequence in import.
- **LC-4:** evidence database work lacks cancellation, so transaction lifetime
  can exceed request and shutdown lifetime.
- **CC-1 / EH-3:** typed config rewrite and malformed migration can lose raw
  configuration data before ordinary persistence concerns apply.
- **CC-5:** the fixed `config.json.tmp` name is the concurrency/recovery half of
  PT-6's durable-write work.

## Positive controls and accepted boundaries

The following reviewed areas do not require new action from Item 5:

- QSO submit stores the QSO and every eligible upload row in one transaction;
  update adds history to that same boundary and uses a monotonic revision CAS.
- QSO/logbook foreign-key races are guarded at mutation time. Logbook delete is
  one conditional statement that refuses a live child QSO, and QSO insert
  verifies the parent from the write transaction.
- Upload claiming is an atomic `UPDATE ... RETURNING`; completion transitions
  require `status='in_progress'`, so a concurrent re-arm is not overwritten.
  Success plus an ADIF stamp is one SQLite transaction.
- Forwarding is deliberately at-least-once across the external HTTP/database
  boundary. A crash after upstream acceptance can resend; the design documents
  upstream deduplication and makes the local disposition observable. No local
  transaction can create exactly-once SMTP/HTTP delivery.
- The post-commit SM Cloud stamp-sync hook is best-effort by design, with hourly
  reconcile as the durable repair path.
- The reference database holds enrichment caches only. Cache warming is outside
  the QSO transaction deliberately; failure cannot roll back or corrupt the
  authoritative QSO.
- Evidence slot coverage and observations share one transaction; retention
  receipts share their deletion transaction; profile activation is
  transactional; sync intent commits before send and outcomes commit as a
  batch. The error/cancellation defects are already tracked above.
- SM Cloud evidence ingest validates content digests, treats a divergent
  same-identity payload as a conflict, processes one batch at `SERIALIZABLE`,
  and retries serialization/deadlock failures. It is a useful reference for
  PT-1's desired equal-identity behavior.
- SM Cloud QSO upsert batches are all-or-nothing, tenant/logbook ownership is
  schema-enforced, and export streams logbooks plus QSO rows from one
  repeatable-read snapshot.
- Restore preserves UUID, revision, timestamps, full JSON, and tombstones;
  concurrent same-UUID insertion is reclassified idempotently after a refetch.
- The one-time log/reference split is backup-first and resume-oriented. Its
  `INSERT OR IGNORE`, guarded drops, and complete split-state inspection make
  interrupted bootstrap retryable.
- SQLite `synchronous=NORMAL`, diagnostic decode-log loss, the PSK Reporter
  identifier's best-effort persistence, and the local sent-ADIF archive are
  explicit availability/durability tradeoffs. They should not be silently
  promoted into authoritative transactions.

## Recommended action order

1. Close **PT-1** before relying on SM Cloud as a trustworthy backup under
   producer bugs or legacy anomalies; add a diagnostic for existing divergence.
2. Close **PT-2** before more append-only audit history accumulates with
   unreconstructable delete preimages.
3. Fix **PT-3** and **PT-4** together as the stale-snapshot mutation pass; reuse
   the QSO revision-CAS pattern where appropriate.
4. Implement **PT-5** with the broader **EH-6** rollback policy.
5. Implement **PT-6** together with **CC-5**, explicitly modelling the
   post-rename durability-uncertain state.

## Verification performed

The audit mapped production transaction owners and persistence calls across
SQLite, Postgres, the forwarder queue, evidence, config, restore, and
filesystem writers. Focused checks performed during the review were:

```text
go test ./internal/qsoservice -run '^TestItem5Probe_' -count=1
go test ./internal/qsoservice -run '^TestItem5Probe_EmailStampIgnoresFetchedRevision$' -count=1
go test ./internal/forwarding/smcloud -run '^TestDiff_EqualIsQuiet$' -count=1
```

All three overlay defect probes and the pure reconcile test passed. The overlay
files were then removed. The existing real-Postgres equal-version tests were
invoked with `-v` but correctly skipped because neither destructive-test opt-in
environment variable was set; no database was touched. Their source and SQL
still pin the current divergent-tie behavior.

The normal focused package suites also passed:

```text
go test ./internal/qsoservice ./internal/database/sqlite \
  ./internal/cloud/reconcile ./internal/forwarding/smcloud \
  ./internal/evidence ./internal/forwarding/worker \
  ./internal/config ./internal/api
```

The repository had a pre-existing untracked Item 4 report. This audit changed
no production or test source; its only new deliverable is this report.
