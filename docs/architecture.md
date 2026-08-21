# Station Manager architecture

This is the Tier 1 map of the current Station Manager system. It answers three questions:
what runs, which package owns a concern, and where a change belongs. For exact HTTP and
configuration contracts, use the canonical [API](v2-design/api-endpoints.md) and
[configuration](v2-design/config.md) references. For FT8 operator behavior and event details, use
the canonical [FT8 reference](ft8.md). Code remains authoritative if this map and an implementation
disagree.

This document describes the system that exists now. ADRs and files under `docs/v2-design/` other
than the two canonical references above are records of decisions and earlier designs, not parallel
current-state maps.

## Runtime topology

```text
browser
  └─ embedded Svelte app ──HTTP/SSE──> smd
                                      ├─ API ──> QSO service ──> log SQLite
                                      │                         └─ upload queue
                                      ├─ workers ──HTTPS──────> online logbooks / SM Cloud
                                      ├─ lookup ──HTTP────────> enrichment providers
                                      ├─ bridge ──serial──────> configured rig
                                      └─ FT8 ──audio/RF───────> sound interface + bridge keyer

optional separate host/process
  smcloud ──> PostgreSQL
```

| Runtime | Role | State and boundary |
|---|---|---|
| [`cmd/smd`](../cmd/smd) | The normal local daemon and composition root. It embeds the SPA, exposes `/v1`, and constructs lifecycle, storage, forwarding, enrichment, bridge, and FT8 services. | The daemon defaults to TCP loopback at `127.0.0.1:8080`; the listener and SPA policy are configurable. |
| `smd import` / `smd restore` | One-shot commands dispatched by [`cmd/smd/main.go`](../cmd/smd/main.go). | They use the same config and local databases. They are not resident companion processes, and must not race a running daemon for database ownership. Import does not enqueue uploads unless the operator names a forwarder. |
| [`cmd/smcloud`](../cmd/smcloud) | Optional, separately deployed backup/restore HTTP service. | It authenticates tenants and stores the retentive replica in PostgreSQL through [`internal/cloud/store`](../internal/cloud/store). It is not required for local operation and is not the live logging authority. |
| Other `cmd/*` programs | Build, documentation, migration, and diagnostic tools. | They are not part of the running station topology. |

The browser client is [`frontend/app`](../frontend/app). Its production build is embedded by
[`frontend/embed.go`](../frontend/embed.go) and served by [`internal/api`](../internal/api); there is
no separate frontend server in the installed system.

## Ownership and package boundaries

| Concern | Owner | Responsibility |
|---|---|---|
| Composition and daemon lifecycle | [`cmd/smd`](../cmd/smd) | Loads config, constructs concrete services, supplies cross-package callbacks, declares the lifecycle graph, and turns signals or a requested restart into shutdown. |
| HTTP transport | [`internal/api`](../internal/api) | Owns `/v1` routing, request limits and validation, response/error shapes, SSE handlers, and embedded-content serving. Business transitions delegate to their owning service. |
| Browser behavior | [`frontend/app`](../frontend/app) | Owns the Svelte 5 operator interface and typed HTTP/SSE clients. Browser state is a projection, never the durable source of QSO truth. |
| Configuration | [`internal/config`](../internal/config) | Owns the `config.json` shape, defaults, migration, validation, snapshots, credential redaction contract, and atomic persistence. |
| QSO transitions | [`internal/qsoservice`](../internal/qsoservice) | Owns submit, import/restore, update, delete, dedupe, and upload-queue fan-out rules. It is the domain boundary shared by HTTP and completed FT8 exchanges. |
| Local persistence | [`internal/database/sqlite`](../internal/database/sqlite) | Owns the log database, the `reference.db` cache database, migrations, adapters, queries, and transaction primitives. Generated files under `models/` are not edited by hand. |
| Domain shapes | [`internal/types`](../internal/types), [`internal/adif`](../internal/adif) | `types.Qso` and related values are the shared canonical shapes; `internal/types` remains standard-library-only. ADIF parsing and conversion belong in `internal/adif`. |
| QSO/forwarding notifications | [`internal/events`](../internal/events) | Owns the in-memory `qso.*` and `forward.*` hub consumed by `/v1/events`. It has no durable backlog or replay cursor. |
| Enrichment | [`internal/lookup`](../internal/lookup) | Owns configured lookup providers, their ordering, cache use, and refresh. An external lookup failure may reduce completeness but may not veto a local QSO write. |
| Forwarding | [`internal/forwarding`](../internal/forwarding) and [`internal/forwarding/worker`](../internal/forwarding/worker) | Concrete destination packages own wire/auth behavior. The registry constructs them; workers own queue claiming, retry, submission, and persisted outcomes. |
| Rig/CAT and keyed RF | [`internal/cat`](../internal/cat), [`internal/bridge`](../internal/bridge) | `cat` owns data-driven rig definitions and encoding/decoding. `bridge` owns the serial connection, identity gate, rig event hub, generic exposed commands, and the single-flight guaranteed-stop TX controller. |
| FT8 | [`internal/ft8`](../internal/ft8) | Owns audio capture, slot scheduling, decode, occupancy, operator-started sequencing, FT8 events, and completed-exchange assembly. Bridge keying and QSO logging are injected seams. |
| Lifecycle mechanism | [`internal/iocdi`](../internal/iocdi), [`internal/lifecycle/orchestrator`](../internal/lifecycle/orchestrator), [`cmd/smd/lifecycle.go`](../cmd/smd/lifecycle.go) | `iocdi` records nodes and dependency metadata; the orchestrator executes bounded start/rollback/shutdown; `cmd/smd` owns the actual daemon graph and adapters. |
| Evidence, spotting, and email | [`internal/evidence`](../internal/evidence), [`internal/pskreporter`](../internal/pskreporter), [`internal/email`](../internal/email) | Own their optional sinks and external protocols. They do not own QSO acceptance. |
| SM Cloud service | [`internal/cloud/server`](../internal/cloud/server), [`internal/cloud/store`](../internal/cloud/store) | Owns the separate backup API, tenant boundary, PostgreSQL schema, retentive QSO payloads, and exports. |

