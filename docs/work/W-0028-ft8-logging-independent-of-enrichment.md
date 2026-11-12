# W-0028 — FT8/FT4 logging independent of enrichment

**Status:** Selected 2026-11-12 as a P1 defect (operator: "The FT8/4 logging is, in my mind a
defect, and needs to be fix as a P1"); ranked in [`docs/backlog.md`](../backlog.md). Built in two
parts (claude-184). **Part 1, the write's own context, is committed:** a lookup that uses up the
job's time limit no longer costs the contact (9b3c4de7). **Part 2, the non-waiting handoff, is
built and its code review is complete (codex-098):** a lookup starts when a contact learns its partner, and that contact's completion uses
the result only if it has already answered. The first version keyed the handoff by callsign;
codex-095 found that a later contact with the same station reused the old result, so it is now
keyed by a per-contact identity (claude-188, codex-096). Acceptance item 5, the operator's
station test run, follows the deploy.
**Outcome:** a completed FT8/FT4 exchange is always stored at once, whatever the lookup
services do. Enrichment that arrives during the exchange is used; enrichment that has not
arrived is left for a later edit.

`W-0028` is an immutable identity. Its status may change, while priority and ranked position live
only in [`docs/backlog.md`](../backlog.md).

## The defect (2026-11-12; reproduced by part 1's test)

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
5. **Station test run (operator, 2026-11-12):** after the fix is complete, the operator deploys
   `smd`, installs the desktop app and operates FT8/FT4, briefly unplugging the Internet. Each
   contact completed during the outage is logged at once, without lookup details.

**Nearest confusable outcome:** a shorter lookup budget that still waits before the write. That
stops the deadline from expiring, but it still delays logging, so the ruling rejects it.

## Test plan (RED first; codex-090)

- Use the real completed-QSO sink with in-memory SQLite.
- Use a provider held behind a barrier. Assert the contact and its upload-queue rows are stored
  **while the lookup is still blocked** (codex-092), then release it and confirm it is cleaned up.
  Never wait 30 s.
- **Two properties, two reversions (codex-093):**
  - *Logging does not wait for the lookup:* the reversion puts back the synchronous
    lookup-before-write. It must fail while the provider is held. Putting back only the shared
    context would not, once the lookup no longer runs inline.
  - *The write has its own context:* expire or cancel the lookup's context and prove the write
    still commits. The reversion is the write inheriting that context.
  - Each fails only against its own reversion.
- Cover a ready result being included, and a stale or abandoned exchange's result being excluded.
- Also cover enrichment success, enrichment error, and decode-loop cancellation.

## Design (codex-092, codex-095, codex-096)

1. **The result is handed off per contact; there is no cache read at completion.**
   - `internal/lookup` has no cache-only read. `Enrich` and `EnrichRefresh` both enter the
     pipeline, which can call providers (`internal/lookup/orchestrator.go:246-263`). Even a
     local cache read would add a wait to completion.
   - **Each contact has an identity.** The sequencer assigns it in `beginContactLocked`, the
     one helper every contact start calls (eight sites: answer, FD answer, Call-CQ pick,
     replacement answerer, FD work, work a caller including the automatic next partner, type-4
     answer and work). A monotonic counter, not `(call, StartedAt)`: timestamps repeat under a
     fake clock and are QSO data, not identity (codex-096). It rides every status frame and
     the completion (`CompletedQso.ContactID`), captured in the locked completion snapshot.
   - **`cmd/smd` owns one lookup per contact**, started through the ordinary pipeline when the
     contact is first reported. The rungs of a contact share it. There is no retry
     (codex-095 answer 2).
   - **Completion takes its own contact's result** if it has answered and the station matches,
     and ends the contact either way. A status published afterwards cannot restart the
     lookup, and a lookup returning afterwards cannot revive the contact.
   - **No fallback.** A zero or unknown identity, a mismatched station, an earlier contact's
     result for the same station: each yields no enrichment, and the contact is still logged.
   - **Abandon.** An abandoned contact is never taken. Nothing else can reach its entry, and it
     is forgotten once 16 newer contacts have begun. Its lookup, if running, finishes by itself
     and only warms the cache. The 16 bounds memory, not running lookups (codex-096 answer 3).
   - The on-air grid and identity win over the lookup. A late result never changes an already
     logged row.
2. **The trigger is an injected notification**, `SetPartnerSink(contact, call)`, on the existing
   `SetQsoLogger`/`SetDecodeSink` pattern. It is fed by the sequencer's own status publication,
   not the hub's stream, which is a UI path and may drop messages. The notification only
   schedules work: no provider call and no wait under the sequencing or TX locks.
   - Lookup work stays out of the earned-write drain (ADR 0100 §4), so a stalled lookup cannot
     hold an archive switch.
   - Lookup workers have bounded cancellation (`ft8LookupTimeout`, the limit the lookup always
     had) and lifecycle cleanup (the enrichment node's stop cancels and waits).
3. **Later edits do not reach every destination.**
   - An edit queues an upload only for enabled routes whose action filter includes update
     (`internal/qsoservice/update.go:292-305`).
   - QRZ supports update, and SM Cloud declares it.
   - ClubLog declares only insert and delete, and refuses update
     (`internal/forwarding/clublog/clublog.go:191-192`, `:372-380`).
   - So a later enrichment edit is not promised to correct every earlier upload. No new
     delete-and-reinsert behaviour is in scope.

## FT4 (checked 2026-11-12, operator: "Have you checked the FT4 side of things for the same issue?")

FT4 is not a separate path: it is a timing profile on the same `ft8.Service` and `Sequencer`
(ADR 0080). A completed FT4 exchange reaches the same `cmd/smd` sink, with `Mode` stamped from
the sequencer's profile, and the partner hook sits in the one status callback. So the defect
applied to FT4 exactly as to FT8, and both parts of the fix cover it.

- **Proved by test, not only by reading.**
  - `internal/ft8/partnersink_test.go` runs the answer-a-CQ ladder and the Call-CQ ladder on both
    lattices through the production callback: the contact and its partner are reported before
    the completed exchange reaches the logger, with the same identity.
  - `internal/ft8/contactidentity_test.go` checks the identity rules in the shared sequencer
    harness, so the existing tests of every mode, including the FT4 matrix, check them too.
  - `cmd/smd/ft8qsolog_enrich_test.go` runs every sink test for an FT8 and an FT4 contact, and
    asserts the stored ADIF pair. FT4 is stored as MODE `MFSK`, SUBMODE `FT4`, a different path
    through the submit's validation.
  - The FT4 cases fail under each reversion.
- **One real difference (inference from the slot lengths, not measured):** an FT4 slot is 7.5 s
  against FT8's 15 s, so the ladder is about half as long and a lookup has about half the time
  to answer before the last rung. A lookup that answers in a second or so fits either. A slow
  provider chain is more likely to miss an FT4 contact, which is then logged without details,
  as the ruling intends.

## Related

- [ADR 0100](../decisions/0100-daemon-owned-operating-sessions-with-explicit-membership.md)
  §4 step 3: the archive-switch FT8 completion drain. Once this is fixed, the drain waits only for
  the write.
- The inbox note of 2026-11-12 in [`dogfood-inbox.md`](../dogfood-inbox.md), selected here.
