# Rig profiles — implementation plan

**Status:** **Phase 1 SHIPPED 2026-06-05** (backend config model, switch-by-restart).
Phases 2 & 3 deferred to the config-SPA work. Decision record: **ADR 0028**. Design
principle: **KISS / frictionless for the operator** (memory
`feedback_kiss_frictionless_for_operator`).

## Problem (recap)

A rig's identity — CAT driver + serial port + audio device — is smeared across loose
single-valued fields on two unrelated config blocks (`bridge.cat.driver`,
`bridge.serial.port` in `internal/types/bridge.go`; `ft8.device` in
`internal/types/ft8.go`). One each → the daemon can't represent more than one
attached rig, and switching means rewriting those fields + restarting. ADR 0028
makes a rig a first-class entity in a catalogue with exactly one active at a time.

## Scope split

| Piece | What | When |
|---|---|---|
| **1. Config model** | `rigs` catalogue (reused `RigConfig` +audio); `default_rig_id` selects the active rig; migration; resolve active → run. **Switch = edit `default_rig_id` + restart.** | **SHIPPED 2026-06-05** |
| 2. Discovery endpoint | daemon enumerates serial ports + audio devices with friendly labels | With config SPA |
| 3. Profile-editor UI + **runtime hot-swap** | SPA dropdowns of discovered hardware; active-rig switcher; `POST /v1/rig/select` + live pipeline re-bind (no restart) | With config SPA |

Pieces 2 & 3 have no consumer until the config SPA exists, and that SPA is their
natural driver — building them now would be speculative against an interface the SPA
will actually define. The **no-restart hot-swap** rides with piece 3 because it's
the riskier half (tear down a live pipeline, rebind, reset per-rig state cleanly)
and is triggered by the SPA switcher; Phase 1 delivers switch-by-restart, which is
already a real improvement (keep both rigs configured, flip one token).

The interim does **not** regress KISS — it's identical to today (hand-edit JSON for
the one rig), just in the catalogue shape. The "operator never types a port path /
device id" requirement is the config-SPA's explicit responsibility (pieces 2 & 3).

**Open option (not decided, 2026-06-06):** split piece 3 across the two SPAs by
audience — put the **active-rig selector** (the switcher: pick which configured rig
is live, drives the hot-swap) in the **logging SPA**, where the operator already
works during a session and would want to flip rigs without leaving the logging view;
keep the **profile editor** (add/remove rigs, assign discovered port + audio device +
model) in the **config SPA**, where infrequent setup belongs. The switcher reads the
catalogue + posts `POST /v1/rig/select`; the editor owns CRUD on the catalogue. This
matches the daemon-owns-state / each-SPA-owns-its-UX split (the daemon is the single
source of truth either way), but it's recorded as an option to weigh when the SPAs are
built, not a commitment.

## Config shape (operator-facing JSON)

The catalogue reuses `types.RigConfig` (NOT a parallel struct — see ADR 0028's
rejected-alternative); `default_rig_id` is the active selector.

```json
"rigs": [
  { "id": 1, "model": "yaesu-ftdx10", "port": "/dev/serial/by-id/...01817BF4-if00-port0", "audio": { "device": "" } },
  { "id": 2, "model": "yaesu-ft710",  "port": "/dev/serial/by-id/...01ABC53F-if00-port0", "audio": { "device": "" } }
],
"default_rig_id": 1,

"bridge": { "enabled": true, "timeouts": { ... }, "tune": { ... } },
"ft8":    { "enabled": false, "enable_osd": true }
```

- **Per-rig facts** live in the `RigConfig` entry: `model` (the rigdef id → projected
  onto `bridge.cat.driver`), `port` (→ `bridge.serial.port`), `audio.device` (→
  `ft8.device`). `id` is the int64 selector key (uniform with `default_logbook_id`).
- **`default_rig_id`** selects the active rig — the one the bridge binds *and* the one
  QSOs attribute to (single-active model; they coincide).
