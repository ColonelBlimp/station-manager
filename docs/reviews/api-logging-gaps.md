# URGENT TODO — `internal/api` logging gaps

**Status:** open · **Raised:** 2026-08-01 · **12 findings** · **Source:** package
logging review of `internal/api` (5,639 non-test lines, 35 files, **27 log call
sites**), operator-directed, review only — no code was changed. A1-A6 from the first
pass; **A7-A12 added the same day from a second source** and verified before filing.
Three items in that batch were folded into A2/A3/A4 rather than re-filed — **and one of
them corrected a recommendation I had made in A3 that would have logged a credential.**
Read A3's amendment before implementing it.

**Siblings:** [`ft8-logging-gaps.md`](ft8-logging-gaps.md) (14) and
[`bridge-logging-gaps.md`](bridge-logging-gaps.md) (14). Same audit, same axis, same
day. `backlog.md` owns the ranking; fold in and delete once shipped.

**This package has the FEWEST findings of the three, and that is the result, not a
shortfall.** 27 log sites across 5,639 lines looks thin until you see why: this package
has a **structurally correct access log**. `logRequests` sits outermost in the
middleware chain (`server.go:332`) and captures every request completion — including
the 403s, 503s and 500s produced by middleware beneath it — carrying `code`, `error`
and `op` on any failure (`middleware.go:297-314`). That single line covers the refusal
surfaces that needed individual findings in the other two packages.

So the gaps here are narrow and specific: **what happens outside a request**, and
**what the access log deliberately does not carry**.

**Read the "NOT gaps" section before acting on any of this.** It is longer than the
findings and more important, because the wrong fix here — logging bodies or full URLs —
would reintroduce the two P1 credential leaks of 2026-07-25.

---

## The axis used

Same as the siblings: **can the operator tell this apart from the nearest confusable
state?**

---

## Tier 1

### A1. ✅ FIXED 2026-08-01 (`0265f04a`) — The shutdown path logs nothing

**Shipped.** `StopAccepting` emits `HTTP server draining` exactly once (via
`draining.Swap`, so the idempotent method cannot duplicate the marker); `Shutdown`
emits `HTTP server shutdown complete` once and **only on success**, guarded by a new
`shutdownLogged` — `shutdownOnce` could not serve, being consumed by the channel close
on the first call even when that call failed. A failed Shutdown deliberately leaves the
opening unmatched, which is the accurate report. The trigger stays in `cmd/smd` per the
operator's decision. Pinned by `internal/api/shutdown_logging_test.go`, including the
repeated-call case. Original finding below.

---


`server.go:438` logs `HTTP server listening` with protocol and address. There is no
counterpart anywhere. Both shutdown entry points are silent:

- **`StopAccepting`** (`server.go:458-474`) raises the drain gate — after which *every*
  new request is refused 503 `shutting_down` (`middleware.go:83-93`) — closes the
  listener, and disables keep-alives. No log.
- **`Shutdown`** (`server.go:490-499`) closes `shutdownCh`, which terminates **every
  SSE stream**, then unlinks the Unix socket. No log.

What the log actually shows during a shutdown: a burst of `status:503
code:shutting_down` access lines, plus one access line per SSE connection carrying its
entire lifetime as `duration_ms` — and nothing saying when or why draining began.

- **Confusable with:** a crash, an OOM kill, or a systemd stop. The `listening` line is
  an unmatched open bracket, so "did this daemon exit cleanly?" is not answerable from
  the API layer.
- **Why it matters here specifically:** the SHIP GATE evidence is 58 distinct builds in
  15.1 days, and `smd` is deliberately not auto-start — so daemon transitions are
  frequent and *expected*, which makes distinguishing a clean one from a fault harder,
  not easier.
