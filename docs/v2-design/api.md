# Station Manager v2 — Daemon HTTP API Design Brief

**Status:** Second entry in `docs/v2-design/`, written 2026-04-16 during session 5. Captures the load-bearing decisions made about the daemon's HTTP API before the first endpoint is implemented.

**This document is a design brief, not a specification.** Every decision below is revisable the moment real code proves it wrong. The purpose is to lock in the **cross-cutting decisions** — the ones that cascade into many places and are painful to change after handlers exist — so that the first coding session doesn't accidentally paint us into a corner. Implementation details (exact endpoint shapes, error code vocabulary, SSE payload schemas) are deliberately left sketchy and will be pinned down against running code, not hypothetical requirements.

**How this document relates to others:**

- `docs/v1-analysis/invariants.md` — load-bearing rules that constrain every design decision in this file. Particularly "Nothing blocks logging a QSO," "Enrichment never blocks logging," "Forwarding never blocks logging," and "Core concern is log + forward, nothing else."
- `docs/v2-design/structure.md` — the repo-layout and module decisions. This file inherits the "daemon plus thin clients" topology and the "shared `internal/` tree" from there.
- `docs/v1-analysis/lessons-for-v2.md` — particularly "Enumerate all API consumers before designing any endpoints," which is the lesson this document exists to honor.
- `docs/session-handoff.md` — the rolling cross-session state, including deferred features (text-file fallback reconciliation) that this API surface is designed to accommodate later without redesign.

---

## 1. Consumer enumeration

> **Updated 2026-05-02 per ADR 0001 and ADR 0013.** The original consumer table assumed three Wails-app backends (`apps/logging`, `apps/logbook`, `apps/config`) hitting the daemon over a Unix socket. Per ADR 0001, the client apps are now browser SPAs (`frontend/logging/`, future `frontend/logbook/` and `frontend/config/`) embedded in the daemon binary; per ADR 0013, the rig-control bridge is a daemon subsystem in the default deployment. The consumer concepts (logging concern, logbook concern, config concern) are unchanged; the *transport* and *binding layer* changed. The original table is preserved below for the historical record because every endpoint shape was designed against those concerns; substitute "SPA route" for "Wails app" mentally, or read the current shape that follows.

**Current consumers (as of 2026-05-02):**

| Consumer | Milestone | Primary workflow | SSE consumer? |
|---|---|---|---|
| `frontend/logging/` (Svelte SPA) | 2 | Real-time QSO entry. Embedded in daemon, served at `GET /` when `Protocol=tcp && ServeSPA=true`. Talks to the daemon directly via `fetch()` / `EventSource`. Highest latency-sensitivity. | Yes — primary. |
| Future `frontend/logbook/` | 2+ | Logbook management, historical editing, batch QSO edit, paging. Lower latency-sensitivity. Same embed-and-serve mechanism as logging SPA; may be a separate route or a separate bundle. Not yet built. | Yes — secondary. |
| Future `frontend/config/` | 2+ | Configuration editor. Reads/writes operator config via daemon `GET/PUT /v1/config` (replacing the v1 edit-the-file workflow). Not yet built. | Optional. |
| `cmd/importer` | 2 | ADIF bulk import from historical logs. One-shot CLI, submits N QSOs and exits. | No. |
| `cmd/udp-bridge` | 3 | Generic UDP-to-daemon bridge. Listens on UDP for ADIF-formatted payloads and forwards them to the daemon's submit endpoint. Not WSJT-X-specific. | No. |
| `internal/bridge` (daemon subsystem) | 2 | Rig-control subsystem per ADR 0013 + ADR 0019. Hosts `/v1/rig/events` SSE for the logging SPA. **v1 is read-only for state display**, with a narrow **inbound command path** added per ADR 0026: `POST /v1/rig/command` (`{op, value}`, or a `{commands:[…]}` batch written as one atomic CAT line) drives `set_freq`/`set_mode`/`swap_vfo`/band on the connected rig, confirmed by the AUTO-mode push (no read-sync). A separate **tune-carrier path** (ADR 0027) — `POST /v1/rig/tune {active}` — keys a daemon-owned reduced-power RTTY carrier for tuning an external linear amp; it is the first feature that transmits, so the daemon (not the SPA) owns the guaranteed stop (hard auto-off timer + release-on-disconnect + single-flight), and a fourth SSE event `tune-state {active}` reflects the carrier state. NOT in v1: PTT-for-operating (phone/CW QSO keying), rig control beyond the landed ops, rigctld-compat TCP, NDJSON Unix-socket frontend, persistent rig-state cache (one hub-cached slot each for `bridge-error`/`rig-disconnected`/`tune-state` replay to late subscribers, plus a two-field mode+power snapshot the tune controller restores from, are the only exceptions). **M3a (bridge subsystem v1) closed 2026-05-11; rig-mode → ADIF translation shipped session 51 same day.** `/v1/config` `bridge` block surfaces: `enabled` (operator intent), `driver` (rigdef id), `rig_name` (rigdef's human-readable name), `rig_modes` (rigdef MAINMODE value list — used by SPA to render Mode Mappings sub-tab rows), `ops` (exposed inbound-command names, e.g. `[set_freq, set_mode]` per ADR 0026 — SPA gates rig-control surfaces on this set), `tune` (bool — the rig can run the tune-carrier feature per ADR 0027; SPA shows the Tune button only when true), `mode_mappings` (merged rigdef defaults + operator overrides; SPA's `displayedState.mode/.subMode` resolves rig literals through this table). PUT `/v1/config` accepts `bridge.mode_mappings` updates; daemon diffs against rigdef defaults and persists only operator-deviations. In-process by default; split-host (`cmd/bridge`) is opt-in and parked. Not a daemon-API consumer in the same sense — it's part of the same binary. | Producer. |

### Non-consumers (deliberate exclusions)

- **The forwarder destinations** (QRZ, future ClubLog/LoTW/eQSL) — these are *outbound* targets, not API callers. The daemon pushes to them via `internal/forwarding/<name>/`.
- **SM-Online (future)** — this is a **forwarding destination**, not a consumer. When SM-Online becomes real, the daemon pushes QSOs outbound to it via `internal/forwarding/smonline/`. SM-Online never calls into the daemon's HTTP API.

### Future speculative consumers (not designed for)

- A standalone daemon dashboard or monitoring UI (considered out loud during session 5). Would consume the same SSE stream as the logging and logbook SPAs. The event stream is open for future subscribers without schema changes.

---

### Original consumer table (pre-ADR 0001, preserved as record)

| Consumer | Milestone | Primary workflow | SSE consumer? |
|---|---|---|---|
| `apps/logging` | 2 | Real-time QSO entry during active operation. Highest latency-sensitivity. Needs draft init, submit, contact history, contest dupe check, live forwarding status. | Yes — primary. |
| `apps/logbook` | 2 | Management and historical editing. Logbook CRUD, batch QSO edit, list with paging, ADIF export (client-side via `internal/adif` — no daemon endpoint), forwarding status review. Lower latency-sensitivity, larger result sets. | Yes — secondary. |
| `cmd/importer` | 2 | ADIF bulk import from historical logs or other software. One-shot CLI, submits N QSOs and exits. | No. |
| `cmd/udp-bridge` | 3 | Generic UDP-to-daemon bridge. Listens on UDP for ADIF-formatted payloads and forwards them to the daemon's submit endpoint. Not WSJT-X-specific — protocol-agnostic. | No. |

Original non-consumers:
- **`apps/config`** — was modeled as filesystem-resident, reading/writing `config.json` directly via `internal/config`. ADR 0001's SPA pivot moves config-read/write onto the daemon (`GET/PUT /v1/config`) so the operator-config can be edited from the embedded SPA without an "edit the file" workflow.
- **The serial/CAT bridge** — was modeled as an independent subsystem reachable via its own frontends (rigctld-compat TCP or SM-native NDJSON). ADR 0013 collapsed this into the daemon as `internal/bridge`; the rigctld-compat TCP frontend remains.

---

## 2. Transport and auth

