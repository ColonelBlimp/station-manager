# Station Manager v2 — Reusing v1's `internal/cat` and `internal/serial`

**Status:** Draft, last revised 2026-04-20. Initial draft earlier the same day captured the audit + carve-out plan; this revision adds the rig-database and operator-config decisions reached later in the session, and notes the compile-drift issues surfaced after `internal/serial` was copied to main.

**Updates 2026-04-30 / 2026-05-02:** Per ADR 0001 the v2 logging client became a browser SPA, not a Gio binary; per ADR 0013 rig control will live in a daemon subsystem (`internal/bridge`), not in the client process. `internal/serial` and `internal/cat` are already in `internal/` on main (carry-forward from v1) and will be the bridge subsystem's dependencies when it's built. The "first v2 client to need rig control" framing below is preserved as the original analysis context; the consumer is now the daemon's bridge subsystem rather than a Gio app, but the carve-out questions about which v1 parts to reuse vs. rework are unchanged.

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

`cmd/catcli` was relocated from `internal/serial/cmd/catcli/` to top-level `cmd/catcli/` and extended with a `-rig <id>` flag that pipes framed responses through `cat.Decode` for live rig verification (see §7.4).

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

### 3c. Three types, three roles — no composition in `types`

A rig is defined by its serial port settings **and** its CAT command set — you do not have one without the other. But that unity is a runtime concept, not a type-level one. Three distinct types carry different responsibilities:

- **`types.RigConfig`** (canonical operator DTO — `internal/types/rig.go`): what the operator writes in their config file. Stdlib-only fields: `ID`, `Model`, `Port`, and an `Overrides` block that optionally shadows the rig database's defaults. This is what gets JSON-loaded from the operator's config.
- **`cat.RigDefinition`** (rig-database entry — `internal/cat/rig.go`): ships with SM, loaded from embedded `rigs/*.json`. Contains the authoritative CAT command table, state parsers, terminator, and per-rig serial **defaults**. No port name — that's operator-specific.
- **`serial.Config`** (runtime port config — `internal/serial/config.go`): the concrete type `serial.Open` accepts, with `go.bug.st/serial` enum values.

**Why `RigConfig` lives in `types` and not composed with `serial.Config`:** `types` is stdlib-only (load-bearing invariant, see `internal/types/doc.go`). If `RigConfig` embedded `serial.Config`, `types` would transitively import `go.bug.st/serial`, breaking the invariant. Instead, `RigConfig.Overrides` holds primitive stdlib fields (`int`, `string`), and the "put it all together" step happens in a separate composition function.

**The composition** — blending `types.RigConfig` + `cat.Lookup(Model)` → `serial.Config` ready for `serial.Open` — is ~40 LOC: resolve rig-database defaults, layer operator overrides on top, parse the `parity` string and `line_delimiter` string into `go.bug.st/serial` types. Home: a new `internal/rigconfig` package introduced when the logging app is built (first consumer). The FT8/4 app is the expected second consumer and will import the same package. Until one of those apps is under construction, the composition function doesn't need to exist — `cat.Lookup(id)` returning a `RigDefinition` is sufficient infrastructure for the §4 carve-out.

**What this rules out:** bare `serial.Config` at the top of any on-disk config file; a single monolithic "rig" type that tries to be DTO and runtime config at once; `types` importing `serial` or `cat` or `go.bug.st/serial`.

---

## 4. Carve-out plan

Order matters. Skipping step 0 is the thing CLAUDE.md's "characterization tests before refactoring" lesson exists to prevent.

### Step 0 — characterization tests against v1's behaviour — **LANDED session 16**

Status: done. `internal/cat/reference_test.go` + `decode_fixtures_test.go` + `encode_fixtures_test.go` pin the expected behaviour as a frozen reference.