- **Includes the restart case.** `handleRestart` (`handler_restart.go:35-53`) is
  attributable — `POST /v1/restart 202` does reach the access log, along with its 409
  `tx_active` / 503 `restart_unavailable` refusals. But the shutdown that follows
  carries no back-reference, so joining them is a manual step. A drain line naming its
  trigger closes both halves at once.
- **Record:** a line at `StopAccepting` (drain begun, and why if known) and one at
  `Shutdown` (streams closed, socket released).

### A2. ✅ FIXED 2026-08-08 (session of 2026-08-07) — Operator actions on OUTBOUND data record the action but not the outcome

> ✅ **FIXED, both halves — and with one deliberate deviation from this entry's
> prescription.** The record lives at the SERVICE layer, not the handler:
>
> - **Reconcile:** `Reconciler.RunOnce` now takes a `trigger`
>   (`TriggerManual` from the on-demand wiring, `TriggerPeriodic` from the
>   loop) and writes the run-complete summary itself on success — so no
>   trigger path can complete a run without leaving a record, and an operator
>   press is distinguishable from the hourly loop firing around the same
>   time. Spec: `internal/forwarding/smcloud/reconcile_log_test.go` (RC1–RC3,
>   criterion in the header).
> - **Enqueue:** closed by Q2's `logEnqueueResult` (all five counts, every
>   return path) **plus a correction made here**: Q2's line hardcoded
>   "manual upload backfill result", and `EnqueueUploads` is ALSO called by
>   the reconciler's heal path — so a reconcile heal logged as an operator
>   press for one day. The line now carries `origin` (manual/reconcile) and
>   the message claims no attribution the field could contradict. Spec:
>   `logginggaps_test.go` `*_LogNamesItsOrigin`.
>
> **The "handler logs the summary it returns" half of the amendment below was
> deliberately NOT built.** With origin on the service line, a handler-layer
> duplicate would add no fact not already durable — the access line supplies
> the HTTP context, the service line the outcome + who asked — at the price
> of doubling the volume of every backfill. Same-layer reasoning as A4's
> `source` field, one line instead of two.

Two endpoints run real work against external services and return a result summary that
exists only in the HTTP response the browser then discards:

| Handler | Returns | Logged |
|---|---|---|
| `handler_forwarder_uploads.go:70-76` `handleEnqueueForwarderUploads` | how many QSOs were enqueued / skipped / classified, per forwarder | nothing |
| `handler_smcloud.go:44-48` `handleSmcloudReconcile` | the reconcile summary — what diverged and what was healed | nothing |

The access log records `POST …/uploads 200` and `POST /v1/smcloud/reconcile 200`. That
the operator pressed the button is durable; **what it did is not.**

This is the surface where it matters most. An enqueue decides what gets pushed to QRZ
and ClubLog — and per the ClubLog backlog entry, the `skipped_no_history` refusal
exists precisely because pushing the wrong set breaks a written promise to a third
party. Whether a given press enqueued 3 rows or 300, and how many were refused, is
exactly the fact that would be asked for afterwards.

- **Confusable with:** a press that did nothing. A 200 with `{enqueued: 0, skipped: 47}`
  and a 200 with `{enqueued: 47}` are the same access-log line.
- **Record:** the summary counts these handlers already have in hand at the point they
  serialise them.
- **Related, deliberately not re-filed:** config saves (SHIP GATE item (a)) are the same
  defect on the same layer. Fix them with one decision about what a mutation line
  carries, not per-handler. See A4.
