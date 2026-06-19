# `cmd/smd` + imported internal packages — code review (2026-05-07)

## Scope

Read-only review of the daemon entry point and every internal package it
directly imports, focused on data races, threading/lifecycle, network
security, and code correctness. Covered:

- `cmd/smd/main.go`, `cmd/smd/doc.go`
- `internal/api/` (server, middleware, body, response, limits, spa,
  validation, all `handler_*.go`)
- `internal/qsoservice/` (service, submit, update, delete, dedupe,
  validation, forwarders)
- `internal/events/` (events, hub)
- `internal/safego/` (safego)
- `internal/forwarding/` (forwarding, registry, worker, qrz, stub,
  backoff)
- `internal/lookup/` (lookup, orchestrator, hamnut, qrz, refresher)
- `internal/iocdi/` (container, helpers, hooks, initializer, internal)
- `internal/config/` (config)
- The `internal/database/sqlite` surface invoked by the above (service
  lifecycle, `api_context.go` methods that main / handlers / worker /
  orchestrator call, `migrations.go`)

Generated `internal/database/sqlite/models/`, frontend, v1 archive,
unrelated `cmd/*` binaries, and test files (read for behaviour
context, not reviewed) were intentionally skipped.

`main_test.go`, the api `handler_*_test.go` files, and the worker /
forwarder / orchestrator test packages were read where they
illuminated intent — not as review targets.

Methodology: read every non-test file in scope; cross-reference the
project's invariants and ADRs (notably the one-fails-all-fail
invariant, "enrichment never blocks logging", ADR 0017) against the
code; pay particular attention to package-level state, goroutine
spawn sites, http.Server config, ctx propagation through external
HTTP calls, and DB transaction boundaries.

## Headline verdict

The daemon is in solid shape for its age. The forwarding subsystem
and the qsoservice tx boundaries hold up to scrutiny: the
one-fails-all-fail invariant is honoured (QSO+upload-queue+history
share a single tx, cache writes live outside it), the
"enrichment never blocks logging" rule is respected, the shutdown
order in `cmd/smd/main.go` correctly drains workers before closing
the DB handle, and the loadLimiter design is appropriate for the
single-operator threat model. The api layer's middleware chain is
careful (panic recovery, structured access log, per-endpoint rate
limits, body size caps, SSE timeout removal via `ResponseController`).

That said, there are findings that warrant attention. Headline
counts: **0 Critical**, **5 Major**, **8 Minor**, **6 Observations**.

The Major items cluster in three places: (a) a small but real
WaitGroup-misuse race in `lookup/refresher` between concurrent
Schedule and Stop, (b) the daemon imports `mattn/go-sqlite3` for its
typed-error path while the actual driver is `modernc.org/sqlite` —
the typed-error branch never matches and the code is silently
relying on a string-match fallback, (c) two unprotected goroutines
in the orchestrator's `Enrich` path that bypass `safego` panic
recovery, (d) `http.Server.ReadHeaderTimeout` is not configured (a
slow-headers DoS vector that the existing `ReadTimeout` does not
fully cover), and (e) `lookup/qrz.Service.Initialize`'s session-key
fetch is performed without any context, so a hung QRZ.com response
can stall daemon startup until the OS TCP timeout fires.

Nothing in this review exposes a corruption path or an invariant
violation. The fixes are all bounded and local.

---

## Critical findings

None.

---

## Major findings

### Data race / threading

#### M1. `refresher.Service.Schedule` can race `Stop` and panic the WaitGroup

**File:** `/home/mveary/Development/station-manager/internal/lookup/refresher/refresher.go:160-191`,
paired with `Stop` at lines 130-144.

`Schedule` checks `s.started.Load() && !s.stopped.Load()` and proceeds
to push a token into `s.sem`, then calls `safego.GoTracked(... &s.wg)`
which does `wg.Add(1)` synchronously. There is no synchronisation
between the started/stopped checks and the `wg.Add(1)` against
concurrent `Stop`. The race window:

1. Goroutine A enters `Schedule`, passes the started/stopped checks.
2. Goroutine B enters `Stop`, CASes `stopped` to true, calls `s.cancel()`,
   then `s.wg.Wait()`. If the wg counter is currently 0 (no in-flight
   refreshes between two scheduled bursts), `wg.Wait` returns
   immediately.