The load-bearing import boundary is deliberately stricter than the single-process deployment:

- `internal/database/sqlite`, `internal/forwarding`, and `internal/qsoservice` do not import
  `internal/bridge`; the bridge does not import those packages.
- `internal/ft8` does not import `internal/bridge` or `internal/qsoservice`. `cmd/smd` injects a
  keyer and a completed-QSO sink.
- `internal/types` imports only the standard library. Consumers reuse its shapes instead of
  defining parallel request or configuration models.
- `internal/api` is the transport boundary. It may coordinate endpoint-specific gates, but durable
  QSO rules belong in `qsoservice` and storage mechanics belong in `sqlite`.

These are package-import rules, not a reason to split the default deployment into more processes.

## State and authority

| State | Authority | Consequence |
|---|---|---|
| Operator configuration and local credentials | `config.json`, owned by [`internal/config`](../internal/config) | Writes use an owner-only file mode and atomic replacement. `GET /v1/config` returns redacted credential state, not secret values. The in-memory snapshot changes only after persistence succeeds. |
| Logbooks, QSOs, sessions, and upload rows | The log SQLite database, owned by [`internal/database/sqlite`](../internal/database/sqlite) | This is the live operational authority. A QSO and the upload rows created for it share one transaction. |
| Country/contact lookup cache | `reference.db`, also implemented by `internal/database/sqlite` but wired as a separate service | It is replaceable, best-effort state. A cache failure cannot roll back an already-committed QSO. |
| QSO and forwarding events | [`internal/events`](../internal/events) memory | Notifications are ephemeral. A reconnecting client opens the stream and fetches a fresh baseline from SQLite. |
| Rig and FT8 events | The private hubs inside [`internal/bridge`](../internal/bridge) and [`internal/ft8`](../internal/ft8) | These describe live subsystem state, not a durable history. Each SSE client must tolerate reconnect and resynchronization. |
| Browser session/UI state | Svelte stores under [`frontend/app/src/lib`](../frontend/app/src/lib) | It accelerates operation and renders projections. It does not replace a successful daemon response or SQLite fetch as proof of persistence. |
| Off-site backup | PostgreSQL behind [`cmd/smcloud`](../cmd/smcloud) | It is a retentive replica and restore source. Local SQLite remains authoritative during normal operation. |
| Logs and captured FT8 evidence | The resolved working directory, owned by the logging and evidence services | They support diagnosis and evidence retention; neither is a substitute for the QSO row. |

## Principal flows

### 1. QSO submission

Phone/CW logging takes this route:

1. [`frontend/app/src/main.ts`](../frontend/app/src/main.ts) combines the draft, current rig state,
   station context, and available enrichment into one ADIF record.
2. [`frontend/app/src/lib/api/qso.ts`](../frontend/app/src/lib/api/qso.ts) sends `POST /v1/qso`.
   [`internal/api/handler_qso.go`](../internal/api/handler_qso.go) applies transport limits, parses
   exactly one record, resolves the selected logbook, and calls `qsoservice.Submit`.
3. [`internal/qsoservice/submit.go`](../internal/qsoservice/submit.go) validates and normalizes the
   record, derives daemon-owned identity fields, checks duplicates, and starts a SQLite transaction.
4. The QSO row and one upload row for each enabled, applicable forwarder are written inside that
   transaction. Any failure rolls them all back; only a successful commit means “stored.”
5. After commit, the service logs and publishes `qso.stored`, then warms the contacted-station cache
   best-effort. Cache or later upstream failure cannot undo the QSO.