- **AMENDED 2026-08-01 (second pass), then CORRECTED by the `internal/qsoservice`
  review the same day, then RE-CORRECTED — the original claim was right.** `qsoservice`
  really does log **only the non-zero** enqueues: `enqueue.go:181` and `:292` return
  before the transaction and before the logging statement when nothing was queued, so an
  all-refused selection writes nothing at all. (My intermediate rebuttal — "the log
  fires unconditionally after commit" — was true and irrelevant, since commit never
  happens on that path. See `qsoservice-logging-gaps.md` Correction 2.) **The other half
  is also true and sharper than stated:** the non-zero line carries `enqueued`, `skipped_uploaded` and `force`, and omits
  `SkippedDeleted`, `NotFound` and — the one that matters — **`SkippedNoHistory`**, the
  ClubLog `realtime.php` refusal that exists because of a written commitment to a third
  party. So SM withholds QSOs to honour that commitment and records no evidence it did.
  The fix still spans both layers: the handler logs the summary it returns, the service
  logs all five outcomes. Detail in
  [`qsoservice-logging-gaps.md`](qsoservice-logging-gaps.md) **Q2**.

### A3. ✅ FIXED 2026-08-08 (session of 2026-08-07) — CSRF rejections record that one happened, not what was rejected

> ✅ **FIXED, per the AMENDMENT below, not the original wording.** Both
> refusal paths emit a dedicated Warn record (distinct messages) carrying the
> refused destination as PARSED fields only: `host` via a `url.Parse("//"+…)`
> round-trip that structurally sheds userinfo (net/url keeps it in `u.User`,
> never `u.Host`), `origin_scheme`/`origin_host` from the parsed Origin with
> the port preserved (the port IS the stale-bookmark diagnosis). A value that
> does not parse to a host — or exceeds 260 octets (RFC 1035's 253-octet name
> cap + ":65535") — logs `*_unparseable=true` and never the raw bytes; that
> length bound is the one judgement call made without asking, on the grounds
> that it is a protocol constant, not an operator tolerance. Spec + criterion:
> `internal/api/csrf_log_test.go` (CL1–CL6), including the
> credential-never-in-the-log rules this amendment exists for.

`csrf.go:36-53`. Both rejection paths call `writeError` with a **static** message:

```go
s.writeError(w, http.StatusForbidden, "cross_origin", "host not allowed", op)
...
s.writeError(w, http.StatusForbidden, "cross_origin", "cross-origin request rejected", op)
```

The access log therefore carries `status:403 code:cross_origin`, plus `remote` and
`path` — but **not the `Host` or `Origin` that failed**, which is the entire diagnostic
content of the event.

`requireSameOrigin` is the API's only security control (SM is unauthenticated by
design — single-operator loopback), it guards the whole mutating surface, and its
comments cite five separate codex P1/P2 findings that shaped it. When it fires, "which
origin" is the finding; today it is discarded.

- **Confusable with:** each other, and with a misconfiguration. A rebinding attempt, a
  browser hitting a stale bookmark on the wrong port, and a LAN deployment bound to
  `0.0.0.0` refusing legitimate traffic all produce the identical line — and the third
  is a support case the operator would hit on the 7Q8AC deployment.
- **Record:** the rejected destination on a dedicated Warn line — worth its own line
  rather than access-log fields, because a security refusal at Info interleaved with
  routine traffic reads as routine.
- **CORRECTED 2026-08-01 (second pass).** ~~`Origin` and `Host` are hostnames, not
  credentials, so both are safe to log.~~ **That advice was wrong and must not be
  followed.** The `Origin` header is client-controlled and is not validated to be a
  bare origin — a non-browser client can send `https://user:pass@host`, which is
  precisely the credential-into-a-`0644`-file shape of the 2026-07-25 P1s. Log
  **parsed, sanitised** fields only: `u.Scheme` and `u.Host`, never the raw header.
  `originAllowed` (`csrf.go:103-111`) **already computes them** — it does
  `url.Parse(origin)` and reads `u.Host` / `u.Hostname()` — so the safe values are in
  hand at the rejection point and the unsafe one never needs to be touched. A parse
  failure should log the fact of the failure, not the unparseable string.

---

## Tier 2

### A4. Config logging is failure-only, so grepping `config` is systematically misleading

