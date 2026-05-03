---
number: 0008
title: Notifications / toast system — hand-rolled `$state`-array singleton with three levels and capped stack
status: Accepted
date: 2026-05-01
---

# 0008 — Notifications / toast system

## Context

ADR 0006 made the notifications/toast system load-bearing for the CAT-handover transition: when the bridge connects after being disconnected, a toast surfaces "CAT connected — reading rig state" so the handover isn't silent. The toast system itself doesn't exist yet; this ADR settles its shape.

The system will also handle other event-style outcomes the SPA needs to surface non-modally: QSO save success/failure, enrichment outcome (per the `enrichment never blocks logging` invariant — failures must be visible without blocking), bridge connect/disconnect, forwarder progress, etc. Those concerns share the "transient signal of an event that just happened" UX shape that distinguishes toasts from inline state messages (per ADR 0006's framing in the precedence rule's references).

Two shape decisions:
1. **Library or hand-rolled?**
2. **What's the state model and what's exposed to consumers?**

## Decision

**Hand-rolled, exposed as a `lib/states/toasts.svelte.ts` `$state`-array singleton.** Imperative `pushToast(...)` (and `info`/`warn`/`error` shortcuts) for fire-and-forget event signalling; the underlying `$state` array is also exported so components that want to render or aggregate the queue can subscribe directly.

**State shape:**

```ts
type ToastLevel = 'info' | 'warn' | 'error';

interface Toast {
    id: number;          // monotonic counter, auto-assigned
    level: ToastLevel;
    message: string;
    createdAt: number;   // Date.now() at push
    ttl: number;         // milliseconds; 0 = sticky (dismissable only)
}

// Module exports:
toastsState.items: Toast[]                                    // reactive $state array
pushToast(level, message, ttl?): number                       // returns id
dismissToast(id: number): void
info(message, ttl?), warn(message, ttl?), error(message, ttl?)  // convenience wrappers
```

**Defaults:**

| Level | Default TTL | Auto-dismiss |
|---|---|---|
| info | 4000 ms | yes |
| warn | 6000 ms | yes |
| error | 8000 ms | yes |

`ttl: 0` opts out of auto-dismiss for cases that genuinely need to stick around until acknowledged (rare — most "must be acknowledged" cases want a banner, not a toast).

**Max stack depth: 5.** When a sixth toast is pushed, the oldest is dropped. Prevents runaway queues from a misbehaving event source obscuring the UI.

**Click-to-dismiss is always available.** Clicking any toast removes it immediately regardless of its remaining TTL.

**Severity prefix (added 2026-05-03).** Each toast renders a bold level label inside the button — `Info: ` / `Warning: ` / `Error: ` — followed by the call-site message. The prefix is computed in `Toasts.svelte` from `toast.level`, not baked into the message at the call site. Two reasons: single source of truth for the level → label mapping, and `aria-live="polite"` reads the label aloud so severity is conveyed to screen-reader and colour-blind operators without depending on the colour palette. Call sites pass plain operator-readable text (no role-prefix in the message string itself).

**Mount point:** `<Toasts/>` is rendered once in `app.svelte`. Container is `fixed top-4 right-4`, stacks top-down (oldest-first DOM order; newest appends below the existing stack). Top-right matches v1's convention and keeps the QSO entry rows below the toast region — the operator's eye drops naturally from a freshly-arrived notification back to the form.

