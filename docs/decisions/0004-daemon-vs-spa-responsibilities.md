---
number: 0004
title: Daemon-vs-SPA responsibility split — daemon owns state, persistence, and external-service orchestration; SPA owns UI reactivity and per-session UX
status: Accepted
date: 2026-05-01
---

# 0004 — Daemon-vs-SPA responsibility split

## Context

ADR 0003 (the same day) collapsed SPA config to be daemon-only by applying ADR 0001's "SPA hosted by daemon" premise consistently. That collapse exposed a broader question: **for any given piece of behaviour, does it belong daemon-side or SPA-side?** Without a rule, every new feature gets re-litigated and the answer drifts.

The temptation is to over-correct toward "everything goes in the daemon, the SPA shrinks to a thin renderer." That's wrong — the SPA still needs to handle keystrokes, render reactive UI, manage focus, and react to event streams in real time. Those are not trivial concerns and they can't be relocated to the daemon without unacceptable HTTP round-trip overhead at the rates the UI operates (10–20 Hz rig state, every keystroke, every focus shift).

What's also wrong is the opposite — "the SPA does as much as possible to keep the daemon API minimal." That puts external-service credentials in the browser bundle, duplicates shared state across browser tabs, and forces every device to maintain its own cache.

The right framing is to name the *nature* of each responsibility and assign it to the side that has natural ownership. Two topologies clarify this:

- **Persistence / authority topology** — where does the truth live, and what survives across sessions, devices, and reboots? This is daemon-side per ADR 0003.
- **Runtime / events topology** — where does code execute and where do events get handled in real time? This is SPA-side, regardless of where the bundle was served from.

The hosting decision (ADR 0001) collapses persistence/authority to the daemon. It does not collapse runtime/events.

## Decision

**The daemon owns persistence, external-service orchestration, and shared cross-session state. The SPA owns UI reactivity, presentation, and per-session UX.**

Each side gets:

**Daemon-side responsibilities:**
- Persistent storage (QSO log, config, enrichment cache, upload queue).
- Orchestration of external services (hamnut, QRZ, ClubLog, LoTW) — including credential management, retry, fallback chains.
- Shared cross-session state (forwarder progress, daemon connection status to external services, anything multiple browser tabs would otherwise want to coordinate over).
- Authoritative validation (defence in depth — the SPA validates for UX, the daemon re-validates for correctness on submit).
- Anything where "what's true" survives a browser refresh.

**SPA-side responsibilities:**
- UI reactivity — Svelte components subscribing to `$state` modules, re-rendering on change.
- Presentation — DOM, CSS, layout, animations, focus management.
- Per-session UX — loading states, optimistic updates, in-progress drafts, AbortController-based cancellation, keyboard shortcuts, undo within a draft.
- Form-level validation for fast feedback (validators stay pure modules; daemon revalidates on submit).
- Reactive views over daemon state — `cat.svelte.ts` is the canonical example: not authoritative for CAT values, but a fast local cache fed by the bridge SSE, that components subscribe to for re-rendering.

## Alternatives considered

### Pure thin-client — SPA renders only; daemon does everything else

Every keystroke, focus shift, and rig state update would have to round-trip through the daemon. Even on loopback, that's untenable at the UI's actual operating rate: 10–20 Hz rig state alone would saturate the request loop, and per-keystroke validation latency would feel like typing through molasses. The reason a JS runtime exists in the browser is to handle this kind of thing locally; pretending it doesn't is fighting the platform.

### Pure fat-client — SPA does as much as possible to keep daemon API minimal