> ✅ **FIXED 2026-08-02 — this was SHIP GATE (a), and it closed A8 with it.** A
> committed save now emits one Info `config saved` record carrying a field-level
> delta, `source` (`api` vs `startup`), and `setup_completed` on the first-run PUT.
> Criterion + the five operator rulings are in the header of
> `internal/api/config_save_log_test.go` (CS1–CS10); the startup half is
> `cmd/smd/config_save_startup_test.go` (B1–B3). The delta lives in
> `internal/config/diff.go`.
>
> **The operator OVERRULED this entry's implied prescription (and A8's explicit
> one) that the record carry "field names and counts only":** non-secret fields
> log their VALUES, because names alone answer *when* and *which* but not "and to
> what?", which is half the question the gate was opened for. Secrets carry
> presence only, mirroring the API's existing `credentials_set` / `password_set`
> masking. Classification is an **allowlist** — a field added later is redacted by
> default, since a denylist fails open.
>
> **One thing this entry got right and a later reading got wrong.** The claim
> below that the daemon rewrites `config.json` at startup was challenged during the
> fix and re-verified as TRUE: `config.Load` does not write, but
> `cmd/smd/main.go:237` calls `Update` on every start and `config.Service.Update`
> (`config.go:1746`) writes unconditionally with no delta check. So mtime moves
> every boot, and that is exactly why `source` is on the record.

`handler_config.go` is 1,263 lines with **two** log calls — `:670` and `:754` — and both
fire only when a PUT is **rejected** for an unstartable forwarder. A successful config
change writes nothing.

The asymmetry is the finding, not just the absence: **every config line in `smd.log` is
a rejection.** An operator or a remote admin grepping for config activity sees only
failures and reasonably concludes nothing else happened.

- **Confusable with:** no config change having occurred. Combined with the daemon
  rewriting `config.json` at startup, "when did this setting change, and to what?" has
  no answer — which is exactly what SHIP GATE item (a) says.
- **Not re-filed as new work** — this is SHIP GATE (a), already operator-directed and
  ranked. Recorded here so an `internal/api` reader finds it in the file for this
  package, and because the asymmetry above is a detail the backlog entry does not state.

### A5. A long-lived SSE connection is invisible at the default log level until it ends

`logRequests`' own doc (`middleware.go:277-280`) is explicit and correct:

> a long-lived `/v1/events` connection is logged once at disconnect with a large
> duration (the connection lifetime). That's the right behaviour: the access log
> records what happened, and "happened" for an SSE connection means "the connection
> ended."

Right for an access log. The consequence is that **currently-attached subscribers do not
appear anywhere at the default level.** A connect breadcrumb does exist — `:288`,
`http request received` — but at Debug, which is off in production.

That matters more than it would in a normal web service, because subscriber count is
load-bearing here: it holds the FT8 capture device, and when it reaches zero past the
linger window it **disarms TX and abandons any active QSO**
(`internal/ft8/service.go:398-429`). Combined with silent eviction in *both* hubs
(`ft8-logging-gaps.md` finding 1, `bridge-logging-gaps.md` B2), "how many subscribers
were attached at time T" is currently unanswerable.

- **Confusable with:** no subscriber ever having connected.
- **Record:** this is a *level* decision, not a missing line. Either promote the SSE
  connect breadcrumb to Info for SSE paths only, or — better, and cheaper — let the two
  hub-eviction fixes carry the subscriber count. Do not promote the general
  request-entry breadcrumb; that would double the access-log volume for no gain.

---

## Tier 3

### A6. `recoverPanic`'s partial-response case is not distinguished

`middleware.go:42-48` documents that when a handler has already written a header before
panicking, the 500 envelope "is effectively swallowed — the client sees whatever partial
bytes made it out first."

The panic itself is logged well (`:57-62`: value, stack, method, path — the best log
line in the package). What is not recorded is whether the client received a clean 500 or
a truncated body. The access log's `status` will show whatever was written first, so a
panic mid-stream can appear as a 200.

