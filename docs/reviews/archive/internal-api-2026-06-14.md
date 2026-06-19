# `internal/api` - code review (2026-06-14)

> **Resolution (2026-06-14): H1 + M1 + M2 + M3 + L1 + L2 fixed; M4 + M5 deferred
> as a focused follow-up (see below).**
> - **H1 (security)** — `safeArchiveFilename` rejects path separators, `..`, and
>   absolute paths (appends `.adi`); the session-email handler 400s an invalid
>   filename, and `archiveSessionAdif` re-checks `filepath.Rel` stays inside the
>   archive dir before any write. Test: `TestSafeArchiveFilename`.
> - **M1** — `/v1/events` now arms a bounded per-write deadline (`armWrite`,
>   `sseWriteTimeout` 10s) before `WriteHeader` and every frame/keepalive, instead
>   of clearing it once-forever — mirrors the bridge/FT8 handlers. Test:
>   `TestHandleEvents_BoundsWriteDeadlinePerWrite`.
> - **M2** — `limitConcurrent` exempts ALL SSE paths via `isSSEPath`
>   (`/v1/events` + `/v1/rig/events` + `/v1/ft8/events`), not just the literal
>   `/v1/events`. Test: `TestLimitConcurrent_ExemptsAllSSEPaths`.
> - **M3** — `UpdateLogbookWithContext` uses the same active-row update as the QSO
>   fix (`UpdateAll WHERE id AND deleted_at IS NULL`, whitelist cols, `ErrNotFound`
>   on zero rows); the PATCH handler maps `ErrNotFound`→404. Tests:
>   `TestUpdateLogbook_RejectsSoftDeletedAndMissing` (sqlite). Closes the logbook
>   half of the backlog sweep; contacted-station remains filed.
> - **L1** — session-email now uses `s.readJSONBody` (413 on oversize, rejects
>   trailing JSON). Test: `TestSessionEmail_OversizeBody_Returns413`.
> - **L2** — `Server.Shutdown` guards the channel close with `sync.Once`
>   (idempotent). Test: `TestShutdown_Idempotent`.
> - **M4 + M5 (config-PUT contract) — DEFERRED** to a focused follow-up (filed in
>   `docs/backlog.md`): wiring `default_logbook.id`/`default_rig.id` (M4) and
>   making `logging_station`/`station` presence-aware via a pointer-block PUT
>   request type (M5). It changes `/v1/config` semantics and deserves its own
>   test/readthrough pass against both SPAs.
>
> Verified: `go test` across api/bridge/ft8/qsoservice/sqlite/config/cmd-smd;
> `go test -race -short` on api/bridge/ft8; `go vet`; static build — all green.

## Scope

Read-only review of the HTTP API package under `internal/api`, with adjacent
checks in the services it exposes directly: config, database/sqlite,
qsoservice, bridge, FT8, events, email, and the logging/config SPAs where they
pin client contracts.

Covered:

- `internal/api/*.go`
- `internal/api/*_test.go`
- adjacent SSE handlers in `internal/bridge` and `internal/ft8`
- adjacent persistence/config contracts used by API handlers
- frontend API wrappers where they document or depend on wire behavior

No production code changes were applied during this review.

Headline counts: **0 Critical**, **1 High**, **5 Medium**, **2 Low**.

## High findings

### H1. Session-email archive filename can escape the archive directory

**Files:**

- `internal/api/handler_session_email.go:33`
- `internal/api/handler_session_email.go:78`
- `internal/api/handler_session_email.go:85`
- `internal/api/handler_session_email.go:153`
- `internal/api/handler_session_email.go:162`
- `internal/api/handler_session_email.go:238`
- `internal/api/handler_session_email.go:244`
- `internal/api/handler_session_email.go:245`
- `internal/api/handler_session_email_test.go:287`
- `internal/api/handler_session_email_test.go:313`

`POST /v1/session/email` accepts a client-supplied `filename`, trims it, and
passes it directly to the local ADIF archive writer. The archive writer builds
the destination with:

```go
dir := filepath.Join(wd, sessionAdifArchiveDir)
path := filepath.Join(dir, filename)
os.WriteFile(path, []byte(body), 0o644)
```

There is no basename check, `..` rejection, separator rejection, or final
"still under archive dir" check. A body such as:

```json
{"to":"x@y","uuids":["..."],"filename":"../../config.json"}
```

resolves outside `<workingDir>/exports/sent-adif` and can overwrite files under
the working directory, including `config.json`, with ADIF content. The archive
happens before SMTP send, so a configured-but-failing SMTP transport still gets
the write attempt after the QSO fetch and ADIF composition succeed.