> **STATUS NOTE (ST-3a, 2026-08-16).** This section is the milestone-1 design brief and
> is preserved as written. It has since drifted from the code: the first-run default is
> now **loopback TCP** (`127.0.0.1:8080`, for the embedded SPA), not a Unix socket, and
> **direct non-loopback TCP is a supported (deliberately-insecure) posture**. The
> Unix-socket-permissions auth model below applies only to the `protocol=unix`
> deployment. For the current, reconciled bind postures (loopback / owner-private
> socket / acknowledged non-loopback) see `config.md` §5.1 and **ADR 0069**. ST-3a closed
> the *silent* exposure (a non-loopback bind is now startup-fatal without
> `server.allow_insecure_network`); authenticated LAN access — a real auth layer — is
> **ST-3b**, still an open decision.

**HTTP over a Unix domain socket.** The daemon binds its HTTP server to a Unix domain socket at a path read from config. Filesystem permissions on the socket are the authorization mechanism — any process that can open the socket is treated as authorized. For the single-user desktop scenario this is equivalent to "authenticated as the user running the daemon," which is exactly the guarantee we need.

**The listener type and socket path are config-driven, not hardcoded.** No `const socketPath = "/tmp/smd.sock"` buried in handlers. The socket path, HTTP timeouts (read, write, idle), request body size limits, SSE heartbeat interval, and graceful shutdown timeout all come from `internal/config.Config` or a server sub-config within it. This follows the "no magic numbers" project rule and gives future deployment flexibility for free.

**No authentication layer in milestone 1.** The Unix socket permissions are the entire auth story. Handlers do not check identity, do not look for tokens, and do not carry a user concept. When a real auth story becomes needed (which is not today and may never be), it enters as middleware without touching handler code.

**No network/LAN deployment story in this document.** HTTP happens to be transport-agnostic (it would run over TCP as easily as over Unix socket) but this is an incidental property of using standard tools, not a design goal. Multi-machine deployment is explicitly out of scope for v2 milestone 1, and the design does not trade simplicity for preserving that option. See session 5 discussion: "keep it simple — we don't know if networking will ever be needed, and it adds massive complexity while trying to design, build and test the core."

---

## 3. Content negotiation and versioning

**URL-prefixed versioning.** All daemon endpoints are under `/v1/`. When a breaking change to the wire format becomes unavoidable (which we are not planning for in milestone 1), new endpoints live under `/v2/` and the old endpoints continue to serve old clients for a deprecation window. Per `structure.md`: "the monorepo tag is the source-level version, the HTTP API version is the wire-level version, they can move at different rates."

**Request bodies:**

- `application/json` for structured requests (logbook CRUD, QSO edits, config introspection if any).
- `application/x-adif` or `text/plain` for QSO submission bodies. The submit endpoint (`POST /v1/qso`, see Section 5) accepts raw ADIF directly — no JSON wrapping — so any ADIF-producing tool can POST without translation glue. Content-Type header distinguishes.

**Response bodies:**

- `application/json` for structured responses (query results, lists, status).
- `text/event-stream` for the SSE event stream endpoint.
- ADIF export is not a daemon endpoint; clients that need ADIF
  serialize client-side using `internal/adif` as a library import.