Approach taken: rather than branch-switching to `v1` and running fixtures through v1's actual `lineProcessor`, v1's parser logic is mirrored inline as `referenceLookup` / `referenceDecode` / `referenceEncode` in `reference_test.go`. The mirror is ~60 LOC, byte-for-byte faithful to v1's `internal/cat/internal.go` (lookup) and `processor.go` (decode), and will not change. Fixtures run against the mirror today (14 decode + 9 encode cases, all pass). When `cat.Decode` / `cat.Encode` are written, they must produce identical outputs for the same fixtures.

What's covered:
- Decode: FA/FB frequency extraction, MD mode-plus-VFO with value mappings, case-insensitive prefix lookup, longest-prefix-wins, out-of-range / clamped / empty-slice marker handling, unknown prefix → no match, empty input → no match, v1's "empty string for unmatched value mapping" quirk.
- Encode: plain templates for both rigs, unknown command → error. No `%s` template fixtures because none of the current rig JSONs use template args; add when the first `%s` command lands.

What's not covered (follow-up if needed):
- Binary Icom CI-V framing — no Icom rig JSON exists yet.
- Real rig captures — current fixtures are synthesised from the rig JSON schema. A live capture from the FTdx10 or FT-710 could surface edge cases (partial lines across reads, spurious bytes) that the reference doesn't exercise. The current fixtures are enough to pin the happy path and documented quirks; captures would extend coverage, not change acceptance criteria.
- Verification against v1's actual running code. The mirror is inspectable by eye against v1; if doubt emerges, a git worktree on `v1` can run the same fixtures through v1's real `lineProcessor` at any time.

These tests are the acceptance criteria for the carve-out: `cat.Decode` and `cat.Encode`, once written, must pass the identical fixture tables.

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
| 2026-04-20 | Rig JSONs sourced from v1's `internal/config/defaults.go` | v1's runtime rig config is battle-tested from daily operation. Lifting it verbatim gives us confidence-by-provenance rather than confidence-by-synthetic-validation. Structure choices that came with the lift: command bursts (INIT / READ / PLAYBACK), ALL-CAPS tag names (VFOAFREQ etc.), MD0/MD1 as separate prefixes (not one MD with a VFO marker). |
| 2026-04-20 | FTdx10 rig JSON cross-checked against Yaesu CAT manual (FTDX10_CAT_OM_ENG_2308-F) | Every command, state prefix, marker length, and value mapping in `yaesu-ftdx10.json` was verified against the official CAT reference. All 16 mode codes (incl. `E=PSK`, `F=DATA-FM-N`), `ID P1=0761 → FTdx10`, `FA/FB` 9-digit Hz range `000030000-075000000`, `ST 0/1/2`, `VS 0/1`, `PC 005-100`, `PB0%s;` template, and `AI` behaviour (USB-only, resets to 0 at power-off — hence the `INIT` burst) all confirmed. Only cosmetic gap: manual labels `ST=2` as "SPLIT ON + 5 kHz Up" where v1 renders "ON+" — UX label choice, not correctness. |
| 2026-04-20 | FT-710 rig JSON cross-checked against Yaesu CAT manual (FT-710_CAT_OM_ENG_2306-C) | Two rig-specific differences from the FTdx10 were found and applied to `yaesu-ft710.json`: (1) identity code is `0800` (not `0761` — added `{"0800": "FT-710"}` mapping to the `IDENTITY` state); (2) `SPLIT` only has values `0=OFF` and `1=ON` — no `2=ON+` like the FTdx10, so the `{"2": "ON+"}` value_mapping was removed. Confirmed identical to the FTdx10: all 16 `MD` mode codes (incl. `E=PSK`, `F=DATA-FM-N`), `FA/FB` 9-digit Hz range, `PC` 3-digit range 005-100, `PB0%s;` template, `AI` behaviour (USB-only, resets at power-off). `VS P1` is semantically different (the manual describes it as main-band/sub-band VFO assignment rather than "VFO-A/B operation") but operationally equivalent — kept the v1 `VFO-A`/`VFO-B` labels. |
| 2026-04-20 | End-to-end CAT pipeline validated against a live FTdx10 | `cmd/catcli -device /dev/ttyUSB0 -rig yaesu-ftdx10 -init -listen` successfully sent the `INIT` burst, received the rig's `ID0761;` response (decoded as `IDENTITY: FTdx10`), tracked live `FA` frequency broadcasts as the operator turned the VFO knob (decoded to `VFOAFREQ: <9 digits>`), and decoded mode changes (`MD02 → MAINMODE: USB`). The serial + codec pipeline works. |
| 2026-04-20 | Minimal state table retained — do not expand per-rig state coverage for its own sake | The FTdx10 in AI mode pushes state for ~15 prefixes beyond v1's configured 8 (`IF`, `SS`, `NB`, `RF`, `AC`, `RM`, `RG`, `MG`, `ML`, `GT`, `SH`, `BI`, `KR`, plus the mystery `FD`). v1 ignored them; v2 flags them as `[no match]` in catcli and the decoder returns `ErrNoMatch` at the API level. The logging app only needs frequency, mode, and identity for the QSO record, so this is fine. Expand the state table only when a specific downstream feature needs a specific prefix — don't pre-broaden. |
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