The existing archive test only covers the generated filename happy path; it
does not exercise custom filenames or traversal.

Suggested fix:

- Treat the request value as an attachment display name, not a path.
- Reject absolute paths, `..`, and any path separator in the supplied name.
- Optionally force or append `.adi`.
- After constructing the target path, verify `filepath.Rel(dir, path)` does not
  start with `..` and is not absolute.
- Add tests for `../../config.json`, `subdir/file.adi`, an absolute path, and a
  valid custom `session-name.adi`.

## Medium findings

### M1. `/v1/events` can still block forever on a wedged SSE write

**Files:**

- `internal/api/handler_events.go:44`
- `internal/api/handler_events.go:56`
- `internal/api/handler_events.go:96`
- `internal/api/handler_events.go:101`
- `internal/api/handler_events.go:104`
- `internal/bridge/handler.go:17`
- `internal/bridge/handler.go:48`
- `internal/bridge/handler.go:64`
- `internal/bridge/handler.go:118`
- `internal/bridge/handler.go:127`
- `internal/ft8/handler.go:38`
- `internal/ft8/handler.go:86`
- `internal/ft8/handler.go:97`
- `internal/ft8/handler.go:133`
- `internal/ft8/handler.go:140`
- `internal/api/handler_events_test.go:291`
- `internal/api/handler_events_test.go:320`

The daemon firehose clears the HTTP write deadline once for the whole SSE
connection:

```go
rc := http.NewResponseController(w)
rc.SetWriteDeadline(time.Time{})
```

After that, event frames and keepalives are written and flushed without a
bounded per-write deadline. If a client keeps the socket open but stops reading,
the handler can block inside `writeSSEEvent`, `io.WriteString`, or `Flush`. Once
blocked, it cannot observe `r.Context().Done()`, `s.shutdownCh`, or hub
eviction. That means one bad `/v1/events` peer can hold a goroutine and
subscriber slot indefinitely and can keep graceful shutdown waiting until the
outer shutdown timeout or TCP stack breaks the write.

The sibling SSE handlers already fixed this class by re-arming a bounded
deadline immediately before each frame/keepalive write. `/v1/events` still has
the older "clear once forever" behavior. Its slow-reader test also does not
assert a postcondition: after publishing 500 events, it only assigns `_ = resp`
and `_ = hub`.

Suggested fix:

- Mirror the bridge/FT8 per-write deadline helper in `internal/api` for
  `/v1/events`.
- Arm the deadline before `WriteHeader`, before every event frame, and before
  every keepalive.
- Add a test equivalent to `internal/bridge`'s deadline-recorder regression so
  a zero-time deadline cannot come back unnoticed.

### M2. Rig and FT8 SSE streams still consume the global request cap

**Files:**

- `internal/api/middleware.go:12`
- `internal/api/middleware.go:17`
- `internal/api/middleware.go:61`
- `internal/api/middleware.go:71`
- `internal/api/server.go:152`
- `internal/api/server.go:159`
- `internal/api/server.go:171`
- `internal/api/server.go:181`
- `internal/api/server.go:246`
- `internal/api/server.go:254`
- `internal/api/limits_test.go:72`
- `internal/api/limits_test.go:85`

`limitConcurrent` exempts only the literal path `/v1/events`. The comments in
`server.go` say SSE does not count against `limitConcurrent`, and both
`/v1/rig/events` and `/v1/ft8/events` are wrapped with
`limitEventSubscribers`, but the global middleware still sees those two paths
first and acquires a normal request slot for the lifetime of the stream.

With defaults, this is masked by `MaxConcurrentRequests=128` and the shared SSE
cap of 16. With a tighter operator config, or multiple browser tabs each
opening rig + FT8 streams, long-lived EventSource connections can exhaust the
ordinary request budget and make unrelated API calls fail with 503
`server_busy`. That is exactly the resource split the comments say the
subscriber cap is meant to avoid.

Suggested fix:

- Replace the single `sseEventsPath` constant with an `isSSEPath` predicate or
  set covering `/v1/events`, `/v1/rig/events`, and `/v1/ft8/events`.
- Add table tests for all three paths with a zero-cap `limitConcurrent`.
- Keep the shared `limitEventSubscribers` cap for all SSE routes.

### M3. `PATCH /v1/logbook/{id}` can resurrect a concurrently deleted logbook

**Files:**

