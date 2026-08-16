# URGENT TODO — whole `internal/` logging-gap review

**Status:** open · **Raised:** 2026-08-12 · **Scope:** all production Go code under
`internal/` · **Reviewed:** 287 files / approximately 68,000 lines · **Review type:**
operator-directed, source-level logging audit. No production code was changed by this
review.

This document consolidates the current release-relevant logging gaps across the whole
`internal/` tree. It cross-checks the older package reviews rather than copying their
open labels blindly: several findings in the API, bridge, cloud, forwarding, FT8 and
QSO-service reviews have since shipped. The list below contains only gaps reproduced
against the working-tree source on 2026-08-12.

**Working-tree caveat:** while this review was running, uncommitted changes appeared in
`internal/forwarding/worker/worker.go`, `internal/forwarding/smcloud/reconcile.go`, and
`cmd/smd/main.go`. They were not made by this review. This document evaluates the latest
working-tree source and therefore does not re-file forwarding F5, F7, F8, F15 or F16,
which those edits were already addressing. Re-check those files before implementing
this list if the uncommitted changes are later discarded.

**Post-review update (2026-08-12, after this audit ran):** the cloud logging cluster was
committed this session — **L8 (local evidence quarantine invisible) is shipped as C1b**
(`internal/evidence/sync.go` now logs one bounded per-batch quarantine Warn; the per-kind
count breakdown L8 also asks for is the one remaining refinement), and **L6's cloud half
is shipped as C4/C5** (`internal/cloud/server/access.go` adds the outer request-id +
application access-log middleware; the logbook-list / export / evidence error lines carry
`tenant_id`). **L6's remainder is still open:** cloud-stack *structured panic recovery*,
and the **daemon-side** request-id at `internal/api/middleware.go`. Verify these two
findings against the committed tree before actioning them.

The release question used throughout is:

> Can an operator distinguish this outcome from its nearest confusable state using the
> default production logs alone?

This deliberately does not require every library layer to log every returned error.
An error should normally be logged once at the boundary that decides its disposition.
The gaps below are places where a decisive fact is lost, falsely classified, visible
only at Debug, or omitted from the fields needed to join related records.

---

## Priority summary

| Priority | Release meaning | Findings |
|---|---|---|
| P0 | Release blocker: the diagnostic system or retained evidence can silently fail | L1–L3 |
| P1 | Fix before a serious release: real-world state changes or failures are ambiguous | L4–L9 |
| P2 | Important operability work | L10–L13 |
| P3 | Useful hardening | H1–H4 |

---

## P0 — release blockers

### L1. The logging system cannot reliably report failure of its own output

> ✅ **FIXED 2026-08-12 (working tree, awaiting commit).** `io.MultiWriter` is
> replaced by a health-tracking fan-out (`internal/logging/healthwriter.go`) that
> (a) delivers each record to every target even when one errors — a failing file
> no longer stops delivery to the console; (b) NEVER returns an error to zerolog,
> so a failing durable writer produces no generic per-write stderr spam and cannot
> recurse; (c) on the healthy→failing and failing→healthy edges writes ONE JSON
> line to a non-recursive fallback (os.Stderr → journald) carrying the failure
> count + cause + timestamp, and re-warns every 5 min while still failing
> (write-driven heartbeat, operator decision); (d) exposes `Degraded()` so
> `/v1/healthz` reports `{"status":"degraded","database":"ok","logging":"degraded"}`
> at HTTP 200 (503 stays reserved for the DB — operator decision). TDD: 6 fan-out
> unit tests (AC1 isolation, AC2 one degraded line + cause, AC3 recovery, AC4
> bounded under a 1000-record outage, heartbeat, AC6 accounting) + 1 healthz
> acceptance test (AC5), all fault-injected + reversion-proved. Not built: the
> per-writer degraded fault-injection *through* a real unwritable file at Init
> (the fan-out is tested directly instead) and a health/status field beyond the
> healthz `logging` key.

Production constructs an `io.MultiWriter` in
[`internal/logging/service.go:110`](../../internal/logging/service.go), with the rolling
file writer appended before the optional console writer in
[`internal/logging/internal.go:58`](../../internal/logging/internal.go). File logging is
the default in [`internal/config/config.go:622`](../../internal/config/config.go), and
the “both disabled” case also forces it back on.

The file is probed for writability during initialization, but there is no runtime
writer-health state. A later disk-full, rotation, permission or I/O failure is not
reflected in service health or a counter. Zerolog falls back to a generic stderr line;
it does not preserve the failed structured event. If console logging is enabled,
`io.MultiWriter` stops at the first failed writer, so a file failure also prevents that
event reaching the later console writer.

