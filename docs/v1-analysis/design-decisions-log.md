# Station Manager — Design Decisions Log

**Status:** v1 analysis document, 2026-04-14. A catalog of every major shape decision identified during the v1 analysis, with an explicit verdict: **keep** / **change** / **delete** / **undecided**. Each entry has an action (if any) and rationale.

**Purpose:** Before v2 starts (or before v1 refactor lands, whichever path is chosen), every major design decision needs an explicit judgment. This document makes those judgments and the reasoning behind them visible.

**How to read this document:** entries are grouped by subsystem. Verdicts mean:
- **Keep** — the decision is correct as-is; preserve verbatim in v2 or leave alone in v1.
- **Change** — the decision is right in spirit but the implementation needs fixing.
- **Delete** — the decision was wrong or the code is dead; remove without replacement.
- **Undecided** — blocked on another decision that hasn't been made yet, or genuinely needs more thought.

---

## Data model and storage

### `types.Qso` follows the ADIF specification — **KEEP**

**Decision:** `types.Qso` is shaped to mirror ADIF. Every ADIF tag has (or will have) a corresponding field. Nested Go sub-structs (`QsoDetails`, `ContactedStation`, `LoggingStation`, `CountryDetails`) are an organizational convenience; ADIF itself is flat and the nesting has no semantic load.

**Rationale:** ADIF is the format the ham radio world speaks. Making the application type track ADIF directly means new ADIF fields are a one-line change to the application type, and the data shape never drifts from the spec. This is a load-bearing decision for the whole data model.

**Action:** keep verbatim in v2. Add doc comments on `types.Qso` explicitly stating the ADIF-alignment intent so future readers don't have to re-derive it from context.

**Related:** `invariants.md` → "types.Qso follows ADIF"; `lessons-for-v2.md` → "Document intent, not just mechanism."

### `additional_data` JSON blob column for non-promoted fields — **KEEP**

**Decision:** Database row types have **real columns only for queryable/indexed/filterable fields** (`call`, `band`, `mode`, `freq`, `qso_date`, `time_on`, etc.). Everything else is serialized into an `additional_data` JSON blob column.

**Rationale:** absorbs ADIF spec evolution without schema migrations. Adding a new ADIF field is a one-line change to `types.Qso`; the storage carries it through automatically via JSON marshaling. Promoting a field to a real column is a deliberate feature decision (indexing, querying, filtering), not an obligation. This is the right shape for a data model where the field set is defined by an external evolving spec.

**Action:** keep verbatim in v2. Document the pattern in the `internal/database/<driver>/adapters` package's `doc.go` so future readers understand the rationale.

