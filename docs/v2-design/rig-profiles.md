# Rig profiles — implementation plan

**Status:** Phase 1 planned (backend, switch-by-restart). Phases 2 & 3 deferred to
the config-SPA work. Decision record: **ADR 0028**. Design principle:
**KISS / frictionless for the operator** (memory `feedback_kiss_frictionless_for_operator`).

## Problem (recap)

A rig's identity — `driver` + serial `port` + audio `device` — is smeared across
loose single-valued fields on two unrelated config blocks (`bridge.cat.driver`,
`bridge.serial.port` in `internal/types/bridge.go`; `ft8.device` in
`internal/types/ft8.go`). One each → the daemon can't represent more than one
attached rig, and switching means rewriting those fields + restarting. ADR 0028
makes a rig a first-class, named entity in a catalogue with one active at a time.

## Scope split

| Piece | What | When |
|---|---|---|
| **1. Config model** | `rigs` catalogue + `active_rig`; per-rig `driver`/`serial`/`audio`; migration; resolve active → run. **Switch = edit `active_rig` + restart.** | **Now (this plan)** |
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

## Config shape (operator-facing JSON)

```json
"rigs": {
  "ftdx10": {
    "driver": "yaesu-ftdx10",
    "serial": { "port": "/dev/serial/by-id/usb-Silicon_Labs_CP2105_..._01817BF4-if00-port0" },
    "audio":  { "device": "" }
  },
  "ft710": {
    "driver": "yaesu-ft710",
    "serial": { "port": "/dev/serial/by-id/usb-Silicon_Labs_CP2105_..._01ABC53F-if00-port0" },
    "audio":  { "device": "" }
  }
},
"active_rig": "ftdx10",

"bridge": { "enabled": true, "timeouts": { ... }, "tune": { ... } },
"ft8":    { "enabled": false, "enable_osd": true }
```

- **Per-rig facts** live in the named `rigs` entry: `driver`, `serial` (reuses
  `BridgeSerialConfig` → `{port}`), `audio` (`{device}` — single, name-based;
  one physical device → one choice, daemon resolves capture/playback direction
  internally per ADR 0028 discussion).
- **Cross-rig knobs** stay where they are: `bridge.enabled`/`timeouts`/`tune`,
  `ft8.enabled`/`enable_osd`.
- The catalogue is **top-level**, not nested under `bridge`, because a profile spans
  two subsystems (bridge serial + ft8 audio).

## Strategy: resolve-and-project (Phase 1)

The catalogue is the **source of truth**; the existing loose fields become a
**resolved runtime view** the subsystems already consume. At config load, after
parse + migration, project the active profile:

```
Bridge.Cat.Driver = active.Driver
Bridge.Serial     = active.Serial          // BridgeSerialConfig
Ft8.Device        = active.Audio.Device
```

So `internal/bridge` (reads `cfg.Cat.Driver` at command.go:61, pipeline.go:121,
tune.go:90/200; `cfg.Serial` at pipeline.go:130) and `internal/ft8` (reads
`cfg.Device` at source_cgo.go:42) and `cmd/smd/main.go` (bridge.New(cfg.Bridge),
ft8.NewService(cfg.Ft8)) are **unchanged** in Phase 1. The structural fix lands in
the config layer; the subsystems get the projection. This is the least-churn,
lowest-risk path and it composes forward: hot-swap (Phase 3) re-projects + signals
the Service to rebind rather than rewiring every call site.

*Alternative considered:* make the subsystems read the active profile directly
(drop the loose fields). More honest to "bridge no longer owns port/driver," but
touches ~6 call sites + the ft8 constructor now for no Phase-1 benefit — defer the
field removal to whenever hot-swap forces a rebind seam anyway.

## Types to add (`internal/types/`)

```go
// rig.go (new)
type RigProfile struct {
    Driver string             `json:"driver"`
    Serial BridgeSerialConfig `json:"serial"`           // reuse
    Audio  RigAudioConfig     `json:"audio,omitempty"`
}

type RigAudioConfig struct {
    Device string `json:"device,omitempty"`             // name-based; "" = system default
}
```

