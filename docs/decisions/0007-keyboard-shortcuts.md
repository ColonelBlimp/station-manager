---
number: 0007
title: Keyboard shortcuts — `@svelte-put/shortcut` library, initial shortcut map, in-field policy
status: Accepted
date: 2026-05-01
---

# 0007 — Keyboard shortcuts

## Context

Pile-up logging is the worst case for mouse-driven UX: every contact is "callsign typed → RST exchanged → next contact" in seconds, and a mouse trip to click a button costs a contact. SM's logging SPA needs keyboard-first operation as a baseline.

This ADR settles three things:

1. **How shortcuts are wired** — library choice or hand-rolled.
2. **The initial shortcut map** — which keys do what.
3. **The policy for in-field behaviour** — when shortcuts fire if focus is in a text input.

ADR 0006 established the precedence rule that gates CAT-state edits when the bridge is operating; some shortcuts (e.g. swap-VFO) write to `catState` and must respect that rule.

## Decision

### Library: `@svelte-put/shortcut`

Use `@svelte-put/shortcut` (action-based Svelte 5 shortcut library, ~1 KB gzipped) rather than hand-rolling a `keydown` handler.

The library handles modifier-key exact-matching, cross-platform Ctrl/Cmd normalisation, and per-binding `enabled` predicates — all things we'd have to re-solve if hand-rolling, and which have well-known platform quirks. The ADR captures *which keys do what*; the library is the implementation detail.

### Initial shortcut map

