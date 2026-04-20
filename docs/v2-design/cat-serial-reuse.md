# Station Manager v2 — Reusing v1's `internal/cat` and `internal/serial`

**Status:** Draft, last revised 2026-04-20. Initial draft earlier the same day captured the audit + carve-out plan; this revision adds the rig-database and operator-config decisions reached later in the session, and notes the compile-drift issues surfaced after `internal/serial` was copied to main.

**Everything in this document is still subject to revision.** Even sections marked "decided" may change once construction starts.

**Purpose:** The v2 logging app (first v2 client to be built) needs rig control. v1 already has working `internal/cat` and `internal/serial` packages. The question is not "write these from scratch" vs "copy them" — it's: which parts drop in as-is, which parts need carving up, and why.

**How this document relates to others:**

- `docs/v2-design/bridge.md` §3c — establishes the v2 layering target (`internal/serial` = byte I/O, `internal/cat` = protocol codec, no I/O). This doc is what it takes to get v1's code into that shape.
- `docs/v2-design/bridge.md` §8.3 — the pluggable-transport abstraction on `internal/cat` that keeps the deferred bridge reachable. That abstraction only makes sense if `internal/cat` is a pure codec, which is what the carve-out below produces.
- `docs/v2-design/milestones.md` — Milestone 1 includes the logging app. Whichever carve-out approach we choose happens under that milestone.
- `docs/v1-analysis/lessons-for-v2.md` §"Characterization tests before refactoring" — the parser extraction below is the exact scenario that lesson was written for.
- `docs/v1-analysis/invariants.md` §"Multi-rig first-class" — the carved-out `internal/cat` must not embed assumptions that a single service instance owns a single port for the process lifetime.

---

## 1. Audit result

### 1a. `internal/serial` — **GREEN architecturally**, minor drift fixes needed

- ~440 LOC implementation + ~1,337 LOC tests across `serial.go`, `transport.go`, `buffer_pool.go`, `config.go`, `errors.go`.
- External dependencies: `go.bug.st/serial` (the underlying port driver). That's it.
- v1-internal imports: `internal/errors`, `internal/types`. No config, no logging, no DI tags, no container references.
- Byte-level I/O only. Delimiter-based framing (split on `;` or `0xFD`), no CAT protocol knowledge.
- Single background reader loop, mutex-serialised writes, context cancellation, buffer pool for allocation efficiency.
- Ships with a `cmd/catcli` utility which we'll either drop or keep as a v2 diagnostic tool (open question §7.4).

**Verdict:** architecturally matches bridge.md §3c. Package was copied onto main 2026-04-20 and a session-16 review surfaced three mechanical drift issues blocking compile (none of them design issues). All resolved the same session:

- `SerialConfig` did not exist on `main`. Added as `serial.Config` in the package itself (not in `internal/types`, because `Config.StopBits` / `Parity` are typed from `go.bug.st/serial`, which would break the stdlib-only `types` invariant). Renamed from `SerialConfig` for Go idiom.
- v1's `internal/errors` used `.Msg()` / `.Msgf()` / `.Err()`. v2's uses `.WithMsg()` / `.WithMsgf()` / `.WithErr()`. All call sites in `serial.go` and `config.go` updated.
- `go.bug.st/serial v1.6.4` added to `go.mod`.

Non-blocking flags from the same review, also resolved: small resource leak in `Open` (now closes the port if `SetReadTimeout` fails); `DEV.md` + `README.md` merged into a Go-conventional `doc.go` (the stale `github.com/Station-Manager/...` import paths came with that consolidation); `cmd/catcli` now traps SIGINT/SIGTERM to close the port cleanly; `Config` struct fields documented with per-field defaults.

Still outstanding — see §7.4: `cmd/catcli` lives at `internal/serial/cmd/catcli/` but per `structure.md` diagnostic binaries belong at top-level `cmd/catcli/`. Relocation is a follow-up, not a blocker.

### 1b. `internal/cat` — **YELLOW**, needs carve-out

- ~265 LOC `service.go` + `listener.go` / `sender.go` / `processor.go` / `helpers.go` / `internal.go` (~500 LOC total impl) + ~1,400 LOC tests.
- External dependencies: `github.com/go-playground/validator/v10` (used once in `helpers.go` to struct-validate `RigConfig`).
- v1-internal imports: `internal/config`, `internal/enums/cmds`, `internal/errors`, `internal/logging`, `internal/serial`, `internal/types`.
- Uses `di.inject` struct tags on `ConfigService` and `LoggerService` — expects v1's IOCDI container.
- Owns a `*serial.Port` directly and runs three goroutines: listener (ticker-driven port read), sender (channel-driven port write), processor (line → `CatStatus` transform using marker-based field extraction from `types.CatState`).
- Lifecycle methods match v2 conventions (`Initialize` / `Start` / `Stop`, idempotent, atomic state flags).