`internal/config/config.go` `Config` gains:

```go
Rigs      map[string]RigProfile `json:"rigs,omitempty"`
ActiveRig string                `json:"active_rig,omitempty"`
```

## Resolution + migration (in `Load` / `applyDefaults`, `internal/config/config.go`)

1. **Legacy config (no catalogue):** if `len(Rigs)==0` and any of
   `Bridge.Cat.Driver` / `Bridge.Serial.Port` / `Ft8.Device` is non-empty →
   synthesize `Rigs["default"] = {Driver, Serial, Audio:{Device}}` and set
   `ActiveRig = "default"`. Existing single-rig configs boot unchanged.
2. **Catalogue present:** resolve `ActiveRig`. If empty and exactly one rig → pick
   it; if empty with multiple rigs → validation error (ambiguous). Project the
   active profile into `Bridge.Cat`/`Bridge.Serial`/`Ft8.Device`.
3. **Do not auto-rewrite** the operator's `config.json` on load — resolve in
   memory. The catalogue becomes persisted when the operator (Phase 1: by hand;
   later: the config SPA) next writes config.

## Validation (`validateBridge` neighbour, or a new `validateRigs`)

- `active_rig`, when set, must reference an existing `rigs` key (else error).
- Each profile: `driver` non-empty; optionally check `cat.Lookup(driver)` exists at
  startup (clear error vs. a deferred bridge-error toast — likely worth it).
- The existing "`bridge.serial.port` + `bridge.cat.driver` required when
  `bridge.enabled`" gates still fire on the *projected* active values — so an active
  profile missing a port is caught with the current message.

## API surface (`/v1/config`)

- **GET** must include `rigs` + `active_rig` (so the future SPA sees the catalogue);
  `bridge`/`ft8` continue to show the resolved active values — existing SPA keeps
  working untouched.
- **PUT** must **round-trip** `rigs` + `active_rig` (the overlay / stash-restore
  config-write pattern, memory `feedback_types_canonical_dto`, must preserve these
  new top-level fields so a My-Station save doesn't drop the catalogue). No SPA
  *writes* to the catalogue in Phase 1 — that's piece 3.

## Tests (Phase 1, `internal/config`)

- Migration: legacy single-rig config → synthesized `"default"` profile, projected.
- Resolution: `rigs` + `active_rig` → correct projection into bridge/ft8.
- Validation: dangling `active_rig`; empty/ambiguous active; profile with empty
  driver; (optional) unknown driver.
- `/v1/config` GET includes catalogue; PUT preserves it across a My-Station-shaped
  write.
- Bridge/ft8 packages need **no** new tests in Phase 1 (their inputs are unchanged).

## Deferred to the config-SPA work (pieces 2 & 3)

- **Hardware-discovery endpoint** — enumerate serial ports (go.bug.st/serial
  enumerator: USB VID/PID/serial → friendly label) + audio devices (malgo, as
  `ft8-capture-probe -list` already does) with friendly labels.
- **Profile-editor UI** — dropdowns of discovered hardware; pick driver from known
  rigdefs; pick port + audio device from discovered lists. Operator never types an
  identifier.
- **Runtime hot-swap** — `POST /v1/rig/select {rig}` + Service re-bind (tear down
  current pipeline, re-project, reopen; reset per-rig live state — tune snapshot,
  hub caches, identity confirmation — as cleanly as a disconnect, ADR 0028).
- **Name-based audio resolution** — match `audio.device` name against the
  direction-appropriate capture/playback enumeration at open time (supersedes the
  current index-based `ft8.device`).
- **Per-rig `tune` / `mode_mappings` overrides** inside a profile (only if a real
  need appears; today they're global on `bridge`).

## See

- ADR 0028 — rig profiles, single-active + hot-swap (the decision).
- `internal/types/bridge.go`, `internal/types/ft8.go` — current loose fields.
- `internal/config/config.go` — `Load` / `applyDefaults` / `validateBridge`.
- `cmd/smd/main.go:406,429` — bridge + ft8 construction (unchanged in Phase 1).
- memory `feedback_kiss_frictionless_for_operator`, `project_sm_serial_bridge`.