### 7.4 `cmd/catcli` — relocated and extended (closed)

Decided: keep and grow. Session 16 landing:

- Relocated from `internal/serial/cmd/catcli/` to top-level `cmd/catcli/` per `structure.md`.
- Extended with `-rig <id>` flag: when set, catcli looks up the rig in the embedded database, uses its serial defaults, and pipes every framed response through `cat.Decode`, printing the raw bytes plus the extracted tag map on each line.
- Extended with `-init` flag: when set with `-rig`, sends the rig's `INIT` command (via `cat.Encode`) at startup. For Yaesu rigs this enables AI push-state mode.
- Typical workflow: `catcli -device /dev/ttyUSB0 -rig yaesu-ftdx10 -init -listen` — gives you a live decoded state stream from the radio.
- No-rig behaviour preserved: catcli without `-rig` works exactly as before (pure serial diagnostic, raw bytes in/out).

This is also the first end-to-end wiring of `serial.Port` + `cat.Lookup` + `cat.Decode` / `Encode` against real hardware. The inline `serial.Config`-from-`RigSerial` conversion (~15 LOC in catcli) foreshadows what `internal/rigconfig` will do more principally once the logging app needs it.

### 7.5 `types.RigConfig` exact shape

§3c settles the type split (DTO in `types`, rig database in `cat`, runtime config in `serial`, composition in a future `internal/rigconfig`). Remaining opens on the DTO itself:

- **`ID` type — settled session 31 (2026-05-03): `int64`.** Originally `string` ("rig1", "ftdx10-shack") to be an operator-chosen free-form label. No consumer ever materialised that needed a string label: the CAT lib looks up by `Model` (e.g. `"yaesu-ftdx10"`), not `ID`. When `/v1/config` landed and the daemon needed a `default_rig_id` field with a sensible first-run default of `1`, the asymmetry with `Logbook.ID int64` and the awkwardness of defaulting a string surfaced. Converted to `int64`. Blast radius: zero (no consumers in code yet). Doc comment in `internal/types/rig.go` records the change.
- **Which fields go in `Overrides`?** Proposed set: `BaudRate`, `DataBits`, `StopBits`, `Parity`, `LineDelimiter`, `ReadTimeoutMS`. Confirm this covers every realistic operator override — in particular, whether operators ever want to override CAT timing (`listener_interval_ms`) per install.
- **`Port` at top level or inside `Overrides`?** Lean: top level. Operators always set it, always for this install. The rest of the block is rarely touched, so surfacing `Port` makes the common operator JSON cleaner.
- **Zero-value-means-inherit vs pointer-to-override?** Lean: zero-value-means-inherit. Operators rarely set "parity = none" explicitly when the rig default is already none; a zero on the wire is the same as "use rig default." Cost: you can't distinguish "operator explicitly set zero" from "operator omitted the field." This is only a concern if a real override-to-zero case exists; for now none does.
- **Overall operator-config file shape** — does the logging app's config wrap rigs in `rigs: [...]` or something else? That's a logging-app-config decision, not a `types.RigConfig` one.