**Mismatch with v2 target layering:** v1's `internal/cat` is a *service that owns a port and orchestrates I/O*, not a stateless protocol codec. `bridge.md` §3c explicitly calls for the codec to be pure — no I/O, no goroutines, no config service. The core logic (prefix matching + marker extraction in `processor.go`; command dispatch in `sender.go`) is the part we want; the orchestration shell is the part we don't.

**Verdict:** reusable in concept, but not as a Go package drop-in. Requires the carve-out below.

---

## 2. Target shape for v2 `internal/cat`

A pure codec package with no I/O, no goroutines, no config service, no logger — plus the embedded rig database (§3).

```
internal/cat/
  codec.go          — Encode(cmd, driver) → []byte; Decode(line, driver) → CatStatus, error
  driver.go         — Driver enum/struct (Yaesu, Icom, Kenwood); owns CatState/CatCommand maps
  driver_yaesu.go   — Yaesu-specific driver registration (ASCII, `;` terminator)
  driver_kenwood.go — Kenwood (same family as Yaesu; separate file for clarity, not separate logic)
  driver_icom.go    — Icom CI-V (binary framing)
  rigs/             — embedded rig definitions (JSON), one file per supported rig
    yaesu-ftdx10.json
    yaesu-ft710.json
  rigdb.go          — //go:embed rigs/*.json; Lookup(id), List(), RegisterExternalDir(path)
  errors.go         — package-scoped error ops
```

Key properties:
- **Pure functions.** `Encode` and `Decode` take inputs and return outputs. No channels, no tickers, no goroutines, no logger, no config service.
- **Driver as data.** The `CatState` / `CatCommand` maps that v1 loads from config at `Initialize` time become part of the `Driver` value constructed by the caller. The codec doesn't read config; the caller hands it a `Driver` it already built.
- **Errors via `internal/errors.Op`** (same convention as v1, already used).
- **No dependency on `internal/serial`.** The codec produces and consumes `[]byte`; what transport carries those bytes is the caller's problem. This is the prerequisite for bridge.md §8.3's `SerialTransport` / `SocketTransport` abstraction.

What moves *out* of `internal/cat`:
- The three goroutines (listener / sender / processor) → caller's responsibility. In the logging app, the caller will be a small glue layer that owns one `serial.Port` + one `cat.Driver` and runs the read loop itself.
- `ConfigService` calls → caller loads `RigConfig` and builds the `Driver` from it before handing it to the codec.
- `LoggerService` calls → codec returns errors; caller decides how to log them.
- DI tags → gone. The codec is constructed with plain Go, not injected.
- `validator/v10` struct validation → moves to the config-load path (wherever v2 lands `RigConfig` validation). The codec trusts what it's handed.

What stays inside `internal/cat`:
- Prefix matching logic from `processor.go` (pick the right `CatState` based on incoming line prefix).
- Marker-based field extraction (index + length + optional value mapping → `CatStatus`).
- Command encoding (given a high-level command like "set mode USB", produce the right wire bytes for the driver).
- The embedded rig database (§3).

---

## 3. Rig database and operator config

v2 splits rig-related configuration into two layers with different lifetimes and owners.

### 3a. Rig database — shared, ships with SM

The set of rigs SM knows how to talk to: CAT command tables, state markers, default serial settings (baud, parity, data bits, delimiter). This is SM's static knowledge of the world — it grows as new rigs are supported but does not change per-operator.

**Where it lives:** `internal/cat/rigs/*.json`, embedded into the binary via `//go:embed` in `rigdb.go`. Any binary that imports `internal/cat` gets the full rig database for free — no per-app embedding, no duplication.

**Why in `internal/cat`:** rig definitions are the data the codec consumes. Putting them anywhere else would force every consumer to bundle the database separately. They are the codec's natural home, not scope creep.

**Initial builtins:**
- `yaesu-ftdx10.json` — the operator's rig 1
- `yaesu-ft710.json` — the operator's rig 2

Both are the same Yaesu ASCII + `;` family, so the driver code path is "Yaesu" once; rig-specific bits are baud defaults, timing, and the exact command table. Adding Kenwood (same family) or Icom (CI-V) later is a new JSON file plus whatever driver-family code path is needed.

