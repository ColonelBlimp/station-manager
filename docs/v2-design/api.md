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

Per the "enumerate all API surfaces before designing any of them" lesson, the starting point is who actually calls the daemon. There are **four real consumers** in scope across milestones 1 through 3, plus a handful of non-consumers whose exclusion is deliberate.

### Real consumers

| Consumer | Milestone | Primary workflow | SSE consumer? |
|---|---|---|---|
| `apps/logging` | 2 | Real-time QSO entry during active operation. Highest latency-sensitivity. Needs draft init, submit, contact history, contest dupe check, live forwarding status. | Yes — primary. |
| `apps/logbook` | 2 | Management and historical editing. Logbook CRUD, batch QSO edit, list with paging, ADIF export (client-side via `internal/adif` — no daemon endpoint), forwarding status review. Lower latency-sensitivity, larger result sets. | Yes — secondary. |
| `cmd/importer` | 2 | ADIF bulk import from historical logs or other software. One-shot CLI, submits N QSOs and exits. | No. |
| `cmd/udp-bridge` | 3 | Generic UDP-to-daemon bridge. Listens on UDP for ADIF-formatted payloads and forwards them to the daemon's submit endpoint. Not WSJT-X-specific — protocol-agnostic. | No. |

### Non-consumers (deliberate exclusions)

- **`apps/config`** — the configuration editor. Config is **filesystem-resident**: `apps/config` reads and writes `config.json` (or equivalent) directly via the shared `internal/config` package, using the same validation code the daemon uses on load. No daemon API is involved. See Section 3 for the reload mechanism.
- **The serial/CAT bridge** (rig control) — an independent subsystem. Clients that need rig state talk to the bridge directly via its own frontends (rigctld-compat TCP or SM-native NDJSON over Unix socket). The daemon has no opinion on rig state; this preserves the "narrow daemon scope" invariant.
- **SM-Online (future)** — this is a **forwarding destination**, not a consumer. When SM-Online becomes real, the daemon pushes QSOs outbound to it via `internal/forwarding/smonline/` (parallel to the future reintroduction of `internal/forwarding/qrz/`). SM-Online never calls into the daemon's HTTP API.

### Future speculative consumers (not designed for)

- A standalone daemon dashboard or monitoring UI (considered out loud during session 5). Would consume the same SSE stream as `apps/logging` and `apps/logbook`. The event stream is open for future subscribers without schema changes.

---

## 2. Transport and auth

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

**Config reload:** the daemon reads its config file once at startup. Changes made by `apps/config` are not picked up until the daemon is restarted. This is acceptable for milestone 1; a future refinement (file watch, SIGHUP, or an explicit reload endpoint) will be designed when it matters. Not today.

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

- **New QSO:** stored normally, response indicates `"stored"` with the new QSO ID.
- **Duplicate:** no new row written, response indicates `"duplicate"` with the existing QSO ID.

**Minute-granularity** for the time component aligns with the ADIF spec's canonical time resolution and matches how most logging software treats "the same QSO."

**Contest edge case override:** a `?force=true` query parameter on the submit endpoint tells the daemon to skip the dedupe check and store the new QSO unconditionally. This covers rare cases (e.g., some contest rules allow working the same station on the same band/mode more than once within a minute). Normal operators never use it; contest operators who need it opt in explicitly.

**Idempotent submit is the foundation of the deferred reconciliation feature.** Because the submit endpoint is idempotent by default, the logging app can safely replay text-file-buffered QSOs after a daemon outage without worrying about creating duplicates. See `docs/session-handoff.md` → "Deferred features to investigate."

### 4.3 Async forward lifecycle

**`POST /v1/qso` returns immediately after the local sqlite transaction commits**, not after any upstream forwarder (QRZ, ClubLog, SM-Online) has accepted the QSO. The transaction wraps the QSO row insert and one `qso_upload` row per configured forwarder destination, per the "one-fails-all-fail for QSO writes" invariant. Cache writes (contacted_station, country) happen outside the transaction and do not affect the response.

**The forwarding worker runs asynchronously** — a background goroutine that picks up pending `qso_upload` rows, attempts each configured destination, writes per-destination status back to the row, and emits SSE events on terminal outcomes. Retries are handled internally; transient failures do not generate events or bubble back to the submit caller.