**Confusable states:** logging healthy vs structured records being lost after startup.

**Required record/behaviour:**

- Isolate writers so failure of one target does not prevent delivery to another.
- Maintain a writer-failure count, last failure and current degraded state.
- Report the degraded/recovered transition through a non-recursive stderr/journald
  fallback with rate limiting.
- Surface logging degradation through health/status.
- Fault-inject an erroring writer and a recovery in tests.

### L2. Evidence retention and capacity decisions silently swallow SQL/filesystem failures

> ✅ **FIXED — Stage 1 (`468a9ad1` + P2 `71088525`) + Stage 2 (this change).**
> The three measurement probes (`physicalUsage`, `freelistBytes`, `metadataBytes`)
> and the whole compaction path (`compactKind`/`compactOnce`/`maybeCompact` + both
> WAL-checkpoint paths) now return errors tagged with a granular, stable operation
> name (`stat_db`/`stat_wal`/`pragma_freelist_count`/`metadata_loss`/
> `checkpoint_drop`/`compaction_commit`, …). The write gate (`processSlot` +
> `tryFreeSpace` + `purgeChunk`) FAILS CLOSED (Q1): a measurement error drops the
> slot ONCE as `measurement_error` (never cap/writer_error), keeps decoding, and
> drives the `retentionHealth` tracker (wired at Start) — edge + 5-min
> write-driven heartbeat (Q2), recovery after the first complete measurement. A
> missing optional `-wal`/`-shm` stays 0; every other stat failure fails closed.
> `Status.usage_bytes`/`retention.metadata_bytes` are `null` when unmeasurable
> (never a false zero) and do NOT drive the tracker (a poll is not a write attempt).
> A post-decision checkpoint failure is reported WITHOUT reclassifying an
> already-justified cap drop (single-count). Peripheral gates (profile activation,
> migration) refuse fail-closed. Tests: `retention_measure_test.go` (fail-closed +
> null status + bounded + recovery; WAL/SHM handling; granular-op propagation;
> single-count), reversion-proved; `api-endpoints.md` updated for the nullable fields.


The evidence subsystem can silently convert measurement failures into valid-looking
zeros:

- freelist queries return zero on error at
  [`internal/evidence/retention.go:124`](../../internal/evidence/retention.go);
- metadata queries discard their errors at
  [`internal/evidence/retention.go:138`](../../internal/evidence/retention.go);
- filesystem `Stat` errors are ignored at
  [`internal/evidence/service.go:862`](../../internal/evidence/service.go).

Compaction is even quieter. Query, scan, marshal, begin, insert and delete failures all
return without a record, and the final commit error is discarded at
[`internal/evidence/retention.go:459`](../../internal/evidence/retention.go) and
[`internal/evidence/retention.go:544`](../../internal/evidence/retention.go).

These failures feed the decision to purge, compact or enter drop-new. The eventual log
can therefore blame capacity or metadata pressure while the actual cause was a database
or filesystem failure.

**Confusable states:** genuine capacity exhaustion vs inability to measure or compact
the archive.

**Required record/behaviour:** return measurement/compaction errors; represent usage as
unknown rather than zero; log one evidence-retention degraded transition with operation,
database path and cause, then a recovery transition. Do not log every failed retry.

### L3. Audio and evidence queue loss is silent or incorrectly classified

> ✅ **FIXED (working tree, awaiting commit).** A shared bounded tracker
> `logging.EpisodeLoss` warns at count-based exponential episode totals (1, 10,
> 100, … — operator decision 2026-08-14) each carrying `reason` + queue
> depth/capacity, and emits one Info recovery summary (total lost + episode
> duration). Evidence: the queue-full drop in `CaptureSlot` is now recorded as
> `evidence_queue_full` (never `writer_error`, since no write was attempted) and
> the tracker recovers on the next successful persist. Audio: a `lossMonitor`
> polls the callback's atomic drop counter OFF the real-time path (the callback
> is unchanged — still atomic-increment only), reports new drops as
> `audio_queue_full`, and declares recovery after 5 s with no new drops
> (operator decision 2026-08-14); the monitor baselines at the current count each
> Start so a restart never replays old drops. All three reasons are now distinct
> and every producer path stays non-blocking. Tests: `episodeloss_test.go`,
> `internal/evidence/queueloss_test.go`,
> `internal/audio/capture/lossmonitor_test.go` (all three rules reversion-proved).