**External override — hybrid pattern, stubbed for now:**

Operators can extend or override the embedded database with user-supplied rig definitions. Registered once at package init via:

```go
cat.RegisterExternalDir("/path/to/operator/rigs")
```

Lookup order at resolve time: external dir → embedded → `ErrRigNotFound`. Same rig id in both wins to external, so operators can override a shipped definition without recompiling.

**Stubbed today:** `RegisterExternalDir` exists as a no-op that accepts the call and does nothing. The actual file-walk / JSON-parse / registration loader is deferred — see §7.8. Flagged so nobody mistakes the empty implementation for "we don't support this" when they return to it.

### 3b. Operator config — per-install, tiny

The operator's config tells SM which rigs they own and which USB ports each is on. That is almost all it contains on the rig side. Rough shape (final schema is open, §7.5):

```
rigs:
  - id: "rig1"
    model: "yaesu-ftdx10"     # key into the rig database
    port: "/dev/ttyUSB0"      # always operator-specific
    # optional overrides (baud, delimiter, etc.) — inherit from model if omitted
  - id: "rig2"
    model: "yaesu-ft710"
    port: "/dev/ttyUSB1"
```

The operator config does **not** contain CAT command tables or state markers — those come from the rig database by `model` lookup. The operator config belongs to whichever client owns rig control (the logging app for now); it is not part of the daemon's config, per the narrow-daemon invariant.

### 3c. Rig, CAT, and serial are one unit

A rig is defined by its serial port settings **and** its CAT command set — you do not have one without the other. This has structural consequences for the types:

- `types.SerialConfig` is a **field of** `types.RigConfig`, not a standalone top-level config.
- `types.CATConfig` (command/state tables) is similarly a field of `RigConfig`.
- A rig-database entry (`RigDefinition`, embedded JSON) supplies default `Serial` values and the authoritative `CAT` tables; operator config supplies `id`, `model`, `port`, and optional `Serial` overrides.

