# W-0010 — Improve forwarding, data, and synchronization reliability

**Status:** Selected — outcome 9 in progress
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
   credential: QRZ `RESULT=AUTH`/401, SM Cloud 401, ClubLog and QRZCQ auth rejections); the worker
   stores it in a new nullable `qso_upload.failure_class` column (log migration 0011). Rows failed
   before the column read NULL.
3. **Boot re-arm.** At worker start, after the orphan sweep, each ENABLED forwarder's
   `failed` rows with `failure_class = 'auth'` return to `pending` (attempts reset), logged with the
   count. A still-bad credential fails them once more per restart — bounded, never a spin.
4. **Card and API.** `GET /v1/forwarder-queues` adds `waiting` (pending) and `failed`; the card
   reads "N waiting · M failed · K in flight", the failed count links to the logbook's
   `missing_from` filter for that forwarder, and a "Retry failed (M)" button posts
   `POST /v1/forwarder/{name}/queue/retry`, which re-arms that forwarder's `failed` rows.

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