External-service credentials end up either in the browser bundle (security smell — anyone with the SPA URL has the operator's QRZ password) or scattered across browser localStorage (per-device, lost on cache clear). Each browser tab maintains its own enrichment cache; same callsign gets looked up N times across N sessions. Cross-session state (forwarder progress, "uploads pending") would have to be reconstructed from the daemon on every page load with no shared coordination. None of this is good.

### Responsibility-based split (chosen)

Each responsibility goes to whichever side has natural ownership of the data and the lifecycle. The split tracks reality: persistent things go where persistence lives; reactive things go where the DOM lives. This is also how the framework choices already lean — Svelte 5's compiled reactivity is specifically for in-browser fine-grained updates, and Go's HTTP layer is specifically for serving persistent storage.

## Consequences

**Concrete examples this decision settles:**

| Concern | Where it lives | Why |
|---|---|---|
| QSO log | Daemon | Persistence; ADIF-shaped rows in SQLite; the v1 design that survives v2 unchanged |
| Operator config (CAT defaults, station info, forwarding creds) | Daemon | Persistence + cross-device consistency (ADR 0003) |
| QSO enrichment (cache + hamnut + QRZ orchestration) | Daemon | External-service orchestration + persistent cache + shared across sessions; ADR 0005 (forthcoming) is the implementation detail |
| Upload queue + retry logic | Daemon | Cross-session state; daemon already owns it in v1 |
| Forwarder back-off, schedule, error tracking | Daemon | Long-lived shared state |
| ADIF import/export | Daemon | One-shot operation against persistent storage |
| Bridge SSE consumption (rig state stream) | SPA | UI reactivity; events fan out to multiple components via `cat.svelte.ts` |
| `cat.svelte.ts` reactive state | SPA | Reactive view over rig state; not authoritative |
| Validators (callsign, RST, ...) | SPA | Per-session UX (fast feedback as operator types); daemon re-validates on submit |
| Form-level QSO draft | SPA | Per-session; lost on refresh by design unless explicit persistence is added later |
| Tab → enrichment trigger orchestration | SPA (UX layer) | Cancellation, partial-result rendering, loading state — all UI concerns. The actual fetch goes to the daemon's enrichment endpoint per ADR 0005 |
| Keyboard shortcuts, focus management | SPA | Pure UI reactivity |
| Toast / inline message rendering | SPA | Presentation |

**Signed up for:**

- **Some data is duplicated SPA-side as a reactive cache.** `cat.svelte.ts` mirrors rig state pushed via SSE; the planned `lib/config.svelte.ts` will mirror the daemon's config. These are *views*, not authority, and need to refresh on event/load. The duplication is intentional — fetching daemon-side per render is what we rejected.
- **Stricter discipline on the API surface.** Every new SPA feature has to ask "does this need to be a daemon endpoint?" Most behaviour-with-state will, even when the SPA could in principle fake it.
- **A longer list of `/v1/*` endpoints than a more SPA-heavy architecture would require.** The daemon ends up exposing enrichment, config, queue inspection, and similar — each its own concern. This is the cost of putting orchestration daemon-side.

**Accepted costs:**

- **The "thin SPA" temptation gets real.** It will sometimes be tempting to put a small piece of orchestration in the SPA "just for now." Most such cases violate this rule. The discipline is to either move it daemon-side or write a follow-up ADR explaining why this case is genuinely a UX concern.
- **Cross-session real-time sync for the SPA is non-trivial.** If two browser tabs are open, the daemon's SSE stream needs to fan out so both tabs see the same enrichment-completed event. Daemon-side implementation detail, but real.

**Gained:**

- **One-line answer for future feature decisions.** "Where does this live?" — apply the rule. Most cases settle in one sentence.
- **Credentials never leave the daemon.** QRZ password, ClubLog token, etc. live in the daemon's config, not in the browser bundle, not in localStorage. Even the SPA hitting `/v1/enrich/callsign` doesn't know what credentials the daemon is using.
- **Shared cache for free.** Two browser tabs both Tab on the same callsign — the daemon serves both from the same cache row.
- **`cat.svelte.ts` and similar SPA state are honestly framed as views.** The SPA never claims to be authoritative for state it isn't authoritative for.

## Triggers to revisit

- **If a deployment shape divorces SPA hosting from the daemon** (e.g. SPA on a CDN). The premise that the SPA is always "near" the daemon weakens; some daemon-side orchestration might need to move SPA-side to keep latency tolerable. ADR 0001's same trigger applies.
- **If a daemon-side responsibility turns out to need real-time sub-100ms responsiveness.** Today the candidates are async — enrichment is "few hundred ms is fine," upload-queue inspection is "user-initiated, polling-ok." If something appears that needs every-keystroke latency *and* is daemon-side under this rule, we have a problem. Hard to imagine concretely; flag if it shows up.
- **If multi-operator scenarios emerge.** Then "shared cross-session state" gets a new dimension (cross-*operator* state) and the daemon's responsibilities grow in ways this rule doesn't enumerate. Revisit then.

## References

- ADR 0001 (`0001-ui-toolkit-browser-spa.md`) — the SPA-hosted-by-daemon premise that makes "the daemon is local to the operator" a reliable assumption.
- ADR 0003 (`0003-spa-config-daemon-only.md`) — config as the first concrete application of this rule. ADR 0003 is *implied* by 0004; it's listed here because it landed first and surfaced the question that 0004 answers.
- ADR 0005 (`0005-enrichment-pipeline-shape.md`, forthcoming) — enrichment as the second concrete application. The previous "SPA orchestrates concurrent fetches" framing in `frontend-spa.md` predates 0004 and is incorrect by this rule.
- `docs/v2-design/topology.md` — bridge as a peer of the daemon. Bridge ownership of CAT/serial/PTT/audio is unchanged; this ADR is about daemon vs SPA, not daemon vs bridge.
- `docs/v1-analysis/lessons-for-v2.md` § "build specific, not generic" — the rule's bias toward putting things on the side with natural ownership rather than abstracting into a layer that spans both.
- Memory `project_sm_spa_config_layering` — captures the daemon-side persistence rule via 0003.