(Original ADR position — bottom-right with stack-bottom-up — was changed to top-right on 2026-05-03 after the implementation surfaced; reasoning above. The "keep the top of the viewport clear for the QSO entry area" framing was wrong: the entry area sits in the middle of a finite-size shell, not at the top of the viewport, so a top-right toast doesn't obscure it. v1 precedence wins.)

**Styling:** Tailwind v4 `@layer components` cluster — `.toast-base`, `.toast-info`, `.toast-warn`, `.toast-error`. Same convention as `.input-base`, `.input-row`, `.invalid-input` already established in `app.css`. Colours come from Tailwind's palette (info = indigo, warn = amber, error = rose) and can move to `@theme` tokens if reused elsewhere.

**No pause-on-hover, no swipe-to-dismiss, no animation choreography for v1.** Plain fade-in (~150 ms) on mount and fade-out on dismiss is sufficient. Trigger to add: if operators report missing important toasts because they happened to look away during the brief display window.

## Alternatives considered

### `@zerodevx/svelte-toast`

Mature, ~3 KB, stable API, well-known in the Svelte ecosystem; the operator has prior fluency.

Rejected on two counts: (a) its API is imperative-only — `toast.push(msg)` — without exposing the queue as a reactive state object. ADR 0006 names `lib/states/toasts.svelte.ts` as a state singleton that future components (notification badge, history view, status bar with recent-events list) might want to subscribe to. The library would force us to maintain a parallel queue alongside its own, which is the kind of duplicated state that drifts. (b) Its theming model (CSS custom properties like `--toastBackground`) doesn't fit the existing `@layer components` Tailwind v4 convention without bridging. Hand-rolling is ~80 lines and avoids both issues.

The library's compelling case for `@svelte-put/shortcut` was platform-quirks normalisation (modifier keys, cross-platform Cmd/Ctrl, key vs code). **Toasts have no equivalent platform layer** — it's a `<div>` with `setTimeout`. The library's value is convention, not hidden complexity.

### `svelte-french-toast` / `svelte-sonner`

Larger (`svelte-french-toast` ~10 KB; `svelte-sonner` ~6 KB), more features (custom toast components, promise-based toasts, swipe-to-dismiss, stacking animations). Overkill for SM's actual needs. `svelte-french-toast` has had Svelte 5 compatibility issues; `svelte-sonner` is newer and less battle-tested. Neither earns its weight here.

### Banner-only / inline-only (no toast at all)

Persistent banners for connection state plus inline messages for field-level feedback could in theory cover the same ground. Rejected because the *event* shape — "this thing just happened" — has no natural inline anchor. Where would "QSO saved" go? Where would "QRZ lookup timed out" go? Toasts exist precisely for non-modal event signalling that doesn't have a permanent home in the layout.

### Hand-rolled (chosen)

`$state`-array singleton + an imperative push API + a `<Toasts/>` renderer. ~80 lines total. Leverages the existing `lib/states/` convention, the existing Tailwind `@layer components` pattern, and gives consumers both an imperative API and a reactive subscribable queue.

## Consequences

**Signed up for:**

- **`lib/states/toasts.svelte.ts` module** — ~50 lines: state class, push/dismiss functions, level-helpers, auto-dismiss `setTimeout` plumbing, max-stack enforcement.
- **`<Toasts/>` component** — ~30 lines: subscribes to `toastsState.items`, renders each as a styled div, click-to-dismiss handler, fade-in/out via CSS transitions.
- **Tailwind cluster in `app.css`** — `.toast-base`, `.toast-info`, `.toast-warn`, `.toast-error` in `@layer components`.
- **One-line addition to `app.svelte`** to mount `<Toasts/>` once.
- **Discipline on what gets toasted.** Toasts are for *events*, not for *state* (per ADR 0006's framing). State goes inline (Fix 13's planned validation message slot, status indicators in a future status bar). Misuse — toasting every field validation failure — would be noise. Where the line falls per concern: `notifications` is for non-blocking *outcomes* of operator-initiated or system-driven *actions*.
- **Unit tests** — toast push/dismiss/auto-dismiss/max-stack-cap behaviour are testable as pure module logic. `<Toasts/>` rendering is testable via `@testing-library/svelte` with `vi.useFakeTimers()` for the TTL paths.

**Accepted costs:**

- **No promise-based API** (`toast.promise(savingQso, { loading, success, error })`). If this becomes wanted, it's additive — wrapping the existing API in a helper.
- **No pause-on-hover.** A toast that fires while the operator's looking elsewhere can be missed. Mitigated by error-level's longer TTL and click-to-dismiss; trigger to revisit named below.
- **Stack-cap of 5 is a guess.** Untuned until real operating use shows whether 5 is too few (events get lost in floods) or too many (visual clutter).

**Gained:**

- **Reactive `$state` queue** subscribable from anywhere. Future status bar showing "3 recent events" reads from the same queue with no parallel-state worry.
- **Tailwind-native styling** that fits the existing `@layer components` convention. No CSS-custom-property bridging.
- **No external dependency**. Bundle size unchanged.
- **Full control over event lifecycle.** TTL math, max-stack semantics, auto-dismiss vs sticky, dismiss interactions — all explicit in code, no configuration-options-API to learn.

## Triggers to revisit

- **Operators report missing important toasts** because the TTL was too short or they happened to look away. Add pause-on-hover, or extend TTLs, or surface the most-recent-N as a persistent banner (depending on which kind of miss is happening).
- **Promise-based toasts wanted.** When async operations want a single toast that walks `loading` → `success`/`error`, add a `toastFromPromise()` helper. Doesn't change the underlying state model.
- **Toast-history view requested** — "show me the last 50 events." Then we either persist `toastsState.items` beyond auto-dismissal (mark dismissed-but-keep-record) or split into a separate `eventLog` state. The current shape doesn't preclude either.
- **Per-toast custom components** — e.g. a toast with an "Undo" button for a destructive action. Would mean accepting `Snippet` or component as the message body type. Additive.
- **Stack-cap of 5 turns out wrong.** Adjust the const. No structural change.
- **Multi-region toasts** — system events bottom-right, contest-specific event log a separate corner. Multi-container. Additive.

## References

- ADR 0006 (`0006-cat-state-precedence-rule.md`) — names this system as load-bearing for CAT-handover transition visibility. The "CAT connected — reading rig state" toast on bridge-connect is the first concrete consumer.
- ADR 0004 (`0004-daemon-vs-spa-responsibilities.md`) — toast notifications are SPA-side per-session UX (UI reactivity / presentation). Daemon doesn't push toast UIs; it pushes events that the SPA renders.
- ADR 0007 (`0007-keyboard-shortcuts.md`) — `Escape` is reserved for revert/cancel and doesn't dismiss toasts. Toast dismissal is click-only for now (could become an additional `Esc`-dismisses-most-recent shortcut later if desired).
- Memory `project_sm_spa_component_patterns` — "inline messages express state; toasts express events" rule (rule 4, settled 2026-05-01) — applied here.
- `frontend/logging/src/styles/app.css` — `@layer components` cluster where the toast classes will land alongside `.input-base`, `.input-row`, `.invalid-input`.
- `frontend/logging/src/lib/states/toasts.svelte.ts` — state singleton (built 2026-05-03).
- `frontend/logging/src/lib/states/toasts.test.ts` — vitest coverage (push, per-level TTL, sticky `ttl=0`, max-stack eviction, dismiss idempotence, `info/warn/error` shortcuts).
- `frontend/logging/src/lib/ui/Toasts.svelte` — single-mount renderer; bottom-right fixed; fade in/out via `svelte/transition`.
- `frontend/logging/src/app.svelte` — mounts `<Toasts/>` once.
- `frontend/logging/src/styles/app.css` — `.toast-base/.toast-info/.toast-warn/.toast-error` cluster in `@layer components`.
- First consumer: `QsoPanel.svelte`'s submit-outcome arms (duplicate=warn, validation/server/network=error).
