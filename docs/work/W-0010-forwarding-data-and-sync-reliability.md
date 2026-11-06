# W-0010 — Improve forwarding, data, and synchronization reliability

**Status:** Selected — outcome 9 CLOSED 2026-09-21 (operator: test proof plus passive station evidence); outcomes 1–8 remain unselected; outcome 11 SELECTED 2026-11-06 (operator: "yes, authorised — start with the transaction design")
**Selected:** 2026-09-20 (operator: "Select W-0010 and start outcome 9")
**Outcome:** Upload recovery, synchronization, email, and duplicate handling preserve operator
intent and converge without routine full-log churn or forbidden third-party API use.

## Ordered outcomes

1. A rotated SM Cloud token does not permanently strand in-flight uploads as terminal failures;
   reconciliation and queue state converge together.
2. Accidental duplicate FT8 QSOs are detected and offered an explicit keep/merge/delete resolution;
   deliberate repeats remain loggable.
3. ClubLog historical backfill uses its bulk-upload contract rather than replaying old QSOs through
   the realtime endpoint. Until then, manual ADIF upload remains the supported history path.
4. Session email subject/body becomes configurable only after PT-3 in
   [W-0008](W-0008-harden-audited-contract-boundaries.md) makes stamps truthful; timed-out submit and
   email writes gain an idempotent, retrievable outcome.
5. FT8 session reconnect reconciles durable QSOs since session start without replaying stale QSOs
   into a fresh session.
6. Phase-2 synchronization may add bucketed range hashes only when whole-manifest bandwidth is a
   measured scaling problem; equal-version conflict correctness remains PT-1 in
   [W-0007](W-0007-close-verified-p1-correctness-findings.md).
7. Low-severity database/import cleanup and a second callsign provider are adjacent-work items, not
   standalone sweeps.