3. A continues, acquires a sem slot, calls `safego.GoTracked` →
   `wg.Add(1)`.

Per `sync.WaitGroup` semantics, "calls with a positive delta that
occur when the counter is zero must happen before a Wait" — A's `Add`
happens-after B's `Wait` only if A observed B's writes, but the
unsynchronised loads above don't establish that ordering. The Go
runtime can detect this and panic
("sync: WaitGroup misuse: Add called concurrently with Wait"); even
when it doesn't panic, the goroutine A spawns runs against an
already-cancelled context and is effectively a leak that Stop didn't
account for.

**Why it matters:** the orchestrator schedules refreshes from any
HTTP handler thread serving `/v1/enrich/callsign`. A daemon shutdown
that coincides with a Tab from the SPA can hit the panic path.

**Suggested fix:** hold a small mutex (or use a single `sync.Mutex`-
protected "running" state machine) across the start-stopped check and
the `wg.Add` so a Stop in flight can't sneak between them. An
alternative is to take the read side of an RWMutex during Schedule,
the write side during Stop, and gate the schedule-after-stop check
under it.

---

#### M2. Orchestrator `Enrich` spawns unprotected goroutines

**File:** `/home/mveary/Development/station-manager/internal/lookup/orchestrator.go:164-169`

```go
cCh := make(chan countryReadResult, 1)
sCh := make(chan stationReadResult, 1)
go func() { cCh <- o.readCountry(ctx, callsign) }()
go func() { sCh <- o.readStation(ctx, callsign) }()
```

These two goroutines are spawned without panic recovery. The rest of
the daemon's worker-style goroutines route through `safego.Go` /
`safego.GoTracked` precisely so a panic in a worker doesn't take the
process down. A panic in `readCountry` or `readStation` (e.g., a
nil-deref inside `adapters.CountryModelToType` from an unexpected
DB row, an upstream provider returning a malformed body that triggers
a slice index out of range, or a future `o.warn` call on a nil
logger) propagates straight to the runtime's default behaviour,
crashing the daemon. The api `recoverPanic` middleware does NOT cover
panics from a child goroutine — it only wraps `next.ServeHTTP` on the
request goroutine.

**Why it matters:** the enrichment code path was specifically called
out in the project invariants as "must not block / must not break
logging." A daemon-wide crash on an enrichment-side panic violates
that spirit at the process level.

**Suggested fix:** route these two goroutines through `safego.Go`
with `respawn=false` (one-shot, panic-recovered) and arrange for the
result channels to receive a zero-result-with-source=none in the
recovery path so `Enrich` doesn't deadlock waiting for a value that
never arrives.

---

### Network security

#### M3. `http.Server.ReadHeaderTimeout` is unset

**File:** `/home/mveary/Development/station-manager/internal/api/server.go:137-142`

```go
s.httpServer = &http.Server{
    Handler:      s.logRequests(s.limitConcurrent(s.recoverPanic(mux))),
    ReadTimeout:  time.Duration(cfg.Server.ReadTimeoutSec) * time.Second,
    WriteTimeout: time.Duration(cfg.Server.WriteTimeoutSec) * time.Second,
    IdleTimeout:  time.Duration(cfg.Server.IdleTimeoutSec) * time.Second,
}
```

`ReadHeaderTimeout` is not configured. `ReadTimeout` covers the whole
request including body, but a slow-headers attack (slowloris-style)
can hold the connection open until `ReadTimeout` fires, which is the
configurable per-request budget — by default 10s in the daemon's
defaults but operator-tunable to longer values. Each held
slow-headers connection consumes one `MaxConcurrentRequests` slot
once the handler runs, but the connection can sit pre-handler under
just `ReadTimeout`. Adding a short, fixed `ReadHeaderTimeout` (a few
seconds) bounds the pre-handler exposure independently of the
operator's larger `ReadTimeout`.

**Why it matters:** the daemon is local-only by default (loopback
TCP or Unix socket), so the practical exposure is small in the
single-operator deployment. But the api.md threat model in §6
explicitly addresses self-DoS from buggy local clients (cron loops,
reconnect storms); a slow-headers bug from a local SPA dev-proxy or
test harness is exactly that class.

