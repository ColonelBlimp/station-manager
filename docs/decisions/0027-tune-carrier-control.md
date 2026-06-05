---
number: 0027
title: Tune-carrier control via daemon-owned TX state machine
status: Accepted
date: 2026-06-04
---

# 0027 — Tune-carrier control via daemon-owned TX state machine

## Context

ADR 0026 shipped the inbound rig-command path (`set_freq`/`set_mode`/`set_vfo`/band) as stateless, fire-and-forget CAT lines. It deliberately stopped short of anything that transmits: ADR 0019 and 0026 both park PTT and TX keying "until a real PTT driver appears," because keying the transmitter is outward-facing and hard to reverse — a stuck carrier into a linear amplifier is the dangerous failure mode.

The operator tunes an **external** linear amplifier, not the rig's internal ATU. The manual ritual is: switch the CW key to bug so one paddle gives a continuous tone, enable break-in, hold the paddle down to produce a carrier, and tune the amp — several fiddly steps on every band change. The request is a single "tune" button: a steady reduced-power carrier on the current frequency, held while the operator tunes both-handed, then off.

This is the first SM feature that transmits, so it activates the deferred half of 0019/0026 and needs a safety model the stateless command path does not have: the daemon — not the browser — must own a **guaranteed stop**, so a closed tab, dropped network, or daemon-visible disconnect mid-tune can never leave the rig keyed.

## Decision

Add a daemon-owned **tune controller** in `internal/bridge`: a small TX state machine driven by a dedicated `POST /v1/rig/tune {active}` endpoint (separate from the generic `/v1/rig/command` path) and reflected to clients by a new `tune-state` SSE event.

Tune-on snapshots the current mode and power, then emits one CAT line — set mode to **RTTY**, set power to the tune power, key TX (e.g. `MD09;PC020;TX1;`). Tune-off emits `TX0;` then restores the snapshotted power and mode. The controller arms three guarantees the moment it keys: a **hard auto-off timer**, **release-on-disconnect**, and **single-flight**; any of the three — and the explicit off — drives the same unkey-and-restore path.

The TX-keying CAT commands (`tx_on`/`tx_off`) are added to the rigdef but **never `exposed`**: they are unreachable through the generic command path; only the tune controller may key TX.

Tune knobs live in a new `bridge.tune` config block, each a code constant overridable by config up to a hard code-enforced ceiling: **power** default 20 W, clamp ≤ 40 W; **auto-off** default 15 s, clamp ≤ 30 s. The **RTTY carrier choice is provisional** — adopted to validate in the field.

## Alternatives considered

### Internal-ATU tune (`AC002;`)

The FTdx10's own tune cycle: self-limiting, rig-managed power, almost no safety machinery needed. Rejected because it tunes only the *internal* ATU; the operator runs an external linear amp, which needs a real carrier on the air, not an internal-ATU match.

### Raw `TX1;`/`TX0;` through the generic `/v1/rig/command` path

Cheapest to build — two more exposed commands. Rejected outright: it puts transmitter keying behind a stateless fire-and-forget endpoint with no auto-off and no disconnect-release, which is exactly the stuck-carrier risk 0019/0026 deferred. TX must go through a controller that owns the stop.

### CW key-down carrier (match the manual workflow literally)

Reproduce what the operator does by hand. Rejected because there is no clean CAT command for a continuous CW key-down — the "hold the paddle" step is precisely the manual hack being replaced; CW keying over CAT sends characters, not a held carrier.

### FM or AM carrier instead of RTTY

Both produce a carrier on `TX1;`. FM is band-restricted on HF (typically 10 m/6 m only), so it cannot tune 40/20 m. AM's carrier sits at ~¼ PEP, so a 20 W setting puts ~5 W into the amp — it breaks the "tune at 20 W on any band" requirement. RTTY keyed with no data gives a steady full-power single-tone carrier on every HF band.

### SPA-owned timer / stop

Let the browser hold the tune state and send the off. Rejected: if the tab closes or the network drops while keyed, nothing unkeys the rig. The kill must be daemon-owned to be a guarantee.

### Hold-to-tune (press-and-hold button) instead of a toggle