**Related:** `invariants.md` → "additional_data absorbs ADIF evolution"; `bug-inventory.md` → "QsoAdditionalData intermediate struct" (the v1 implementation has a bug that violates the pattern's goal).

### Per-database adapter pattern scoped to each driver — **KEEP**

**Decision:** Each database driver (sqlite, postgres if it comes back) gets its own adapter package under `internal/database/<driver>/adapters/`. They share only `internal/types`. No generic cross-backend adapter layer.

**Rationale:** the generic attempt (`internal/adapters/`) was abandoned as too complicated and too hard to use correctly. Per-driver duplication of small amounts of mechanical field-copying is cheaper than maintaining a framework that tries to abstract over different backends' conventions.

**Action:** keep. Simplify the sqlite version (see next entry). Delete the generic version (see separate entry).

### `types.QsoAdditionalData` intermediate struct in sqlite adapter — **CHANGE (delete and simplify)**

**Decision:** Delete `types.QsoAdditionalData` entirely. Simplify the sqlite adapter to use `json.Marshal(qso)` for the blob and explicit column assignments for the promoted fields.

**Rationale:** the intermediate struct is "good design, unfortunate implementation." The `additional_data` pattern is correct, but the v1 implementation goes through a separate hand-maintained struct (`QsoAdditionalData`) as an intermediate form for the forward direction, which:
- Creates a second shape that has to be manually kept in sync with `types.Qso` (violating the "new ADIF field = one edit" invariant).
- Produces 100+ lines of manual field-copying in `type_to_model.go`.
- Makes the forward and reverse adapter directions asymmetric (forward goes through the intermediate struct, reverse unmarshals the blob straight into `types.Qso`).

The fix is straightforward: let `json.Marshal(qso)` produce the blob directly. Promoted fields end up duplicated (once in the column, once in the blob — trivial cost, ~50 bytes per row), and on read the column values overlay the blob values (columns are authoritative). New ADIF fields become one-line changes as the invariant requires. Adapter code collapses from ~200 lines across both files to ~50 lines.

**Action:** fixable in v1 as a cleanup commit (one of the "code-level, not architecture-level" issues). If v2 is rebuilt from scratch, use the simplified shape from day one and never build `QsoAdditionalData`.

**Related:** `bug-inventory.md` → "QsoAdditionalData"; `lessons-for-v2.md` → "Mostly-blob + promoted fields pattern," "Asymmetric round-trips are a clue."

### `internal/adapters/` — generic reflection-based adapter framework — **DELETE**

**Decision:** Delete `internal/adapters/` including the whole `converters/{common,sqlite,postgres}/` subtree.

**Rationale:** This was a sophisticated reflection-based struct-to-struct adapter framework with field converters, tag-based ignores, and `AdditionalData` handling. 30+ test files, builder API, generics. Author has confirmed it is "abandoned as too complicated to maintain and use correctly." It represents the generic-across-backends approach that was explicitly rejected in favor of the per-driver pattern.

**Action:** delete from the repo as part of v1 cleanup. Do not carry into v2. Do not conflate with `internal/database/sqlite/adapters/`, which is a different and much smaller package that is being kept.

**Related:** `bug-inventory.md` → "internal/adapters generic framework"; `lessons-for-v2.md` → "Build specific, not generic."

### `internal/database/postgres` — **RELOCATE (stage for future server repo)**

**Decision:** Move `internal/database/postgres/` to the future SM-Online server repo (whenever it exists) rather than keeping it in the client repo.

**Rationale:** postgres was originally for a planned public online SM server. It has diverged from the sqlite package and is not used by any current client code. The generic-across-backends adapter attempt (which is being deleted) was partly motivated by wanting to share code between sqlite and postgres, but with that attempt abandoned, postgres has no active role in the client repo.

**Action:** in the short term, tag the current state and remove postgres from this repo. Recreate it in the future server repo when that server is actually being built. Until then, "server design" is a blank slate and the client doesn't carry its dead weight.

**Related:** `bug-inventory.md` → "postgres package relocation."

### ORM / query generator choice (sqlboiler, Bob, sqlc, hand-rolled) — **UNDECIDED**

**Decision status:** undecided, revisit during v2 DB layer design.

**Rationale:** v1 uses sqlboiler. Alternatives are Bob (sqlboiler successor), sqlc (query-first code generation), or hand-rolled SQL + structs. The user's key insight: **the transformation layer between DB rows and application types exists regardless of which generator you pick**, because `types.Qso` is your application-facing type and the generator produces database-row types, and those two shapes are never identical no matter who generated them. The generator choice affects ergonomics (query surface, type safety, debuggability of generated code) but not whether the adapter exists.

**Action:** defer until v2 DB design starts in earnest. Evaluate Bob (migration path from sqlboiler), sqlc (query-per-type approach changes where the tension lives), and hand-rolled (maximum control, most boilerplate) against the ADIF-alignment + additional_data pattern. The generator should support the pattern efficiently; whichever one makes the adapter simplest wins.

**Related:** `lessons-for-v2.md` → the generator choice is not a data-model decision, it's an ergonomics decision.

## Dependency injection

### `internal/iocdi` home-grown IoC/DI container — **KEEP**

**Decision:** Keep the home-grown DI container.

**Rationale:** The author considers it lightweight enough and flexible enough for managing service lifecycle and dependency wiring, and prefers it over alternatives like Uber fx or Google wire. DI containers in Go are controversial; for a solo personal project, the author's preference for a hand-built minimal solution over a heavier framework is reasonable.

**Action:** keep. Revisit only if the maintenance burden grows or a lighter alternative surfaces that the author prefers.

## Enums

### `internal/enums/*` per-concept subpackage split — **KEEP**

**Decision:** Keep the per-concept subpackage organization (`bands`, `cmds`, `events`, `modes`, `tags`, `upload`, with `upload/action` and `upload/status` as further subpackages).

**Rationale:** unusual for Go (most codebases would inline enums into the package they belong to), but the split grew organically from a single `internal/enums` package and the author finds it cleaner than inlining. It keeps related enums together and makes them easy to locate. Not a common pattern, but it works for this project and has no downside.

**Action:** keep. Don't try to "fix" an unusual-but-working pattern just because it's unusual.

## Application structure

### Three-concerns split across `apps/logging` / `apps/logbook` / `apps/config` — **KEEP**

**Decision:** Preserve the three-Wails-apps split in v2, even after the daemon decomposition.

**Rationale:** these are three distinct user workflows with different latency profiles, access patterns, and UI focus. Merging them would produce a bloated one-app that's bad at each of its jobs. The split is deliberate, not accidental.

- **`apps/logging`** — real-time QSO entry, high-frequency, low-latency.
- **`apps/logbook`** — management and historical editing, low-frequency, handles large datasets.
- **`apps/config`** — settings, rare-use, focused UI.

**Action:** v2 daemon API must serve all three concerns as first-class slices, not just logging. The logging-centric API sketch produced earlier in session discussions was missing the logbook-management surface entirely. A dedicated API-shape exercise for v2 needs to enumerate logbook-management endpoints (logbook CRUD, batch QSO edit, export to ADIF, etc.) alongside the logging endpoints.

**Related:** `invariants.md` → "Three-concerns split must carry forward"; `lessons-for-v2.md` → "Enumerate all API surfaces before designing any of them."

### Core concern is "log + forward"; everything else is a client — **KEEP**

**Decision:** The daemon's core is narrow: log QSOs locally, forward to online services. Everything else (capture, rig control, FT8 protocol, WSJT-X ingest) is a client of this core.

**Rationale:** v1 conflated capture with storage. Adding FT8 and WSJT-X support exposed this as a structural mistake that required pulling FT8 into its own repo and left the WSJT-X listener as dead code. The narrow-core decision is what keeps the daemon stable while clients can churn freely.

**Action:** carry forward into v2. The daemon has a minimal, narrow scope; everything else is separate.

**Related:** `invariants.md` → "Daemon scope is explicitly narrow."

## Forwarding

### Hardcoded QRZ forwarder in `LogQso` / `UpdateQso` — **CHANGE (redesign fan-out)**

**Decision:** Replace the hardcoded `upload.OnlineServiceQRZ` in `facade.go` with a dynamic loop over configured forwarders, reading from `ForwarderConfigs()` in the config service.

**Rationale:** v1 hardcodes a single forwarder (`upload.OnlineServiceQRZ`) at the ingest site. Any additional forwarder (ClubLog, future SM-Online, email) requires editing the hardcoded site. This also blocks a proper multi-forwarder design where each QSO gets one `qso_upload` row per configured destination. The design question — "what's the right shape for configured forwarders" — is underdetermined in v1 because there's never been more than one.

**Action:** redesign the forwarder configuration model as part of the v2 daemon's upload queue API. Probably: `ForwarderConfig` has a name, a service type, enabled/disabled, an action filter (some might handle inserts only, some might handle updates too), and per-destination credentials. `LogQso` iterates configured-and-enabled forwarders and queues one upload row per (QSO, destination) pair. In v1, this is an **undecided** design that the author has explicitly flagged as "probably needs a re-design" — defer to v2 DB design.

**Related:** `bug-inventory.md` → "Hardcoded QRZ forwarder."

### `internal/listeners/handlers/wsjtx/` — WSJT-X UDP listener — **DELETE**

**Decision:** Delete the WSJT-X listener code. Do not carry into v2.

**Rationale:** this code is non-functional in v1 and always has been. WSJT-X and JTDX require exclusive serial-port access for CAT/PTT, which conflicts with the user's own serial library. There is no working configuration in which the UDP ingest path from WSJT-X actually runs end-to-end. It was written in anticipation of an approach that the serial contention made impossible.

V2's plan is different: WSJT-X (or whatever FT8 code is used) logs QSOs via the daemon's HTTP API, through a separate `wsjtx-bridge` client process that translates UDP → HTTP. The bridge runs alongside the serial/CAT bridge (which is a prerequisite for WSJT-X being able to run at all on the same rig Station Manager controls). The `wsjtx-bridge` client has nothing to do with the current `internal/listeners` framework.

**Action:** delete `internal/listeners/handlers/wsjtx/`. Investigate whether `internal/listeners/` itself is the only remaining consumer — if WSJT-X was the only handler, the generic listener framework may also be dead and can be deleted.

**Related:** `bug-inventory.md` → "WSJT-X listener is dead code"; `architecture-map.md` → observation #9.

### Interface vs concrete-type mismatch in facade — **CHANGE (resolve the mismatch)**

**Decision:** Either make `DatabaseServiceInterface` truthful (match the concrete `*sqlite.Service` signatures), or delete the interface entirely and use the concrete type directly.

**Rationale:** v1 has a `DatabaseServiceInterface` defined in `apps/logging/backend/facade/interfaces.go` whose method signatures do not match `*sqlite.Service` (the interface uses `interface{}` for action/service params; the concrete type uses typed enums). The field in the facade's Service struct is declared as `*sqlite.Service` directly, not via the interface. The interface exists only to satisfy `MockDatabaseService`, which is never actually instantiated in any test. This is aspirational scaffolding with no actual use.

**Action:** resolve the mismatch as part of v1 cleanup. Options: (a) update the interface to match, (b) delete the interface and its mocks, (c) update the concrete type's signatures to match the interface (probably wrong — the typed-enum version is better). Recommended: (b) for immediate cleanup, or (a) if you actually want interface-based mocking for real tests.

**Related:** `bug-inventory.md` → "DatabaseServiceInterface mismatch."

## Error handling

### `internal/errors` custom error package with `errors.Op` pattern — **UNDECIDED (for HTTP serialization)**

**Decision status:** keep the internal usage; undecided about HTTP serialization.

**Rationale:** the `errors.Op` + `.Err(err).Msg(...)` pattern works well as internal error-tagging and context-carrying. The open question is how errors serialize over the HTTP API in v2: do they become structured JSON error bodies with operation codes, or does the handler layer translate them into plain HTTP status codes + simple message strings? The first approach carries the internal error richness to clients; the second is simpler but loses information at the boundary.

**Action:** defer until v2 HTTP handler design. Don't redesign the internal errors package in the meantime — it works.

**Related:** `lessons-for-v2.md` → internal patterns and wire patterns are different design problems.

## Transport

### Log/forward daemon: HTTP+JSON/ADIF over Unix domain socket + SSE events — **DECIDED**

**Decision:** HTTP over a Unix domain socket, JSON for structured data, ADIF as raw POST body for submit endpoints, Server-Sent Events for push (qso.stored, forward.*, session.*, logbook.*).

**Rationale:** see earlier session discussion. Unix socket gives filesystem-permissions auth for the single-user case. ADIF as raw body lets any ADIF-producing tool POST directly without translation. SSE is enough for push; websockets aren't needed because bidirectional is a rig-bridge concern, not a log-daemon concern. gRPC considered and rejected.

**Action:** v2 daemon implements this shape. Capture in the v2 API spec document (not yet drafted).

### Serial/CAT bridge SM-native frontend: NDJSON over Unix socket — **DECIDED**

**Decision:** Newline-delimited JSON over a Unix domain socket. One bidirectional connection per client, each line a JSON object with a `type` field. No HTTP layer.

**Rationale:** the rig bridge's traffic is continuous bidirectional streaming (AUTO/transceive push from the rig, commands going back), not request/response. HTTP+SSE is the wrong rhythm. NDJSON gives one socket per client, connection-lifetime = lease-lifetime for free, debuggable with `socat`, ~30 lines in Go.

**Action:** v2 serial/CAT bridge implements this frontend (alongside the rigctld-compat TCP frontend for third-party interop).

## Client identity and server architecture

### `internal/apikey` placement — **UNDECIDED**

**Decision status:** undecided, blocked on v2 client/server split design.

**Rationale:** the API key handling exists in the shared internal library but most of its logic belongs to either the client side (store the full key, send with requests) or the server side (hash/validate/revoke), and the two sides share almost nothing beyond a key-format spec.

**Action:** defer until v2 client/server split is designed. Probable eventual outcome: two independent `apikey` packages (one per side), sharing only a constants file for the key format. Don't try to solve this now.

---

## Still-open entries (appended as they come up)

Use this section for decisions that surface during future sessions and haven't been classified yet. Each entry should be promoted into the numbered sections above once a verdict is reached.

*(No entries yet.)*