**Config reload:** the daemon reads its config file once at startup. Changes made externally (whether by hand-edit or by a future config SPA writing through `PUT /v1/config`, per ADR 0001's pivot from filesystem-resident config) are not picked up until the daemon is restarted. This is acceptable for milestone 1; a future refinement (file watch, SIGHUP, or an explicit reload endpoint) will be designed when it matters. Not today.

---

## 4. Cross-cutting decisions

These are the load-bearing decisions settled during session 5. Each one cascades into multiple handlers; each one is hard to change after code is written; each one is preserved here as a starting point, not a final spec.

### 4.1 Nothing blocks logging a QSO

The daemon's submit path exists to service an invariant that is strictly bigger than the daemon itself: the logging app must be able to record a QSO even when the daemon is unreachable. See `docs/v1-analysis/invariants.md` → "Nothing blocks logging a QSO, except catastrophic local failure" for the full rule.

**What this means for the API:**

- The submit endpoint must have the lowest possible latency. It may not wait for external services (QRZ, ClubLog), must not wait on network I/O beyond the local sqlite commit, and must not hold the request open longer than strictly necessary.
- The logging app maintains its own local text-file fallback when the daemon is unreachable. The daemon does not need to know about this fallback — reconciliation happens via the normal `POST /v1/qso` submit endpoint once the daemon returns, and the dedupe key (Section 4.2) absorbs any replay.
- Clients that submit QSOs must receive a response as soon as the authoritative local transaction commits, not after forwarding completes. See Section 4.3.

### 4.2 Dedupe key and the idempotent submit path

**Every submitted QSO gets a dedupe key**, computed as `hash(call + band + mode + start_time_rounded_to_minute)` and stored on the QSO row. On submit, the daemon checks whether a QSO with the same dedupe key already exists within the same logbook:

- **New QSO:** stored normally, response indicates `"stored"` with the new QSO's `uuid`.
- **Duplicate:** no new row written, response indicates `"duplicate"` with the existing row's `uuid`.

**QSO identity on the wire is the UUIDv7,** not the internal sqlite
auto-increment row id. Per ADR 0016, the canonical external identifier
for every QSO is a UUIDv7 generated at submit time and persisted on
the row. The integer primary key is an internal storage detail; it
does not appear on the API surface beyond a transitional `id` field
in the submit response that exists only to keep older logging code
compiling and is scheduled for removal. All retrieval / edit / delete
paths route by UUID (Section 7a).

**Minute-granularity** for the time component aligns with the ADIF spec's canonical time resolution and matches how most logging software treats "the same QSO."

**Contest edge case override:** a `?force=true` query parameter on the submit endpoint tells the daemon to skip the dedupe check and store the new QSO unconditionally. This covers rare cases (e.g., some contest rules allow working the same station on the same band/mode more than once within a minute). Normal operators never use it; contest operators who need it opt in explicitly.

**`force` is parsed via Go's `strconv.ParseBool`,** so any of `1`/`0`/`t`/`f`/`T`/`F`/`true`/`false`/`True`/`False`/`TRUE`/`FALSE` is accepted. Unknown values (e.g. `yes`, `y`, `force`) return `400 invalid_query_param` rather than silently falling through to dedupe-checked behaviour — a contest-mode operator's typo should fail loud, not lose the dup. Empty/missing keeps the dedupe-checked default.

**Idempotent submit is the foundation of the deferred reconciliation feature.** Because the submit endpoint is idempotent by default, the logging app can safely replay text-file-buffered QSOs after a daemon outage without worrying about creating duplicates. See `docs/session-handoff.md` → "Deferred features to investigate."

**Race-resolution refetch.** When two submits with identical dedupe-key inputs both pass the pre-transaction check and one wins the race at the UNIQUE-index commit, the loser's `qsoservice.Submit` translates the constraint violation into the same `"duplicate"` outcome the pre-check would have produced — so clients always see the same shape regardless of timing. The follow-up "find the winning row's id" lookup runs on a fresh 2-second context detached from the request context: the duplicate row is committed in sqlite by then, so the lookup is bounded and pure-read, and a request-deadline expiry can't turn a known-duplicate into a generic 500.

### 4.3 Async forward lifecycle

**`POST /v1/qso` returns immediately after the local sqlite transaction commits**, not after any upstream forwarder (QRZ, ClubLog, SM-Online) has accepted the QSO. The transaction wraps the QSO row insert and one `qso_upload` row per configured forwarder destination, per the "one-fails-all-fail for QSO writes" invariant. Cache writes (contacted_station, country) happen outside the transaction and do not affect the response.

**The forwarding worker runs asynchronously** — a background goroutine that picks up pending `qso_upload` rows, attempts each configured destination, writes per-destination status back to the row, and emits SSE events on terminal outcomes. Retries are handled internally; transient failures do not generate events or bubble back to the submit caller.

**Clients observe forwarding state in two ways:**

- **Pull:** `GET /v1/qso/:uuid/uploads` returns the current forwarding status of each configured destination for a specific QSO.
- **Push:** SSE events (`forward.succeeded`, `forward.failed`) on the daemon's event stream (Section 4.5).

Clients can freely ignore forwarding state — the local log is already durable. Operators who want real-time feedback on QRZ uploads use the SSE-backed UI in the logging app; automated clients like `cmd/importer` ignore it entirely.

**V1's forwarding code is good** and should be the reference point when the v2 forwarder subsystem is reintroduced in milestone 2+ (see `docs/session-handoff.md`). The retry loop, goroutine topology, and upload-queue polling pattern are shapes that work. The piece that needs redesign is the fan-out shape, because v1 hardcoded QRZ and v2 needs multi-destination config.

### 4.4 Pagination — forward-cursor-only at the API, client-side windowed buffer

**Daemon implements forward pagination only.** List endpoints (QSO lists, upload queue lists, search results) use an opaque cursor token:

- **Request:** `GET /v1/logbook/:id/qso?after=<cursor>&limit=<N>` — `after` is the opaque token from a previous response (omitted on the first request), `limit` is the client-requested page size (clamped to a configured max and a configured default, no magic numbers).
- **Response body:**
  ```json
  {
    "items": [ ... ],
    "next_cursor": "opaque_base64_token_or_null"
  }
  ```
  `next_cursor` is null when the client has reached the end of the list.

**The daemon never implements reverse pagination.** Backward navigation is the client's responsibility, and the natural implementation is a local in-memory buffer with a viewport. The client fetches pages forward, accumulates them in a local list, and scrolls the viewport up and down within the buffer with zero network calls. When the viewport reaches the end of the buffer, the client fetches the next page via the cached cursor. This is the standard infinite-scroll pattern.

**Client-side staleness is orthogonal to pagination** and handled via SSE events (Section 4.5). If a new QSO is logged while the user is browsing, the client either subscribes to `qso.stored` and appends to its buffer, or offers a manual refresh that dumps the buffer and re-fetches the first page.

**Internal cursor shape** (not visible to clients) decodes to a sort-key tuple: `(qso_date, time_on, id)` for QSO lists, `(queued_at, id)` for upload queue lists, etc. Each endpoint's cursor is specific to its natural sort order.

### 4.5 SSE event stream

**The daemon exposes a single event stream endpoint**: `GET /v1/events` with `Accept: text/event-stream`. Clients subscribe once and receive all events the daemon publishes; per-client filtering happens on the client side. At personal-operator scale, event volumes are low enough that a firehose-plus-client-filter model is simpler and cheaper than server-side topic subscriptions.

**Event vocabulary:**

| Event | When emitted | Primary consumer |
|---|---|---|
| `qso.stored` | A new QSO has been committed to the local database. | Both — `apps/logging` appends to recent-QSOs list, `apps/logbook` invalidates its current view. |
| `qso.updated` | An existing QSO has been edited. | Both — keeps open client views consistent. |
| `qso.deleted` | An existing QSO has been soft-deleted. | Both — same reason. |
| `forward.succeeded` | The forwarding worker has successfully pushed a QSO+destination pair to its upstream service. | Both — updates forwarding status badges in the UI. |
| `forward.failed` | The forwarding worker hit a terminal failure for a QSO+destination pair (retries exhausted or non-retryable rejection). | Both — shows failure indicators; operator may need to take action. |

**Explicitly NOT in the vocabulary:**

- `forward.attempted` — noise. Every retry cycle would emit one. Clients that want spinner UX show a spinner for any QSO in a "pending" or "retrying" state (visible via query) and remove it on the terminal `forward.*` event.
- Session lifecycle events — deferred until the logging app is being designed in milestone 2 and we know whether sessions are a first-class concept in v2.
- Operational/health events (daemon started, config reloaded) — no clear consumer today; add when a dashboard-style client actually needs them.

**Wire format** is the standard `text/event-stream` encoding, one frame per event:

```
id: 42
event: forward.succeeded
data: {"qso_id":123,"forwarder_name":"qrz","action":"insert","upstream_id":"1234567","attempts":1}

```

- `id` — monotonic per-hub counter, primarily useful for debug-tracing and client-side dedup. Not a resume cursor (see "Reconnect semantics" below).
- `event` — one of the five names above.
- `data` — JSON. Never contains embedded newlines; always a single `data:` line per frame.
- Comment lines (`: keepalive`) arrive every 30 s while the connection is idle, to keep intermediaries from dropping the TCP/unix-socket. Per SSE spec, clients MUST ignore them.

**Payload shapes** (struct definitions live in `internal/events`):

```json
qso.stored        {"qso_id": int64, "logbook_id": int64}
qso.updated       {"qso_id": int64, "logbook_id": int64}
qso.deleted       {"qso_id": int64, "logbook_id": int64}
forward.succeeded {"qso_id": int64, "forwarder_name": string,
                   "action": "insert"|"update"|"delete",
                   "upstream_id": string, "attempts": int}
forward.failed    {"qso_id": int64, "forwarder_name": string,
                   "action": "insert"|"update"|"delete",
                   "attempts": int, "reason": string}
```

Payloads are intentionally minimal. Clients re-query via `GET /v1/qso/:uuid` (etc.) for any details beyond identifiers — the authoritative state is in the database, not on the wire. `logbook_id` is carried on the `qso.*` events so the logbook-app can filter without a fetch; `action` is carried on `forward.*` so a single QSO's INSERT vs. DELETE transitions are distinguishable. `upstream_id` is omitted from `forward.succeeded` when the destination doesn't produce one (e.g. stub).

**Known gap (ADR 0016 follow-up):** the `qso_id` field on every payload above is currently the internal sqlite int — predating Phase 2 of ADR 0016. Real SSE consumers don't exist yet (`bridge.svelte.ts` is stubbed and the e2e test correlates by int), so the wire shape is correct *for current consumers* but inconsistent with the rest of the API surface. When the SPA wires up live event consumption, the payloads grow a `qso_uuid` field and clients migrate to it; the int field stays for one release as a transitional shim, then disappears alongside the `id` field on the submit response.

**Reconnect semantics and buffering:**

- The hub keeps **no backlog**. A client that connects (or reconnects) receives events from that moment forward only. `Last-Event-ID` is not honored — event IDs are monotonic within a daemon lifetime and useful for dedup, but are not a resume cursor.
- Baseline state on connect is reconciled via ordinary GET endpoints. Client contract: **open the SSE stream first, then fetch current state** — the logbook-scoped list `GET /v1/logbook/{id}/qso` for the baseline, `GET /v1/qso/{uuid}` for per-row detail (there is no `/v1/qsos` route). Any event for an ID the fetch already returned is a no-op; any event for an ID newer than the fetch is applied. Following this order avoids the race where an event fires between the fetch and the subscribe. (The handler now `Subscribe()`s before the `200 OK` flush, so the window the contract guards against is closed at the server too — review 2026-06-19 M1.)
- **Slow-reader policy:** each subscriber has a 64-event in-memory buffer. When Publish finds the buffer full, the daemon disconnects that subscriber (closes its channel). The handler returns, the HTTP connection ends, the client sees EOF and reconnects + resyncs. Silent event-dropping was rejected: a client that can't tell it's out of sync is worse than a clean disconnect.
- **Daemon shutdown:** `server.Shutdown` does *not* cancel `r.Context()` on a still-open SSE connection — that only fires when the underlying connection actually closes. The daemon therefore exposes a separate `Server.shutdownCh` channel, closed at the start of `Shutdown()` before `httpServer.Shutdown(ctx)` runs, which the SSE handler watches alongside its event channel. An idle subscriber sees the close, returns, and the HTTP connection ends promptly — without this the graceful-shutdown timeout would always be paid in full when any SSE client was attached. Final cleanup (`hub.Close()`) runs after publishers (forwarder workers, in-flight HTTP handlers) have stopped.

### 4.6 Error response envelope

**All 4xx and 5xx responses use `Content-Type: application/json` with a stable envelope:**

```json
{
  "code": "invalid_adif",
  "message": "missing required <call> tag at position 42",
  "op": "api.v1.qso.submit"
}
```

- **HTTP status code** is the primary signal. Standard semantics — 4xx = client's fault, 5xx = daemon's fault.
- **`code`** is a short, stable, machine-readable identifier. Clients switch on this for behavior. The vocabulary grows naturally as real error cases are encountered during implementation; no preemptive enumeration.
- **`message`** is human-readable for UI display. Clients must not string-match on it.
- **`op`** is the internal `errors.Op` value from the daemon, which maps directly to the `internal/errors` pattern (`errors.New(op).WithErr(err).WithMsg("context")`). Carries operation context from the internal error chain to the client so bug reports name exactly where the error originated.

**Mapping from internal errors to HTTP responses is handler-layer code** in `internal/api`, not in `internal/errors`. The `internal/errors` package stays focused on internal error context and does not grow HTTP-awareness. The handler layer has a small `writeError(w, err)` helper that inspects the error and picks a status/code; the exact mechanism (sentinel errors, typed `HTTPError` wrapper, mapping registry) is a detail to settle when we write the first handler.

**RFC 7807 Problem Details** (`application/problem+json`) is the alternative I considered and did not propose. It's industry-standard but overkill for a project where clients are written by the same person as the daemon and share a Go struct definition via the monorepo. The envelope above is forward-compatible with adding Problem Details fields later if a genuine need appears.

**Deliberately excluded from the milestone 1 envelope:** `details` object for structured context, stack traces, timestamps, request IDs. Any of these can be added as optional fields later without breaking existing clients.

---

## 5. Provisional endpoint sketch

This section is the starting point for review, not the final shape. URLs, methods, and one-line purposes are listed; full request/response bodies are deliberately not fleshed out because most of them will only become clear when the first handler is written against real sqlite data and a real client. Everything here is subject to revision once code exists.

**Endpoints are grouped by the primary consumer that motivates them.** An endpoint may be used by multiple consumers; the grouping reflects whose needs it was added for, not exclusivity.

### QSO submission (all consumers)

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/qso` | Submit a QSO. Body is raw ADIF (`application/x-adif` or `text/plain`). Query param `?force=true` bypasses dedupe check. Response is JSON indicating `"stored"` or `"duplicate"` with the QSO's UUID. |

### QSO retrieval and editing (apps/logging and apps/logbook)

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/v1/qso/:uuid` | Fetch a single QSO by UUID. |
| `PATCH` | `/v1/qso/:uuid` | Edit an existing QSO. JSON body contains fields to update. |
| `DELETE` | `/v1/qso/:uuid` | Soft-delete a QSO (deleted_at, not physical removal, per v1 convention). |
| `GET` | `/v1/qso/:uuid/uploads` | Fetch the per-destination forwarding status for a QSO. Pull-based alternative to `forward.*` SSE events. |

### Logbook management (apps/logging and apps/logbook)

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/v1/logbook` | List all logbooks. Small result set; no pagination. |
| `GET` | `/v1/logbook/:id` | Fetch a single logbook. |
| `POST` | `/v1/logbook` | Create a new logbook. |
| `PATCH` | `/v1/logbook/:id` | Edit logbook metadata (name, callsign, contest association, etc.). |
| `DELETE` | `/v1/logbook/:id` | Soft-delete a logbook. |
| `GET` | `/v1/logbook/:id/qso` | List QSOs in a logbook. Forward-cursor pagination via `?after=<cursor>&limit=<N>`. |
| `GET` | `/v1/logbook/:id/count` | Total non-deleted QSO count for a logbook. Drives the LoggingCard header badge; refetched after each successful submit. |

ADIF export is **not** a daemon endpoint. Clients that need an
ADIF file (session email, full-logbook export, migration) page
through `/v1/logbook/:id/qso` and serialize client-side using
`internal/adif` as a library import. The daemon's
backup/redundancy story is forwarding to online services
(QRZ/LoTW/SM-online), not file export. See the decision log for
the rationale.

### QSO draft support (apps/logging primarily)

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/v1/contact-history?call=<callsign>` | Contact history for a specific callsign — prior QSOs with this station across all logbooks. Drives the "recent contacts" panel when a new QSO is being drafted. |
| `GET` | `/v1/contest-dupe?logbook=<id>&call=<callsign>&band=<band>&mode=<mode>` | Contest-rule dupe check. Per `docs/v1-analysis/invariants.md`, this is a different concept from general ingest dedupe — contest rules vary by contest, and the semantics are "has this station already been worked on this band/mode in this logbook under the current contest rules?" |
| `GET` | `/v1/enrich/callsign?call=<callsign>` | Enrichment pipeline (ADR 0017). Aggregates country / station / source-indicator data for the callsign. **Always 200** — transient upstream failures fall through to "empty fields, source=none" rather than non-2xx, per the `enrichment never blocks logging` invariant. The SPA fires this on Tab-out from the Callsign field. See §7a for the response shape. |

### Events (apps/logging and apps/logbook)

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/v1/events` | Subscribe to the SSE event stream. `Accept: text/event-stream`. Firehose of all daemon events; clients filter on their side. |

### Operational

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/v1/healthz` | Health check. Returns 200 if the daemon is running and its sqlite database is reachable. Used for process monitoring. |
| `GET` | `/v1/version` | Daemon version info (build version, go version, maybe schema version). Diagnostic. |

**This sketch is deliberately incomplete.** Endpoints for upload-queue introspection (e.g., "list all failed uploads"), for batch QSO operations (e.g., "bulk edit all QSOs in a logbook matching a filter"), and for search (e.g., "find QSOs by callsign substring across all logbooks") are not listed because their shape depends on the client UX that hasn't been designed yet. They will appear as they become real.

---

## 6. Explicitly deferred to implementation time

These are known concerns that were raised during session 5 and deliberately not decided now because they are cheaper to answer against running code than in the abstract.

- **Concrete error code vocabulary.** The envelope shape is settled (Section 4.6) but the specific `code` values are not. Will grow naturally from the first few handlers.
- **Internal error typing for HTTP mapping.** Whether the handler layer uses sentinel errors, a typed `HTTPError` wrapper, or a mapping registry to translate internal errors to response codes — to be decided when the first handler is written.
- **Request ID / trace ID propagation.** If added, it would be a middleware concern and a field in the error envelope. Not needed for a single-user desktop deployment; add if a debugging need surfaces.
- **Rate limiting, quotas, request size caps beyond a simple global body size limit.** Not a concern at personal-operator scale *from a malicious actor*. But **accidental self-DoS is a real single-user scenario**: a misbehaving local client — a cron job that submits every second without dedupe, a shell retry loop, a buggy client that reconnects to `/v1/events` in a tight loop — can exhaust sqlite connections, goroutines, or file descriptors and knock the daemon over with HTTP 500s long before any hostile actor enters the picture. The Unix-socket / single-user framing does NOT eliminate this; it just changes *who* the bad client is (yourself, by accident). **Minimal floor (recommended even in milestone 1):** (a) a global concurrent-request cap that returns 503 rather than piling up, (b) a per-endpoint request-rate cap on the submit path so a runaway loop gets 429'd instead of starving forward processing, (c) a generous SSE subscriber cap so a reconnect storm can't spawn unbounded goroutines. All three are ~20–40 LOC of middleware backed by config knobs, and they fail *loud* (429 / 503) instead of failing silent (timeouts, OOM). **Fuller hardening trigger** — per-client quotas, dynamic rate adjustment, fair-share across endpoints — still deferred until a TCP listener, non-owner clients, or a real multi-client workload exists.
- **Authentication beyond socket permissions.** §2 already pins the milestone-1 story: filesystem permissions on the Unix socket ARE the auth. Per §2 this was always intended to be replaceable by middleware without handler changes. **Trigger to revisit:** same as rate limiting — TCP exposure, non-owner clients, multi-user daemon. Likely shape when it lands: a token / pre-shared-key middleware, not username/password; the daemon has no user model. OIDC / OAuth-style flows are firmly out of scope unless the daemon grows a web UI, which it won't.
  > **STATUS NOTE (ST-3a, 2026-08-16).** The trigger has fired — TCP is now the default listener and non-loopback TCP is supported. No auth layer exists yet; ST-3a instead makes the insecure posture **fail-closed and explicitly acknowledged** (`server.allow_insecure_network`) rather than silent. The actual auth layer (browser session for the LAN SPA, or loopback-only + authenticated TLS proxy) is **ST-3b**, an open decision — see ADR 0069.
- **Transport encryption (TLS).** Not applicable while the daemon speaks only over a Unix domain socket (filesystem permissions are stronger than TLS in that context). **Trigger to revisit:** any TCP listener, even loopback, if traffic could be sniffed by a non-owner process. If the daemon ever needs to serve a remote client, TLS is mandatory before that listener binds.
  > **STATUS NOTE (ST-3a, 2026-08-16).** "TLS mandatory for any TCP listener" was not adopted for the **loopback** default (127.0.0.1 traffic is not visible to non-owner processes), which is an accepted no-TLS posture. For **non-loopback** TCP, TLS/proxy is part of the still-open ST-3b topology decision (ADR 0069); ST-3a ships only the acknowledgement gate, not TLS.
- **The forwarder fan-out config shape.** v2's forwarder redesign (to support multi-destination fan-out, replacing v1's hardcoded QRZ) is covered in `docs/v2-design/forwarding.md` (draft 2026-04-18). The API side of it (how clients query forwarder status) is already covered in Section 5; the internal shape lives in that sibling document.
- **Session lifecycle.** Whether v2 has sessions as a first-class concept, and what their endpoints look like, is part of the logging app design in milestone 2.