- **Confusable with:** a successful request. Rare, and the panic line is the actionable
  half — hence Tier 3 — but a truncated response is what a client-side bug report would
  describe, and nothing connects the two.

---

## Second pass — findings A7-A12 (added 2026-08-01, separate source)

Raised independently after the first six and verified against the code the same day.
Three items in that batch were folded rather than re-filed: outbound-summary logging
into **A2** (with a real addition about `qsoservice`), the CSRF destination into **A3**
(where it **corrected** my recommendation — see the amendment there, it matters), and
the config-commit case into **A4**. A8 below is the genuinely new half of that last one.

### A7. A database failure is reported to the operator as unset configuration — Tier 1

> ✅ **FIXED 2026-08-01**, and the operator OVERRULED this entry's own prescription —
> correctly. "Record the underlying error … do not change the fail-closed behaviour"
> would have logged the cause while leaving the operator at the screen still reading
> *"set your station callsign in My Station"*. His ruling: **fail closed means never
> fall back to another callsign and never transmit after a DB error; it does not
> require preserving the misleading 400.**
>
> So `currentStationIdentity` now returns an error, and all three FT8 TX entry points
> split the two cases:
>
> | condition | response |
> |---|---|
> | logbook missing/empty AND config callsign empty | 400 `no_station_callsign` (unchanged) |
> | unexpected datastore error | **503 `db_unavailable`** + Error log with cause, `logbook_id`, `op` |
>
> Neither starts a session nor keys PTT, and neither disarms: these routes require TX
> already armed and the refusal leaves that state alone. `db_unavailable` is not new vocabulary — `handler_health.go`
> already answers this fault with that status and code, so the operator meets one
> name for one condition.
>
> **The trap, called out by the operator before the build:** `writeServerError`
> hardcodes 500 (`httpkit.go:99`), so reaching for it here would have reported a 500
> for a 503 condition. Proof R2 demonstrates it — `status = 500, want 503`.
>
> Rules + reasoning: `internal/api/station_identity_test.go` (S1-S4). The DB fault is
> real, not mocked — closing the sqlite service makes `getOpenHandle` fail, which is
> the shape an unreadable datastore actually produces. `api-endpoints.md` updated in
> the same commit per the route-change rule.
>
> One rule deliberately NOT written, so its absence is not read as an oversight: the
> criterion's "neither refusal starts a session or keys PTT" has no test of its own, because TX is
> never armed in these fixtures — so no session could start whatever the handler did,
> and a fixture that makes both paths agree proves nothing. S1/S2 pin it instead by
> asserting the exact code: reaching the sequencer would answer with the arm-gate
> refusal, so the codes ARE the evidence of the early return.

`handler_ft8_qso.go:30-43`, `currentStationIdentity`:

```go
lbCall, err := s.db.LogbookCallsignByIDWithContext(ctx, logbookID)
if err == nil { ... }
if err == nil || stderr.Is(err, errors.ErrNotFound) { ... }
return "", logbookID          // <- every other DB error, discarded
```

Failing closed is **correct** and deliberate — the comment above it says so, citing a
codex review: any error other than not-found returns an empty callsign "so the caller
refuses to arm/transmit". The behaviour is right. The error vanishing is the defect.

Three FT8 handlers then emit `400 no_station_callsign`. So the operator is told **their
station callsign is not configured** when the truth may be that the database is
unreadable.

- **Confusable with:** genuinely unset configuration — and this is the worst kind of
  confusion, because the two demand *opposite* actions. The message sends the operator
  to the Settings screen to fix a field that is already correct, while the actual fault
  (a failing DB) goes uninvestigated. A wrong instruction is worse than no instruction.
- **Sharper still:** the 400 says "client error". A DB fault is a 5xx condition, so this
  is also the one place in the package where a server fault is misclassified as a client
  one — which means it never reaches `writeServerError`, the mechanism that would
  otherwise have logged the cause at ERR.