Inherently safe (release = stop). Rejected because tuning the amp needs both hands; a held button is not practical. A toggle with the daemon auto-off backstop gives hands-free tuning without sacrificing the guaranteed stop.

## Consequences

- **First retained rig-state in the bridge.** The controller needs the current mode+power to restore them, so the bridge keeps a minimal two-field snapshot fed by the read loop (which already parses `MD0`/`PC`). This is a scoped exception to ADR 0009's "no persistent rig-state cache" (that exclusion was about SSE-replay caching, not a restore snapshot). The controller **refuses to start a tune if it cannot determine the current mode+power** to restore.
- **New daemon surface:** `POST /v1/rig/tune {active}` (registered, like `/v1/rig/command`, only when the bridge is enabled) and a `tune-state {active}` SSE event so the SPA button reflects the daemon's authoritative state — including an auto-off the operator did not trigger.
- **New config block** `bridge.tune` (`power_w`, `max_duration_ms`, `restore_settle_ms`), all clamped in code. Config can tune the feel but cannot create an unsafe tune (no > 40 W, no > 30 s carrier).
- **Activates part of the deferred PTT scope, not all of it.** This builds the daemon-owned TX-keying machinery, but PTT-for-phone, VOX, CW message keying, and TX arbitration remain deferred; a later ADR can build on this controller.
- **Carrier mode is provisional.** If RTTY proves unsuitable in the field, the carrier-mode decision reopens without disturbing the rest of the controller.
- Static CGO-free build is unaffected (pure CAT lines + a timer; no new native deps).

## Implementation note (2026-06-05) — tune-off is two writes, not one (task #270)

The Decision above describes tune-off as "`TX0;` then restores the snapshotted
power and mode." HW testing on the FTdx10 showed the rig **ignores a mode change
(`MD0`) sent in the same burst immediately after `TX0;`** — it accepts the power
change but drops the mode change, leaving the rig in the tune carrier's RTTY-U
mode. The rig accepts `MD0` fine when it's back in RX (proven by tune-*on*,
which sets the mode before keying).

So `releaseTune` now sends **two separate writes**: (1) `TX0;` alone — the
safety-critical unkey, which still owns the guaranteed-stop / fail-safe-retry
semantics; then (2) after a short **TX→RX settle** (`bridge.tune.restore_settle_ms`,
default 150 ms, clamp ≤ 2 s) the best-effort `PC…;MD0…;` restore. The carrier is
already down before the settle, so the guarantee is unaffected — the settle only
gates the best-effort restore (and is skipped if the context cancels mid-pause).
This is a refinement of the unkey-and-restore path, not a change to the decision.

## Triggers to revisit

- **RTTY carrier proves unsuitable** — the amp/ATU dislikes the ~2 kHz RTTY offset, or RTTY keying behaves oddly on the FTdx10 → reopen the carrier mode (FM/AM/CW-keying), per the provisional note.
- **Restore snapshot proves stale** — front-panel changes that AUTO mode does not push are not reflected at tune-start → switch to a fresh `PC;MD0;` query-at-tune-start instead of the read-loop snapshot.
- **A second operator or remote operation appears** — single-flight is no longer enough; TX arbitration and an interlock against an already-transmitting rig become necessary.
- **Full PTT-for-operating is wanted** (phone/CW QSOs, not just tuning) — a follow-on ADR extends this controller into the broader PTT scope 0019/0026 still defer.

## References

- ADR 0019 (bridge subsystem v1 design — deferred PTT) and ADR 0026 (rig command path — deferred TX): the decisions this one partially un-defers.
- ADR 0009 (CAT-state decomposition — "no persistent rig-state cache"); this adds the scoped restore-snapshot exception.
- ADR 0010 (rig SSE wire shape) — the `tune-state` event follows it.
- ADR 0020 (pipeline supervisor) — release-on-disconnect ties into the same disruption handling.
- FTdx10 CAT manual: `AC` (antenna tuner), `TX`/`MX` (TX / MOX keying), `PC` (power), `MD0` (mode).
- `internal/cat/rigs/yaesu-ftdx10.json` — `set_power`/`tx_on`/`tx_off` commands added here.