- **Cross-rig knobs** stay where they are: `bridge.enabled`/`timeouts`/`tune`,
  `ft8.enabled`/`enable_osd`.
- The catalogue is **top-level**, not nested under `bridge`, because a profile spans
  two subsystems (bridge serial + ft8 audio).
- `RigConfig.overrides` (per-rig serial-param shadowing) exists but is **not yet
  wired** — serial defaults still come from the rigdef, as before. Wiring it is future
  work alongside the picker UI.

## Strategy: resolve-and-project via helpers (as built)

The catalogue is the **source of truth**; the existing loose fields are a **resolved
runtime view** the subsystems still consume — but the projection happens at the seams
via helper methods, NOT by mutating `cfg.Bridge`/`cfg.Ft8` in `Load` (which would
persist redundant derived fields on the next PUT). So the catalogue stays the single
clean on-disk source:

- `Config.ActiveBridge() types.BridgeConfig` — `cfg.Bridge` (cross-rig knobs) with the
  active rig's `Model`→`Cat.Driver` + `Port`→`Serial.Port` projected on.
- `Config.ActiveFt8() types.Ft8Config` — `cfg.Ft8` with the active rig's
  `Audio.Device`→`Device` projected on.
- `Config.RigByID(id) *types.RigConfig` — catalogue lookup.

A resolved active rig **always wins** over any stale loose field left on disk. The
`internal/bridge` and `internal/ft8` package internals are **unchanged** — they still
receive a plain `BridgeConfig`/`Ft8Config`; only the *construction site* in
`cmd/smd/main.go` switched to `cfg.ActiveBridge()` / `cfg.ActiveFt8()`.