**Suggested fix:** add `ReadHeaderTimeout` to the `http.Server`
struct; a 5-second value or pulling from a new
`cfg.Server.ReadHeaderTimeoutSec` config field both work.

---

#### M4. QRZ session-key fetch ignores context

**File:** `/home/mveary/Development/station-manager/internal/lookup/qrz/internal.go:39-49`

`requestAndSetSessionKey` builds the request with `http.NewRequest`
(no context) and runs `s.client.Do(req)`. The HTTP client carries
its own per-call timeout from `utils.NewHTTPClient(...)`, so the
call cannot hang indefinitely, but it cannot be cancelled by daemon
shutdown either. Initialize is called from `iocdi.Container.Build()`
during startup — if QRZ.com is briefly unreachable and the client
timeout is the operator's configured `HttpTimeoutSec` (default 10s),
the daemon takes that long to fail-closed before continuing. Fine in
isolation; the concern is that no context is threaded through, so a
future Initialize-during-running scenario (config reload, lookup
chain resync) cannot interrupt a stuck handshake.

**Why it matters:** QRZ.com going slow during a daemon restart is a
real failure mode the operator network-memory item flags. Context
propagation is the project's standard pattern; missing it here is an
inconsistency, not just style.

**Suggested fix:** use `http.NewRequestWithContext` with a
context derived from the Initialize call site (Initialize itself
could take a `ctx context.Context` parameter, defaulting to
`context.Background()` for the existing DI shape), and let the
caller decide how aggressively to cancel.

---

### Code correctness

#### M5. Dead `mattn/go-sqlite3` typed-error path; correctness relies on string match

**File:** `/home/mveary/Development/station-manager/internal/database/sqlite/internal.go:212-222`,
**File:** `/home/mveary/Development/station-manager/internal/qsoservice/submit.go:300-307`

Both files import `github.com/mattn/go-sqlite3` purely for the
`sqlite3.Error` type and use it as the primary detection in
`isUniqueConstraintError`:

```go
var sqliteErr sqlite3.Error
if stderr.As(err, &sqliteErr) {
    return sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique ||
        sqliteErr.Code == sqlite3.ErrConstraint
}
return strings.Contains(err.Error(), "UNIQUE constraint failed")
```

But the daemon's actual driver is `modernc.org/sqlite` — registered
under the name `"sqlite"` and configured at `internal/database/sqlite/consts.go:26`
(`SqliteDriver = "sqlite"`). Errors returned by modernc are NOT
`mattn/go-sqlite3.Error` values; the `errors.As` branch can never
match. Detection falls through to the substring fallback every
time, which the code's comment frames as belt-and-braces but is
actually load-bearing.

**Why it matters:** the substring path works today against modernc's
exact error formatting, but a driver upgrade that reformats the
message — say, ending the string in a period or capitalising
differently — would silently break `Submit`'s race-resolution path
and the duplicate-key handling in `Update`. A submit that should
return `200 duplicate` would instead surface as `500
submit_failed`. The H1 finding from the milestone-1b review is
predicated on this detection working.

`migrations.go:11` similarly imports
`github.com/golang-migrate/migrate/v4/database/sqlite3` (the mattn-
based migrate driver) and calls `sqlite3.WithInstance(handle, ...)` —
this is a separate concern; it works because migrate's sqlite3
adapter only uses the `*sql.DB` interface, but it's another sign
that the project is straddling two sqlite drivers without a
documented "why both."

**Suggested fix:** detect the modernc error type instead (modernc's
errors expose a `Code()` method or are typed `*sqlite.Error` from
`modernc.org/sqlite/lib`) and remove the dead `mattn/go-sqlite3`
import. If keeping the substring fallback is still desired as a
safety net, that's fine — but the typed-error path should actually
match the driver in use.

**Status:** primary fix landed when ADR 0018 was written; the
migrations dual-driver concern (paragraph above) was closed
2026-05-20 by swapping `migrations.go` to
`migrate/v4/database/sqlite` (pure Go, modernc-backed). `mattn/go-
sqlite3` is no longer in the dep graph and `CGO_ENABLED=0 go build
./...` succeeds. See ADR 0018 § Consequences for the swap detail.

---

## Minor findings

### Threading / lifecycle

#### m1. `safego.runWithRespawn` doesn't recover panics in `onPanic` itself

**File:** `/home/mveary/Development/station-manager/internal/safego/safego.go:99-111`