**Clients observe forwarding state in two ways:**

- **Pull:** `GET /v1/qso/:id/uploads` returns the current forwarding status of each configured destination for a specific QSO.
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

Payloads are intentionally minimal. Clients re-query via `GET /v1/qso/:id` (etc.) for any details beyond identifiers — the authoritative state is in the database, not on the wire. `logbook_id` is carried on the `qso.*` events so the logbook-app can filter without a fetch; `action` is carried on `forward.*` so a single QSO's INSERT vs. DELETE transitions are distinguishable. `upstream_id` is omitted from `forward.succeeded` when the destination doesn't produce one (e.g. stub).

**Reconnect semantics and buffering:**

- The hub keeps **no backlog**. A client that connects (or reconnects) receives events from that moment forward only. `Last-Event-ID` is not honored — event IDs are monotonic within a daemon lifetime and useful for dedup, but are not a resume cursor.
- Baseline state on connect is reconciled via ordinary GET endpoints. Client contract: **open the SSE stream first, then fetch current state via `GET /v1/qsos` (etc.)**. Any event for an ID the fetch already returned is a no-op; any event for an ID newer than the fetch is applied. Following this order avoids the race where an event fires between the fetch and the subscribe.
- **Slow-reader policy:** each subscriber has a 64-event in-memory buffer. When Publish finds the buffer full, the daemon disconnects that subscriber (closes its channel). The handler returns, the HTTP connection ends, the client sees EOF and reconnects + resyncs. Silent event-dropping was rejected: a client that can't tell it's out of sync is worse than a clean disconnect.
- **Daemon shutdown:** `server.Shutdown` cancels in-flight SSE handlers via request context; remaining subscribers see their channels closed by `hub.Close()`.

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
| `POST` | `/v1/qso` | Submit a QSO. Body is raw ADIF (`application/x-adif` or `text/plain`). Query param `?force=true` bypasses dedupe check. Response is JSON indicating `"stored"` or `"duplicate"` with the QSO ID. |

### QSO retrieval and editing (apps/logging and apps/logbook)

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/v1/qso/:id` | Fetch a single QSO by ID. |
| `PATCH` | `/v1/qso/:id` | Edit an existing QSO. JSON body contains fields to update. |
| `DELETE` | `/v1/qso/:id` | Soft-delete a QSO (deleted_at, not physical removal, per v1 convention). |
| `GET` | `/v1/qso/:id/uploads` | Fetch the per-destination forwarding status for a QSO. Pull-based alternative to `forward.*` SSE events. |

### Logbook management (apps/logging and apps/logbook)

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/v1/logbook` | List all logbooks. Small result set; no pagination. |
| `GET` | `/v1/logbook/:id` | Fetch a single logbook. |
| `POST` | `/v1/logbook` | Create a new logbook. |
| `PATCH` | `/v1/logbook/:id` | Edit logbook metadata (name, callsign, contest association, etc.). |
| `DELETE` | `/v1/logbook/:id` | Soft-delete a logbook. |
| `GET` | `/v1/logbook/:id/qso` | List QSOs in a logbook. Forward-cursor pagination via `?after=<cursor>&limit=<N>`. |

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
- **Transport encryption (TLS).** Not applicable while the daemon speaks only over a Unix domain socket (filesystem permissions are stronger than TLS in that context). **Trigger to revisit:** any TCP listener, even loopback, if traffic could be sniffed by a non-owner process. If the daemon ever needs to serve a remote client, TLS is mandatory before that listener binds.
- **The forwarder fan-out config shape.** v2's forwarder redesign (to support multi-destination fan-out, replacing v1's hardcoded QRZ) is covered in `docs/v2-design/forwarding.md` (draft 2026-04-18). The API side of it (how clients query forwarder status) is already covered in Section 5; the internal shape lives in that sibling document.
- **Session lifecycle.** Whether v2 has sessions as a first-class concept, and what their endpoints look like, is part of the logging app design in milestone 2.

