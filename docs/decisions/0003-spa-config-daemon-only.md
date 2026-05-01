---
number: 0003
title: SPA config — daemon filesystem is the only source; hardcoded constants are bootstrap fallback only
status: Accepted
date: 2026-05-01
supersedes: 0002
---

# 0003 — SPA config — daemon filesystem is the only source; hardcoded constants are bootstrap fallback only

## Context

ADR 0002 (the same day, earlier) chose a three-layer config resolution: daemon → localStorage cache → hardcoded defaults, motivated by "the SPA must continue to function when the daemon is unreachable." That motivation was incoherent against an earlier-settled architectural premise.

**The premise from ADR 0001:** the SPA is *hosted* by the daemon — the daemon serves both `/v1/*` (API) and `/` (the SPA bundle, via `//go:embed`). The browser fetches the SPA from the daemon's HTTP server.

**The implication 0002 missed:** if the daemon is unreachable, the SPA cannot load. The browser receives nothing to render. Loading the SPA *is* a successful daemon round-trip. There is no scenario where the SPA is running while the daemon is unreachable — only scenarios where the daemon goes unreachable *during* a session that already started. Mid-session daemon disappearance is a much narrower problem than 0002 tried to solve, and is not load-bearing for config because config is already in memory once loaded.

From the SPA's standpoint, **the daemon is local**: it lives on the same machine (loopback) for the all-in-one personal-use shape, and on the network endpoint the SPA was loaded from for split-deployment shapes. Either way, "config persists across SPA reloads" is solved by the daemon's filesystem, not by a browser-side cache.

Triggering observation (operator, 2026-05-01): *"if the daemon is unreachable, then the logging app is also unreachable — so this is a non-starter. Configs are read from the filesystem, and there is no point logging locally, because from the standpoint of the SPA, local is the daemon."*

## Decision

**SPA config is read from the daemon's filesystem via `/v1/config`. There is no local cache.** Hardcoded module-level constants in code (e.g. `cat.svelte.ts`'s `DEFAULT_VFO_HZ`) exist purely as bootstrap fallback — used when no daemon response has arrived yet (boot transient on first paint) or when the daemon's config file doesn't yet exist (first-install case). Once the daemon has responded, daemon values override hardcoded; live runtime values from the bridge override the daemon defaults.

Two layers, not three:

1. **Daemon** (`/v1/config`, schema TBD) — the only persistent source.
2. **Hardcoded module-level constants** — bootstrap and first-install fallback only.

## Alternatives considered

### Three-layer with localStorage cache (ADR 0002, now superseded)

Rejected because the offline scenario it was solving doesn't exist. The SPA can't run while the daemon is unreachable. localStorage as a cache would have added implementation complexity (sync logic, write queue, conflict resolution) for a benefit that never materialises.

### Daemon API only with no in-code fallback at all

Rejected because the SPA needs *something* to render before the first `/v1/config` response arrives, and on first-install the daemon's config file may not yet exist at all. Hardcoded constants cover both cases at zero implementation cost — they're already in `cat.svelte.ts` and the convention is established. A few `const` declarations per state module is not the kind of complexity worth eliminating.

### Daemon-only chosen

The simpler architecture wins because the constraint that justified the more complex one was illusory. The hardcoded constants pull double duty as both first-paint fallback and first-install fallback, which is sufficient.

## Consequences

**Signed up for:**

- **Daemon-side operator-config API.** `GET /v1/config` returns the operator-config blob; `PUT /v1/config` (or per-section endpoints — schema TBD in implementation pass) accepts edits. Daemon's existing `internal/config/config.go` covers system config (server protocol, ports, paths); the operator-config layer is a new adjacent concern.
- **First-paint behaviour.** SPA renders with hardcoded defaults momentarily, then re-renders when the first `/v1/config` response arrives. Acceptable for a couple hundred ms at boot.
- **First-install UX.** If the operator launches the SPA before configuring anything, hardcoded defaults are what they see. Saving the first config writes through the daemon API to the filesystem.
- **No offline writes.** A config edit attempted while the daemon is mid-session unreachable simply errors and asks the operator to retry. This is not a regression: a config edit during an outage was always going to need to land somewhere; "tell the user to retry" is acceptable for a problem that will be rare in practice.
- **No QSO offline mode either.** The same logic applies: SPA needs daemon to load at all, and the daemon owns the QSO log. The forthcoming "offline QSO storage" ADR flagged in 0002 is *no longer needed* — the SPA submits QSOs to the live daemon; daemon-side write resilience (atomic transaction, retry on forwarding) is daemon internals and already part of the v2 design. Removed from the queue.

**Accepted costs:**

- **Mid-session daemon disappearance is harsh.** If the daemon goes down mid-session, the SPA stays loaded but config edits and QSO submits start failing. The SPA needs to surface this as a connection-status indicator (toast or banner) so the operator knows what's happening. This is a UI concern, not an architectural one.
- **No cross-device cache** — but this never made sense anyway. Each device hits the same daemon and gets the same config.

**Gained:**

- **Architecture simplifies dramatically.** No localStorage namespace, no `unsynced` flag, no last-write-wins conflict policy, no boot-time merge logic. The unified `lib/config.svelte.ts` module (still planned) is a thin fetch wrapper over `/v1/config` plus the hardcoded constants as fallback.
- **Single source of truth.** Operator's filesystem (under daemon control) is authoritative. No two-store consistency to reason about.
- **The hardcoded-constants role is honest.** They were always meant to be fallback per the `no magic numbers` rule; this ADR makes them the *only* in-code fallback layer, with no cache layer pretending to be something more.

## Triggers to revisit

- **If a deployment shape divorces SPA hosting from the daemon.** Hypothetical: SPA bundle on a CDN, daemon at a separate URL. Then "SPA load implies daemon reachable" no longer holds, and an offline-cache decision becomes relevant again. Current architecture (ADR 0001) doesn't allow this, but if that changes, this ADR needs revisiting.
- **If mid-session config-write resilience becomes a real pain point.** Specifically: if operators routinely edit config during transient outages and "retry the save" UX is unacceptable. Then a small write-queue *just* for config writes (not reads) might be worth adding — narrower scope than 0002 attempted.
- **If multi-operator scenarios emerge.** Same trigger as 0002. Multi-operator config editing has its own consistency problems (last-write-wins isn't enough); revisit then.

## References

- ADR 0001 (`0001-ui-toolkit-browser-spa.md`) — the SPA-hosted-by-daemon premise that this ADR finally applied to its own reasoning. The "Triggers to revisit" of 0001 includes "if SPA hosting in the daemon becomes a maintenance burden" — which would also trigger this ADR.
- ADR 0002 (`0002-spa-config-shape.md`) — superseded. Its alternatives analysis is still useful as a record of how a more complex shape was considered and rejected.
- `frontend/logging/src/lib/states/cat.svelte.ts` — first consumer; hardcoded defaults are the bootstrap fallback per this ADR.
- Memory `project_sm_spa_config_layering` — updated to match this ADR (was originally written to match 0002).
- (Future implementation) `lib/config.svelte.ts` — the SPA module that exposes config state and fetches `/v1/config`. Now significantly simpler than 0002 implied.
- (Removed from queue) The "offline QSO storage" ADR forward-referenced from 0002 is no longer needed; same SPA-hosted-by-daemon reasoning applies.