The deferred `recover()` catches panics from `fn()`, but if
`onPanic(name, r, debug.Stack())` itself panics (e.g., the panic
handler tries to log via a closed logger), that panic escapes the
deferred recover and crashes the daemon. The intent of safego is
"long-lived workers can't take the process down"; an `onPanic`
that misbehaves currently can.

**Suggested fix:** wrap the `onPanic` call in its own
`func() { defer recover(); onPanic(...) }()` so a misbehaving handler
degrades to a silent skip rather than a crash.

---

#### m2. `time.After` channel leak on respawn-cooldown ctx-cancel

**File:** `/home/mveary/Development/station-manager/internal/safego/safego.go:115-119`

```go
select {
case <-ctx.Done():
    return
case <-time.After(time.Duration(respawnCooldown.Load())):
}
```

When `ctx.Done()` wins the race, the timer started by
`time.After` continues to run and is never received. The runtime
eventually GCs the timer (since nothing references it after the
function returns), but until expiry it's a small live timer. A
daemon that respawns a worker many times across its lifetime
accumulates timers up to the cooldown duration. Negligible at
practical respawn rates; flagged for completeness because every
other long-running select in the codebase uses `time.NewTimer` +
explicit `Stop()`.

**Suggested fix:** swap `time.After` for `time.NewTimer` and call
`Stop()` on the ctx-cancel path, or accept the leak as bounded.

---

### Network security

#### m3. SSE keepalive write doesn't reset the (cleared) write deadline

**File:** `/home/mveary/Development/station-manager/internal/api/handler_events.go:49-50, 94-98`

`handleEvents` clears the write deadline once via
`rc.SetWriteDeadline(time.Time{})` so an idle but still-connected
client doesn't get force-disconnected at `WriteTimeout`. A reader
might wonder: what if the underlying connection's deadline is
re-armed by a write attempt that the client is slow to ack? In
practice, removing the deadline once is sufficient — `net.Conn` does
not automatically re-arm — but the comment frames this as
"best-effort", which understates the actual behaviour. Worth a brief
comment note that deadline removal is sticky for the connection
lifetime once `SetWriteDeadline(time.Time{})` succeeds.

**Suggested fix:** strengthen the comment or check the controller's
return value to log when deadline-clearing fails.

---

#### m4. Default config seeds `0.0.0.0`-equivalent SocketPath only via convention

**File:** `/home/mveary/Development/station-manager/internal/config/config.go:262-266`

The TCP default is `127.0.0.1:8080`, which is correctly loopback-
only — good. There's no validation that an operator who edits
`SocketPath` to `0.0.0.0:8080` or `:8080` (LAN-exposed) gets a
warning, however. The handler chain has no auth, so an open-binding
LAN exposure would let any LAN client submit QSOs. Not a defect in
the loaded code path, but a soft warning would be defensible.

**Suggested fix:** at config load, if `Protocol == "tcp"` and the
host portion of `SocketPath` is not loopback, log a `warn` line.
Don't reject — operators in trusted-LAN setups may want this.

---

### Code correctness

#### m5. `FetchCountryByCallsignWithContext` LIKE pattern unescaped

**File:** `/home/mveary/Development/station-manager/internal/database/sqlite/api_context.go:722-725`

```go
mods := []qm.QueryMod{
    qm.Where("? LIKE "+models.TableNames.Country+".prefix || '%'", callsign),
    ...
}
```

The bound `?` is the `callsign` argument, used as the left-hand side
of a LIKE — sqlite interprets the bound value as a literal string,
not a pattern, on this side. That's correct. But the right-hand side
concatenates `prefix || '%'`, where `prefix` comes from the
`country` table — values written by hamnut. Hamnut prefixes are
expected to be plain alphanumerics; if a future provider or admin
import inserted a row with a prefix containing `%` or `_`, the
longest-prefix-match read-path would silently match unintended rows.
Not exploitable today (the country table is hamnut-only writer), but
the read query has no defensive escape clause.

**Suggested fix:** add `ESCAPE '\'` and escape `%`/`_` in `prefix`
at upsert time, or document the prefix-charset invariant explicitly
on `UpsertCountryWithContext`.

---

#### m6. `qsoservice.IsValidCallsign` doesn't restrict character set