- **Record:** the underlying error and `logbook_id` at the point it is discarded, before
  failing closed. Do not change the fail-closed behaviour.

### A8. A config PUT can return 500 with the change already committed — Tier 1

> ✅ **FIXED 2026-08-02, together with A4 as this entry prescribed.** The record is
> emitted immediately after `Update` returns and BEFORE `buildConfigResponse`, so a
> response-build failure still leaves proof that the change applied. Pinned by CS4
> (`TestConfigSave_CommitLoggedEvenWhenResponseFails`), which breaks the DB after
> setup and asserts the 500.
>
> **CS4's first fixture was wrong in a way worth recording:** it changed the
> station callsign, which trips the callsign-lock guard at `:621` — that guard
> reads the DB and 500s *before* the commit. Same status code, no commit, so the
> rule passed against a daemon that logged nothing. The fixture now repeats the
> callsign unchanged and edits a different field. A rule asserting "the commit is
> logged despite the 500" is worthless if the 500 arrives before the commit.
>
> The "field names and counts only" line below was overruled — see A4's banner.

`handler_config.go:778`. The commit happens inside `s.cfg.Update(...)` (`:704`). If
`buildConfigResponse` then fails, the handler calls `writeServerError` → **HTTP 500**,
and the access line reads `PUT /v1/config 500`.

The configuration change is already persisted.

This is a step beyond A4's "no durable record". Here the record that *does* exist is
**actively wrong**: an operator or admin reading the log concludes the config change
failed, when it succeeded. A retry then re-applies a change that was already applied,
and on a PUT carrying credentials the retry may not be idempotent in the way the
operator assumes.

- **Confusable with:** a config PUT that genuinely failed and left the live config
  untouched — which is what every *other* 500 on this path means (`errPutValidation`
  aborts inside the lock, live config untouched, `:698-703`).
- **Record:** a commit line **immediately after** the update returns, before response
  construction can fail. That single line resolves A4 and A8 together — which is why
  they should be done in one edit.
- **Secret-safe, and this is not optional here:** field names and counts only. Never the
  request body, never the merged config, never a credential value. See NOT gaps.

### A9. ✅ FIXED 2026-08-01 (`0265f04a`) — `http.Server.ErrorLog` unset

**Shipped.** `httpErrorLogWriter` (`internal/api/middleware.go`) adapts net/http's
`*log.Logger` sink onto the structured logger at **Warn**, per the operator's decision
that Debug would hide exactly what this restores. It never returns an error —
propagating a logging fault into net/http's error path would turn it into a serving
fault. Pinned by `TestHTTPServerErrorLog_*`, including the negative case.
**Reversion-proof note:** the first revert attempt (deleting the field) broke the build
and proved nothing; the valid proof reverts to `stdlog.New(os.Stderr, …)` — the actual
pre-fix behaviour. Original finding below.

---


`server.go:331-332` constructs `&http.Server{Handler: …, ReadHeaderTimeout: …, …}` and
never sets `ErrorLog`. Go therefore falls back to `log.Default()` — stderr.