**Current deploy posture (2026-04-22):** local dev only, supporting logging-app development. Single-user desktop, Unix socket, filesystem permissions. None of the three hardening items above are active concerns in this posture; they are captured here so future-us doesn't forget them the moment the deploy shape changes.

**Status update (2026-05-02 audit):** the "minimal floor" inside the rate-limiting bullet — global concurrent cap, per-endpoint submit rate cap, SSE subscriber cap — has **shipped**; see §7a "Load limits and middleware" for the landed shapes and config knobs. Body-size limit and panic-recovery middleware also shipped. The remaining items in this section (per-client quotas, auth-beyond-socket-perms, TLS, request-ID propagation) are still genuinely deferred per the same triggers.

---

## 7. Anti-waterfall commitment

**This document is a working document, not a contract.** Every decision in it is revisable the moment real code proves it wrong. The purpose of settling the cross-cutting decisions before writing handlers was to avoid building into a corner on the few things that are genuinely painful to change after the fact (pagination model, error envelope, dedupe invariance, async forward lifecycle, "nothing blocks logging" architecture). Everything else is intentionally left vague so the design of the endpoints emerges from the reality of writing them.

**The only decision in this document that is genuinely load-bearing and should not be quietly walked back:** the "nothing blocks logging" invariant and the client-side text-file fallback architecture that implements it. That one is baked into every ingest path from day one; retrofitting it after the logging app is written is expensive, and the cost of getting it right from the start is small. Everything else can evolve.