6. The browser adds the returned UUID to its session view. `/v1/events` lets other views notice the
   change; a normal GET remains the source for a complete baseline.

An FT8 QSO joins at step 3 through a different entry seam. Only after the operator-started sequencer
has completed the exchange does `internal/ft8` emit a `CompletedQso`. The injected sink in
[`cmd/smd/lifecycle_adapters.go`](../cmd/smd/lifecycle_adapters.go) assembles/enriches the record and
calls the same `qsoservice.Submit`. A decode, a queued message, or a partial exchange is not a QSO.

Nearest confusable outcome: an enrichment result or browser toast is not proof of storage. The
successful local transaction and returned UUID are.

### 2. Forwarding

1. The submission/update/delete domain transition creates or re-arms destination-specific
   `qso_upload` rows in the same transaction as the authoritative QSO change.
2. At startup, [`cmd/smd/lifecycle_adapters.go`](../cmd/smd/lifecycle_adapters.go) resets orphaned
   `in_progress` rows, applies the disabled-forwarder policy, builds configured forwarders, and
   starts one worker per destination.
3. [`internal/forwarding/worker/worker.go`](../internal/forwarding/worker/worker.go) claims a bounded
   batch, reloads the QSO, and calls the concrete forwarder. Network, authentication, pacing, and
   response interpretation belong to that destination package.
4. The worker persists success, retry, unreachable, or terminal failure. Where a destination stamps
   an ADIF upload field, the upload success and QSO stamp commit together; row-mirror synchronization
   is triggered only after that commit.
5. Terminal transitions publish `forward.succeeded` or `forward.failed`. These events update clients,
   while the upload row remains the durable explanation of the outcome.

Forwarding is asynchronous and opt-in. An unavailable upstream leaves local logging usable and the
queue retryable; it does not roll back a committed QSO. The optional SM Cloud reconciler compares
manifests to repair replica drift, but it does not sit on the local QSO commit path.

Nearest confusable outcome: “QSO stored” does not mean “uploaded,” and an accepted upstream request
does not mean “complete” until the local outcome write succeeds.

### 3. Rig commands and RF control

1. The browser sends an allowed operation through
   [`frontend/app/src/lib/api/rig-command.ts`](../frontend/app/src/lib/api/rig-command.ts) or uses
   the dedicated tune/FT8 endpoints.
2. [`internal/api/handler_rig_command.go`](../internal/api/handler_rig_command.go) validates a scalar
   command or bounded batch. A frequency-changing command first asks the shared controller to end
   any keyed transmission; if unkey cannot be confirmed, retuning is refused.
3. [`internal/bridge`](../internal/bridge) requires a connected, positively identified rig and a
   command marked `exposed` in the data-driven definition under
   [`internal/cat/rigs`](../internal/cat/rigs). It serializes the write to the configured port.
4. The rig's resulting AUTO/CAT state returns through the bridge hub and `/v1/rig/events`; the
   browser treats the accepted write and later observed state as distinct facts.

Tune and FT8 keying do not use the generic command surface. They share the bridge's single-flight,
guaranteed-stop controller; raw `tx_on` and `tx_off` are never exposed as generic operations. FT8
receives keying, dial-state, and capture callbacks from `cmd/smd`, preserving the import boundary.

Nearest confusable outcome: HTTP `202 Accepted` means the command was written or the controller
accepted the transition. The later rig event confirms observed hardware state.

### 4. Startup and shutdown

[`cmd/smd/lifecycle.go`](../cmd/smd/lifecycle.go) declares the current lifecycle graph and
[`cmd/smd/lifecycle_adapters.go`](../cmd/smd/lifecycle_adapters.go) binds each node to concrete work.
The generic executor is [`internal/lifecycle/orchestrator`](../internal/lifecycle/orchestrator).

Startup follows dependency order: configuration and logging precede the databases and services;
the bridge precedes FT8; storage and the event hub precede forwarder workers; and HTTP starts only
after every service it exposes is ready. A startup failure rolls back only milestones that were
actually reached.

On `SIGINT`, `SIGTERM`, or an accepted self-restart request:

1. Every active node receives a non-blocking `PrepareStop`. HTTP stops admitting new work and worker
   contexts are cancelled.
2. One global timeout bounds shutdown.
3. The active bridge is the `RFCritical` fence and is the only `Stop` attempted until it returns or
   times out. This makes unkey/serial shutdown the first blocking teardown action.
4. The orchestrator then follows real producer/drain edges: FT8 drains before its evidence, spotting,
   and QSO-log consumers; HTTP, workers, and QSO-loggers drain before the event hub; the hub and
   enrichment drain before their databases; logging drains last.
5. If a prerequisite fails or times out, a dependent closer is skipped rather than closing a
   resource underneath a possibly live producer. The returned report records the exceptional path.