The audio callback drops chunks when its channel is full and only increments an atomic
counter at [`internal/audio/capture/capture.go:380`](../../internal/audio/capture/capture.go).
Production code never reads `DroppedChunks`; only probe commands do.

The evidence handoff also drops a slot when its writer queue is full at
[`internal/evidence/service.go:357`](../../internal/evidence/service.go), but records it
using `lossReasonWriter` (`writer_error`) even though no database write was attempted.
Actual slot-write errors use the same reason later at
[`internal/evidence/service.go:720`](../../internal/evidence/service.go).

**Confusable states:** audio-device silence, audio callback backpressure, evidence queue
backpressure, and an evidence database write failure.

**Required record/behaviour:** distinguish at least `audio_queue_full`,
`evidence_queue_full` and `writer_error`; warn on the first loss and exponentially
spaced totals with queue depth/capacity; emit a recovery summary with the total lost and
duration. Keep all producer paths non-blocking.

---

## P1 — fix before a serious release

### L4. Rig mutations do not have an authoritative audit trail

> ✅ **FIXED (working tree, awaiting operator's push).** Two parts.
> **Part A — command outcome** (`8c3b69e6` + review-fixes `af9e78df`, entropy fix
> rode into `86b05ab5`): `SendCommands` returns a generated op-id and logs one
> structured outcome at the bridge boundary (both protocols) — op-id, protocol, op
> names/values, batch size, applied count, and on a mid-batch CI-V failure the failed
> index/op, so a fully-applied batch is distinguishable from a partial one and both
> from a pre-write rejection (the returned error). Rapid `set_freq`/`set_freq_b` steps
> coalesce into one Info summary (1 s quiet window, a different op or a failure flushes
> immediately) carrying every request's op-id, capped at 256 ids per chunk. The op-id
> is a 96-bit per-boot prefix + counter (unique across restarts) and is echoed onto the
> `POST /v1/rig/command` access-log line.
> **Part B — tune records** (`004acacf`): a durable start record (reason/power/mode/
> auto-off) and one uniform stop record shared by every teardown path (operator/
> auto-off/disconnect) with reason, power, mode and actual duration. Logging only — no
> control-flow change on the TX path. All reviews clean.

A CI-V batch is non-atomic: earlier operations can be ACKed and applied before a later
operation fails. `sendCommandsCIV` returns at the first failure without logging how
many commands applied, the failed batch index, or the operation at the durable bridge
boundary ([`internal/bridge/command.go:202`](../../internal/bridge/command.go)).

Successful generic rig commands are represented only by the HTTP access record
`POST /v1/rig/command 202`; the handler emits no command outcome containing the operation
or value ([`internal/api/handler_rig_command.go:70`](../../internal/api/handler_rig_command.go)).

Normal tune start and stop publish SSE state but leave no durable start/stop record
([`internal/bridge/tune.go:225`](../../internal/bridge/tune.go),
[`internal/bridge/tune.go:366`](../../internal/bridge/tune.go)). Auto-off and disconnect
paths do log, making the normal operator path the least visible one.

**Confusable states:** fully applied batch vs partially applied batch; successful tune
carrier vs no tune; operator stop vs other teardown.

**Required record/behaviour:** one structured command outcome with a generated operation
ID, protocol, command names, batch size, applied count, failed index/op and safe values.
Tune start/stop should carry reason, power, mode and duration. Avoid logging every
high-frequency VFO step at Info; coalesce or retain detail at Debug where necessary,
while keeping the final operation outcome default-visible.

### L5. Malformed CAT telemetry can leave safety-relevant state stale silently

> ✅ **FIXED (working tree, awaiting commit).** Two parts, operator rulings
> 2026-08-15 (OPEN-1 b / OPEN-2 32 / OPEN-3 3).
> **Part A — malformed telemetry** (`mapStatusToPayload` + `telemetryguard.go`): a
> `VFOAFREQ`/`VFOBFREQ`/`TXPWR` value that won't parse now returns a parse diagnostic
> instead of dropping silently. The readLoop guard warns once per episode per tag (raw
> bounded to 32 bytes), invalidates the affected rolling snapshot so `CurrentDialMHz`/
> `CurrentPowerW` report *unknown* rather than a retained stale value — the "unknown
> beats wrong" doctrine `CurrentDialMHz` already documents — and logs one recovery when
> a valid value resumes. Power invalidation respects the tune-restore freeze
> (`!tuneActive`), so a malformed `TXPWR` during a tune cannot corrupt the pre-tune
> snapshot `StartTune` restores to; no TX control-flow or timing changed.
> **Part B — stranded identity re-probe** (`readLoop`): consecutive identity re-probe
> WRITE failures (rig alive but unwritable → every operator write blocked) now promote
> from Debug to a default-level Warn at exactly 3 consecutive, once per episode, reset by
> any landed write. Full-TDD (RED skeleton → GREEN, reversion proofs kept incl. the
> tune-freeze guard); `-race`, `go vet`, `gofmt` clean.

`mapStatusToPayload` discards invalid VFO A, VFO B and TX-power values without a result
or log at [`internal/bridge/pipeline.go:1129`](../../internal/bridge/pipeline.go). The
prior frequency/power can remain apparently current. This matters because dial state is
used in FT8 attribution and safety decisions.

Repeated identity re-probe write failures, which can leave all rig writes blocked, are
only Debug at [`internal/bridge/pipeline.go:792`](../../internal/bridge/pipeline.go).

**Confusable states:** a valid unchanged value vs a malformed new value; identity still
pending normally vs identity confirmation being stranded by repeated write failures.

**Required record/behaviour:** return parse diagnostics from `mapStatusToPayload`,
invalidate affected state rather than retain a stale value, and warn once per
connection/tag with bounded raw input. Emit recovery when valid data resumes. Promote a
sustained identity re-probe failure transition to the default level.

### L6. HTTP request correlation is incomplete, especially on SM Cloud

> ✅ **FIXED (working tree, awaiting commit).** Sliced A→B→C (operator, 2026-08-15).
> The cloud request-ID + access-log middleware was **already built** (C4,
> `internal/cloud/server/access.go`: bounded inbound `X-Request-Id` / crypto-random /
> echo / one access line with `request_id` + `tenant_id`) — left untouched.
> **A — cloud panic recovery** (`a11980ae` + review-fixes `14c8b809` abort-on-committed
> + `32cb234b` keep-access-line-on-unwind): a handler panic surfaces one structured
> `panic in HTTP handler` line (`request_id`/`tenant_id`/value/stack/`response_committed`)
> and a generic 500; a committed-response panic re-panics `ErrAbortHandler` so a
> truncated body can't read as a clean snapshot; distinct from a transport disconnect.
> **B — cloud failure-line correlation** (`320d2a14`): `tenant_id` + `request_id` added to
> the 8 authenticated application-failure lines that omitted them (health/unauth and the
> generic encode helper intentionally excluded). **C — daemon `request_id`** (`ca7b9e60`):
> `logRequests` assigns a bounded inbound / crypto-random id, echoes `X-Request-Id`, and
> logs it on the access line; it rides the `responseRecorder` so `writeServerError` (via
> the kit) and `recoverPanic` stamp the SAME id on the inner failure line — deterministic
> join, no ripple to the 34 call sites. Coexists with `op_id`. Full-TDD, `-race` clean.
> **Deferred (mechanical sweep, operator's call):** `request_id` on the ~25 direct
> handler breadcrumb logs (`s.logger.Error/Warn`) across 8 daemon handler files — mostly
> Warn-level, not on the 5xx/panic join paths L6 targets.

The cloud handler stack has no application access log, request ID or structured
application panic recovery
([`internal/cloud/server/server.go:73`](../../internal/cloud/server/server.go)). Direct
deployments therefore lack a complete application request trail, while proxy access
records cannot be joined deterministically to application errors.

Authenticated failure lines are also inconsistent about `tenant_id`: logbook lookup,
provisioning, upsert, list, manifest and pre-stream export paths omit it in several
places beginning at
[`internal/cloud/server/server.go:147`](../../internal/cloud/server/server.go) and
[`internal/cloud/server/server.go:280`](../../internal/cloud/server/server.go).

The daemon has a substantially better access logger, but it still has no request ID to
join the access record to an inner service failure
([`internal/api/middleware.go:324`](../../internal/api/middleware.go)).

**Confusable states:** concurrent requests producing similar errors; the same error for
different tenants; an application panic vs a transport-level disconnect.

**Required record/behaviour:** install outer request-ID/access/panic middleware on both
HTTP surfaces; accept a safe inbound request ID or generate one; echo it in the response;
propagate a request-scoped logger; include authenticated tenant context on every cloud
application outcome. Never log bearer tokens or authorization headers.

### L7. A post-commit forwarding-hook panic creates a false diagnostic narrative

> ✅ **FIXED (working tree, awaiting commit).** `persistOutcome`'s `OnQsoStamped`
> mirror-notify hook now runs behind its OWN recovery boundary (`Worker.notifyStamped`):
> a hook panic is contained there and logged as the post-commit event it is —
> `phase=post_commit`, `hook=on_qso_stamped`, `upload_committed=true`, `upload_id`,
> `qso_id`, panic + stack — instead of escaping to `processRowSafely`, which would log
> the pre-submit "panic processing row; resetting to retry" narrative AND re-arm the
> already-committed upload through the transient path (a duplicate forward). The
> committed upload is left untouched (not retried). Full-TDD (RED showed the escaped
> panic mislabelled + the reset; reversion-proved), `-race` clean.

After the forwarding attempt and successful queue-row/stamp commit have been logged,
`persistOutcome` invokes the fallible `OnQsoStamped` hook directly at
[`internal/forwarding/worker/worker.go:456`](../../internal/forwarding/worker/worker.go).
If it panics, the outer per-row recovery logs “panic processing row; resetting to retry”
at [`internal/forwarding/worker/worker.go:188`](../../internal/forwarding/worker/worker.go).
At that point the upload has already committed, and the attempted reset can be a no-op.

**Confusable states:** pre-submit row-processing failure vs a post-commit mirror-notify
failure; upload not committed vs upload safely committed.

**Required record/behaviour:** put the hook behind a separate recovery boundary and log
`phase=post_commit`, `hook=on_qso_stamped`, `upload_committed=true`, `upload_id`,
`qso_id`, panic and stack. Do not retry the already-completed upstream upload because
the hook failed.

### L8. Local evidence quarantine is invisible when it happens

> ✅ **FIXED (working tree, awaiting commit).** The core was **already built** (C1b,
> `TestQuarantine_IsLoggedForTheOperator`): `applyOutcomes` emits one bounded per-batch
> Warn when `quarantined > 0`, carrying the total count + a representative
> `sample_uuid`/`sample_reason` (reason bounded to 256 runes) — resolving the
> invisibility / batch-accepted-vs-batch-quarantined confusable state. **L8 delta added
> here:** a per-kind `by_kind` breakdown on that same Warn (`{observation: N, coverage:
> M, …}`, iterated in the fixed `syncTables` order), so an operator sees WHICH evidence
> kinds SM Cloud refused. The bounded representative reason is kept as-is: grouping by
> truncated raw reason would bound length, not cardinality, and imply a taxonomy that
> does not exist (operator, 2026-08-15) — if stable reason categories are later defined,
> category counts are a separate change. Full-TDD (RED had no `by_kind`; reversion-proved
> against a per-kind DB ground truth that catches a total mislabelled under one kind).
> Still one line per batch, no per-row flood, no unrestricted remote text.

The cloud server now logs per-batch outcome counts, but the daemon silently persists a
`permanent_reject` quarantine reason at
[`internal/evidence/sync.go:607`](../../internal/evidence/sync.go). The normal sync path
then records success/recovery, so an operator without cloud-log access cannot see that
some records were rejected.

**Confusable states:** batch fully accepted vs batch completed with locally quarantined
records.

**Required record/behaviour:** one local Warn per affected batch containing total
quarantined, counts by evidence kind and bounded reason category. Do not emit one line
per record or log unrestricted remote text.

### L9. Critical long-running goroutines bypass structured panic logging

> ✅ **FIXED (working tree, awaiting commit).** All four named service-lifetime
> goroutines now run under `internal/safego` with an explicit per-worker respawn
> decision (operator rulings 2026-08-15); each records goroutine/panic/stack via an
> `onPanic` handler. **evidence** (`1f9c53bf` + 3 review-fixes) — writer/queue-loss/sync
> under `safego.GoTracked` **respawn=true**; shutdown moved from three done-channels to
> one WaitGroup; a write-path panic is counted as a distinct `writer_panic` loss
> (panic-safe `processSlot` lock so a panic-under-mu can't wedge; accounting disarmed
> once the slot commits/classifies). **bridge** (`dfc08999`) — `runSupervisor` (owns rig
> control) under `safego.GoTracked` **respawn=true**; GoTracked owns the wg.Add/Done.
> **serial** (`a20a5583`) — `readerLoop` under `safego.Go` **respawn=false (log-and-die)**
> via an INJECTED `PanicHandler` (the package has no logger; the bridge injects
> `s.onPanic`), because the supervisor's liveness already reopens a dead port and
> respawning would race it. **ft8** (`2d6ab84f`) — the CGO capture `pump` under
> `safego.Go` **respawn=false (log-and-die)**: it closes `m.out` on exit (respawn would
> send on a closed channel) and the scheduler's dead-source restart / view re-acquire
> owns recovery. Added `safego.SetRespawnCooldownForTest`. Full-TDD each (RED skeleton /
> reversion proofs, incl. bare-`go`→crash for the log-and-die pair); `-race` clean; every
> codex review cleared. That closes the logging-gaps P0/P1 tier (L1–L9).

Several service-lifetime goroutines use bare `go`, including:

- evidence writer and sync loops at
  [`internal/evidence/service.go:298`](../../internal/evidence/service.go);
- bridge supervisor at
  [`internal/bridge/service.go:751`](../../internal/bridge/service.go);
- the FT8 capture pump at
  [`internal/ft8/source_cgo.go:70`](../../internal/ft8/source_cgo.go);
- the serial reader at
  [`internal/serial/serial.go:213`](../../internal/serial/serial.go).

A panic bypasses the structured application logger and normally terminates the process,
leaving only the runtime stderr stack. The repository already has `internal/safego` and
uses it for FT8, lookup and forwarding workers.

**Confusable states:** unexplained process exit vs a named subsystem panic; a live
service vs a recovered panic that left its worker dead.

**Required record/behaviour:** use `safego` or an equivalent named wrapper to record
worker, panic and stack. Decide explicitly per worker whether to respawn or re-panic;
do not silently recover a worker that must remain live.

---

## P2 — important operability work

### L10. Health-check logging is wrong in opposite directions

> ✅ **FIXED (working tree, awaiting commit).** A small concurrency-safe
> transition-only `dbHealthLog` in each package (operator ruling 2026-08-15: one Warn on
> the unhealthy edge with cause, repeated failures silenced at the default level via
> Debug, one recovery Info with the elapsed unhealthy duration). **Daemon**
> (`internal/api`): `/v1/healthz` no longer discards the ping cause — it logs it on the
> unhealthy transition. **Cloud** (`internal/cloud/server`): the per-probe `health: db
> ping failed` Warn flood collapses to the edge Warn + recovery Info. Two independent
> commits (each reviewable/revertible); full-TDD (transition + concurrent-probe `-race`
> tests, reversion-proved).

The daemon discards the SQLite `Ping` cause and returns only a generic 503 at
[`internal/api/handler_health.go:9`](../../internal/api/handler_health.go). SM Cloud
logs the complete cause at Warn on every failed probe at
[`internal/cloud/server/server.go:168`](../../internal/cloud/server/server.go), which can
flood logs under frequent monitoring.

**Required record/behaviour:** log unhealthy and recovered transitions with elapsed
duration and cause; send repeated failures to Debug or exponentially sample them.

### L11. Forwarding logs lack queue context and can bury signal during outages

Normal attempt records omit `upload_id`, queue age and an unconditional attempt number
at [`internal/forwarding/worker/worker.go:513`](../../internal/forwarding/worker/worker.go).
There is no periodic destination queue depth or oldest-row age. `OutcomeUnreachable`
retries indefinitely, producing individual Info records, and cancellation during
shutdown can be classified as an upstream failure.

**Required record/behaviour:** add `upload_id`, `attempt`, `queue_age` and `queued_at` to
attempt records; publish destination-down/recovered transitions; periodically summarize
pending depth, oldest age and exhausted/failed totals; suppress expected shutdown
cancellation.

> ✅ **FIXED (working tree, awaiting operator's push).** Store query `0c73d92c` +
> worker discipline `b198f81f`, operator rulings 2026-08-15. Four behaviours:
> **(1)** every `forwarding: attempt` record carries `upload_id`, an UNCONDITIONAL
> `attempt` (= `row.Attempts+1`), `queued_at` and a non-negative `queue_age_seconds`
> (the singular supersedes the old conditional `attempts`). **(2)** reachability is
> logged as transitions — one Warn on the first `OutcomeUnreachable`, one Info (with
> `unreachable_seconds`) on the first non-unreachable outcome incl. Terminal; in-outage
> per-attempt records drop to Debug so an indefinite outage stops flooding.
> **(3)** a fixed-60s `forwarding: queue summary` reports pending depth, `oldest_age_seconds`
> and the durable failed DB count; emitted while backed up / on a failed-total change /
> once on drain-to-empty, silent when idle, and run as a PEER goroutine so a slow Submit
> can't starve it. **(4)** a shutdown-attributable cancel (`ctx.Err()!=nil` AND cause is
> `context.Canceled`) is fully suppressed and leaves the row for the orphan reset, while a
> coincident real failure or a racing success still logs + updates reachability. Review
> hardening rode in: the depth query is a single atomic snapshot (epoch-based oldest),
> `created_at` is reset on re-arm so a re-enqueued row is not a months-old backlog, and the
> summary's zero-timestamp age is guarded. The unbounded-scan on the depth query is
> accepted with rationale in code (a partial index is deferred — it is a migration that
> bumps three version-pinned tests, premature for a 60s diagnostic).

### L12. SM Cloud flattens diagnostically different successful outcomes

An acknowledgement with `received=1, applied=0` means the cloud already held a newer
copy, but it is returned as ordinary success at
[`internal/forwarding/smcloud/smcloud.go:309`](../../internal/forwarding/smcloud/smcloud.go).
That is a valid backup outcome, but an unexpected single-writer observation.

Reconcile summaries carry only enqueued counts and a `truncated` flag, losing total
discovered, skipped and remaining work
([`internal/forwarding/smcloud/reconcile.go:146`](../../internal/forwarding/smcloud/reconcile.go)).

**Required record/behaviour:** carry a disposition such as `cloud_newer_noop`; summarize
discovered, enqueued, skipped, deferred/remaining and the truncation limit. Keep the
overall result successful while making the unusual disposition visible.

> ✅ **FIXED (working tree, awaiting operator's push).** Submit detail `0fb78caa` +
> reconcile summary `6e4d43ee`, operator rulings 2026-08-16. **Part 1:** `forwarding.Result`
> gains an optional `Detail`; SM Cloud sets `stored` (applied=1) / `cloud_newer_noop`
> (applied=0), BOTH staying `OutcomeSuccess` + uploaded, and the worker logs it as
> `outcome_detail` on the attempt record (Info; omitted when unset, so QRZ/ClubLog are
> untouched). The distinct field name avoids the worker's existing local `disposition`.
> Review hardening rode in: `applied` is now a `*int`, so an ack that OMITS the field (or
> sends null) is TRANSIENT — never a false `cloud_newer_noop`. **Part 2:** the
> `run complete` record now logs `discovered` (actionable upserts+deletes before truncation),
> `enqueued` and `skipped` (attempted−enqueued, idempotently absorbed) always, plus `deferred`
> (discovered−attempted) and the per-list `limit` only when truncated — identity
> `discovered = enqueued + skipped + deferred`. Per-list caps kept (logging-only), factored
> into `truncateBatch`; `Discovered`/`Attempted` are `json:"-"` so the reconcile endpoint's
> wire shape is unchanged.

### L13. Oversized serial frames disappear silently

The serial reader discards a frame after it exceeds `maxLineSize`, then resumes at the
next delimiter, without a counter or diagnostic callback
([`internal/serial/serial.go:437`](../../internal/serial/serial.go)). This commonly
indicates line noise, a wrong delimiter, wrong baud or an incorrect driver.

**Required record/behaviour:** expose a dropped-oversized-frame counter/callback to the
bridge and emit a rate-limited warning carrying port/driver, byte threshold and total.
Do not log the raw frame by default.

> ✅ **FIXED (working tree, awaiting operator's push).** Serial `21e0de82` + bridge
> `f2b3b908`, operator rulings 2026-08-16. The reader now counts each dropped oversized
> frame EXACTLY ONCE and notifies an injected `serial.Config.OnOversizeFrame` callback
> carrying only `threshold_bytes` + a per-session `dropped_total` — never the raw bytes.
> Rate-limited INSIDE the reader: warn immediately on the first drop, then at most once per
> 60s while drops continue (`oversizeThrottle`); the total resets per reader session. The
> bridge wires the callback (`pipeline.go`, beside `PanicHandler`) to a Warn adding `port` +
> `driver`. Ruling 5 folded in: oversized handling is now UNIFORM — a frame over 4096 is
> dropped + counted whether its delimiter spans reads or lands in-chunk (the in-chunk case
> previously EMITTED a garbage 4 KB response); exactly 4096 stays valid. Review hardening: the
> callback's doc states it runs on the reader goroutine and must not block or re-enter the
> Port (a reentrant `Close()` would deadlock).

---

## P3 — useful hardening

### H1. Restore encode failures are silently skipped

Tune power/mode restore encoding appends only successful commands and discards failures
at [`internal/bridge/tune.go:624`](../../internal/bridge/tune.go). FT8 mode restore has
the same `err == nil` pattern at
[`internal/bridge/ft8tx.go:352`](../../internal/bridge/ft8tx.go). Write-failure logging
cannot fire when encoding failed first.

Return the failed restore components and log one Warn after PTT/carrier is confirmed
down, including restore type and cause.

> ✅ **FIXED (working tree, awaiting operator's push).** `61bb5870`, operator rulings
> 2026-08-16. `encodeTuneRestore` returns per-component encode errors while still encoding
> whatever succeeded; the tune caller logs ONE aggregated Warn after the carrier is down —
> `phase=encode`, with `restore_power_error` and/or `restore_mode_error` (the successfully-
> encoded component's field is omitted) — and invalidates the tune snapshot (it now lies),
> still writing the components that encoded. The FT8 mode restore logs one Warn (`phase=encode`)
> on an encode failure after PTT-down (log-only). Existing write-failure Warns gained
> `phase=write` for the distinction. All logging is strictly downstream of confirmed
> unkey/PTT-down; keying, power and stop sequencing are unchanged.

### H2. Evidence shutdown lacks a completion record

`Stop` drains the writer and sync loop, then discards the archive close error and emits
no stopped/drained summary
([`internal/evidence/service.go:314`](../../internal/evidence/service.go)). Record the
final dropped count, pending count, sync/quarantine state, close error and duration.

> ✅ **FIXED (working tree, awaiting operator's push).** `9015b33c`, operator rulings
> 2026-08-16. `Stop` now emits exactly ONE shutdown-completion record — after the workers/sync
> have drained AND the archive close has been attempted — carrying `dropped_total`, `pending`,
> `sync_state` and `run_duration_seconds`, at **Info** for a clean stop and **Error** (with
> `close_error`) when the archive close fails. The former separate close-error line is folded
> in. Ruling: `sync_state` only, no shutdown-time quarantine query. A `startedAt` field set in
> `Start` backs the duration. Tested at two levels (`logStopSummary` unit with `closeErr`
> injected + a real `Start→Stop` integration).

### H3. Long-lived SSE requests are invisible at Info until disconnect

The daemon access logger deliberately records an SSE request only when it ends
([`internal/api/middleware.go:320`](../../internal/api/middleware.go)). That leaves no
default-visible proof that a currently connected client exists. Emit subscriber-count
transition records from the shared SSE admission layer, not a full access record at
both connect and disconnect for every stream.

### H4. Email success logs retain full PII and operator-supplied subject text

The mailer writes the complete recipient and subject at Info
([`internal/email/email.go:237`](../../internal/email/email.go)); the API error path also
logs the recipient. This is over-logging rather than a missing log, but it requires an
explicit retention decision before release.

Prefer destination domain or a stable redacted/hash representation, and a fixed message
kind instead of the raw subject. If full addresses are operationally required, document
the reason and align log retention/access accordingly.

---

## Recommended release-gate order

1. **L1** — prove the diagnostic channel itself remains observable when its file sink
   fails.
2. **L2 + L3** — make evidence measurement, retention and every loss class honest.
3. **L4 + L5** — establish an authoritative audit trail for hardware mutations and
   malformed safety-relevant telemetry.
4. **L6 + L9** — add request correlation and structured panic capture at process
   boundaries.
5. **L7 + L8** — make post-commit forwarding and evidence quarantine outcomes
   truthful.
6. **L10–L13**, then P3 hardening.

P0 and P1 should be treated as the logging release gate. P2 should be completed before
claiming the service is operationally mature; P3 can follow without compromising the
core incident trail.

## Test standard for closing a finding

A test that asserts only “a log line exists” is insufficient. Each fix should exercise
the two nearest confusable states and assert that their structured records differ in the
field that drives diagnosis. At minimum:

- healthy writer vs runtime writer failure vs recovery;
- full evidence queue vs database write failure;
- fully applied CI-V batch vs partially applied batch;
- valid CAT value vs malformed CAT value followed by recovery;
- cloud applied vs stale-guard no-op;
- pre-commit row panic vs post-commit hook panic;
- fully accepted evidence batch vs batch containing quarantine;
- health failure transition vs repeated failed probes vs recovery.

Tests should also assert bounded volume for every retrying or high-frequency path.

## Verification

`/usr/lib/golang/bin/go test ./internal/...` passed against the reviewed working tree on
2026-08-12. The source review made no production changes.