**File:** `/home/mveary/Development/station-manager/internal/qsoservice/validation.go:8-18`

The check is "length 3-32 AND contains at least one digit". A string
like `Q5%` or `_5*` passes. The validator is consulted by api
handlers (`handleEnrichCallsign`, `handleSubmitQso`,
`handleContactHistory`, `handleContestDupe`) before passing the
callsign through to DB queries. The contact-history LIKE in
`FetchQsoSliceByCallsignWithContext` (`api_context.go:97`) builds
`callsign+"/%"` as a LIKE pattern — a callsign containing `%` would
make the lookup match much more than intended. For
`FetchContactedStationByCallsignWithContext` the equality lookup
is unaffected.

**Why it matters:** practical exploitability is low (the daemon is
single-operator), but a typo'd callsign with a `%` would silently
return wrong contact-history results. Any future endpoint that
forwards the callsign into a SQL LIKE is vulnerable until the
validator is tightened.

**Suggested fix:** restrict the character set to ASCII alphanumerics
and a small set of recognised separators (`/`, `-`) — the project's
existing callsign data shape supports this.

---

#### m7. SSE handler swallows `writeSSEEvent`-Marshal errors silently

**File:** `/home/mveary/Development/station-manager/internal/api/handler_events.go:116-123`

```go
func writeSSEEvent(w io.Writer, evt events.Event) error {
    data, err := json.Marshal(evt.Payload)
    if err != nil {
        return nil  // ← silent skip
    }
    _, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", evt.ID, evt.Name, data)
    return err
}
```

The comment says "treated as a non-fatal skip so one bad event
doesn't kill the stream for unrelated good events," which is fine.
But there's no log line for the marshal failure — a future bug that
makes one payload type unmarshallable will be invisible. The hub's
own structured-typed payloads make this very unlikely today, but
the silence is worth a `WarnWith` line at least.

**Suggested fix:** log a warn line with the event name when
`json.Marshal` fails; keep the skip.

---

#### m8. `EventsHub.Publish` discards events when subs map is empty between Close and the next Subscribe

**File:** `/home/mveary/Development/station-manager/internal/events/hub.go:38-55`

Not actually a defect for the current shutdown sequence
(workers stop publishing before `hub.Close()`), but worth flagging:
`Publish` takes `mu`, checks `closed`, fans out. If a worker
publishes an event during the small window after `workerCancel` but
before `wg.Wait` returns and `hub.Close` runs, that event is
delivered to whatever subscribers still exist (good); after
`hub.Close`, Publish becomes a silent no-op (good). The intermediate
case — publish-while-Close-is-iterating-the-map — is serialised by
the same mutex, so no race. The concern is the operator-visible
behaviour: an SSE subscriber that disconnects right before a
forward-success event is published will never see that event again
(no backlog). This is documented in `handler_events.go`'s comment
and is the deliberate design; cited here only because a future
"add event replay" feature would change the picture.

---

## Observations

### O1. `cmd/smd/main.go` shutdown order is correct and well-commented