**Next action after this document commits:** implement `cmd/smd/main.go` actually starting up, opening sqlite via the carry-forward `internal/database/sqlite` service, binding a Unix socket from config, and serving exactly one endpoint: `POST /v1/qso` accepting a raw ADIF body and writing the QSO to the database. No forwarding, no SSE, no pagination, no logbook CRUD. One endpoint that proves the plumbing. Every subsequent decision gets validated against that running code.

---

## 7a. Landed endpoints (current daemon state, audited 2026-05-02)

> **The complete, current endpoint list lives in
> [api-endpoints.md](api-endpoints.md)** (the canonical reference, every route
> with full request/response/error/gating detail). This section is the
> *original* landed-endpoints audit (2026-05-02) and has since drifted — it
> predates the FT8, session-email, hardware-enumeration, and several rig
> endpoints. Treat api-endpoints.md as authoritative for *what exists*; this
> file (the design brief) remains the home for *why*. New endpoints are
> documented in api-endpoints.md, not here.

This section captures the concrete shapes that actually shipped, to
supplement the sketch in Section 5. When the sketch and this section
disagree, **this section is authoritative**; the sketch reflects
design intent, this reflects code.

**Origin note.** The daemon described here is **v2 milestone-1 work
that already landed.** v1 was a Wails app with no daemon process;
the daemon (`cmd/smd`, `internal/api`, `internal/qsoservice`,
`internal/forwarding`, `internal/events`, `internal/safego`, etc.)
was the very first piece of the v2 rewrite, started in the session-8
cluster once the v1 analysis was complete. The `structure.md`
"Migration from main's current state to milestone 1" restructure
already ran: the v1 Wails `apps/` directory is gone, `go.work` is
gone, the daemon and its new internal packages all exist, and
`internal/forwarding/` has been reshaped from v1's hardcoded-QRZ
into the multi-destination `Forwarder` interface + worker +
registry. The current `main` IS the milestone-1 target layout.

Some `internal/` packages are name-stable carry-forwards from v1
(`types`, `adif`, `errors`, `iocdi`, `enums`, `config`, `logging`,
`utils`) — same package paths, reviewed and corrected as they were
brought across. Others (`api`, `qsoservice`, `events`, `safego`)
were created fresh for v2. `internal/database/sqlite/` is the v1
package with the v2 simplified-adapters work merged in. The only
outstanding *structural* addition is `internal/bridge` per ADR 0013,
which is a new package to add when the bridge subsystem lands — not
a reshape of anything that exists.

The `/v1/` in the URL prefix is the **API** version (the API's first
iteration; see §3 "URL-prefixed versioning"), unrelated to the
project's v1 / v2 distinction.

### QSO submission

- `POST /v1/qso?logbook=<id>[&force=true]` — body is ADIF
  (`application/x-adif` or `text/plain`). `?logbook` is **required**;
  the daemon verifies the logbook exists and its callsign matches
  `STATION_CALLSIGN`. Response:
  `{"status":"stored"|"duplicate","uuid":"<uuidv7>","id":<int>}`,
  201 on stored, 200 on duplicate. The `uuid` is the canonical
  external identifier (ADR 0016 phase 1) generated server-side at
  submit time and persisted on the row; clients pin to it for every
  follow-up call. The `id` field is the internal sqlite row id and
  is **transitional** — it stays in the response for one release so
  older logging code keeps compiling, then disappears.

### QSO retrieval and editing

QSO paths route by **UUIDv7** (ADR 0016 phase 2). The path segment
must be a valid UUIDv7 string; mismatch returns
`400 invalid_uuid`. The internal int row id is not exposed on these
paths.

- `GET /v1/qso/{uuid}` — returns `types.Qso` JSON. 404 if missing or
  soft-deleted. Response body carries the QSO's `uuid`; the
  transitional `id` field is also present until the submit-response
  shim is removed.