8. SM Cloud is served over https even on the LAN (operator decision 2026-09-05, alpha.2 dogfood
   Finding #1), so the `allow_insecure_http` cleartext acknowledgement can be removed from the live
   station config; `docs/smcloud-deploy.md` owns the TLS steps.

9. A credential rejection at any forwarder does not strand its rows as terminal failures that read as a
   live backlog (alpha.2 dogfood Finding #19, 2026-09-12: one QRZ insert failed on 2026-08-06 during an
   invalid-key window, never retried after the key was corrected, and sat on the Forwarding card as
   "1 queued" for five weeks). Operator-observable outcomes: the Settings → Forwarding card shows failed
   rows apart from rows waiting to send and names the QSOs or links to the logbook's missing-from filter;
   a row failed for an authentication reason is re-armed when that forwarder's credential is changed via
   `PUT /v1/config` (or the card offers "retry failed"), sharing outcome 1's mechanism rather than an
   SM Cloud-only one; a terminal forwarding failure lands in the durable notification history (verify
   the category exists — unconfirmed 2026-09-12); and `qrz.classifyResponse` redacts the key QRZ echoes
   before the message reaches `last_error`, `smd.log` or the wire. Nearest confusable outcomes: a card
   that still counts `clearable` as "queued"; a blanket retry that re-sends rows QRZ rejected for a
   station-callsign mismatch (terminal for a different reason); a redaction that also strips the
   operator-readable reason. Same family, seen while planning the test row's removal: a delete-forward for
   a QSO whose insert never succeeded at an id-keyed destination (QRZ needs the prior `upstream_id`,
   `qrz.buildForm`) is today a NEW terminal failure rather than a no-op — the worker passes the empty id
   through by design so field-keyed deletes (ClubLog) stay reachable, so the skip belongs in the id-keyed
   forwarder, or at enqueue when that forwarder holds no successful insert for the QSO.
   Fixture and scope, ruled 2026-09-19: the terminal `failed` qrz insert of 2026-08-06 (inbox 2026-09-12,
   alpha.2 Finding #19) is preserved untouched — not retried, not cleared — as this outcome's regression
   fixture; the work must distinguish failed from waiting items, identify the QSO, recover from
   authentication failures, and make a QRZ deletion without an upstream ID a no-op.
10. Non-secret stored forwarder fields show their value (inbox 2026-09-26; routed by the triage of
    2026-10-06, not selected). Settings → Forwarding shows only "✓ Saved" for fields such as SM
    Cloud's "Cloud logbook name" and Service URL, because the daemon reports only that a value is
    set. Showing the value needs an API contract change: the descriptor marks each field secret or
    non-secret, and the view carries the value for non-secret fields only. Secrets stay write-only.

11. A session-email mark is not lost to delivery bookkeeping (inbox 2026-11-06, triage item 1).
    A QSO whose attached contact content is unchanged between compose and mark is marked, even when
    an upload stamp moved its `revision` meanwhile. A real content change or a deletion leaves it
    unmarked. Nearest confusable outcome: marking a row whose exported content did change.

## Outcome 9 — slice plan (2026-09-20)

Verified against the tree at `39c854e3` before any code:

- Already shipped: the QRZ key redaction (`68e74f90`, `qrz.redactKey` before classification), and
  the durable terminal record — `worker.markFailed` writes an operator event (category
  `notification`, kind `forward.failed`, W-0001/ADR 0076). The fixture row (2026-08-06) predates
  that write, so no event row exists for it.
- Config changes require a daemon restart (`docs/v2-design/forwarding.md` §8). Re-arming at
  `PUT /v1/config` would hand the rows to the still-running worker holding the OLD credential, which
  fails them terminally again. The re-arm therefore runs at worker start (every restart), for rows
  whose failure is classed as authentication — shared by every forwarder, so SM Cloud's 401 (outcome
  1's queue half) uses the same path.
- The card counts `pending + failed` as one `clearable` number (`GET /v1/forwarder-queues`), which
  is exactly the "1 queued" the fixture produced.

Slices, each its own commit:

1. **QRZ delete with no upstream id is a no-op.** `qrz.Submit(delete, "")` returns Success with
   detail `no_upstream_record` and fires no HTTP; the row settles `uploaded`. Forwarder-level (the
   worker keeps passing the empty id through so field-keyed deletes stay reachable). Post-commit
   review added log migration 0010's nullable `upstream_id_generation`: queue timestamps/status
   cannot safely choose among IDs retained across re-arms, so the lookup follows immutable success
   order and only treats a genuinely absent ID as the no-op ([ADR 0081](../decisions/0081-preserve-upstream-id-success-order.md)).
2. **Durable failure class.** `forwarding.Result` gains a terminal `Class` (`auth` for a rejected
   credential: QRZ `RESULT=AUTH`/401, SM Cloud 401, ClubLog 403 and its breaker, QRZCQ 401/403 by
   HTTP semantics — an inference, no QRZCQ citation); the worker stores it in the nullable
   `qso_upload.failure_class` column (log migration 0011, CHECK-enumerated like `origin`); any re-arm
   clears it; `GET /v1/qso/{uuid}/uploads` carries it as `failure_class`. Rows failed before the
   column read NULL — the migration backfills nothing (ruling (d)). Built 2026-09-20; sqlboiler
   models regenerated from a head-migrated scratch database.
3. **Boot re-arm.** At worker start, after the orphan sweep, each ENABLED forwarder's
   `failed` rows with `failure_class = 'auth'` return to `pending` (attempts reset), logged with the
   count. A still-bad credential fails them once more per restart — bounded, never a spin. Built
   2026-09-20: `RearmAuthFailedUploadsForForwarderWithContext` (mirrors the enqueue re-arm; selects by
   class only, never by `last_error`) called from the workers node before any worker claims; an
   `smd.log` info line `forwarder: credential-rejected uploads re-armed …` with `rearmed` carries the
   count. Proofs: storage (only the named forwarder's auth rows; a NULL-class row with an auth-looking
   message stays failed; re-armed rows claimable) and the orchestrated daemon (an enabled stub's auth
   row leaves `failed`, its unclassified sibling does not).
4. **Card and API.** `GET /v1/forwarder-queues` adds `waiting` (pending) and `failed`; the card
   reads "N waiting · M failed · K in flight", the failed count links to the logbook's
   `missing_from` filter for that forwarder, and a "Retry failed (M)" button posts
   `POST /v1/forwarder/{name}/queue/retry`, which re-arms that forwarder's `failed` rows.
   Design (2026-09-21): the GET keeps `clearable` as `waiting + failed` (the Clear button's
   count) and adds the two parts. The retry answers `{rearmed}`; 400 `invalid_forwarder`, 404
   `unknown_forwarder`, 400 `forwarder_disabled` — a forwarder disabled at startup has no worker, so
   its re-armed rows would read as "waiting" until the next start discards them (the nearest
   confusable outcome); the exact path name is looked up, like clear, and worker availability comes
   from the startup snapshot because a config save changes the live config before workers restart.
   The failed count links to `/logbook?missing_from=<name>`,
   a one-shot handoff the logbook applies at mount and then canonicalises away (the destination
   picker owns that state and does not write the URL); it is offered only for types that stamp
   per-QSO upload status, and the card says the filter lists every QSO not on the destination, not
   only the failed rows. Retry asks no confirmation — it is not destructive; a still-invalid row
   fails once more, terminally (ruling (a)). Built 2026-09-21: storage
   `RearmFailedUploadsForForwarderWithContext` shares one UPDATE with the auth re-arm;
   `ForwarderQueueCounts` carries `Waiting`/`Failed` with `Clearable()` derived; the SPA card reads
   "N waiting · M failed · K in flight" with "Retry failed (M)" and the gap link; the router's
   `takeLogbookMissingFrom` handoff; `api-endpoints.md` and `forwarding.md` §7 updated. Proofs:
   storage (auth-only re-arm and a merged count both fail), handler (no enabled gate → 200 for a
   disabled forwarder; auth-only re-arm → 1 not 2), SPA (link without the stamp guard, mount
   without the handoff, handoff not cleared). Deployed 2026-09-21 as `2.0.0-alpha.3-17-gc295b627`
   (schema 11); the boot re-arm logged nothing (no `auth`-classed rows, as ruled). **The fixture
   row is gone, and was before the preservation ruling:** `smd.log` 2026-09-12 13:19:15 —
   `forwarder disabled; discarded queued uploads`, `discarded: 1`, forwarder `qrz` — the
   disabled-forwarder startup discard (ADR 0039) removed the 2026-08-06 failed insert at a restart
   with QRZ turned off, seven days before the 2026-09-19 "preserve it" ruling. Today
   `GET /v1/forwarder-queues` reads all zeros for QRZ, correctly; QSO 7025 (7Q7EB) carries only its
   SM Cloud row and still counts as not-on-QRZ (`missing_from=qrz` count 1). The card's "M failed"
   reading is therefore proven by the handler and SPA tests, not by the live fixture. The alpha.2
   acceptance record's Finding 19 says the operator confirmed QSO 7025 was a UI test, not a contact;
   do not backfill it to QRZ merely to exercise the card. The passive station check confirms the
   empty-queue reading and the logbook gap, while live failed-row display remains unobserved. A live
   failed-row check, if required, must use a legitimate contact that fails naturally.
   **Closed 2026-09-21 (operator):** outcome 9 accepted on the test proofs and the passive station
   evidence; the live failed-row reading stays an open observation, to be recorded here if a real
   contact ever fails at a destination. Finding 19 of the alpha.2 record is answered by slices 1–4.

CI note (2026-09-20). Slice 2's run (35511192956) tripped the 10-minute per-package race timeout in
`internal/api`: the package had grown to 349–587 s on the runner (233 s of that is runner variance
between two docs-only runs), because each of its ~370 test servers migrated a fresh database through
both migration sets (~0.46 s each under `-race`), so every new migration grew all of them. Operator
ruling: raise the timeout to 15 m in a CI-only commit (`98af6e2c`), then fix the cause as a separate
test-infrastructure commit — one migrated template per package run, closed, copied per test into
`t.TempDir()`; remeasure and consider restoring 10 m. Measured after the refactor: `-race -short`
239 s → 12 s locally, plain run 12 s → 4 s, 488/488 passing, template directory removed by `TestMain`.

Rulings for slices 2–4 (2026-09-20):

- (a) **"Retry failed" re-arms all failed rows for the enabled, path-named forwarder.** It is an
  explicit operator action, unlike automatic recovery. A still-invalid row may fail once more and
  write one more `forward.failed` event, but it cannot duplicate an accepted upload.
- (b) **Authentication failures re-arm at worker start, not at `PUT /v1/config`.** Each enabled
  forwarder gets one attempt per daemon restart; the running worker is never handed rows while it
  still holds the old credential.
- (c) **The next log migration adds nullable `qso_upload.failure_class` (now 0011) and bumps the
  schema head.** Durable typed state is the contract; recovery must not parse redacted,
  provider-owned error text. Migration 0010 was consumed by slice 1's reviewed
  `upstream_id_generation` prerequisite (ADR 0081); the policy ruling is unchanged.
- (d) **Migration 0011 leaves every existing row's `failure_class` NULL.** In particular, the
  preserved 2026-08-06 QRZ fixture remains untouched by automatic boot recovery. It moves only if
  the operator explicitly invokes "Retry failed", consistent with (a).

## Outcome 11 — transaction design (2026-11-06, PROPOSAL for review; no code yet)

Authorised by the operator, design first: "yes, authorised — start with the transaction design".
The comparison policy is the reviewed one in the inbox triage (item 1): ignore the listed
delivery-bookkeeping fields; compare every other field actually emitted, including QSL fields,
exported enrichment and QSO identity; keep comparison and marking atomic; preserve `revision`
increments and cloud propagation. Size is not estimated until this design is reviewed.
**Implementation waits for review of the retry and result contract below.** Revision 2 is
`4ef2b9bd`, and revision 3 is `83127331` (the deadline, the enqueue after cancellation, the
failed-commit behaviour, the result wording). Revision 4 (this text) aligns the retry rule,
restates the marking context as an admission budget, replaces case 12a's trigger, and qualifies
the response estimate.

### Today (verified against the tree at `9db1351d`)

- `handleSessionEmail` (`internal/api/handler_session_email.go`):
  1. fetches each QSO by UUID with `fetchSessionQsos`, one `FetchQsoByUUIDWithContext` per UUID
     (`handler_session_export.go:52`), up to `maxSessionQsoUUIDs` = 10,000;
  2. composes with `adif.ComposeToAdifString`, archives, and sends;
  3. after a successful send, `stampSessionEmailed` calls
     `MarkSessionEmailedAtRevisionWithContext` (`internal/database/sqlite/api_context.go:2433`).
- **The mark** is one autocommit `UPDATE … WHERE deleted_at IS NULL AND (id, revision) IN
  (json_each(?)) RETURNING id`. The composed revision is the guard, so any later update (an
  upload stamp included) makes the row miss: the observed defect.
- **Dropped (revision 3):** revision 2 raised a hypothesis about the autocommit `RETURNING` scan
  path. It is not part of this fix's rationale or required work. The explicit marking transaction
  is justified on its own: it makes each attempt's outcome a checked commit.
- **Revision and sync:** the `qso` update trigger bumps `revision` on every update, including the
  mark itself (ADR 0050). Afterwards `EnqueueStampSync` re-enqueues the stamped ids to row-mirror
  forwarders (SM Cloud), best-effort (`internal/qsoservice/stamp_sync.go`).
- **The cloud guarantee is convergence to the latest row, not delivery of every revision.**
  - The worker fetches the **current** row when it processes a queue item (`fetchQsoForAction`,
    `internal/forwarding/worker/worker.go`).
  - The cloud upsert is guarded by `revision` (ADR 0050; the guard ordering is described at
    `internal/cloud/store/store.go:85`).
  - The hourly reconcile is the backstop.

  Intermediate revisions may be skipped. **This design adds no requirement that any intermediate
  revision reach the cloud.**
- **Deadlines today:**
  - `sessionEmailResponseMargin` extends only the HTTP **write** deadline
    (`rc.SetWriteDeadline`, `handler_session_email.go:155-156`; rationale comment from line 146). It does **not** bound
    `r.Context()`, which ends only when the client disconnects or the server shuts down.
  - Each database call is bounded separately: `ensureCtxTimeout`, and
    `TransactionContextTimeout` in `BeginTxContext`, when the caller's context has no deadline.
- **Database:**
  - WAL, a connection pool larger than one (`SetMaxOpenConns`, `service.go:145`), deferred
    `BEGIN` (`BeginTxContext`, `service.go:303`) and `busy_timeout`.
  - A deferred transaction that reads first can strand on a concurrent commit when it later tries
    to write. The repository already pins that premise with a real pool on a temporary file
    (`TestReadFirstTxStrandsOnConcurrentCommit`, `internal/database/sqlite/review_findings_test.go:201`).
  - The shared `:memory:` helpers pin `MaxOpenConns` to 1, so they cannot show it.

### Design

1. **One definition of "emitted".**
   - **Refactor:** `parseToAdifString` (`internal/adif/string.go:63`) is refactored into a field
     visitor that yields `(TAG, formatted value)` for each field it would write. That is after
     `formatField` (FREQ in MHz, dates without dashes, times without colons), the `adif`-then-`json`
     tag fallback, and the DXCC numeric filter.
   - **Two users:** `Record.String()` writes the visitor's output exactly as now. A new
     `adif.EmittedFields(Record) map[string]string` collects it. The comparison and the attachment
     therefore cannot disagree about which fields exist or how they read.
   - **Deterministic golden test, written before the refactor:**
     - the header's `CreatedTimestamp` is **fixed** in the fixture. `HeaderSection.String`
       otherwise stamps `time.Now()` (`string.go:16-20`), so comparing two live calls proves
       nothing;
     - the expected output is a checked-in literal, compared byte for byte;
     - the fixture QSOs cover:
       - FREQ/FREQ_RX conversion, and date and time formatting;
       - empty fields being omitted;
       - a field taking its tag from the `json` fallback;
       - a non-numeric DXCC being dropped;
       - several records, in order.
2. **The ignore list is a named, reviewed set** in `internal/adif`, next to the visitor:
   - `CLUBLOG_QSO_UPLOAD_DATE`, `CLUBLOG_QSO_UPLOAD_STATUS`;
   - `QRZCOM_QSO_UPLOAD_DATE`, `QRZCOM_QSO_UPLOAD_STATUS`;
   - `APP_QRZLOG_LOGID`;
   - `SM_QSO_UPLOAD_DATE`, `SM_QSO_UPLOAD_STATUS`;
   - `SM_FWRD_BY_EMAIL_DATE`, `SM_FWRD_BY_EMAIL_STATUS`;
   - `QRZCOM_QSO_DOWNLOAD_DATE`, `QRZCOM_QSO_DOWNLOAD_STATUS` (prospective).

   **Guard tests:**
   - the set equals exactly this list, so any change is a reviewed change;
   - every member is a real `adif` tag of `Record`, so a typo cannot silently ignore nothing.

   Every other emitted field is compared, `APP_SM_QSO_ID` (the QSO's identity) included.
3. **Comparison key:** for a QSO `q`, `contentOf(q) = EmittedFields(QsoToRecord(q))` minus the
   ignore list. This is the same function `ComposeToAdifString` applies per record, so the
   composed side is exactly what was attached.
4. **Verify, then mark, per attempt, with a compare-and-set on the revision just verified.**

   After a successful send, the daemon takes **one stamp date** (UTC `YYYYMMDD`, one clock reading)
   for the whole operation. It keeps `committed`, a set of ids, empty at first, and runs up to
   **three attempts in total**. That is a proposal for operator confirmation: three attempts, not
   three retries after a first attempt. Each attempt works on the composed rows not yet in
   `committed` that are still candidates:

   - **(a) Verification read**
     - **One bounded read transaction:** batched by id (up to the 10,000-row cap) and **finished
       and closed** before any marking begins. The rows are fully read, `rows.Close` is called and
       checked, and the read transaction is committed (or rolled back) and checked.
     - **The candidates:** a soft-deleted or missing row stops being a candidate.
   - **(b) Compare in Go,** holding no transaction. A row is *eligible* when
     `contentOf(current) == contentOf(composed)`; its current revision `r'` is remembered. A row
     whose content differs stops being a candidate.
   - **(c) One marking transaction** for this attempt's eligible rows:
     1. `BEGIN`;
     2. `UPDATE … WHERE deleted_at IS NULL AND (id, revision) IN (eligible id, r') RETURNING id`,
        with the operation's stamp date;
     3. **consume every returned id, call `rows.Close`, and check `rows.Err` and the close error**;
     4. `COMMIT`, and check its error.

     **Rollback and failure, as the pinned stack behaves:**
     - **An error in steps 2–3:** the transaction is still open, so `tx.Rollback()` issues a real
       SQL `ROLLBACK`. Its error is checked and logged. The attempt is **not committed**.
     - **A failed `tx.Commit()` (step 4).** Go's `database/sql` marks the transaction done before
       calling the driver (`sql.go:2287` onward; the context is checked first, and a done context
       returns `ctx.Err()` without a `COMMIT`). A later `tx.Rollback()` therefore returns
       `ErrTxDone` and issues **no** SQL. The design does not claim a rollback it cannot issue.
       Instead it relies on what the pinned driver does: modernc `v1.48.1`'s `Commit`
       (`modernc.org/sqlite@v1.48.1/tx.go:35`) checks `sqlite3_get_autocommit`, and if the failed
       `COMMIT` left the transaction active, it executes `rollback` itself, ignoring that
       rollback's error, before returning the commit error.
     - **The attempt's state after a failed commit is *uncertain*,** not "unmarked". Another
       attempt follows (see "Which outcomes allow another attempt"):
       - its ids are **not** added to the confirmed set;
       - they are **not** reported or logged as definitively unmarked; the log calls them
         "unconfirmed";
       - they stay candidates. If an attempt remains, its verification read re-reads them. A mark
         that did commit has changed only the ignored email-mark fields and `revision`, so the read
         establishes only that the row is still **eligible**. The row is **confirmed**, and added
         to `emailed`, only when that attempt's own marking `COMMIT` succeeds. Re-marking is
         idempotent, with the same stamp date;
       - earlier confirmed commits are unaffected.

     **The confirmed set:** only the ids an attempt returned **and** whose `tx.Commit()` returned
     `nil` are added to `committed`.

     **The write lock:** the `UPDATE` is the transaction's first statement, so the transaction is a
     writer from its first statement. There is no read-to-write upgrade.
   - **(d) Lost races.** An eligible row the `UPDATE` did not return was written by someone else
     between (a) and (c). It stays a candidate for the next attempt.
   - **Which outcomes allow another attempt (revision 4).** One rule, stated once:
     - **Another attempt follows** while attempts remain (three in total) and the marking context is
       not done, after any of these, each leaving the affected rows as candidates:
       - a **lost race** (an eligible row the `UPDATE` did not return). This is not an error;
       - a **verification-read error** in (a). Nothing was written;
       - a **failed `BEGIN`**. There is no transaction, so there is nothing to roll back;
       - a **marking-transaction error after `BEGIN` and before `COMMIT`** (the `UPDATE`, the
         scan, or `rows.Close`), after an explicit `tx.Rollback()`;
       - a **failed `COMMIT`**, where the attempt is *uncertain*. The next attempt's verification
         read resolves it.
     - **The loop ends early** only when the marking context is done (cancellation or budget),
       **or** when the explicit `tx.Rollback()` after an error between `BEGIN` and `COMMIT` itself
       fails. In that
       case the connection's state is unknown, so the loop stops rather than compound it, and the
       log names the reason.
     - **Normal end:** no candidates remain, or the third attempt is done.
   - **The marking context: a cancellation and admission budget, not a completion deadline.**
     - **What it is:** `markCtx := context.WithDeadline(r.Context(), markDeadline)`, created after
       the send. `markDeadline` is the write deadline set at `handler_session_email.go:156`
       (`now + mailer.Timeout() + sessionEmailResponseMargin`, computed once and kept), minus the
       response reserve (below).
     - **What it does:** it decides whether an attempt or a database operation is **admitted**. No
       attempt starts, and no new verification or marking statement is issued, after it is done.
       It keeps client cancellation, because detaching is out of scope.
     - **Cleanup is exempt.** Closing rows, closing the verification read transaction, and
       `tx.Rollback()` after an error still run after the budget is spent. They are never skipped
       because it has expired.
     - **What it does not do: guarantee completion.**
       - Passing a context that has a deadline **bypasses** the package defaults:
         `ensureCtxTimeout` returns a context with a deadline as-is (`internal.go:129`), and
         `BeginTxContext` adds `TransactionContextTimeout` only when there is no deadline
         (`service.go:317-321`).
       - The pinned driver runs `COMMIT` and `ROLLBACK` on `context.Background()`
         (`modernc.org/sqlite@v1.48.1/tx.go:35-59`), so a `COMMIT` already issued can outlive the
         marking context.
       - `database/sql` checks the transaction's context before `COMMIT` and returns `ctx.Err()`
         without committing when it is done (`sql.go:2287`).
     - **Shorter per-operation limits are preserved explicitly.** Each database operation runs on
       its own child context: `context.WithTimeout(markCtx, limit)`, where `limit` is the existing
       default for that kind of call (`ContextTimeout` for the verification read and the marking
       statement, and `TransactionContextTimeout` for the marking transaction). `WithTimeout`
       takes the earlier of the two deadlines, so neither bound is lost.
5. **The result contract (revision 2).**
   - **`emailed` means confirmed commits.** It is the union of the confirmed set across **all**
     attempts, in request order. A later attempt cannot undo an earlier confirmed commit, so it
     holds even when a later attempt fails or is uncertain, the context is cancelled, or the budget
     runs out.
     - **Rows outside `emailed`** are *not confirmed* as marked. That covers both "not marked" and
       "unconfirmed" after a failed commit; the log distinguishes the two.
     - **Today's rule**, where any stamp error gives `emailed: []`, now applies only when **no**
       attempt confirmed a commit.
   - **One stamp date** is used by every attempt and reported as `date`. `date` is empty only when
     `emailed` is empty.
   - **Stamp sync:** `EnqueueStampSync` is called once, after the loop, with the whole confirmed
     union, however the loop ended.
     - **It is best-effort and not guaranteed.** It runs on the marking context, so after a
       cancellation or an expired budget it may fail at once.
     - Its failure is logged and changes nothing reported. The reconcile remains the backstop.
     - Guaranteeing the enqueue after cancellation would need a separate context and lifecycle
       decision, which is out of scope.
   - **Atomicity is per attempt.** Within an attempt, the eligible rows are marked all-or-nothing,
     and each only at the revision whose content was compared. **The retry loop as a whole is not
     atomic.** An operation can end with some rows marked by an earlier attempt and others
     unmarked, and the response says exactly which.
   - **Sent and marked stay distinct (ADR 0095):**
     - `sent` lists every composed UUID in request order, and `omitted` the UUIDs not found at
       compose;
     - `emailed` is the committed subset;
     - the response stays `200`, because the mail left.
   - **The log line** gives the reason counts: confirmed marked, content changed, deleted,
     unconfirmed (failed commit), lost race after the last attempt, and stopped by error,
     cancellation or budget. It carries no QSO content.
6. **Unchanged:** upload stamps keep bumping `revision`, and the mark's own bump propagates through
   the stamp-sync enqueue or the reconcile. Nothing in this design alters the trigger, the queue, the
   worker, the upsert guard or the reconcile.
7. **Why not one transaction around read, compare and mark.**
   - **A deferred transaction** would not hold the writer lock while the rows are read and compared.
     It takes a read snapshot, and becomes a writer only at its first write. But that upgrade fails
     if another connection committed meanwhile: the stranded-snapshot premise pinned by
     `review_findings_test.go:201`. So it would refuse exactly in the race this fix is about.
   - **An immediate transaction** would avoid the strand, but would hold SQLite's single write lock
     while up to 10,000 rows are read, mapped and compared. Every QSO submit would wait behind it,
     against the invariant that the operator can always log.
   - **The per-attempt compare-and-set** gives the per-row guarantee without either cost.

### Known limitation (not changed by this fix)

**The mark runs on the request context.** A client disconnect after a successful send cancels the
remaining attempts, and the mail is left unmarked for the rows not yet committed. Detaching the
mark from client cancellation is **out of scope** (reviewer direction). Rows committed before the
cancellation stay marked and are reported in `emailed` if a response can still be written. Their
stamp-sync enqueue is best-effort and may fail after the cancellation; the reconcile heals the
mirror.

### Tests (barriers, not sleeps)

- **Seams:** unexported hooks, nil in production, like `wsWriteDone`:
  - `sessionEmailAfterSend`, on `Server`, between the send and the first attempt's (a);
  - `sessionEmailBeforeMark(attempt)`, on `Server`, between (b) and (c) of each attempt;
  - a fault seam in the sqlite marking method for steps 2–4.
- **Two fixture shapes:**
  - **in-memory SQLite** (the ordinary single-connection helpers) for the comparison and for writes
    placed **between** attempts with the seams;
  - **a temporary-file WAL database with a real multi-connection pool** (`concurrentCacheService`,
    `internal/database/sqlite/enrichment_cache_test.go:250`) for every claim about snapshots,
    write locks and concurrent commits.

| # | Case | Fixture | Expected |
|---|---|---|---|
| 1 | Stamp only: the worker's `MarkUploadSuccessWithAdifStampWithContext` (`worker.go:945`) for ClubLog/QRZ after send | memory | marked; `revision` bumped by the stamp and again by the mark; the stamp-sync enqueue holds the id |
| 2 | Content edit (`NAME`) after send | memory | not marked; in `sent`, not in `emailed` |
| 3 | Enrichment change to an exported field (`COUNTRY`) | memory | not marked |
| 4 | QSL field change (`QSL_SENT`) | memory | not marked |
| 5 | Deletion after send | memory | not marked |
| 6 | Mixed batch: unchanged, stamp-only, edited, deleted | memory | marked = {unchanged, stamp-only}; `sent` = all four, in order |
| 7 | Lost race: a stamp-only write at `sessionEmailBeforeMark(1)` | memory | attempt 2 re-verifies and marks |
| 8 | Lost race with a content edit at `sessionEmailBeforeMark(1)` | memory | attempt 2 finds the content changed; not marked |
| 9 | Attempts exhausted: a write at every `sessionEmailBeforeMark` | memory | unmarked after the third attempt; reason logged |
| 10 | **Mixed success then failure:** attempt 1 marks A and loses B to a race; the marking transactions of attempts 2 **and 3** fail before `COMMIT` (fault seam), so retries run out | memory | `emailed` = [A] in request order; `date` = the one stamp date; A marked on read-back, B not; the stamp-sync enqueue holds exactly [A] |
| 11 | **Cancellation after a commit:** attempt 1 marks A and loses B; the request context is cancelled at `sessionEmailBeforeMark(2)` | memory | no attempt 2 `UPDATE`; A stays marked on read-back; A is in the accumulated result (`emailed` = [A] when the response is written). **No assertion that the enqueue succeeded**: it is best-effort, and the reconcile is the backstop |
| 12 | Rollback within an attempt: fault after the `UPDATE`, before `COMMIT` | memory | real SQL rollback; nothing from that attempt on read-back; earlier confirmed ids kept |
| 12a | **Failed commit**, triggered by a **test-only deferred foreign-key violation**. The fault seam runs `PRAGMA defer_foreign_keys = ON` and writes a row that violates a foreign key, inside the marking transaction. SQLite reports a deferred violation at `COMMIT`, and the `COMMIT` fails, leaving the transaction open ([deferred constraints](https://www.sqlite.org/foreignkeys.html#fk_deferred)). That exercises modernc's cleanup branch. A reader snapshot is **not** a trigger: in WAL a reader does not block a writer's commit ([WAL concurrency](https://www.sqlite.org/wal.html#concurrency)) | memory or WAL file | `tx.Commit` returns the constraint error; a later `tx.Rollback` returns `ErrTxDone`; read-back shows the attempt's rows unmarked (the driver's own rollback); the ids are not confirmed and are logged "unconfirmed"; attempt 2's read finds them eligible, and only attempt 2's successful `COMMIT` adds them to `emailed`; earlier confirmed ids kept. **Driver behaviour VERIFIED** by a scratch check, 2026-11-06 (see *Pre-implementation checks*); the repository test is still to be written |
| 12b | **Deadline:** the marking context's deadline passes before attempt 2 | memory (fake clock or short deadline) | no attempt 2; attempt 1's confirmed ids reported; the log reason is "budget" |
| 13 | Edit then revert (A→B→A) before verification | memory | marked: the marked content equals the sent content |
| 14 | Re-send of an already-emailed row | memory | marked; the email-mark fields are ignored |
| 15 | Concurrent writer on another connection, committing between the verification snapshot and the mark | WAL file, pool | the compare-and-set misses that row; the next attempt marks it if only bookkeeping changed |
| 16 | The marking transaction is a writer from its first statement: a concurrent commit just before `BEGIN` does not strand it | WAL file, pool | no `SQLITE_BUSY_SNAPSHOT`; marks commit |
| 17 | Cloud: after a mark, the mirror's queue holds the row's id, and the worker sends the **current** row | memory | id enqueued. No assertion on intermediate revisions. |

**Golden and guard tests:** the deterministic compose golden, and the ignore-list guards (design 1–2).

**Reversion proofs:**
- with the comparison reverted to composed-revision equality, cases 1, 6 and 7 fail ("stamp-only row
  unmarked");
- with the ignore list emptied, case 1 fails;
- with the union replaced by the last attempt's result, case 10 fails (`emailed` = [], A marked on
  read-back).

Each proof is checked to reach its assertion.

### Open for review

- **Attempts: three in total** (not three retries), within the marking context's admission budget.
  - **Reviewer:** approved as the technical choice (2026-11-06).
  - **Operator:** confirmed by the station operator on 2026-11-06, in their own message: "Confirmed:
    three attempts in total, within the marking admission budget." The wording was suggested by the
    reviewer. The operator affirmed the decision as their own when asked to choose: "(b), and
    commit".
- **The response-write reserve** subtracted from `markDeadline`. **Provisional: 5 s.** The reviewer
  calls it reasonable as a provisional value, and it is to be confirmed by measurement.
  - **The size estimate, and what bounds it:**
    - **The all-valid case:** every requested string is a canonical 36-character UUID, all are
      found, and all are emailed. `sent` and `emailed` then hold about 20,000 × 39 bytes ≈ 780 KB.
      That is an estimate for that case, **not an enforced maximum**.
    - **`omitted` is not length-validated.** Request strings are checked only for count
      (`maxSessionQsoUUIDs`, `handler_session_email.go:129`). Strings that do not resolve are
      echoed back in `omitted` as received (`handler_session_export.go:63`). Their total size is
      bounded only indirectly, by the request body cap `server.max_body_bytes` (default 1 MiB,
      configurable; `internal/config/defaults.go:51`).
    - **JSON escaping can enlarge them.** For example, the encoder writes `<` as `\u003c`, six bytes
      for one. A pathological `omitted` could therefore approach six times the body cap.
    - Imposing a bound on `omitted` is **not** part of this fix. It is recorded here as an
      observation.
  - **The measurement**, before the value is fixed:
    - a real `httptest.Server` (not a `ResponseRecorder`, which cannot measure transport time),
      and a client that reads the **entire** response body;
    - timing the encoding and delivery of two responses: the all-valid 10,000-UUID one, and a
      pathological `omitted` at the default body cap.
  - **What the measurement can show:** loopback timings support the choice; they cannot establish
    performance on slower connections. The browser normally runs on the station host, over
    loopback.
  - **No reserve guarantees timely completion.** A `COMMIT` issued before the deadline can outlive
    it (above). The reserve only reduces how often the response misses its write deadline.
- **Batched verification:** one bounded read snapshot (supported by the reviewer).
- **Case 12a's trigger:** reproduced against the pinned driver (*Pre-implementation checks*,
  check 2).
- **The reserve value:** measurements are recorded below. The value stays **provisional** until
  they are reviewed.

### Pre-implementation checks (2026-11-06; scratch investigations, outside the repository)

These were authorised by the operator as scratch investigations: no product change and no commit.
The programs live in the session scratchpad, not the repository. Environment: Go 1.26.2,
`modernc.org/sqlite v1.48.1` (the version pinned in `go.mod`), Linux 7.2.15 (Fedora 44), 24 CPUs,
an unloaded host.

**Check 2: case 12a's failed-commit trigger. VERIFIED at driver level.**

- **Setup:** a temporary-file database with the daemon's pragmas (WAL, `foreign_keys(on)`,
  `busy_timeout(5000)`, `synchronous(normal)`), and `MaxOpenConns(1)` so every statement runs on the
  same connection.
- **The marking shape:** `tx.QueryContext(UPDATE … RETURNING id)`, with rows consumed, `rows.Err`
  checked, and `rows.Close` checked.
- **The fault:** `PRAGMA defer_foreign_keys = ON` plus a violating `INSERT`, both inside the
  transaction.

| Step | Observed |
|---|---|
| journal mode, foreign keys | `wal`, `1` |
| `UPDATE … RETURNING` | 2 ids returned |
| violating `INSERT` inside the transaction | no error (deferred) |
| `tx.Commit()` | `constraint failed: FOREIGN KEY constraint failed (787)` |
| `tx.Rollback()` afterwards | `sql: transaction has already been committed or rolled back`; `errors.Is(err, sql.ErrTxDone)` is true |
| `BEGIN` again on the same connection | succeeds: no transaction left open |
| read-back | marked rows 0, child rows 0: nothing from the attempt persisted |
| a fresh attempt on the same connection | 2 rows marked and committed; read-back shows 2 |

- **Control (the same faulting sequence, issued as plain statements, bypassing `tx.Commit`):** the
  plain `COMMIT` fails with the same error. A following `BEGIN` on that connection fails with
  `cannot start a transaction within a transaction (1)`.
  - **What it shows:** SQLite does leave the transaction open after this failed `COMMIT`. So the
    clean connection in the main check is the work of modernc's own `rollback` in its `Commit`
    cleanup branch (`tx.go:35`), as designed.
  - **Consequence:** the deferred-FK fault is a valid trigger for case 12a.
  - **Verified:** the driver's behaviour.
  - **Not yet written:** the repository acceptance test. It needs the fault seam inside the marking
    transaction, and will be judged when it is written.

**Check 3: response encode-and-deliver time. MEASURED on loopback.**

- **Setup:** a real `httptest.Server`. The handler mirrors `httpkit.WriteJSON`
  (`Content-Type`, then `json.NewEncoder(w).Encode`) and the `SessionEmailResponse` shape. The
  client reads the entire body with `io.ReadAll`. Each case was run 30 times after 3 warm-up runs.

| Case | Body | Client: request to full body (median / max) | Server: encode and write (median / max) |
|---|---|---|---|
| all-valid: 10,000 `sent` + 10,000 `emailed` canonical UUIDs | 780,070 B | 1.04 ms / 1.68 ms | 0.78 ms / 1.32 ms |
| pathological `omitted`: 10,000 distinct strings of 95 `<` plus a 5-digit suffix. The request array is about 1,030,002 B, within the 1 MiB default cap. | 5,780,063 B (`<` escaped as `\u003c`) | 4.81 ms / 6.07 ms | 3.62 ms / 5.19 ms |

- **On loopback:** the station's normal case, with the browser on the station host. Both cases
  finish in under 7 ms, roughly 700 times inside a 5 s reserve.
- **Slower links:** arithmetic on the body size only, **not measured**:

  | Case | 10 Mbit/s | 1 Mbit/s |
  |---|---|---|
  | all-valid | about 0.62 s | about 6.2 s |
  | pathological | about 4.6 s | about 46 s |

  So 5 s covers the all-valid case down to about 1.25 Mbit/s. The pathological case exceeds it
  below about 10 Mbit/s.
- **The pathological response is synthetic.** It has `sent` and `emailed` empty and everything in
  `omitted`, and the real handler never produces it: a request with no matching QSOs is refused
  with `400 no_qsos` (`handler_session_email.go`). A reachable pathological response needs at least
  one found QSO, so it is slightly smaller. The measurement stands as encoding and transport
  evidence for a body of that size.
- **Reading:** the measurement supports 5 s for the station's loopback use. It cannot establish
  performance on slower connections, and the pathological case is the response-size observation,
  kept separate from this fix.

### Implementation (2026-11-06; authorised: "authorised — implement W-0010 outcome 11")

Uncommitted at the time of writing; awaiting review.

- **What was built:**
  - `internal/adif`: a field visitor shared by `Record.String` and `EmittedFields`, plus
    `EmailMarkContent` and the reviewed ignore list.
  - `internal/database/sqlite/session_email_mark.go`: the explicit-transaction mark, with
    classified failures (`ErrSessionMarkUncertain`, `ErrSessionRollbackFailed`), a test-only
    fault seam, the batched verification read, and per-operation limits that survive a longer
    caller deadline.
  - `internal/api/handler_session_email.go`: the three-attempt loop on a marking context
    (`writeDeadline − 5 s`), the accumulated confirmed result, and the best-effort stamp sync.
    It has seams on `Server`, nil in production.
- **Tests:** the deterministic compose golden; the guard tests; cases 1–14, 12a and 12b; the WAL
  pool case (15 and 16); and the per-operation limit.
- **Reversion proofs, each failing at its assertion, with each restore verified by hash:**
  - R1: revision equality instead of content;
  - R2: an empty ignore list;
  - R3: a failure discarding earlier marks;
  - R4: commit instead of rollback;
  - R5: no admission check before the mark;
  - R6: changed FREQ formatting (golden);
  - R7: a caller deadline replacing the per-operation limit.
- **Gates:** whole-tree `gofmt` and `go vet`, `go test ./...`, `-race` on `adif`, `api` and
  `sqlite`, the maintainability observatory (0 regressions), and `golangci-lint` (0 issues).
- **Canonical reference updated:** `docs/v2-design/api-endpoints.md` (`POST /v1/session/email`).
- **Review corrections (2026-11-06, before commit)**, each with deterministic coverage and a
  reversion proof (F1–F5, plus R4 re-run):
  1. **The statement limit:** the marking `UPDATE` runs on its own child context, limited by
     `ContextTimeout`, inside the transaction's `TransactionContextTimeout`. It is observed on the
     real marking path.
  2. **An automatic rollback is not a failure:** `sql.ErrTxDone` from the explicit rollback means
     `database/sql` already rolled the transaction back when its context ended. It is classified
     as rolled back, not as a rollback failure, so retries continue while the budget remains.
  3. **One cleanup-failure classification:** `ErrSessionRollbackFailed` now covers the
     verification read as well as the mark. The loop stops on either, matching the stop rule;
     an ordinary read error still allows another attempt.
  4. **The budget test:** it controls expiry exactly after the first confirmed commit, through a
     marking-context seam, so it no longer depends on a 250 ms clock window. The stop reason is
     taken from the context's cause, so a budget ended with `context.DeadlineExceeded` reads as
     "budget", and a client cancellation as "cancelled".

## Verification boundary

Fixtures must make retry, historical backfill, deliberate repeat, accidental duplicate, stale
session, and outcome-unknown states observably different. Enrichment and mirror failures remain
best-effort; QSO plus upload-queue writes remain atomic. Third-party credentials and network calls
are never required by ordinary automated tests.

## References

- [`ADR 0039`](../decisions/0039-forwarder-enabled-gates-enqueue-config-driven.md)
- [`ADR 0050`](../decisions/0050-sync-protocol-revision-counter.md)
- [`ADR 0052`](../decisions/0052-smcloud-identity-backup-first-passive-store.md)
- Expanded evidence and resolved substeps: `d0391ed7:docs/backlog.md`.