- `internal/api/handler_logbook.go:105`
- `internal/api/handler_logbook.go:120`
- `internal/api/handler_logbook.go:136`
- `internal/api/handler_logbook.go:141`
- `internal/database/sqlite/api_context.go:1373`
- `internal/database/sqlite/api_context.go:1521`
- `internal/database/sqlite/api_context.go:1533`
- `internal/database/sqlite/models/logbook.go:30`
- `internal/database/sqlite/models/logbook.go:176`
- `internal/database/sqlite/models/logbook.go:571`
- `internal/database/sqlite/models/logbook.go:585`
- `internal/database/sqlite/models/logbook.go:592`
- `internal/database/sqlite/models/logbook.go:615`
- `docs/backlog.md:14`

The QSO active-row update bug is fixed, but the API-reachable logbook sibling
remains. The handler first fetches an active logbook and then calls
`UpdateLogbookWithContext`. The database helper fetches an active SQLBoiler
model, mutates fields, and calls `model.Update(ctx, h, boil.Infer())`.

The generated logbook update:

- infers all non-primary-key columns except `created_at`;
- includes `deleted_at` in the update set;
- uses `WHERE "id" = ?` only;
- returns rows affected, which the wrapper ignores.

There are two race outcomes after the handler's initial active-row read:

1. If `DELETE /v1/logbook/{id}` commits before the database helper's internal
   `FindLogbook`, the helper returns `errors.ErrNotFound`, but the handler maps
   only duplicate-name errors and otherwise returns a 500.
2. If the delete commits after the helper's internal fetch but before the
   generated update, the stale model still has `DeletedAt` null, so the update
   matches by primary key and clears the tombstone.

This is already filed in `docs/backlog.md` as a deferred database sweep, but it
is worth counting here because `PATCH /v1/logbook/{id}` exposes the logbook half
over HTTP today.

Suggested fix:

- Use an explicit active-row update for logbooks:
  `WHERE id = ? AND deleted_at IS NULL`.
- Whitelist mutable fields and omit `created_at` / `deleted_at`.
- Return `errors.ErrNotFound` on zero rows affected.
- Map `ErrNotFound` from `handleUpdateLogbook` to 404.
- Add a stale-update-after-delete regression test.

### M4. Documented default-logbook/default-rig writes are accepted but ignored

**Files:**

- `internal/api/handler_config.go:31`
- `internal/api/handler_config.go:139`
- `internal/api/handler_config.go:159`
- `internal/api/handler_config.go:160`
- `internal/api/handler_config.go:161`
- `internal/api/handler_config.go:172`
- `internal/api/handler_config.go:245`
- `internal/api/handler_config.go:347`
- `internal/api/handler_config.go:348`
- `docs/v2-design/api.md:606`
- `docs/v2-design/api.md:607`
- `docs/v2-design/api.md:608`
- `frontend/logging/src/lib/api/config.ts:217`
- `frontend/logging/src/lib/api/config.ts:222`
- `frontend/logging/src/lib/api/config.ts:223`

Both the handler comment and the API design doc say `PUT /v1/config` honors
`default_logbook.id` and `default_rig.id`. The frontend wrapper also includes
both blocks in the writable payload type. The handler never copies either value
from the request into the candidate config. It updates `LoggingStation`,
`Station`, optionally `Ft8Display`, and optionally mode mappings, then writes the
candidate.

An API client can send:

```json
{"default_logbook":{"id":2},"default_rig":{"id":3}}
```

and receive a 200 response, but the persisted `DefaultLogbookID` and
`DefaultRigID` remain unchanged. That makes the endpoint a silent no-op for
two fields the public contract explicitly marks writable.

Suggested fix:

- Assign `candidate.DefaultLogbookID` and `candidate.DefaultRigID` from present
  request blocks.
- Validate that the logbook id exists and is active when changing it.
- Let existing config validation enforce `default_rig_id` against configured
  rigs, or add a friendly handler error if the current validator message is too
  config-file-oriented.
- Add PUT tests for changing each id and for invalid ids.

### M5. `PUT /v1/config` zeroes omitted station blocks

**Files:**

- `internal/api/body.go:51`
- `internal/api/body.go:54`
- `internal/api/body.go:59`
- `internal/api/handler_config.go:142`
- `internal/api/handler_config.go:159`
- `internal/api/handler_config.go:160`
- `internal/api/handler_config.go:161`
- `internal/config/validate.go:125`
- `internal/config/validate.go:127`
- `frontend/logging/src/lib/states/config.svelte.ts:397`
- `frontend/logging/src/lib/states/config.svelte.ts:400`
- `frontend/logging/src/lib/ui/panels/Ft8SettingsPanel.svelte:10`
- `frontend/logging/src/lib/ui/panels/Ft8SettingsPanel.svelte:11`
- `frontend/logging/src/lib/ui/panels/MyStationPanel.svelte:56`
- `frontend/logging/src/lib/ui/panels/MyStationPanel.svelte:76`