- `PATCH /v1/qso/{uuid}` — JSON body matching `types.Qso`'s field
  shape. Missing keys leave fields alone; `uuid`, `id`, `logbook_id`,
  `station_callsign`, `dedupe_key`, forwarding-state and enrichment
  fields are immutable (silently restored server-side). Dedupe-key
  inputs (CALL/BAND/MODE/FREQ/QSO_DATE/TIME_ON) trigger a key
  recompute and collision check; collision → 409 `duplicate_key`.
  No `force=true` bypass on edit. **Audit trail (ADR 0016 prep #2):**
  every successful PATCH appends one row to `qso_history` with
  `op='update'`, `source='api'`, and the pre-edit `types.Qso` snapshot
  in `before_image`. The audit-row insert shares the QSO update's tx
  under one-fails-all-fail.
- `DELETE /v1/qso/{uuid}` — soft-delete (`deleted_at`). 204 on success.
  Same audit-trail behaviour as PATCH: one row appended with
  `op='delete'`, `source='api'`, pre-delete snapshot in
  `before_image`, all in the same tx as the soft-delete.

### Logbook management

- `GET /v1/logbook` — list all logbooks. Small result set, no
  pagination.
- `GET /v1/logbook/{id}` — single logbook. 404 if missing.
- `POST /v1/logbook` — JSON `{name, callsign, description?}`.
  Callsign is validated. 409 on duplicate name.
- `PATCH /v1/logbook/{id}` — JSON partial update; `callsign` is
  **immutable** (silently ignored in the body).
- `DELETE /v1/logbook/{id}` — soft-delete. 409 `has_qsos` if the
  logbook still contains QSOs.

### QSO list

- `GET /v1/logbook/{id}/qso?after=<cursor>&limit=<N>` — forward-
  cursor pagination, newest-first by `(qso_date, time_on, id)` DESC.
  Both params optional: `after` omitted on first request, `limit`
  falls back to `Server.DefaultPageLimit` (50), caps at
  `Server.MaxPageLimit` (500).
- Cursor is opaque base64url-encoded JSON `{"d","t","i"}`; clients
  must not parse or construct it.
- Response: `{"items": types.Qso[], "next_cursor": string|null}`.
  `next_cursor` is JSON `null` on the last page.
- Soft-deleted rows are always hidden. Opt-in visibility is deferred
  until the logbook-app needs it.

### Logbook QSO count

- `GET /v1/logbook/{id}/count` — total non-deleted QSO rows for a
  logbook. No pagination, no filters: a single integer is the whole
  point. 404 `logbook_not_found` for an unknown id, mirroring the
  list endpoint so an empty count vs missing logbook stays
  distinguishable.
- Response: `{"logbook_id": <int64>, "count": <int64>}`.
- The SPA hydrates the LoggingCard header from this on boot (after
  `/v1/config` resolves) and refetches on every successful `POST
  /v1/qso` so the displayed count tracks reality without polling.
  Soft-deleted rows are excluded — the same definition the list
  endpoint uses, so `count` and a full pagination walk agree.

### Contest dupe

- `GET /v1/contest-dupe?logbook=<id>&call=<callsign>&band=<band>[&mode=<mode>]`
  — required: `logbook`, `call`, `band`. Optional: `mode` (include
  for band+mode contests like CQ WW, omit for band-only contests
  like ARRL DX). Client owns the contest rule.
- Response: `{"duplicate": bool}`.
- **Contest isolation is achieved via the logbook, not a separate
  DB file.** Hits in other logbooks do not count.

### Event stream

- `GET /v1/events` — SSE firehose. Serves all five event names
  from Section 4.5 with the payloads documented there. No
  backlog, no replay; slow subscribers are disconnected so they
  reconnect + resync. Keep-alive comment every 30 s.
- The handler disables the server's per-request write deadline
  for the duration of the stream (via `http.ResponseController`),
  so an idle-but-connected client isn't force-cut every
  `Server.WriteTimeoutSec`. `ReadTimeout` is unaffected.

### QSO upload status (per-destination forwarding)

- `GET /v1/qso/{uuid}/uploads` — pull counterpart to the
  `forward.*` SSE events. Returns
  `{"items": types.QsoUpload[]}` with one row per
  `(forwarder_name, action)` pair touched for this QSO. Soft-
  deleted QSOs still report status (the delete-action upload row
  is legitimate work and stays observable). 404 only when the
  QSO uuid has never existed in any state. Empty result is
  `{"items": []}`, not `null`.

### QSO draft support

- `GET /v1/contact-history?call=<callsign>` — prior QSOs with
  the supplied callsign across all logbooks. Drives the "recent
  contacts" panel in the logging client. Result count is capped
  by `Server.MaxContactHistoryResults` (default 100). Each item
  carries `uuid` so the client can deep-link rows into the
  logbook viewer.

- `GET /v1/enrich/callsign?call=<callsign>` — enrichment pipeline
  per ADR 0017 (supersedes ADR 0005). Aggregates country and
  station data for the callsign by reading the local domain tables
  (`country` keyed on prefix, `contacted_station` keyed on
  callsign), firing upstream calls (hamnut for country, the
  callsign-class chain for station) on cold misses, and merging
  the layers at the response boundary so the SPA gets a uniform
  shape regardless of cache state.

  **Always 200** per the `enrichment never blocks logging`
  invariant — transport / upstream failures collapse into "empty
  fields, source=none" rather than non-2xx. The 4xx paths are
  reserved for malformed input (`400 missing_required_param` when
  `call` is absent; `400 invalid_field_value` when `call` fails
  the standard 3-32 chars + at-least-one-digit check). Internal
  failures (DB error, etc.) still surface as 5xx via the standard
  error envelope.

  **Cancellation:** `r.Context()` is propagated through to the
  orchestrator's upstream HTTP calls. SPA-side `AbortController`
  on Tab-out cancels in-flight upstream calls promptly.

  Response body shape (`lookup.Result`):

  ```json
  {
    "callsign": "M0CMC",
    "country": {
      "name": "England",
      "prefix": "M",
      "ccode": "GB",
      "continent": "EU",
      "cq_zone": "14",
      "itu_zone": "27",
      "dxcc_prefix": "G"
    },
    "station": {
      "call": "M0CMC",
      "name": "Marc Veary",
      "qth": "Birmingham",
      "gridsquare": "IO92",
      "country": "England",
      "cqz": "14",
      "ituz": "27"
    },
    "country_source": "country_table",
    "station_source": "qrzlookupservice"
  }
  ```

  The `_source` indicators tell the SPA where each layer's data
  came from — useful for the operator's "is this from the local
  cache or did we just fetch it?" mental model:

  - `country_source`: `"country_table"` (cache hit, fresh or
    stale) | `"hamnut"` (cold-miss upstream call) | `"none"` (no
    data — hamnut down or disabled, no cache row).
  - `station_source`: `"contacted_station"` (cache hit) | the winning
    chain provider's service name — today the DI bean name, e.g.
    `"qrzlookupservice"` (review 2026-06-04 L2) | `"none"` (no chain
    provider had a record).

  When a layer's source is `"none"`, its `country` / `station` object is
  **omitted entirely** from the response rather than emitted as an empty `{}`
  (review 2026-06-04 H2). A present layer carries its `last_refreshed_at`,
  including on a cold-miss fetch.

  See `docs/v2-design/enrichment.md` for the read-state matrix
  (9-cell cold/stale/fresh grid), filter+merge sequencing, and
  the async-refresh contract.

### Config (operator-relevant subset of `config.json`)

`/v1/config` is the SPA's read/write endpoint for the operator-edited
parts of `config.json`. The shape is **uniform with `types.X` for every
nested object** (no parallel structs) — `default_logbook` projects
`types.Logbook`, `default_rig` projects `types.RigConfig`,
`logging_station` is `types.LoggingStation` directly. Fields whose
source-of-truth lives elsewhere (DB row, future `cfg.Rigs[]` lookup)
are joined at GET time; the on-disk Config carries only scalar
pointer ids.

- `GET /v1/config` — operator's writable config + joined display
  details. Shape:
  ```json
  {
    "setup_complete": true,
    "logging_station": { "station_callsign": "M0XYZ", ... },
    "default_logbook": { "id": 1, "name": "Default", "callsign": "M0XYZ" },
    "default_rig":     { "id": 1, "model": "...", "port": "..." }
  }
  ```
  Pre-setup (no logbook row, no rigs configured) the joined fields
  are zero-value: `default_logbook` is `{"id": 1}`, `default_rig`
  is `{"id": 1}`, `logging_station` is `{}` (everything omitempty).
  SPA reads `setup_complete` to decide whether to render the setup
  dialog.

- `PUT /v1/config` — same shape; operator-writable subset is honoured.
  - **Writable:** `logging_station.*`, `default_logbook.id`,
    `default_rig.id`.
  - **Server-managed (ignored if present in body):**
    `setup_complete`. The daemon flips it false → true on the first
    PUT that supplies a non-empty `station_callsign`. The flag can
    never go backwards.
  - **Read-only join (ignored if present in body):**
    `default_logbook.{name,callsign,description}`,
    `default_rig.{model,port,...}`. Edit those via dedicated
    resource endpoints when they land (`PUT /v1/logbook/{id}`,
    future rig-config endpoints).
  - **Setup transition side-effect:** when the false → true
    transition fires, the daemon also seeds a logbook row at
    `default_logbook_id` (if no row exists yet) using the operator's
    just-set callsign with `name="Default"`. Idempotent: re-PUT with
    the same or a different callsign updates `logging_station` only;
    the existing logbook row is not rewritten.
  - **ADIF identity materialisation (one-shot, setup transition only):**
    on the same false → true transition, when the request body leaves
    `logging_station.operator` and/or `logging_station.owner_callsign`
    empty, the daemon copies `station_callsign` into the empty
    field(s). Aligns with the ADIF fallback rule (absent `OPERATOR`
    means it equals `STATION_CALLSIGN`; absent `OWNER_CALLSIGN` means
    it equals `STATION_CALLSIGN`). A request that supplies non-empty
    `operator` / `owner_callsign` (club-station case) is honoured
    as-is. Post-setup PUTs do NOT re-seed — operator edits via the
    My Station panel are authoritative, including blanking either
    field.

  Returns `200` with the post-write body shape. Validation errors
  (`invalid_field_value` for malformed callsign, malformed
  `my_gridsquare` (must be 4/6/8-char Maidenhead),
  `my_cq_zone` outside 1–40, `my_itu_zone` outside 1–90,
  `my_dxcc` outside 0–522, `station.amp_multiplier` outside 0–1000,
  or `station.default_power` outside 0–2000; `invalid_json` for
  bad body) return `400`. Disk-write or DB-write errors return
  `500 config_write_error` / `db_error` with a generic wire message
  and the full error in the structured log.

### Operational

- `GET /v1/healthz` — 200 if the daemon is up and sqlite is
  reachable. `{"status":"ok"}`.
- `GET /v1/version` — diagnostic. Shape:
  `{"daemon":"<build>","go":"<runtime>","schema":{"version":N,"dirty":bool}?}`.
  `daemon` is the `-X …/internal/buildinfo.Version=...` ldflag value (defaults
  to `dev`). **NB:** the symbol moved from `main.Version` to `…/internal/buildinfo.Version` on 2026-08-01. `-X` on a symbol that does not exist **exits 0 and stamps nothing**, so a stale `-X main.Version=` silently produces a `dev` build.
  to `"dev"`). `schema` is omitted (not failed) if the
  migration-version probe errors — the rest of the info still
  responds 200.