| Key | Action | CAT-gated? | In-field? |
|---|---|---|---|
| `Tab` | Move focus through fields; fires enrichment on Callsign blur | — | n/a (focus management is native) |
| `Enter` | Within a field: commit (e.g. VfoInput frequency) | — | Yes (already implemented per-component) |
| `Escape` | Within a field: revert/cancel current edit (no commit). Outside fields: clear in-progress QSO draft | — | Yes |
| `Ctrl+Enter` | Submit current QSO (when draft is valid) | — | Yes |
| `Ctrl+\` | Swap `selectedVfo` (A ↔ B) | **Yes** (writes `catState`) | Yes (modifier-keyed) |
| `?` | Show keyboard help overlay | — | **No** (bare key — operators typing `?` in callsign field shouldn't trigger) |

This is the **initial** map — the bare minimum for at-speed logging. First revision after a few weeks of operating use is expected; revisit triggers below.

**Key choices that aren't obvious:**

- **`Ctrl+\` for VFO swap.** Backslash is unbound in browsers and OS shortcuts across Linux/macOS/Windows; near Enter for left-hand reachability. Alternatives considered: `Alt+V` (Alt opens View menu in some browsers), `Ctrl+Tab` (browser tab cycling), `F2` (reserved for future contest macros).
- **`Ctrl+Enter` for QSO submit.** Standard "commit" shortcut across many apps (Slack, GitHub, etc.); muscle memory likely.
- **`Escape` overloaded by context.** In a field: revert. Outside fields: clear draft. Both are "back out of the current thing" — consistent enough that the overload reads naturally.

### In-field policy

The library doesn't enforce in-field behaviour; we encode it ourselves with a target check.

- **Modifier-keyed shortcuts** (`Ctrl+...`, `Alt+...`, `Cmd+...`) **work in-field by default.** Operator typing a callsign can hit `Ctrl+Enter` to submit or `Ctrl+\` to swap VFO without first blurring. This matches modifier-as-explicit-intent semantics across most apps.
- **Bare-key shortcuts** (`?`, single letters, etc.) **never fire when focus is in a text input, textarea, or contenteditable element.** Operator typing a `?` in a notes field opens nothing. Wrapping each bare-key binding's callback with a `event.target` check (rejecting `INPUT`, `TEXTAREA`, `[contenteditable=true]`) accomplishes this.
- **`Escape` is the exception.** It fires regardless because the in-field semantics ("revert current edit") are intentional — the existing VfoInput already does this. The clear-draft semantics outside fields is layered on top.
- **`Tab` and `Enter` within fields are component-local.** The shortcut library doesn't bind them globally; per-component handlers (already in place — Callsign's onenrich, VfoInput's commit) handle them. Keeps the shortcut registry to *cross-component* keys only.

### CAT-state gating

Shortcuts that mutate `catState` use a per-binding `enabled` predicate hooked to ADR 0006's `editable` derived helper:

```ts
{ key: '\\', modifier: ['ctrl'], callback: handleSwap, enabled: () => editable }
```

`editable` is `!(catState.enabled && bridgeState.connected)`. When CAT is operating, the swap shortcut is silently no-op — same lockout as the VfoBox click and VfoInput edit affordances.

`Ctrl+Enter` (submit) is **not** CAT-gated. Submitting a QSO is always allowed regardless of CAT state — the QSO payload reads from `catState` at submit-time, but submission itself doesn't write back to CAT-state fields.

`Escape` and `?` aren't gated either — they don't mutate CAT-state.

### Help overlay

A separate `<KeyboardHelp/>` component (planned), bound to `?`, lists all shortcuts. Hand-written content — the library doesn't generate it — so this ADR is the source of truth for what the overlay shows. When new shortcuts land, both this ADR and the overlay's content update together.

### Reserved key real estate

- **F-keys (F1–F12) reserved for future contest macros.** N1MM-style logging uses F-keys for canned messages ("CQ TEST", "TU 73", "QRZ?"). Out of scope for v1; explicitly not bound now so the namespace is preserved.
- **Plain letter keys (a-z, 0-9) reserved for typing.** Never bind to a global shortcut — too easy to collide with in-field input.
- **`Ctrl+1..9, Ctrl+T, Ctrl+W, Ctrl+R, Ctrl+L, Ctrl+P, Ctrl+S` reserved by browsers.** Don't try to override; some browsers prevent it, others swallow it inconsistently.

## Alternatives considered

### Hand-rolled `keydown` handler

Direct `<svelte:window onkeydown={handleKeydown}>` with a switch/if-chain. Zero dependencies; full control.

Rejected: modifier-key normalisation is fiddly (Ctrl vs Cmd, exact vs subset match, key vs code, upper/lower case), and any platform quirk found later is ours to fix. By the 8th or 10th binding we'd have built half the library. The 1 KB gzipped of `@svelte-put/shortcut` is irrelevant for an embedded SPA.

### Other shortcut libraries (`mousetrap`, `hotkeys-js`, `tinykeys`)

All viable; rejected on scope/fit grounds. `mousetrap` is React/jQuery-era and not Svelte-shaped. `hotkeys-js` is bigger and framework-agnostic where Svelte-shaped is preferred. `tinykeys` is a reasonable alternative — similar size, no framework opinions — but `@svelte-put/shortcut`'s action-based API is more idiomatic for Svelte 5 and the operator already has prior usage with it. No strong technical preference; existing-fluency tiebreaker per the lessons-doc rule that picking the tool you know is right on a solo project.

### Different initial shortcut map

Several alternatives weighed:

- `Alt+V` for VFO swap — rejected because Alt sometimes opens the menu bar in Linux browsers.
- `Ctrl+/` for VFO swap — rejected because some browsers use it for "focus search" assistance.
- `Space` for swap — rejected because operators often hit Space mid-typing in notes fields.
- `Ctrl+S` for QSO submit — rejected because browsers may swallow it as "save page" depending on context.

`Ctrl+\\` and `Ctrl+Enter` are the most universally-unbound and intuitive options.

## Consequences

**Signed up for:**

- **Add `@svelte-put/shortcut` as a runtime dependency** (devDep + prod). Lockfile committed; CI's `npm ci` keeps it deterministic.
- **One global shortcut registry** in the SPA shell (likely `app.svelte` or a dedicated `lib/shortcuts.svelte.ts`) — the canonical place where all bindings live. New shortcuts get added here; per-component shortcuts are component-local (Tab, Enter, Escape within VfoInput) and not registered globally.
- **`<KeyboardHelp/>` component** — one screen, hand-written, kept in sync with this ADR.
- **Test discipline** — keyboard shortcuts are testable via `fireEvent.keyDown` on `<svelte:window>` events. New shortcuts should land with at least one test verifying the binding fires and respects its `enabled` predicate.
- **Help-overlay maintenance burden.** When a shortcut is added/changed, three things update together: this ADR, the binding registration, and the help overlay's content. Treat as a checklist.

**Accepted costs:**

- **One more dependency** in the SPA. Mitigated by the dependency being small (~1 KB), focused, and well-maintained.
- **Initial shortcut map will need iteration** based on actual operating use. The "Triggers to revisit" section names the signals that warrant reopening this ADR.

**Gained:**

- **Pile-up speed.** Operator can stay on the keyboard for the entire QSO flow.
- **One source of truth for shortcuts.** The registry lives in one place; this ADR documents intent; the help overlay surfaces it to the operator.
- **CAT-state precedence respected automatically.** The `enabled` predicate threads ADR 0006's lockout into shortcut behaviour without per-handler boilerplate.

## Triggers to revisit

- **Operator's first weeks of real operating use.** The initial map is a guess; some bindings will feel wrong. Expect a revision within ~30 days of regular use. Revisions are amendments to this ADR (status stays Accepted; date the amendment) rather than supersession, since the *library choice* and *policy* are stable while the *map* is iterative.
- **Contest macro layer needed.** When F-keys for canned messages become a feature, the F1–F12 reservation lifts and a contest-macro sub-system emerges. Likely earns its own ADR rather than amending this one — different shape (configurable per-operator macros, not fixed bindings).
- **Multi-mode operating** (e.g. paddle keyer for CW). If keying behaviour gets baked into the SPA at all, keyboard shortcuts grow a "transmit while held" semantics that this ADR's edge-triggered model doesn't cover. Likely another sibling ADR.
- **`@svelte-put/shortcut` maintenance falters.** If the package goes unmaintained or breaks compatibility with a future Svelte release, swap to `tinykeys` or hand-roll. The ADR's *map* and *policy* sections are library-independent.
- **Cross-platform behaviour bites.** If a binding works on Linux but breaks on macOS (or vice versa), this ADR documents which key was intended; the binding's implementation can be adjusted (e.g. use `Cmd` instead of `Ctrl` on Mac via the library's modifier-aliasing).

## References

- ADR 0001 (`0001-ui-toolkit-browser-spa.md`) — Svelte 5 + Vite scaffold; sets the framework context for Svelte-shaped library choices.
- ADR 0004 (`0004-daemon-vs-spa-responsibilities.md`) — keyboard handling is per-session UX; SPA-side per the responsibility rule.
- ADR 0006 (`0006-cat-state-precedence-rule.md`) — `editable` derived helper that CAT-mutating shortcuts use as their `enabled` predicate.
- `@svelte-put/shortcut` — https://svelte-put.vnphanquang.com/docs/shortcut (library docs).
- `frontend/logging/src/lib/ui/components/Callsign.svelte` — Tab-as-enrichment-trigger; component-local, not in the shortcut registry.
- `frontend/logging/src/lib/ui/components/VfoInput.svelte` — Enter/Escape within field; component-local.
- (Planned) `frontend/logging/src/lib/shortcuts.svelte.ts` — global shortcut registry.
- (Planned) `frontend/logging/src/lib/ui/KeyboardHelp.svelte` — help overlay bound to `?`.
- Memory `project_sm_cat_precedence_rule` — the CAT lockout that gates CAT-mutating shortcuts.