This rules out putting a bare `SerialConfig` at the top of any config file. It also means `SerialConfig` can live in `internal/types` as a standalone Go type (it's a shared payload shape), but it never appears as a root-level entity anywhere on disk or in a runtime config.

---

## 4. Carve-out plan

Order matters. Skipping step 0 is the thing CLAUDE.md's "characterization tests before refactoring" lesson exists to prevent.

### Step 0 — characterization tests against v1's behaviour

Before touching `internal/cat`, freeze what it currently does:

- Collect representative input lines for each driver (Yaesu AUTO-mode output samples, Icom CI-V frames, Kenwood responses). Where possible, capture real bytes from the operator's rigs rather than synthesising them.
- Write table-driven tests: `(driver config, input bytes) → expected CatStatus`.
- Write table-driven tests for the send path: `(driver config, high-level command) → expected wire bytes`.
- Run them against the v1 package (on the `v1` branch or via a temporary test harness on main). They must all pass before any carve-out code is written.

These tests are the acceptance criteria for the carve-out: the new pure codec must pass the identical test table.

### Step 1 — extract the pure codec

- New package `internal/cat` on `main` with just the codec (no `Service` struct, no goroutines).
- Copy the prefix-match + marker-extraction logic from v1's `processor.go` into `codec.Decode`.
- Copy the command → bytes logic into `codec.Encode`.
- Build `Driver` value type that holds the `CatState` / `CatCommand` maps (populated by caller).
- Run the Step 0 tests against the new codec. They must pass unchanged.

### Step 2 — caller owns the I/O

- In the logging app (not yet built), write a small `catloop` or similar that:
  - Opens a `serial.Port` (v1's, dropped in unchanged).
  - Constructs a `cat.Driver` from loaded `RigConfig`.
  - Runs the read loop: `port.ReadResponseBytes` → `cat.Decode` → push `CatStatus` onto whatever the logging app's internal channel is.
  - Handles writes: receive command → `cat.Encode` → `port.WriteCommand`.
- This is ~50–100 LOC of glue in the logging app. It's the part v1 generalised into a service; v2 keeps it specific per CLAUDE.md's "build specific, not generic" lesson.

### Step 3 — delete the v1 service shell from v2's `internal/cat`

Once the codec + caller-glue pattern is proven on at least one driver (Yaesu is the natural starting point; it's what v1's tests cover best), the old `Service` / `listener.go` / `sender.go` / `processor.go` orchestration does not come across from v1. Only the logic inside them does.

---

## 5. What this enables downstream

### 5a. Bridge YAGNI path stays open

`bridge.md` §8.3 proposed a `Transport` abstraction on `internal/cat`:

```go
type Transport interface {
    Send(ctx context.Context, cmd Command) error
    Events() <-chan Event
    Close() error
}
func SerialTransport(cfg SerialConfig) (Transport, error) { ... }
func SocketTransport(cfg SocketConfig) (Transport, error) { ... }
```

This abstraction only makes sense if `internal/cat` is a pure codec with no built-in I/O ownership. The carve-out is the prerequisite. With it done, deferring the bridge (the leaning recommendation at the end of session 15) costs nothing — the logging app uses `SerialTransport` today, and if a CAT control app ever lands on the same rig, we add `SocketTransport` and the bridge without touching the logging app.

### 5b. Multi-rig becomes trivial

With `Driver` as a value type and I/O owned by the caller, running two rigs means two `serial.Port` + two `cat.Driver` + two loops. No per-service mutex coordination needed — each rig is its own goroutine(s), owned by whatever app cares.

---

## 6. Decision log

| Date | Decision | Rationale |
|---|---|---|
| 2026-04-20 | `internal/serial` reused as-is | Audit shows it's already a pure byte-level I/O package with minimal dependencies. Matches bridge.md §3c layering target without changes. |
| 2026-04-20 | `internal/cat` carved out into pure codec | v1 shape conflates protocol logic with I/O orchestration. Pure codec is what bridge.md §3c and §8.3 both require. |
| 2026-04-20 | Characterization tests first | Per CLAUDE.md lesson. The codec is value-producing logic where "same input, same output" is exactly what needs freezing before a refactor. |
| 2026-04-20 | I/O orchestration moves to caller (logging app) | "Build specific, not generic." v1's service shell is the generic version; the logging app's ~50–100 LOC glue will be the specific one. |
| 2026-04-20 | Two-layer config model: rig database (shared) + operator config (per-install) | Operator config stays tiny; rig database is SM's static knowledge and scales as new rigs are supported without bloating per-install config. |
| 2026-04-20 | Rig database embedded in `internal/cat` via `go:embed` | Rig definitions are the data the codec consumes. Embedding them in the codec package means any binary importing `cat` gets the database for free — no per-app duplication. |
| 2026-04-20 | Initial builtins: `yaesu-ftdx10.json`, `yaesu-ft710.json` | Operator's two rigs. Both same Yaesu ASCII+`;` family — covers the driver family code path completely on day one. |
| 2026-04-20 | External override via `cat.RegisterExternalDir(path)`, stubbed | Hybrid pattern (embedded + external). Package-level func is ergonomic; realistically one override dir per install. Stub today, implement when a real need emerges. |
| 2026-04-20 | `SerialConfig` / `CATConfig` are fields of `RigConfig`, never standalone | Rig + CAT + serial are one unit. A bare `SerialConfig` at the top of any config file would be meaningless. |

---

## 7. Open questions

### 7.1 Carve-out before or after bridge YAGNI decision?

`bridge.md` §6 is open: build the bridge now or defer? The carve-out described here is a prerequisite either way:
- If bridge deferred (current lean): the carve-out is *all* the CAT work v2 needs until a CAT control app is proposed.
- If bridge built now: the carve-out is step 1, bridge is step 2, logging app is step 3.

Recommendation: do the carve-out next, regardless of the bridge decision. It unblocks both paths and lets the YAGNI question stay deferred without cost.

### 7.2 `internal/enums/cmds` — keep or fold in?

v1's `internal/enums/cmds` holds the command name enum (`SetMode`, `SetFreq`, etc.). It's imported only by `internal/cat`. Options:
- Fold into `internal/cat` as `cat.Command` (one fewer package, tighter cohesion).
- Keep separate if the logging app or other clients need to reference command names without importing the codec.

Lean: fold in. `internal/cat` is the natural home.

### 7.3 `validator/v10` dependency

v1's `internal/cat/helpers.go` uses `github.com/go-playground/validator/v10` for one `validator.Struct(cfg)` call. The carve-out moves validation to the config-load path, which removes this as a `cat` dependency. Whether v2 keeps validator anywhere is a separate question for the config-load path design.

Lean: drop from `internal/cat` for sure; decide for config-load separately.

### 7.4 `cmd/catcli` — relocation outstanding

Decided: keep. The CLI is useful for poking rigs while developing the logging app, and porting was zero-effort (it came across with the rest of the package).

Outstanding: it currently sits at `internal/serial/cmd/catcli/` — an unusual nesting inherited from v1's standalone-package shape. Per `structure.md`, binaries live at top-level `cmd/<name>/`. Move to `cmd/catcli/` when convenient; `doc.go`'s reference to `cmd/catcli` will become accurate once moved. Not a blocker for any current work.

### 7.5 `types.RigConfig` exact shape

§3c sketches `RigConfig = {ID, Model, Port, Serial, CAT}`. The exact field set and JSON schema is still open:
- Which `Serial` fields are operator-overridable vs authoritative-from-model?
- Does `Port` live at top level of `RigConfig` or nested inside `Serial`? Lean: top level — operators always set it, the rest of `Serial` is rarely touched, so surfacing `port` makes the common operator-facing JSON cleaner.
- Final field list for the `RigDefinition` JSON (embedded, not operator-facing).

Resolve when the first rig JSON is actually written.

### 7.6 Kenwood driver status

`bridge.md` §3f closed the "Kenwood as outlier" question — Kenwood is the same ASCII + `;` family as Yaesu. v1's `internal/cat` doesn't ship explicit Kenwood configs (operator's station is Yaesu). For v2, the codec design should make adding Kenwood a data-only change (new JSON file with appropriate maps), not a new code path. Confirm this holds when the codec is written.

### 7.7 Rig JSON schema

The embedded rig-database JSON files need a schema. Fields include: command table (name → wire-bytes template), state table (prefix → field markers), serial defaults (baud, data bits, stop bits, parity, delimiter), timing (listener interval, read timeout). Derive from v1's `types.RigConfig` structure but strip operator-specific fields (`Port`, any overrides — those belong in operator config).

Decide when `yaesu-ftdx10.json` is actually authored. Treat the first JSON as the de-facto schema and formalise once both rigs' files agree.

### 7.8 External override directory loader

`cat.RegisterExternalDir(path)` is stubbed in §3a. A real implementation needs:
- File walk, JSON parse, validation against the §7.7 schema.
- Collision policy: external overrides embedded by `id` (already decided in §3a).
- Error handling for malformed files — skip and log, or fail loud? Lean: fail loud at init, since a broken override directory is a config error the operator should fix.
- Is loading eager at `Register` time, or lazy on first `Lookup`? Lean: eager, so errors surface at startup.

Deferred until a concrete need surfaces.

---

## 8. Not yet addressed

- **Test fixture sourcing.** Step 0's characterization tests need real rig outputs. Simulated bytes work for most cases, but there are edge cases (malformed responses, partial lines across reads, etc.) where real captures are more trustworthy. Plan: capture a representative session from the operator's FT-710 during a normal operating session before starting Step 1.
- **Concurrency contract for the caller.** The carved-out codec is pure, so it's trivially safe for concurrent `Decode` / `Encode` calls. But the caller (logging app's glue) owns a single `serial.Port`, which is not concurrent-safe for simultaneous reads+writes on the same goroutine. The glue pattern needs to be documented once written — probably a single read goroutine + mutex-guarded writes, matching v1's pattern.
- **Error taxonomy.** v1's `internal/cat` returns `errors.Op`-tagged errors. The carved-out codec will too, but the specific ops (`cat.Decode`, `cat.Encode`, `cat.DriverLookup`, `cat.RigLookup`, etc.) get named during Step 1. Not pre-decidable here.

---

## 9. Session pick-up point

`internal/serial` is green on `main` as of session 16 (builds, tests pass, doc.go published). Next session starts with the data layer:

1. Author `yaesu-ftdx10.json` and `yaesu-ft710.json`. The first one forces the schema decision in §7.7; treat it as the de-facto schema and formalise once both files agree.
2. Stand up `internal/cat/rigdb.go` with `go:embed rigs/*.json`, `Lookup(id)`, `List()`, and a stubbed `RegisterExternalDir(path)` (returns nil, does nothing; real loader deferred per §7.8).
3. Define `RigConfig` per §3c and §7.5. Open question: which package owns it? `internal/cat` is the natural home since it composes `serial.Config` and the rig database; putting it in `internal/types` would require `types` to import `serial`, breaking the stdlib-only invariant.
4. With the data layer in place, begin the carve-out: §4 Step 0 (characterization tests against v1) → Step 1 (extract pure codec) → Step 2 (caller-owned I/O glue in the logging app).

The §7.1 "carve-out before bridge YAGNI" recommendation still stands: do the carve-out next, regardless of the bridge decision.

Relocation of `cmd/catcli` (§7.4) is a separate low-priority follow-up; fit it in whenever.
