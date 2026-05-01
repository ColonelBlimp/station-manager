---
number: 0002
title: SPA config — daemon-authoritative with local cache, three-layer resolution
status: Superseded by 0003
date: 2026-05-01
---

> **Superseded by ADR 0003** (same day). The reasoning below assumed the SPA could
> run while the daemon was unreachable. That premise is incoherent: the SPA is
> hosted by the daemon (ADR 0001), so loading the SPA *requires* a successful
> daemon round-trip. There is no SPA-running-offline scenario to cache for. The
> body of this ADR is preserved as a record of how the more complex three-layer
> shape was considered and rejected.

# 0002 — SPA config — daemon-authoritative with local cache, three-layer resolution

## Context

The v2 logging SPA needs operator-set configuration: preferred default frequencies, default mode, station identity, bridge URL, daemon URL, forwarding credentials, and similar. The first concrete consumer is `frontend/logging/src/lib/states/cat.svelte.ts`, which currently holds hardcoded fallback defaults (settled 2026-05-01) but explicitly notes those are placeholders waiting for this decision.

Constraints driving the choice:

- **The operator's network is slow and unreliable** (memory `project_sm_operator_network`). The SPA cannot assume the daemon is reachable on every page load.
- **The SPA must continue to function when the daemon is unreachable.** Operator's stated requirement (2026-05-01) — narrow scope here is *config remains usable*; broader scope (offline QSO logging) is a separate forthcoming decision and is *not* settled by this ADR.
- **Cross-device usage is plausible.** Operator may run the SPA from laptop and phone in different sessions; preferences should follow the operator, not stay pinned to a single browser.
- **Per the `no magic numbers` rule** (memory `feedback_no_magic_numbers`), runtime values come from config; code constants are fallback defaults only. Hardcoded constants in `cat.svelte.ts` are explicitly documented as placeholders pending this ADR.

## Decision

**SPA config is daemon-authoritative with a local-storage cache.** Read resolution at runtime is three-layered:

