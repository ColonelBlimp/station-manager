# W-0028 — FT8/FT4 logging independent of enrichment

**Status:** Selected 2026-11-12 as a P1 defect (operator: "The FT8/4 logging is, in my mind a
defect, and needs to be fix as a P1"); ranked in [`docs/backlog.md`](../backlog.md). Not started;
the design questions are answered (codex-092). The build starts on the operator's direction.
**Outcome:** a completed FT8/FT4 exchange is always stored at once, whatever the lookup
services do. Enrichment that arrives during the exchange is used; enrichment that has not
arrived is left for a later edit.

`W-0028` is an immutable identity. Its status may change, while priority and ranked position live
only in [`docs/backlog.md`](../backlog.md).

## The defect (source reading, 2026-11-12; not yet reproduced)

The completed-exchange job (`launchFt8QsoLog`, `cmd/smd/main.go:346-362`) runs under **one**
30-second context (`ft8QsoLogTimeout`, `main.go:81`). It looks the call up
(`d.enrich.Enrich(ctx, q.Call)`, `cmd/smd/lifecycle_adapters.go:756`) and then writes the QSO
(`d.qso.Submit(ctx, …)`, `:778`) under that same context. On a cache miss the station lookup tries
the providers one after another (`runChain`, `internal/lookup/orchestrator.go:602`), each request
bounded by the provider's `timeout_sec` (default 10 s, `internal/config/defaults.go:86`). If the
lookup uses the whole 30 s, the write starts on an expired context:
- `ensureCtxTimeout` passes an existing deadline through unchanged
  (`internal/database/sqlite/internal.go:129-136`);
- `database/sql` refuses an expired context before it takes a connection (`DB.conn`);
- so `submit` fails at its first read (`internal/qsoservice/submit.go:359`), before any insert.

The contact is not stored, and smd.log reads "ft8: failed to log completed QSO". This breaks
the invariant "Enrichment never blocks logging" (AGENTS.md). It was confirmed at source level by
Codex (codex-090). Phone/CW is not affected: its lookup is a separate request made by the page
before the submit.

## Operator ruling (2026-11-12)

"To me, FT8/4 should go through the ladder and not be dependant on enrichment. If enrichment
occurs with the boundaries of the ladder - great, otherwise log and move on. The QSO can always
be edited later and enriched."

Consequences:
- **Lookup:** starts when the partner's call is known at the start of the ladder.
- **Completion:** never waits for a lookup.
- **The write:** has its own context, created when the write starts.
- **Tracking:** the earned job stays tracked until the write returns, and is never cancelled by
  FT8 stop, app exit or the archive-switch drain (ADR 0100 §4).
- **No new machinery:** later enrichment is an ordinary edit; no automatic background update of
  stored rows is added (codex-091).

## Acceptance criteria

1. With a lookup held behind a barrier, a completed FT8/FT4 exchange is stored with its known
   fields (the QSO row and its applicable upload-queue rows committed) **before the barrier is
   released or the lookup times out**. A fix that waits for the lookup's timeout must fail this.
2. With a lookup that finishes during the ladder, the stored QSO carries its details. A result
   from an abandoned or earlier exchange, or for another partner, is never used.
3. A lookup error never prevents or delays storage.
4. An earned job survives decode-loop cancellation and is drained before shutdown.

**Nearest confusable outcome:** a shorter lookup budget that still waits before the write. That
stops the deadline from expiring, but it still delays logging, so the ruling rejects it.

## Test plan (RED first; codex-090)

- Use the real completed-QSO sink with in-memory SQLite.
- Use a provider held behind a barrier. Assert the contact and its upload-queue rows are stored
  **while the lookup is still blocked** (codex-092), then release it and confirm it is cleaned up.
  Never wait 30 s.
- Reversion proof: put back the shared context and show the non-waiting assertion fails for that
  reason.
- Cover a ready result being included, and a stale or abandoned exchange's result being excluded.
- Also cover enrichment success, enrichment error, and decode-loop cancellation.

## Design (codex-092, verified 2026-11-12)

1. **The result is handed off; there is no cache read at completion.**
   - `internal/lookup` has no cache-only read. `Enrich` and `EnrichRefresh` both enter the
     pipeline, which can call providers (`internal/lookup/orchestrator.go:246-263`).
   - Even a local cache read would add a wait to completion.
   - Instead, `cmd/smd` owns one result per exchange. It is started when the partner is known.
     Completion takes it only if it is already ready, and otherwise logs the exchange's own
     fields.
   - The result is tied to the exchange's identity and callsign. It covers abandon, retry, a new
     partner and late completion. The on-air grid and identity win over the lookup.
   - A late result may warm the lookup cache but never changes an already logged row.
2. **The trigger is an injected partner-known notification**, on the existing
   `SetQsoLogger`/`SetDecodeSink` pattern (`internal/ft8/servicetx.go:1152-1170`). It is not the
   hub's status stream, which is a UI path and may drop messages. The notification only
   schedules work: no provider call and no wait under the sequencing or TX locks. It covers
   answering a CQ, selected callers and automatic next partners, for both FT8 and FT4.
   - Lookup work stays out of the earned-write drain (ADR 0100 §4), so a stalled lookup cannot
     hold an archive switch.
   - Lookup workers still have bounded cancellation and lifecycle cleanup.
3. **Later edits do not reach every destination.**
   - An edit queues an upload only for enabled routes whose action filter includes update
     (`internal/qsoservice/update.go:292-305`).
   - QRZ supports update, and SM Cloud declares it.
   - ClubLog declares only insert and delete, and refuses update
     (`internal/forwarding/clublog/clublog.go:191-192`, `:372-380`).
   - So a later enrichment edit is not promised to correct every earlier upload. No new
     delete-and-reinsert behaviour is in scope.

## Related

- [ADR 0100](../decisions/0100-daemon-owned-operating-sessions-with-explicit-membership.md)
  §4 step 3: the archive-switch FT8 completion drain. Once this is fixed, the drain waits only for
  the write.
- The inbox note of 2026-11-12 in [`dogfood-inbox.md`](../dogfood-inbox.md), selected here.