**Current deploy posture (2026-04-22):** local dev only, supporting logging-app development. Single-user desktop, Unix socket, filesystem permissions. None of the three hardening items above are active concerns in this posture; they are captured here so future-us doesn't forget them the moment the deploy shape changes.

---

## 7. Anti-waterfall commitment

**This document is a working document, not a contract.** Every decision in it is revisable the moment real code proves it wrong. The purpose of settling the cross-cutting decisions before writing handlers was to avoid building into a corner on the few things that are genuinely painful to change after the fact (pagination model, error envelope, dedupe invariance, async forward lifecycle, "nothing blocks logging" architecture). Everything else is intentionally left vague so the design of the endpoints emerges from the reality of writing them.

**The only decision in this document that is genuinely load-bearing and should not be quietly walked back:** the "nothing blocks logging" invariant and the client-side text-file fallback architecture that implements it. That one is baked into every ingest path from day one; retrofitting it after the logging app is written is expensive, and the cost of getting it right from the start is small. Everything else can evolve.

**Next action after this document commits:** implement `cmd/smd/main.go` actually starting up, opening sqlite via the carry-forward `internal/database/sqlite` service, binding a Unix socket from config, and serving exactly one endpoint: `POST /v1/qso` accepting a raw ADIF body and writing the QSO to the database. No forwarding, no SSE, no pagination, no logbook CRUD. One endpoint that proves the plumbing. Every subsequent decision gets validated against that running code.

---

## 7a. Landed endpoints (as of session 9, 2026-04-17)

This section captures the concrete shapes that actually shipped, to
supplement the sketch in Section 5. When the sketch and this section
disagree, **this section is authoritative**; the sketch reflects
design intent, this reflects code.

### QSO submission

- `POST /v1/qso?logbook=<id>[&force=true]` — body is ADIF
  (`application/x-adif` or `text/plain`). `?logbook` is **required**;
  the daemon verifies the logbook exists and its callsign matches
  `STATION_CALLSIGN`. Response: `{"status":"stored"|"duplicate","id":<int>}`,
  201 on stored, 200 on duplicate.

### QSO retrieval and editing

- `GET /v1/qso/{id}` — returns `types.Qso` JSON. 404 if missing or
  soft-deleted.
- `PATCH /v1/qso/{id}` — JSON body matching `types.Qso`'s field
  shape. Missing keys leave fields alone; `id`, `logbook_id`,
  `station_callsign`, `dedupe_key`, forwarding-state and enrichment
  fields are immutable (silently restored server-side). Dedupe-key
  inputs (CALL/BAND/MODE/FREQ/QSO_DATE/TIME_ON) trigger a key
  recompute and collision check; collision → 409 `duplicate_key`.
  No `force=true` bypass on edit.
- `DELETE /v1/qso/{id}` — soft-delete (`deleted_at`). 204 on success.

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

### Operational

- `GET /v1/healthz` — 200 if the daemon is up and sqlite is
  reachable. `{"status":"ok"}`.

### Error envelope

All 4xx/5xx responses use the `ErrorResponse` shape from
Section 4.6: `{"code":"<machine>", "message":"<human>", "op":"<package.Func>"}`.

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

---

## 8. Related documents

- `docs/v1-analysis/invariants.md` — especially "Nothing blocks logging a QSO, except catastrophic local failure," "Enrichment never blocks logging," "Forwarding never blocks logging," "One-fails-all-fail for QSO writes," "Core concern is log + forward, nothing else," "Contest dupe check and general ingest dedupe are two different things."
- `docs/v1-analysis/lessons-for-v2.md` — "Enumerate all API consumers before designing any endpoints" is the lesson this document exists to honor.
- `docs/v1-analysis/design-decisions-log.md` — "Error handling → `internal/errors` custom error package" (undecided for HTTP serialization, now decided in Section 4.6 above).
- `docs/v2-design/structure.md` — source of the "shared `internal/` tree" and "HTTP+JSON over Unix domain socket" decisions this document inherits.
- `docs/session-handoff.md` — rolling session state, deferred features list (logging-app text-file fallback reconciliation), v1 branch follow-ups.
- Memory: `feedback_no_magic_numbers`, `feedback_one_question_at_a_time` — process rules that shaped how session 5 arrived at the decisions in this document.
