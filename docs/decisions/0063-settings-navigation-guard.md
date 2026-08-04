---
number: 0063
title: Settings warns on the way out rather than preserving drafts on return
status: Accepted
date: 2026-08-04
---

# 0063 — Settings navigation guard

## Context

Settings renders behind a router branch (`App.svelte`:
`{:else if router.view === 'config'}`), so navigating away **unmounts** it while
the section state modules — `stationState`, `rigsState`, `forwardingState`,
`emailState`, `enrichmentState`, all module-level singletons — survive.

That produces a loss with a counter-intuitive shape, and getting the shape wrong
is what makes the obvious fix the wrong one:

> **The edits are not lost when you leave. They are lost when you come back** —
> the remount's `onMount` → `load()` → `#apply()` overwrites the draft with the
> daemon's stored values.

Nothing on screen marks either moment. A half-filled SMTP block, or a password
marked for removal, disappears between two clicks with no warning. Because the
destruction happens on *return*, there is no natural "as you leave" event to
hang a warning on; the guard has to be placed deliberately.

Two facts found while scoping this narrowed it considerably — and a third,
found only in review, reversed part of the design:

- **Switching between Settings tabs was never at risk.** All five sections stay
  mounted and hide via CSS (`Settings.svelte`), a deliberate fix from the
  2026-07-20 review precisely so tab switches don't remount and reload.
