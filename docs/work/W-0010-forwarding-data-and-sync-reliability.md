# W-0010 — Improve forwarding, data, and synchronization reliability

**Status:** Selected — outcome 9 in progress (slice 4 built, awaiting deploy check)
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
   without the handoff, handoff not cleared). Not yet deployed — the fixture check on the card is
   the next step.

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