The config endpoint is partly patch-like and partly full-replace. `ft8_display`
is presence-aware, but `logging_station` and `station` are copied from the
zero-valued request struct unconditionally. Because `readJSONBody` treats an
empty body as `{}` and validation explicitly allows empty logging-station
fields, a request that only intends to update another block can clear the
operator identity and station preferences while leaving `setup_complete` true.

The frontend knows about this and works around it: comments in `configState` and
the FT8 settings panel say callers must bundle the current writable station
blocks with unrelated saves so the daemon does not zero them. That keeps the SPA
safe when every caller follows the rule, but the API itself is still hazardous
for direct clients and future UI code.

Suggested fix:

- Use a dedicated request type with pointer fields for presence:
  `*types.LoggingStation`, `*types.StationConfig`, `*types.Logbook`,
  `*types.RigConfig`, `*types.Ft8DisplayConfig`, etc.
- Mutate a candidate block only when the block is present.
- Decide whether an empty body is a no-op 200 or a 400; either is safer than
  "clear station identity".
- Add a regression test: after setup, `PUT {"ft8_display":{...}}` must preserve
  `logging_station` and `station`.

## Low findings

### L1. Session-email bypasses the shared JSON body helper

**Files:**

- `internal/api/handler_session_email.go:78`
- `internal/api/handler_session_email.go:79`
- `internal/api/handler_session_email.go:80`
- `internal/api/body.go:23`
- `internal/api/body.go:33`
- `internal/api/body.go:35`
- `internal/api/body.go:54`
- `internal/api/body.go:62`
- `internal/api/handler_session_email_test.go:120`
- `internal/api/handler_test.go:280`

Most JSON endpoints use `readJSONBody`, which centralizes the max-body handling
and returns 413 `body_too_large` for `*http.MaxBytesError`. The session-email
handler decodes directly with `json.NewDecoder(http.MaxBytesReader(...))` and
maps every decode failure to 400 `invalid_json` with the raw decoder error in
the response.

That creates two API inconsistencies:

- an oversized session-email request is a 400 instead of the common 413;
- a body with a valid JSON object followed by trailing JSON is accepted by the
  single `Decode` call, while `json.Unmarshal` in `readJSONBody` rejects
  trailing non-whitespace.

Suggested fix:

Use `s.readJSONBody(w, r, op, &req)` here as well, then add a too-large test for
`POST /v1/session/email`.

### L2. `Server.Shutdown` panics on a second call

**Files:**

- `internal/api/server.go:315`
- `internal/api/server.go:316`
- `internal/api/server.go:317`
- `cmd/smd/main.go:576`

`Shutdown` closes `s.shutdownCh` unconditionally. The current daemon path calls
it once, so this is not a present startup/shutdown failure, but the method is
exported and `http.Server.Shutdown` itself is safe to call more than once. A
future test, supervisor retry, or duplicated teardown path would panic on
"close of closed channel" before reaching the underlying server shutdown.

Suggested fix:

Guard the channel close with `sync.Once` or a small mutex/boolean on `Server`,
and add an idempotency test.

## Checked, not findings

- The recent QSO update fix is present: `handleUpdateQso` maps the active-row
  update race `ErrNotFound` to 404, and the database QSO update path now uses an
  explicit active-row update.
- The nil mailer path is safe: `email.Service.Enabled`, `DefaultRecipient`, and
  `Send` are nil-safe, so a test server or unconfigured daemon returns
  `mailer_disabled` rather than panicking.
- `GET /v1/qso/{uuid}/uploads` intentionally includes soft-deleted QSOs so the
  delete-action upload status remains observable.
- The RF boolean endpoints accepting an omitted boolean as `false` are not
  counted here because the default direction is stop/disarm. They may still be
  worth tightening with pointer booleans if the command contract becomes
  stricter.

## Verification

Initial sandbox runs failed only where `httptest.NewServer` needed a localhost
listener (`socket: operation not permitted`). The same tests were rerun outside
the sandbox and passed.

Commands run:

```sh
GOCACHE=/tmp/go-build go test ./internal/api
GOCACHE=/tmp/go-build go test ./internal/bridge ./internal/ft8 ./internal/qsoservice ./internal/database/sqlite ./internal/config
GOCACHE=/tmp/go-build go vet ./internal/api ./internal/bridge ./internal/ft8 ./internal/qsoservice ./internal/database/sqlite ./internal/config
GOCACHE=/tmp/go-build go test -race ./internal/api
GOCACHE=/tmp/go-build go test -race ./internal/bridge ./internal/ft8
GOCACHE=/tmp/go-build go test ./cmd/smd
GOCACHE=/tmp/go-build go vet ./cmd/smd
```