Because the loose fields are now resolved views, not on-disk sources, the leaf
identity fields carry `json:",omitempty"` (2026-06-06) so the inert empty values stop
persisting in rewritten configs — hand-setting them has no effect once a catalogue
exists, so they shouldn't linger as misleading on-disk knobs: `Ft8Config.Device`,
`BridgeSerialConfig.Port`, and `BridgeCatConfig.Driver`. The `ft8.device` field drops
entirely (it's a flat leaf on `Ft8Config`); `bridge.serial` / `bridge.cat` still
serialize as empty `{}` objects because their *parent* fields on `BridgeConfig` are
non-pointer structs and Go's `encoding/json` won't omit a zero struct (same as
`bridge.tune:{}`). Fully removing the empty objects would mean making `Serial`/`Cat`
pointer fields — an invasive nil-deref-risk refactor across `ActiveBridge` /
`validateBridge` / the migration / the bridge package — judged not worth it for the
cosmetic gain.

## Resolution + migration (`applyRigProfiles`, `internal/config/config.go`)

Runs in `Load` after `applyDefaults` (so `DefaultRigID` is already its `1` default),
before `validateBridge`:

1. **Legacy config (no catalogue):** if `len(Rigs)==0` and any of
   `Bridge.Cat.Driver` / `Bridge.Serial.Port` / `Ft8.Device` is non-empty →
   synthesize `Rigs[0] = {ID:1, Model, Port, Audio:{Device}}` and force
   `DefaultRigID = 1`. Existing single-rig configs boot unchanged. No loose identity
   at all (bridge-disabled / FT8-only-default host) → no catalogue, nothing to do.
2. **Catalogue present:** validate each entry (`id > 0`, ids unique, `model`
   non-empty); confirm `default_rig_id` resolves to a defined rig (clear error
   otherwise — also catches the multi-rig "forgot to set `default_rig_id`" case, since
   it defaults to 1 and errors if no id-1 rig exists).
3. **No auto-rewrite** of `config.json` on load — resolution is in memory. The
   catalogue persists when the operator (Phase 1: by hand; later: the config SPA)
   next writes config.

## Validation

`validateBridge` now runs against `cfg.ActiveBridge()` — the projected active values —
so the existing "`port`/`driver` required when `bridge.enabled`" gates fire on the
active rig. Per-profile structural checks live in `applyRigProfiles` (above).

## API surface (`/v1/config`)

- The `default_rig` join (`DefaultRig` in the config response, `handler_config.go`) is
  **now live** — `cfg.RigByID(cfg.DefaultRigID)` populates it. It was scaffolded in
  session 31 and deferred "until CAT lands and `cfg.Rigs` is populated"; it is now.
- `bridgeInfoFor` resolves the driver via `cfg.ActiveBridge()` rather than reading
  `cfg.Bridge.Cat.Driver` directly.
- **PUT** round-trips `rigs` + `default_rig_id` automatically — the `Update` closure
  mutates only specific fields and persists the whole (unmutated-elsewhere) config, so
  the catalogue survives a My-Station save. No SPA *writes* to the catalogue in Phase 1
  (that's piece 3); the response doesn't yet surface the raw `rigs` list (no consumer).

## Tests (`internal/config/config_test.go`)

`TestApplyRigProfiles_MigratesLegacy`, `_NoLooseIdentityNoMigration`,
`_ResolvesAndProjects`, `_Errors` (non-positive id / duplicate id / empty model /
dangling default_rig_id), `TestLoad_MigratesLegacyBridge`,
`TestLoad_RigCatalogueRoundTrip`. Bridge/ft8 packages need no new tests — their inputs
are unchanged.

## Deferred to the config-SPA work (pieces 2 & 3)

- **Hardware-discovery endpoint** — enumerate serial ports (go.bug.st/serial
  enumerator: USB VID/PID/serial → friendly label) + audio devices (malgo, as
  `ft8-capture-probe -list` already does).
- **Profile-editor UI** — dropdowns of discovered hardware; pick model from known
  rigdefs; pick port + audio device from discovered lists. Operator never types an
  identifier.
- **Runtime hot-swap** — `POST /v1/rig/select {id}` + Service re-bind (tear down
  current pipeline, re-project, reopen; reset per-rig live state — tune snapshot, hub
  caches, identity confirmation — as cleanly as a disconnect, ADR 0028).
- **Name-based audio resolution** — match `audio.device` name against the
  direction-appropriate capture/playback enumeration at open time (supersedes the
  current index-based `ft8.device`).
- **`RigConfig.Overrides` wiring** — per-rig serial-param shadowing into the bridge's
  `buildSerialConfig` (today the rigdef defaults win).
- **Per-rig `tune` / `mode_mappings` overrides** — only if a real need appears; today
  they're global on `bridge`.

## Deferred — config-shape full cleanup (not SPA-gated)

- **Drop the residual empty `bridge.serial` / `bridge.cat` (and `bridge.tune`) `{}`
  objects from serialized config.** Leaf `omitempty` (2026-06-06) cleared the inner
  `port`/`driver` values, but the parent struct fields on `BridgeConfig` are
  non-pointer, so `encoding/json` still writes empty objects. Removing them means
  making `Serial`/`Cat`/`Tune` pointer fields and threading nil-checks through
  `ActiveBridge`, `validateBridge`, the legacy migration, and the bridge package.
  Deferred (operator, 2026-06-06) — low value, real nil-deref risk; do it as a single
  focused pass when the config shape is next revisited, not piecemeal.

## See

- ADR 0028 — rig profiles, single-active + hot-swap (the decision).
- `internal/types/rig.go` — `RigConfig` (+`RigAudioConfig`).
- `internal/config/config.go` — `Rigs`, `applyRigProfiles`, `RigByID`,
  `ActiveBridge`/`ActiveFt8`, `Load`.
- `cmd/smd/main.go` — bridge + ft8 construction via `ActiveBridge`/`ActiveFt8`.
- `internal/api/handler_config.go` — `bridgeInfoFor` + the `default_rig` join.
- memory `feedback_kiss_frictionless_for_operator`, `project_sm_serial_bridge`.