- **Rigs *appeared* to survive the round trip.** Its `#applyFetched`
  re-baselines only *pristine* drafts, so a dirty draft keeps its edits and its
  baseline (review Rigs-editor #6).
- **It does not.** `load()` wipes `drafts` and `baselines` outright two
  statements after calling `#applyFetched` — the preservation is real and is
  immediately undone by its own caller. The first version of this ADR, and the
  implementation under it, exempted Rigs from the prompt on the strength of the
  first half of that sequence; rig edits were silently lost on return exactly
  like everyone else's. The reading error was stopping at the step that looked
  decisive instead of reading to the end of the function. Corrected before this
  ADR was committed; recorded because the *shape* of the error is the reusable
  part.

## Decision

**Guard the exit; keep discarding on return.** A blocking, cancellable
`window.confirm` naming the affected sections, plus a `beforeunload` warning for
tab close and reload. Confirming the discard performs it immediately.

Concretely (`frontend/app/src/lib/config/unsaved.ts`):

- `router.svelte.ts` gains a single `setLeaveGuard` slot — one slot, not a
  registry, because there is exactly one guarded view.
- **Three exits are guarded, not one.** `navigate()` (sidebar links), `popstate`
  (browser Back/forward, which never passes through `navigate()`), and
  `setMode()` (the sidebar's Phone/FT8 buttons — `OperateNav` lives in the
  always-visible sidebar, so it leaves Settings without touching `navigate()`).
- Refusing a `popstate` **puts the URL back**. It fires after the address bar
  has already moved, so returning `false` alone would leave the view reading
  `config` under the previous entry's path, and a reload would then land
  somewhere the operator never chose.
- **One dirty-set.** Every section is treated alike; there is no exemption. An
  earlier draft had two sets (Rigs excluded from the leave prompt, included in
  the unload one) on the false premise above.
- **A save in flight refuses the exit outright**, with a toast rather than a
  prompt. The dialog promises a discard, and a PUT already on the wire cannot be
  recalled: it lands, the daemon persists the very edits the operator was told
  were thrown away, and `#apply()` writes them back into the form. Offering a
  choice that cannot be honoured is worse than a short wait, and the wait is
  bounded — these config writes use `safeFetch`'s default 15-second timeout, so
  a wedged daemon cannot strand anyone in Settings.
- Rigs is asked via a new `anyDirty`, not `dirty`: `dirty` is scoped to the
  *selected* rig, so a rig edited and then switched away from went uncounted.
- A confirmed leave calls each at-risk section's `reset()` **there and then**,
  so the app stops holding edits it has just reported as gone.

## Alternatives considered

**Preserve the drafts across the round trip — rejected.** The appealing option:
it needs no dialog and no `popstate` interception. It loses on a concrete
hazard. All the sections build a **whole-block PUT** from the draft, so a draft
preserved across an arbitrary absence rewrites every field at its stale value on
the next save — the defect clean-room review `dcb0316e69b9` filed as P1 and
which the current unconditional reload exists to prevent. The one section that
could safely preserve is Rigs, whose save re-fetches and writes a **field-level
diff** against the draft's own baseline so a stale draft touches only the fields
actually edited — but Rigs does not in fact preserve (see Context). Preserving
safely everywhere means giving the other four that same re-fetch-and-merge save
first. A legitimate future direction, but a save-path change, not a navigation
guard.

**Preserve within the session, discard on reload — rejected.** Carries the
identical staleness cost for a narrower benefit.

**Don't block; mark Settings with an unsaved-changes dot — rejected.** Only
coherent if edits are preserved; otherwise the dot marks something already
destroyed.

**A custom in-app modal offering "Save & leave" — rejected for now.** More
helpful, but "Save" can be refused by the daemon, which needs its own answer
(stay put, surface the error) and its own rules. `window.confirm` matches the
Restart daemon prompt already in Settings and costs no new UI.

**Skip `beforeunload` — rejected.** The browser's dialog can't be worded or
styled by us and fires on reload too, which is mildly obstructive. Accepted
anyway: a reload destroys every draft including Rigs', and that is exactly the
case with no in-app moment to warn at.

## Consequences

- Leaving Settings with unsaved edits now costs one extra click. Leaving a saved
  Settings costs nothing and shows nothing — the load-bearing half, since a
  guard that fires on a clean form trains the operator to dismiss it unread.
- Rigs gains a `discardDrafts()`, since `resetDraft()` covers only the selected
  rig while edits persist per rig id.
- A refused `popstate` pushes a history entry, so the forward stack is lost.
  Standard for this pattern.
- `beforeunload` fires from any view, so closing the tab warns even from
  Operate if a Settings draft is dirty. Correct — the edits are about to die.
- The guard is installed from the **shell** (`App.svelte`'s `onMount`), not from
  Settings, which unmounts at exactly the moment the guard must still be
  listening.
- The unload handler calls **both** `e.preventDefault()` and
  `e.returnValue = true`. Per MDN (*Window: beforeunload event*): "best practice
  is to trigger the dialog by invoking `preventDefault()` on the event object,
  while also setting `returnValue` to support legacy cases", the latter
  annotated "Included for legacy support, e.g. Chrome/Edge < 119". The vitest
  rule asserts only that the event is cancelled, so the dialog itself is still
  worth one eyeball on the real browser (edit a field, press F5).

## Triggers to revisit

- **Give the four whole-block sections a re-fetch-and-merge save.** That removes
  the staleness hazard, at which point preserving drafts becomes the better
  answer and most of this ADR is superseded — no prompt and no `popstate`
  interception.
- **A second guarded view.** The single `setLeaveGuard` slot becomes a registry.
- **Abortable saves.** If the API layer grows per-request `AbortController`s the
  in-flight-save refusal could become "cancel the PUT and discard", removing the
  wait entirely.
- **Operators reporting they click through the prompt without reading.** That is
  the false-positive failure; find which rule is over-firing.
- **Settings gaining routed sub-routes** (`/config/email`). Section switches
  would then become real navigations and the tab-switch exemption would need
  re-deriving.

## References

- `frontend/app/src/lib/config/unsaved.ts` — the guard.
- `frontend/app/src/lib/config/unsaved.test.ts` — acceptance criteria in the
  file header, including the three exits and the reversion proofs taken.
- ADR 0044 — the app shell that Settings lives in.
- Review `dcb0316e69b9` — the stale whole-block-write P1 that rules out
  preserving drafts today.
- Review Rigs-editor #6 — why `#applyFetched` preserves dirty drafts, which is
  true of that method and undone by `load()` two statements later.