Nearest confusable outcome: registration or startup order is not mechanically reversed on shutdown.
`DrainAfter` records data-safety relationships, and the RF fence takes precedence over both.

## External and safety boundaries

| Boundary | Current rule |
|---|---|
| Inbound network | `smd` defaults to TCP loopback. Unix-socket and explicitly acknowledged wider TCP configurations are supported; the exact gates live in the [configuration reference](v2-design/config.md). The embedded SPA is served only on the configured TCP/SPA surface. |
| Outbound network | Enrichment, forwarding, SMTP, PSK Reporter, evidence sync, and SM Cloud are explicit configured consumers. Their errors degrade their own result; only failure of authoritative local storage may prevent a QSO commit. Credential-bearing HTTP destinations use the repository's secure-URL policy. |
| Credentials | Local operator secrets are plaintext in owner-only `config.json` because the daemon must use them; the config API redacts them. SM Cloud receives its DSN and bearer tokens from environment variables. The private dogfood build may inject the ClubLog application key; public release scripts refuse that key and the build-boundary test verifies it. |
| Hardware | [`internal/hardware`](../internal/hardware) discovers candidate devices; config selects them. [`internal/bridge`](../internal/bridge) alone owns the live serial rig connection, and [`internal/ft8`](../internal/ft8) owns configured audio capture/playback. Ordinary tests and documentation work do not touch real devices. |
| RF | An operator starts every FT8 session or tune action. An open FT8 event subscription is the enforced presence signal, not proof someone remains at the desk. Only the shared guarded controller keys TX; generic CAT cannot. No live/keyed test is part of routine verification. |
| Local durability | The log SQLite transaction is the QSO acceptance boundary. Uploads, caches, notifications, logs, evidence, and remote replicas have different durability and must not be described as equivalent to it. |

## Change this here

| Change | Start here | Also update or verify |
|---|---|---|
| Add or change an HTTP route or wire shape | [`internal/api/server.go`](../internal/api/server.go) and the relevant handler | The matching client under [`frontend/app/src/lib/api`](../frontend/app/src/lib/api), tests, and the canonical [API reference](v2-design/api-endpoints.md). |
| Add or change configuration | [`internal/config`](../internal/config) | Settings UI/state, persistence and migration tests, and the canonical [configuration reference](v2-design/config.md). |
| Change QSO acceptance, identity, dedupe, update, delete, or queue fan-out | [`internal/qsoservice`](../internal/qsoservice) | Real in-memory SQLite integration tests and the transaction boundary; do not put the rule only in an HTTP handler. |
| Change the SQLite schema or persistence | [`internal/database/sqlite/migrations`](../internal/database/sqlite/migrations) and adapters/queries | Both log/reference database roles, round-trip tests, and generated-model rules. Do not hand-edit `models/`. |
| Add a forwarding destination | A concrete package under [`internal/forwarding`](../internal/forwarding) and its registry descriptor | Worker outcomes, secure transport, retry/pacing defaults, configuration validation/UI, and credential redaction. |
| Change generic rig operations or a rig definition | [`internal/bridge`](../internal/bridge), [`internal/cat`](../internal/cat), and [`internal/cat/rigs`](../internal/cat/rigs) | The scoped bridge rules, identity/exposure gates, SSE behavior, and offline codec/fixture tests. |
| Change FT8 receive, sequencing, or transmit | [`internal/ft8`](../internal/ft8) | The scoped FT8 rules, [FT8 reference](ft8.md), injected bridge/QSO seams in `cmd/smd`, and offline tests. Any keyed verification needs fresh operator agreement. |
| Change service startup or shutdown | Graph in [`cmd/smd/lifecycle.go`](../cmd/smd/lifecycle.go); concrete transition in [`cmd/smd/lifecycle_adapters.go`](../cmd/smd/lifecycle_adapters.go) | Orchestrator and real-graph lifecycle tests. Add a `DrainAfter` edge only for an actual producer/resource hazard. |
| Change SM Cloud backup/restore | [`internal/forwarding/smcloud`](../internal/forwarding/smcloud) for the daemon client; [`internal/cloud`](../internal/cloud) and [`cmd/smcloud`](../cmd/smcloud) for the service | Tenant isolation, reconcile/restore behavior, deployment runbook, and local-authority semantics. |
| Change architecture or work priority | This file for current ownership/flow; [`AGENTS.md`](../AGENTS.md) for short load-bearing rules | [`docs/backlog.md`](backlog.md) alone owns priority. Record genuinely weighed alternatives in an ADR; do not create another current-state map or ranked worklist. |

Run `task docs:find QUERY=<topic-or-path>` to load the applicable Tier 1 reference and scoped rules
without reading the documentation tree. `task docs:check` validates the live catalog, generated map,
and relative links in live Markdown documents and the two public documentation indexes.