What goes there instead of `smd.log`: temporary `Accept` errors, connection and
protocol-level diagnostics, and any panic that escapes on a path `recoverPanic` does not
wrap (the middleware covers the handler chain, not the server's own machinery).

Under systemd these land in the journal rather than being destroyed, so this is a
**split** rather than a loss — but it splits along the worst possible line: the file the
operator is told to read, and the file a remote admin would be sent, is `smd.log`, and
these are precisely the diagnostics for "the daemon is up but connections are failing".

- **Confusable with:** nothing having gone wrong at the transport layer.
- **Record:** wire `ErrorLog` to a `*log.Logger` adapter over `logging.Service`. One
  field on an existing struct literal.
- **Note for the fix:** the adapter should emit at Warn, and must not re-enter the
  logging service on its own failure path.

### A10. Successful rig commands record that one happened, not what it was — Tier 2

`handler_rig_command.go:70` returns 202 after `SendCommands`; neither the handler nor
`bridge.SendCommands` logs the validated batch. A band change, a mode change, a VFO
swap and a power change are all `POST /v1/rig/command 202`.

This is the **inbound rig-control path** (ADR 0026) — the one that moves a real radio.
The refusals are covered (they exit through the access log with distinct codes — see NOT
gaps), so once again the failure paths have the evidence and the success path does not.

- **Confusable with:** each other. "Why is the rig on 40 m?" and "what changed the
  power?" are unanswerable, and the operator's own SPA drives these from keyboard
  shortcuts, so the answer is often "something I did without noticing".
- **Volume warning — the reason this is Tier 2 and needs a decision, not just a line.**
  Frequency stepping is bound to Shift+Ctrl+arrows with key-repeat, so a single dial
  spin can emit many `set_freq` commands per second. Logging each one at Info would
  swamp `smd.log` — which already has `http request` at 23% of bytes. Either log at
  Debug, or log the op names without per-step values, or coalesce. **Do not simply add
  an Info line per command.**
- **Record:** the derived op names (and values for low-rate ops), never the raw body.
- **Completes a set:** with `bridge-logging-gaps.md` **B12** (tune key/unkey silent),
  the daemon currently has no record of *anything* it told the rig to do on the success
  path.

### A11. Malformed stored forwarder credentials are silently treated as absent — and can be DROPPED — Tier 3

Two sites, and the second does more than hide a diagnostic:

- `handler_config.go:1146` — `credentialKeysSet` returns `nil` when
  `json.Unmarshal` fails, so the masked GET view reports **"no credentials set"** for a
  forwarder whose credentials are stored but corrupt.
- `handler_config.go:1234` — `_ = json.Unmarshal(ex.Credentials, &base)` on the merge
  path. A decode failure leaves `base` empty, so the merge **rebuilds the credential
  block from an empty base** — a PUT that was meant to preserve existing credentials
  silently discards them.

- **Confusable with:** a forwarder that was never configured. The operator sees an empty
  credential list, re-enters what they think is missing, and the second site quietly
  drops whatever survived.
- **This is data loss, not only a logging gap**, and worth flagging as such: it needs
  already-corrupt stored JSON to trigger, so it is rare — but there is no signal
  anywhere that it happened, at either site.
- **Record:** forwarder **name and type** plus the decode error. **Never the credential
  blob, and never the unmarshal target** — the failing bytes ARE the credential.

### A12. A health-check failure loses the database cause — Tier 3

`handler_health.go:12` reduces every `s.db.Ping()` error to `503 db_unavailable`. The
access log carries the code; the cause is discarded.

The DB being unreachable is the one condition the project's own invariant treats as
fatal — *"the only thing that should stop logging is a broken local DB"* — so the cause
of exactly that condition is the cause most worth having.

- **Confusable with:** any other DB failure. "Disk full", "file locked by another
  process", and "schema corrupt" are one line.
- **Record: rate-limited or transition-based only.** `/healthz` is a probe endpoint; a
  monitor polling it every few seconds against a down DB would flood `smd.log` at
  exactly the moment disk space may be the fault. Log the transition into and out of
  unhealthy, not each probe.

---

## Verified NOT gaps — READ THIS BEFORE CHANGING ANYTHING HERE

Checked against the code 2026-08-01. This section is longer than the findings because
this package's logging is mostly right, and two of the plausible "improvements" are
actively dangerous.

**Do NOT log request bodies, and do NOT log full URLs.** `logRequests` logs
`r.URL.Path` — deliberately not `RawQuery` — and never touches the body. That is
correct and must stay: config PUT bodies carry forwarder credentials and SMTP
passwords, `config.json` is `0600` while `smd.log` is `0644`, and two P1 credential
leaks on 2026-07-25 came from exactly this shape (helpful diagnostics widening a
sensitive value's audience). Any fix for A2 or A4 must log *derived facts* — counts,
field names, which forwarder — never the payload. A3's `Host`/`Origin` are safe because
they are hostnames.

- **The access log covers the refusal surfaces that needed findings elsewhere.**
  `middleware.go:280-315`, outermost at `server.go:332`, captures all four completion
  shapes its doc enumerates: normal 2xx/3xx/4xx, `writeServerError` 5xx,
  `limitConcurrent`/`limitEventSubscribers` 503, and `recoverPanic` 500 — each with
  `code`, `error` and `op` when present. This is why `handler_ft8_tx.go`,
  `handler_ft8_qso.go`, `handler_rig_command.go`, `handler_rig_tune.go` and
  `handler_qso.go` need no lines of their own despite having zero.
- **`writeServerError` logs the wrapped error chain at ERR separately** from the access
  line. 5xx detail is not lost.
- **Panics are handled properly** — `recoverPanic:57-62` logs value, stack, method and
  path, and deliberately returns a generic client message because "panic values can
  contain sensitive internals". Correct on both halves.
- **Path-carried subjects ARE captured.** `DELETE /v1/qso/{uuid}` and the logbook
  routes put the identifier in the path, so the access line attributes them. **This
  partially mitigates SHIP GATE item (b)**: `qsoservice.Delete` logs nothing, but the
  API-layer delete is traceable. The gap is deletes that do not arrive via HTTP.
- **`rejectWhenDraining`, `limitConcurrent` and `limitEventSubscribers` all use distinct
  codes** (`shutting_down`, `server_busy`, the subscriber cap), so the three 503 causes
  are already separable by `code` — no extra lines needed.
- **`handler_session_email.go` (11 sites) and `handler_session_export.go` (4) are the
  best-covered handlers in the package** — 15 of the 27 total. Email is the sharpest
  irreversible action here (a duplicate lands in a real inbox) and it is logged
  accordingly. Leave them alone.
- **`server.go:287`** warns when pprof is mounted. Correct and worth keeping.
- **Files with zero log calls that correctly have zero:** `response.go`, `body.go`,
  `validation.go`, `doc.go`, `manual.go`, `spa.go`, `limits.go`,
  `handler_forwarder_types.go`, `handler_health.go` — pure plumbing, static serving, or
  covered entirely by the access log.

---

## Suggested order

1. **A7** first — the only finding in any of the three files where the daemon tells the
   operator to take the *wrong* action, and the fix is one log line before an existing
   `return`.
2. **A8 with A4** in one edit — a commit line immediately after `s.cfg.Update` returns
   resolves both, and A8 is currently emitting a 500 for a change that succeeded.
3. **A1** — smallest fix in any of the three files, and it closes the "did the daemon
   exit cleanly" question that the SHIP GATE's build-attribution work also needs.
4. **A3** — one Warn line, security-relevant, the current message is static so nothing
   is lost by replacing it. **Implement the amendment, not the original wording:**
   parsed `u.Scheme`/`u.Host`, never the raw `Origin`.
5. **A2** (with its `qsoservice` half) — same decision as A4/A8 about what a mutation
   line carries: actor, subject, outcome counts, never payload.
6. **A9** — one struct field plus a small adapter; disproportionate coverage for the
   effort.
7. **A10** — needs a volume decision first (see the finding); do not add a bare Info
   line. Settle with `bridge-logging-gaps.md` B12.
8. **A5** — resolve as part of the two hub-eviction fixes rather than on its own.
9. **A11, A12, A6** whenever adjacent code is open. **A11 carries a data-loss bug
   alongside its logging gap** — fix the empty-base merge at `:1234`, not only the
   missing line.

Per the standing TDD directive, the behaviour statement for each is the
confusable-state clause above. Assert that the two confusable states produce
**distinguishable** output — and for anything touching A2/A3/A4, assert that a
credential-bearing field does **not** appear in the output at all.