Settle remaining items when the logging app is being built and the first real config file gets written.

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
- **Mystery `FD` prefix on the FTdx10.** Observed during live rig validation: the FTdx10 emits `FD000<9-digit-Hz>` immediately after every `FA` broadcast when AI mode is on. It is not in the FTDX10_CAT_OM_ENG_2308-F command index. Harmless today (decoder returns `ErrNoMatch`), but worth clarifying — possibly a newer firmware addition, or a non-documented sidecar to FA. Investigate if the logging app ever needs whatever value this carries (possibly the display-formatted frequency vs VFO-A hardware frequency).

---

## 9. Session pick-up point

Data layer + characterization tests landed in session 16:

- `internal/serial` green (builds, tests pass, doc.go published).
- `internal/cat/rigs/yaesu-ftdx10.json` lifted from v1's battle-tested `defaultRigConfigs` in `internal/config/defaults.go` on the `v1` branch — command bursts (INIT / READ / PLAYBACK), 8 state parsers (ID / FA / FB / ST / VS / MD0 / MD1 / PC), v1 tag names (VFOAFREQ, MAINMODE, etc.), mode codes 1-F including `E → PSK` and `F → DATA-FM-N`. Verified against the Yaesu CAT manual (FTDX10_CAT_OM_ENG_2308-F). `yaesu-ft710.json` starts from the FTdx10 schema and is verified against the FT-710 CAT manual (FT-710_CAT_OM_ENG_2306-C): identity `0800 → FT-710`, `SPLIT` restricted to `0=OFF/1=ON` (no ON+). See §6 decision log for full verification notes.
- `internal/cat/rig.go` + `rigdb.go` + `rigdb_test.go`: `go:embed` loader, `Lookup(id)`, `List()`, stubbed `RegisterExternalDir(dir)`. Five tests passing.
- `types.RigConfig` + `types.RigOverrides` shaped per §3c (DTO-only, stdlib-friendly, no composition of `serial.Config` inside `types`).
- §4 Step 0 done: `reference_test.go` mirrors v1's decode + encode logic inline; `decode_fixtures_test.go` (14 cases) and `encode_fixtures_test.go` (9 cases) pin expected behaviour. Together with `rigdb_test.go` the cat package has 29 tests green.

Next:

1. Extract the pure codec (§4 Step 1): `cat.Decode(def RigDefinition, line []byte) (CatStatus, error)` and `cat.Encode(def RigDefinition, name string, args ...any) ([]byte, error)`. Port the logic from `referenceDecode` / `referenceEncode`; the fixture tables in `decode_fixtures_test.go` and `encode_fixtures_test.go` are the acceptance criteria (swap the `reference*` function calls for `cat.*` once the real functions exist).
2. `internal/rigconfig` composition function — landing criterion is "the logging app is under construction and needs it," not calendar. Expected second consumer is the FT8/4 app (if built).
3. §4 Step 2 / Step 3 follow §4 Step 1 once the codec is in place.

Deferred follow-ups:

- §7.4: relocate `cmd/catcli` from `internal/serial/cmd/` to top-level `cmd/catcli/`.
- §7.5: settle the `Overrides` field set and `Port`-at-top-level call against the logging app's real config file.
- §7.8: real implementation of `RegisterExternalDir`.

The §7.1 "carve-out before bridge YAGNI" recommendation still stands: do the carve-out next, regardless of the bridge decision.