1. **Daemon** — `GET /v1/config` (schema TBD; first fields are the CAT defaults). Authoritative when reachable.
2. **Local cache** — `localStorage` under a defined key namespace (e.g. `sm.config.*`). Reflects the last-known daemon state, plus any operator edits made while offline.
3. **Hardcoded fallback defaults** — module-level `const`s in code (e.g. `cat.svelte.ts`'s `DEFAULT_VFO_HZ`, `DEFAULT_MODE`). Used only when neither daemon nor local has a value.

When the daemon is reachable, its values override local and hardcoded. When unreachable, local takes over, falling back to hardcoded for any field local doesn't have. Operator edits made while the daemon is unreachable land in local first and sync to the daemon on reconnect.

## Alternatives considered

### Option A — Daemon API only

SPA reads `/v1/config` on every page load; falls back to hardcoded defaults if daemon is unreachable. Simplest implementation; no localStorage state to manage; no sync logic.

Rejected because the operator's network is unreliable enough that daemon-unreachable cases will happen routinely. Falling all the way through to hardcoded defaults during those cases discards the operator's preferences entirely — every offline session would start from "USB on 14.250 MHz" regardless of what the operator was actually using yesterday. That's a significantly worse UX than the implementation simplicity is worth.

### Option B — localStorage only

SPA reads/writes config exclusively from `localStorage`. Zero daemon changes; zero network dependency; instant reads. Simple to implement.

Rejected on two counts: (a) defaults wouldn't sync across devices — running the SPA from laptop one day and phone the next gives different defaults, and any preference change has to be repeated per device; (b) localStorage is per-origin-per-browser and is wiped by "clear browsing data," browser reinstalls, or moving to a new machine. The daemon as a single source of truth, with the operator's data persisted in the daemon's working directory, survives all of those. localStorage as the *only* store would mean the operator effectively maintains config separately on every device, which contradicts the single-operator-per-instance model.

### Option C — Daemon-authoritative with local cache (chosen)

Three-layer resolution. Daemon over local over hardcoded. Local is a write-through cache when daemon is reachable, and a write-deferred queue when it isn't. On reconnect, local edits push up to the daemon.

Best of both: fast first paint from local cache (no round-trip on every load), daemon as authority when reachable (cross-device sync, durable storage), full offline capability for config reads and edits. Tradeoffs are write-while-offline sync logic and cache invalidation discipline — both manageable for single-operator usage.

## Consequences

**Signed up for:**

- **Daemon-side operator-config API.** `GET /v1/config` and `PUT /v1/config` (or per-section endpoints — schema and HTTP shape TBD in the implementation pass). Daemon's existing `internal/config/config.go` covers system config (server protocol, ports, paths); operator-config is a new layer with operator-set fields like CAT defaults, station identity, forwarding creds.
- **localStorage namespace** for cached config. Convention: keys prefixed `sm.config.<section>` (e.g. `sm.config.cat`, `sm.config.station`). Per-section keys allow partial cache invalidation without rewriting the whole config blob.
- **Offline-write protocol.** When daemon is unreachable and the operator changes a config value, the change lands in local with an `unsynced` flag. On reconnect, unsynced changes push to the daemon. **Conflict policy: last-write-wins by timestamp** for single-operator use; revisit when multi-operator scenarios emerge.
- **Cache freshness.** SPA boots with local immediately, fires a daemon fetch in the background, applies daemon values when they arrive, and writes them back to local. This means the UI may briefly show stale values right after a daemon-side change made on another device — acceptable for a few hundred milliseconds.

**Accepted costs:**

- **Boot complexity.** Config resolution is three-tier rather than one. Implementation lives in a single `lib/config.svelte.ts` module so consumers see a unified `config` state object regardless of which layer the value came from.
- **localStorage size budget.** ~5 MB per origin in most browsers. Configs are KB-scale; not a concern. The same store is *not* available for offline QSO log unless the QSO storage decision lands separately and chooses localStorage.
- **Two write paths.** Online writes go daemon → local. Offline writes go local → (queue) → daemon. A small amount of glue logic per writable field, but the protocol is uniform.

**Gained:**

- Offline-capable SPA for config reads and edits. The hardcoded fallback only matters on first launch (no local cache yet) with daemon down, which is a vanishingly rare edge case.
- Fast first paint — no daemon round-trip blocks the UI.
- Cross-device consistency via the daemon as authority.
- The hardcoded defaults in code stay as a minimum-viable safety net rather than being load-bearing — exactly the role the `no magic numbers` rule prescribes.

## Triggers to revisit

- **If the operator's network reliability changes substantially** and offline-mode effectively never fires, Option A's simpler architecture becomes attractive — local cache and offline writes are dead weight in that scenario.
- **If multi-operator scenarios emerge** (multiple operators editing the same daemon's config concurrently), last-write-wins is no longer adequate. A field-level merge with operator attribution would be needed; revisit then.
- **If browser storage becomes a constraint** — extremely unlikely for KB-scale config, but if `sm.config.*` ever approaches the 5 MB localStorage limit, switching to IndexedDB is the next step. (More likely IndexedDB enters the picture for offline QSO storage, not for config.)
- **If the operator-config API surface grows beyond a handful of fields**, splitting `/v1/config` into per-domain endpoints (`/v1/config/cat`, `/v1/config/station`, `/v1/config/forwarding`) becomes worthwhile. Captured here so the eventual split has prior context.

## References

- `frontend/logging/src/lib/states/cat.svelte.ts` — first consumer; hardcoded defaults documented as placeholders pending this decision.
- `internal/config/config.go` — existing daemon system-config layer; the new operator-config layer is adjacent to (not replacing) this.
- ADR `0001-ui-toolkit-browser-spa.md` — established the daemon-hosts-SPA topology that makes the daemon a natural authoritative store.
- Memory `feedback_no_magic_numbers` — runtime values from config; constants as fallback defaults.
- Memory `project_sm_operator_network` — operator's network is slow/unreliable; supports the "must work offline" stance.
- (Forthcoming) ADR for offline QSO log — *separate decision*. The "log locally when daemon is unreachable" requirement raised during this conversation has wider implications (local QSO persistence, sync protocol, conflict resolution) than just config storage. Capture in its own ADR before SPA-side QSO write code lands.
- (Future implementation) `lib/config.svelte.ts` and `lib/api/config.ts` — the SPA module that exposes the unified config state and the fetch wrapper that talks to `/v1/config`.