The order of work in `run()` lines 290-329 is exactly right:
`workerCancel()` first so any `safego.GoTracked` worker mid-`processRow`
sees ctx-cancel and finishes cleanly; `server.Shutdown(ctx)` to drain
HTTP handlers (the SSE handler observes `s.shutdownCh` immediately so
it doesn't block on a slow client); a bounded `wg.Wait` for forwarder
workers before `dbSvc.Close` fires (per the M2 finding from the
forwarding-subsystem review, now applied); `hub.Close()` last so SSE
subscribers that reconnect during shutdown see a clean channel-close.
The deferred `lookupRefresher.Stop()` is registered before the
deferred `dbSvc.Close()` so refresh fns drain against a still-open
DB. This is the kind of careful lifecycle plumbing that is easy to
get wrong; it is worth preserving.

### O2. `internal/api/limits.go` token-bucket implementation is clean

The lazy-refill model (compute elapsed time at each `allowSubmit`
call, no ticker goroutine) is exactly the right shape for a
single-process rate limiter. The "always advance lastFill" comment
on line 103 correctly handles clock-step-backward and injected-test-
time cases. The semaphore-as-channel pattern for concurrent-request
limiting is also right (non-blocking acquire so we refuse fast under
load rather than queueing goroutines).

### O3. The orchestrator's "always merge filter→hamnut" rule is implemented and pinned

`Enrich` always runs `FilterToCallsignFields` then
`MergeStationFromCountry` regardless of cache state. ADR 0017 #2's
"hamnut country-exclusive on write" is enforced at the merge boundary
exactly as specified. The chain-runner's "skip-on-empty" via
`IsEmpty(station)` matches ADR 0017 #8.

### O4. Forwarder worker correctly publishes `forward.*` events only after DB write succeeds

`worker.markSuccess` and `worker.markFailed` both publish to the hub
only on the post-DB-write success path; a DB failure is logged and
the row is left untouched (or transitions back to pending). The hub
is never lied to about a transition that didn't actually persist.
This is the right side of the one-fails-all-fail invariant for
event emission.

### O5. `qrz.UserAgent` and `adif.ProgramVersion` mutated from main are safe

These two package-level vars are mutated in `cmd/smd/main.go:92-93`
before any goroutine is spawned. Goroutine spawn establishes a
happens-before edge with all writes that completed in the parent
goroutine, so subsequent reads from worker / forwarder goroutines
are race-free. Worth noting because at first glance "package var
mutated after init" looks like a race risk; it isn't here.

### O6. `qsoservice.Submit` correctly puts the contacted_station upsert outside the QSO transaction

Line 278 calls `UpsertContactedStationWithContext(ctx, qso.ContactedStation)`
*after* the transaction has committed. The comment block (lines
269-277) explicitly cites the one-fails-all-fail invariant and
classifies the upsert as best-effort. This is the right place for
that classification — cache writes are deliberately decoupled from
the QSO-row tx so a hamnut/QRZ blip never blocks logging.

---

## What's solid

A short list of things working well that should be preserved through
future refactors.

1. **The forwarder package boundary holds.** `Forwarder` doesn't
   know about `qso_upload`; the worker doesn't know about upstream
   protocols; the registry's panic-on-misuse contract surfaces wiring
   bugs at startup. Adding a new destination is a self-contained
   five-line `init()` registration plus the classifier matrix. The
   `AdifPrefix()` declaration that lets the worker stamp the QSO row
   in the same tx as the queue-row transition is a clean answer to
   what was a tangled v1 problem.

2. **`safego` separates panic recovery from logging.** Taking a
   `PanicHandler` callback rather than a `*logging.Service` keeps
   `safego` cycle-free and lets each call site choose its log shape.
   The loop-based respawn (rather than recursive self-call) keeps
   stack traces readable.

3. **`Hub.Publish` is non-blocking by construction.** The buffered
   per-subscriber channel + drop-on-full + close-on-evict policy is
   the right shape for a publisher that must never be slowed by a
   slow consumer. The same publishers (qsoservice, worker) are
   guaranteed by design not to be impeded by HTTP handler quirks.

4. **`api/middleware.go` records access logs uniformly across all
   completion shapes.** 2xx, 4xx, 5xx, 503-from-limiter, 500-from-
   panic-recovery: every one produces the same INF line shape with
   `code` / `error` / `op` fields populated. `responseRecorder.noteError`
   captures the 4xx/5xx classification for the access log without
   coupling the handlers to the middleware.

5. **The QSO + upload-queue + qso_history transactions are real.**
   `qsoservice.Submit`, `Update`, `Delete` all enter `BeginTxContext`,
   write the QSO row + every applicable `qso_upload` row + (for
   Update/Delete) the `qso_history` audit row inside the same tx,
   commit once. A tx failure rolls back everything. This is the
   one-fails-all-fail invariant honoured in code, not just in
   docstrings.

6. **The orchestrator's three-state read policy is implemented
   exactly per ADR 0017.** Cold miss → block on upstream + write
   back synchronously. Stale hit → return cached + schedule async
   refresh. Fresh hit → return cached. The merge runs on every path
   so the SPA's response shape is uniform regardless of which cache
   state each layer landed in. The implementation matches the design
   doc word-for-word.

7. **The api `recoverPanic` middleware is correct.** It logs the
   panic with method/path context, writes a generic envelope so the
   panic value (which may carry stack slices or unredacted inputs)
   never reaches the client, and the comment correctly notes the
   double-write caveat. This is the right defence-in-depth: keep the
   daemon alive, log the incident, return a sanitised response.