### Error envelope

All 4xx/5xx responses use the `ErrorResponse` shape from
Section 4.6: `{"code":"<machine>", "message":"<human>", "op":"<package.Func>"}`.

**Shipped error codes (audited 2026-05-02).** The `code` field is the
client's machine-readable signal; clients switch on this. The
`message` field is for human display and may change wording; clients
must not match on it. New codes appear here as endpoints land — do
not preempt the vocabulary.

| HTTP | Code | When it fires |
|---|---|---|
| 400 | `invalid_id` | Logbook path id missing or not a positive integer. |
| 400 | `invalid_uuid` | QSO path UUID missing or not a valid UUIDv7 (per ADR 0016: 36 chars, version nibble `7`, RFC 4122 variant). |
| 400 | `invalid_query_param` | Query param fails parse (e.g. `?force=yes` doesn't pass `strconv.ParseBool`). |
| 400 | `missing_required_param` | Required query param absent (e.g. submit without `?logbook=`). |
| 400 | `missing_required_field` | ADIF body missing CALL / BAND / MODE / QSO_DATE / TIME_ON / FREQ / STATION_CALLSIGN, or JSON body missing required field on logbook create. |
| 400 | `invalid_field_value` | Field value malformed (unrecognised band/mode, bad date/time, callsign too short or no digits, etc.). |
| 400 | `invalid_adif` | ADIF body fails to parse. |
| 400 | `invalid_time_range` | TIME_ON > TIME_OFF without QSO_DATE_OFF on the following day. |
| 400 | `invalid_json` | JSON body is malformed or empty where required. |
| 400 | `too_many_records` | POST `/v1/qso` body parses to more than one QSO record (single-record endpoint; bulk imports use `cmd/importer`). |
| 400 | `callsign_mismatch` | Submit's STATION_CALLSIGN doesn't match the target logbook's callsign. |
| 404 | `not_found` | Path id resolves to no row (or a soft-deleted row, except on `/v1/qso/{id}/uploads` which surfaces delete-action upload status). |
| 404 | `logbook_not_found` | Submit's `?logbook=<id>` doesn't exist. |
| 409 | `duplicate_key` | QSO edit (PATCH) would collide with another row's dedupe key in the same logbook. Submit's same-key collisions resolve to `200 {"status":"duplicate"}` rather than 409 — they're idempotent by design. |
| 409 | `duplicate_name` | Logbook create or rename collides with an existing logbook name. |
| 409 | `has_qsos` | Logbook delete blocked because the logbook still owns QSO rows. |
| 413 | `payload_too_large` | Request body exceeds `Server.MaxBodyBytes` (default 1 MiB). |
| 415 | `unsupported_media_type` | Submit's Content-Type is set and is neither `application/x-adif` nor `text/plain`. Empty Content-Type is accepted as ADIF (curl-without-headers operator path). |
| 429 | `rate_limited` | `POST /v1/qso` token-bucket exhausted (`Server.SubmitRatePerSec` / `SubmitRateBurst`). |
| 500 | `config_write_error` | `PUT /v1/config` failed to atomically rewrite `config.json` on disk (typically permissions or out-of-space). In-memory state is unchanged. |
| 500 | `db_error` | Unspecified sqlite error during a fetch. Wire message is generic ("database operation failed"); the actual error is logged with full op context. |
| 500 | `submit_failed` | Submit pipeline error not classified by `qsoservice.SubmitError`. Wire message generic; logs carry the wrap chain. |
| 500 | `update_failed` | Update pipeline error not classified by `qsoservice.SubmitError`. Same generic-wire / structured-log treatment. |
| 500 | `internal_error` | Panic recovered by `recoverPanic` middleware. Generic message; full stack in logs. |
| 503 | `server_busy` | `Server.MaxConcurrentRequests` cap exceeded for non-SSE traffic, or `Server.MaxEventSubscribers` cap exceeded on `/v1/events`. |

### Field units & canonical forms

- **FREQ:** `types.Qso.Freq` is the **ADIF-native MHz decimal string**
  on the API surface (e.g. `"14.074"`). kHz is only the sqlite
  storage unit, translated in the adapter. Three-decimal-place
  canonical form matches kHz granularity and is round-trip stable.
- **QSO_DATE / TIME_ON / TIME_OFF:** ADIF-native formats (`YYYYMMDD`,
  `HHMM` or `HHMMSS`).
- **CALL / STATION_CALLSIGN:** uppercased at the daemon boundary.
- **BAND:** lowercased (`"40m"`, not `"40M"`).
- **MODE:** uppercased (`"SSB"`, `"CW"`, etc.).
- **`APP_SM_QSO_ID`** — application-defined ADIF field carrying the
  QSO's UUIDv7 on every record the daemon emits (forwarder uploads,
  client-side library exports via `internal/adif`, characterization
  test fixtures). Per ADR 0016 phase 2, the canonical external
  identifier round-trips through the ADIF surface as well as the
  HTTP wire shape so a re-imported export preserves identity. The trust
  boundary is by entry point (review 2026-06-04 H1): `adif.RecordToQso`
  restores it onto `types.Qso.UUID`; the import/restore path
  (`qsoservice.SubmitImport`, used by `smd import`) preserves a valid
  supplied UUIDv7; the public `POST /v1/qso` path (`qsoservice.Submit`)
  always mints a fresh UUID and never adopts a client-supplied one
  (identity-spoofing guard). Emitted
  with `,omitempty` semantics — a QSO with no UUID does not emit an
  empty `APP_SM_QSO_ID` tag, which keeps pre-Phase-1 historical rows
  clean. The `APP_SM_` prefix follows the ADIF spec's
  application-defined field convention; no header declaration is
  required.

### Transport, listener and SPA hosting

The daemon supports both transports the design always allowed for,
and adds an in-binary SPA-hosting mode that the §1 "real consumers"
table didn't anticipate.

- **`Server.Protocol`** — `"unix"` (default) or `"tcp"`. The
  socket path / TCP `host:port` comes from `Server.SocketPath`.
- **Unix-socket cleanup:** stale socket files are removed before
  bind, and removed on graceful shutdown. Bind-then-rename is
  not used (single-binary, no rolling restart story yet).
- **TCP mode** is what makes the embedded SPA reachable from a
  browser. In TCP mode `Server.ServeSPA` defaults to `true`; in
  Unix-socket mode it defaults to `false` (browsers can't reach
  Unix sockets). The flag is explicitly settable to override
  either default.
- **SPA-hosting handler:** when both `Protocol == "tcp"` and
  `ServeSPA == true`, the mux registers a `GET /` catch-all that
  serves the embedded SPA filesystem (`frontend.LoggingFS()`,
  produced by `frontend/logging/dist/` at build time). Path
  resolution: real file → serve as-is; non-existent path →
  rewrite to `/` and serve `index.html` so the SPA's client-side
  router can dispatch. Go 1.22+ pattern routing dispatches
  `/v1/*` first, so the catch-all is naturally bounded to "paths
  that match nothing else."
- **CORS:** the default deployment is single-origin (daemon
  hosts SPA + serves API on the same `host:port`), so no CORS
  headers are emitted. Split-host deployments (separate
  `cmd/bridge` per ADR 0013) are an opt-in shape; the operator
  takes the CORS hit then.
- **Auth posture:** unchanged from §2. Unix socket = filesystem
  permissions are the auth. TCP listener = currently no auth
  layer; revisit per §6 trigger when non-loopback exposure
  appears.
  > **STATUS NOTE (ST-3a, 2026-08-16).** Non-loopback exposure has
  > appeared and is supported. It is now **startup-fatal without
  > `server.allow_insecure_network`** (fail-closed acknowledgement,
  > not an auth layer). Real auth = ST-3b (open). See config.md §5.1
  > + ADR 0069.

### Load limits and middleware (the §6 "minimal floor", shipped)

§6 listed three accidental-self-DoS mitigations as *recommended
even in milestone 1*. All three landed; this section pins their
shipped shapes.

- **Concurrent-request cap.** `limitConcurrent` middleware caps
  simultaneous non-SSE requests at `Server.MaxConcurrentRequests`
  (default 128). Over-budget requests get
  `503 server_busy` with the standard error envelope —
  no queue, no wait. `/v1/events` is exempt because SSE
  connections are long-lived by design.
- **Submit rate limit.** `limitSubmitRate` middleware applies a
  token bucket (`Server.SubmitRatePerSec` rate /
  `Server.SubmitRateBurst` burst, defaults 20 / 40) only to
  `POST /v1/qso`. Over-budget requests get `429 rate_limited`.
  This is per-endpoint on top of the global concurrent cap; a
  runaway logging client gets 429'd before it can starve other
  endpoints.
- **SSE subscriber cap.** `limitEventSubscribers` middleware
  caps simultaneous `/v1/events` subscribers at
  `Server.MaxEventSubscribers` (default 16). Over-budget
  connections get `503 server_busy` and never enter the
  long-lived SSE handler. Released when the handler returns.
- **Panic recovery.** `recoverPanic` wraps the mux; per-handler
  panics are logged through `logging.Service` (with stack,
  method, path) and converted to a `500 internal_error`
  envelope. The daemon stays alive.
- **Body-size limit.** `Server.MaxBodyBytes` (default 1 MiB)
  caps request bodies via the body-reader helper; oversize
  reads return `413 payload_too_large`.
- **Access log.** `logRequests` is the outermost wrapper.
  Emits one structured `INF http request` line per completion
  with fields `method`, `path`, `status`, `duration_ms`,
  `bytes`, `remote`. On 4xx/5xx the line additionally carries
  `code`, `error`, `op` — the envelope classification from
  `writeError` / `writeServerError`, propagated up through the
  `responseRecorder.noteError` hook so the line names *what*
  the failure was, not just its HTTP status. Captures every
  shape of completion uniformly: 2xx/3xx normal returns, 4xx
  from `writeError`, 5xx from `writeServerError` (which also
  emits its own ERR line with the wrapped error chain — the
  access-log line is the request-level summary), 503 from the
  concurrent / subscriber caps, 500 from `recoverPanic`. Status
  defaults to 200 to mirror net/http's implicit-WriteHeader
  behaviour; the recorder forwards `Flush()` so SSE handlers
  keep working through the wrapper. Operators grep `status:5`
  for any 5xx, `status:4` for any 4xx — the level stays Info
  uniformly so a structured extractor can pivot on the field
  rather than the level. SSE connections log once at
  disconnect with the connection lifetime as `duration_ms`.
- **Order in the chain:** outermost first —
  `logRequests → limitConcurrent → recoverPanic → mux →
  per-route (limitSubmitRate | limitEventSubscribers)`. Access
  log sits outside the concurrent cap so 503-rejected requests
  are still observable. Recovery sits inside the concurrent
  cap so a panicked handler still releases its slot via the
  deferred release closure.

### Server-config knobs (current `Server` struct, defaults applied at load)

| Field | Default | Used by |
|---|---|---|
| `Protocol` | `"unix"` | Listener type. |
| `SocketPath` | (no default — required) | Unix path or `host:port`. |
| `ReadHeaderTimeoutSec` | 5 | `http.Server.ReadHeaderTimeout` — slow-headers DoS guard, fixed short cap independent of `ReadTimeoutSec`. |
| `ReadTimeoutSec` | 15 | `http.Server.ReadTimeout`. |
| `WriteTimeoutSec` | 15 | `http.Server.WriteTimeout` (disabled per-handler for SSE). |
| `IdleTimeoutSec` | 60 | `http.Server.IdleTimeout`. |
| `ShutdownTimeoutSec` | 30 | Graceful-shutdown deadline. |
| `MaxBodyBytes` | 1 MiB | Body-size cap. |
| `MaxConcurrentRequests` | 128 | Global cap, non-SSE. |
| `MaxEventSubscribers` | 16 | SSE subscriber cap. |
| `SubmitRatePerSec` | 20 | `POST /v1/qso` token-bucket rate. |
| `SubmitRateBurst` | 40 | `POST /v1/qso` token-bucket burst. |
| `DefaultPageLimit` | 50 | List-endpoint default `limit`. |
| `MaxPageLimit` | 500 | List-endpoint clamp. |
| `MaxContactHistoryResults` | 100 | `GET /v1/contact-history` clamp. |
| `ServeSPA` | `Protocol=="tcp"` | Mounts `GET /` SPA catch-all. |

All defaults are applied in `internal/config.applyDefaults` so a
zero-value `Server` block produces a working daemon. Operators
override individual fields in `config.json`; everything else
falls through.

---

## 8. Related documents

- `docs/v1-analysis/invariants.md` — especially "Nothing blocks logging a QSO, except catastrophic local failure," "Enrichment never blocks logging," "Forwarding never blocks logging," "One-fails-all-fail for QSO writes," "Core concern is log + forward, nothing else," "Contest dupe check and general ingest dedupe are two different things."
- `docs/v1-analysis/lessons-for-v2.md` — "Enumerate all API consumers before designing any endpoints" is the lesson this document exists to honor.
- `docs/v1-analysis/design-decisions-log.md` — "Error handling → `internal/errors` custom error package" (undecided for HTTP serialization, now decided in Section 4.6 above).
- `docs/v2-design/structure.md` — source of the "shared `internal/` tree" and "HTTP+JSON over Unix domain socket" decisions this document inherits.
- `docs/session-handoff.md` — rolling session state, deferred features list (logging-app text-file fallback reconciliation), v1 branch follow-ups.
- Memory: `feedback_no_magic_numbers`, `feedback_one_question_at_a_time` — process rules that shaped how session 5 arrived at the decisions in this document.
