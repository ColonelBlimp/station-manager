# Station Manager — keyboard shortcuts

**Purpose:** running inventory of every keyboard shortcut wired up in the
Operate surface of the current app (`frontend/app`), kept in sync with the
code so the user-facing manual can draw from one source. Each row pins:

- **Where** it fires (window-level, or a specific component / focus
  context).
- **The keys** themselves, including modifier behaviour where it differs.
- **What it does**, in operator language.
- **Source** — the file + the handler function so the next contributor
  can find the gate logic without grep gymnastics.

When a new shortcut is added, append it here and reference this doc in
the user manual update. When one is removed or rebound, update the row
in the same commit as the code change. Treat this doc the same as
`docs/session-handoff.md` — stale rows are worse than no rows.

---

## Global (window-level)

Two `<svelte:window>` handlers own these. The **rig-control family**
(`Shift+Ctrl+…`) lives in `RigKeys.svelte`, mounted once in `Operate.svelte`,
so it is **Operate-wide — identical in Phone/CW and FT8**, binding the shared
actions in `rig.svelte.ts` (one behaviour, not a per-mode keymap). The
**logging family** (Esc, Ctrl+Enter, F2, F3, Shift+Enter, Shift+↑/↓) lives in
`LoggingCard.svelte` and is therefore **Phone/CW-only**: that window listener
lives and dies with the card. The two use disjoint modifier namespaces, so they
never touch the same key. An open modal (Session-panel edit, Export, Duplicate
dialog) suppresses both families.

| Keys                          | Action                            | Notes                                                                                                                                                                                                                                                                                          | Source                                                                                  |
| ----------------------------- | --------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| **ESC**                       | Clear the QSO form                | Same effect as the Clear button — clears the draft, then returns focus to the Callsign field. Inert while the Session-panel edit modal, the Export modal, or the Duplicate dialog is open — each owns its own Escape, so dismissing it never also wipes the live draft underneath.                | `frontend/app/src/lib/operate/LoggingCard.svelte` — `windowKeydown`                     |
| **Ctrl + Enter**              | Log the QSO                       | Same effect as the Log QSO button. Routed through `logDraft`, which enforces the CAT/rig gate and validation — the shortcut does not bypass them. On a successful log, focus returns to the Callsign field (a refusal leaves focus where the operator is fixing things) and the logged comment joins the recent-comments paste list. Bound on `ctrlKey` only (no `metaKey`/⌘ variant here — unlike the edit modal's save). Inert while the Session-panel edit modal, Export modal, or Duplicate dialog is open. | `frontend/app/src/lib/operate/LoggingCard.svelte` — `windowKeydown` (→ `logAndRefocus` → `logDraft`) |
| **F2**                        | Lookup-only "peek"                | Reveals the worked-before panel for the typed call WITHOUT starting the QSO timer (`openWorkedForQso`) — so the operator can scan prior contacts and station info and decide whether to commit: Tab is the commit signal, F2 is the peek. Enrichment already auto-loads (EnrichmentCard); F2 adds the contact-history reveal. Gated on a valid callsign (`isValidCallsign`); empty or invalid input is a silent no-op. Focus-independent (a function key has no typing collision), and no-op on auto-repeat — one press is one action. | `frontend/app/src/lib/operate/LoggingCard.svelte` — `functionKeydown`                   |
| **F3**                        | Freeze Time Off / start the clock | If the clock is ticking, freeze Time Off (`holdOffTimes`) — the contact has ended while its details are still being typed. If the clock hasn't started and a callsign is typed, start it (`startQso`). After a hold it is a SILENT no-op — re-ticking would overwrite a hand-set end time, the exact value `holdOffTimes` exists to protect. Focus-independent; no-op on auto-repeat (one press is one action); `preventDefault`'d to defang the browser's default Find-Next. | `frontend/app/src/lib/operate/LoggingCard.svelte` — `functionKeydown`; methods on `frontend/app/src/lib/operate/qso.svelte.ts` — `startQso` / `holdOffTimes` |
| **Shift + Enter**             | Stack the current callsign        | Push the trimmed/uppercased callsign onto the pile-up stack (`callsignStack`), then clear the draft — the same "start fresh" as Esc, plus the push. Only from the Callsign field or from no field (in the Notes textarea Shift+Enter is an ordinary newline and does NOT stack). Empty or invalid callsign is a silent no-op; a duplicate push is silently rejected. Focus returns to the Callsign field. | `frontend/app/src/lib/operate/LoggingCard.svelte` — `pileupKeydown` (helper `stackCall`); state on `frontend/app/src/lib/operate/callsignStack.svelte.ts` |
| **Shift + ↑**                 | Pop the newest stacked callsign   | Load the top (most-recently-stacked) call into the draft and remove it from the stack (`loadPopped` ← `callsignStack.popTop`). Stands down while a text field is focused (Shift+Arrow is native line-select there), and is guarded `!ctrlKey && !metaKey` so it does NOT fire for Shift+Ctrl+↑ (the ±100 Hz coarse freq-step below). `preventDefault`'d. Empty stack is a no-op. Focus moves to the Callsign field. | `frontend/app/src/lib/operate/LoggingCard.svelte` — `pileupKeydown`; `frontend/app/src/lib/operate/callsignStack.svelte.ts` — `popTop` |
| **Shift + ↓**                 | Pop the oldest stacked callsign   | Load the bottom (oldest-stacked) call into the draft and remove it from the stack (`loadPopped` ← `callsignStack.popBottom`). Same focus/guard semantics as Shift+↑ (`!ctrlKey && !metaKey`; not while typing; `preventDefault`'d) — Shift+Ctrl+↓ is the ±100 Hz coarse freq-step. | `frontend/app/src/lib/operate/LoggingCard.svelte` — `pileupKeydown`; `frontend/app/src/lib/operate/callsignStack.svelte.ts` — `popBottom` |
| **Shift + Ctrl + \\**          | Swap the VFOs (A↔B)               | Always a true SWAP. CAT live + `swap_vfo` → `SV;` (optimistic `vfoB` mirror + rollback, confirm-by-push); CAT off → toggles the local selection; no capability → silent no-op. Fires regardless of focus, `preventDefault`'d. NB clicking the unselected VFO box is NOT the same action on a rig with `select_vfo` (e.g. FTdx10 `VS`): the click SELECTS — operation moves, both boxes keep their contents — whereas the keyboard `\` always swaps. | `frontend/app/src/lib/operate/RigKeys.svelte` — `onKeydown`; action `frontend/app/src/lib/operate/rig.svelte.ts` — `swapVfo` |
| **Shift + Ctrl + ]** / **Shift + Ctrl + [** | Band up / band down  | `band_up` / `band_down` (`BU0;` / `BD0;`, `0` = main band) — the rig restores that band's stack memory (last freq + mode) and the new band shows via the resulting freq push (confirm-by-push). CAT-live + capable only (no manual equivalent — no rig to step); silent no-op otherwise. Fires regardless of focus, `preventDefault`'d. | `frontend/app/src/lib/operate/RigKeys.svelte` — `onKeydown`; actions `frontend/app/src/lib/operate/rig.svelte.ts` — `bandUp` / `bandDown` |
| **Shift + Ctrl + 1…0**        | Jump to band (direct)             | The digit→band map **follows the operator's `station.operating_bands` order** (digit 1 = first configured band …), NOT a fixed 160m…6m table — so a pruned band list gets clean digits. `set_band` → `BS<code>;` with the band NAME. Matched on the physical digit (`e.code`) since Shift+digit is a symbol. **Default-set caveat:** with no `operating_bands` configured, digits map onto the HF..6m default in order (digit 0 = 10m); 6m is index 10 → not reachable by a single digit until the operator configures their bands (accepted — config is the intended path). CAT-live + `set_band` only. `preventDefault`'d. | `frontend/app/src/lib/operate/RigKeys.svelte` — `onKeydown` (`bandForDigit`); action `frontend/app/src/lib/operate/rig.svelte.ts` — `bandForDigit` / `selectBand` |
| **Shift + Ctrl + ↑** / **Shift + Ctrl + ↓** | Tune the selected VFO ±100 Hz (coarse) | Nudges the selected VFO ±100 Hz (↑ = higher). CAT live → `set_freq` (`FA`) for VFO-A, `set_freq_b` (`FB`) for VFO-B, each capability-gated on its own op; CAT off → adjusts the single manual freq field. Uses an optimistic per-VFO target so fast key-repeat tracks cleanly despite push lag (re-syncs to the displayed freq after a ~350 ms pause). **Gated on `!typing`** — the freq-step arrows stand down while a text field is focused, so Shift+Ctrl+Arrow stays native word-select there. `preventDefault`'d. | `frontend/app/src/lib/operate/RigKeys.svelte` — `onKeydown`; action `frontend/app/src/lib/operate/rig.svelte.ts` — `nudgeFreqCoarse` |
| **Shift + Ctrl + →** / **Shift + Ctrl + ←** | Tune the selected VFO ±10 Hz (fine) | Same routing and `!typing` guard as the coarse step, 10 Hz per press (→ = +10 Hz). All freq-step keys sit on the arrow cluster (↑/↓ coarse, →/← fine, Alt+↑/↓ hop). **Page keys were avoided deliberately** — Firefox's Ctrl+Shift+PageUp/Down move-tab won't yield to `preventDefault`. `preventDefault`'d. | `frontend/app/src/lib/operate/RigKeys.svelte` — `onKeydown`; action `frontend/app/src/lib/operate/rig.svelte.ts` — `nudgeFreqFine` |
| **Shift + Ctrl + Alt + ↑** / **Shift + Ctrl + Alt + ↓** | Tune the selected VFO ±5 kHz (band hop) | Same behaviour and routing as the coarse/fine steps, ±5 kHz per press (↑ = higher) — a quick hop across a band, handled inside the same Shift+Ctrl arrow branch by the **Alt** modifier. **Caveat:** `Ctrl+Alt+↑/↓` is a common Linux window-manager binding; the WM may grab it before the browser, where `preventDefault` can't reclaim it. `preventDefault`'d. | `frontend/app/src/lib/operate/RigKeys.svelte` — `onKeydown`; action `frontend/app/src/lib/operate/rig.svelte.ts` — `nudgeFreqJump` |
| **ESC** (edit modal open)     | Dismiss the QSO edit modal        | Active only when the Session-panel edit modal is open. Suppressed while a save is in flight (the operator's intent during save is to wait, not cancel). The Cancel button is the equivalent mouse path.                                                                                        | `frontend/app/src/lib/logbook/EditQsoModal.svelte` — `onKeydown`                        |
| **Ctrl + Enter** / **⌘+Enter** (edit modal open) | Save the QSO edit    | Active only when the edit modal is open — saves the current patch (`PATCH`). `metaKey` IS accepted here (⌘+Enter), unlike the logging card's log shortcut. Suppressed while a save is already in flight. The Save button is the equivalent mouse path.                                          | `frontend/app/src/lib/logbook/EditQsoModal.svelte` — `onKeydown`                        |

> **Tune carrier (ADR 0027) is deliberately click-only.** The Tune button
> (`frontend/app/src/lib/operate/RigPanel.svelte` — inline `<button onclick={onTune}>`
> → `toggleTune`) has **no** keyboard shortcut, unlike the rest of the
> rig-control family above. Keying a transmission should be a deliberate, visible
> action — not a stray keypress — so the omission is a safety choice, not a gap.
> Don't "complete the set" with a Shift+Ctrl+T.

> **Guards.** The rig family is `Ctrl+Shift`+`e.code` (the physical key, since
> Shift mutates the character). Swap / band / digit fire regardless of focus; the
> freq-step **arrows** stand down while a text field is focused (`!typing`). A
> modal overlay open (`operate.exportOpen` / `submitState.duplicate` /
> `sessionEdit.row`) suppresses the whole family; the docked pile-up drawer does
> not. Only a genuine command rejection toasts (silent no-ops don't), so
> key-repeat never spams.

> **Configurability status.** The key→action map is a hardcoded if-chain in
> `RigKeys.svelte`; only the digit→band piece is data-driven (from
> `operating_bands`). A global user-configurable keybinding block is deferred
> (NOT op-profile — TX-adjacent keys must stay stable across a profile switch).
> When it lands, lift the if-chain to a `code → action-id` table the config can
> override.

## Field-level (require focus in a specific input)

| Context                          | Keys              | Action                                              | Notes                                                                                                                                                                                                                                                       | Source                                                                                |
| -------------------------------- | ----------------- | --------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| Callsign field (focus inside)    | **Tab** / **Enter** / **Space** | Commit the callsign and start the QSO timer | All three commit the callsign (`commitCall` → `startQso`): stamp Date/Time On, start the ticking Time Off (the QSO timer), open the worked-before panel, and warn if the rig gate isn't confirmed. **Tab** commits only when the field is non-empty and keeps its native focus-advance to RST. **Enter** commits without moving focus (`preventDefault` — no `<form>` submit) and **Space** both commits and is swallowed (a callsign is a single token, so a literal space is never wanted); Enter/Space require a VALID call (`isValidCallsign`). Modified variants bail untouched so the window-level meanings survive — `Shift+Enter` = stack, `Ctrl+Enter` = log. | `frontend/app/src/lib/operate/LoggingCard.svelte` — `callKeydown` (→ `commitCall`)     |
| Comment paste-list popover (open)  | **ESC**         | Close the paste-list dropdown                       | Active only when the Comment field's recent-comments popover is open (the clipboard-list icon trigger). The popover is focused on open so the handler lives on it, not the window; it `stopPropagation`s so the form-level ESC (clear QSO) does NOT also fire. Closes the popover and returns focus to the trigger. Outside-click and picking an item also close it (picking replaces the Comment field value). | `frontend/app/src/lib/operate/CommentField.svelte` — `onListKeydown` (history in `frontend/app/src/lib/operate/commentHistory.svelte.ts`) |

## Component activation (button-role keyboard parity)

These mirror what a mouse click on the same element would do — they exist
so a keyboard-only operator never gets stuck. Most are native `<button>`
elements, so Enter/Space activation is the browser's, not project-wired.

| Element                                    | Keys                  | Action                                              | Notes                                                                                                                                                                                                                                                                                                                          | Source                                                                                  |
| ------------------------------------------ | --------------------- | --------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| VFO box (rig label "VFO-A" / "VFO-B")      | **Enter** / **Space** | Select / swap onto that VFO                         | The box is a native `<button>` (rendered when CAT is live and the rig exposes `select_vfo` or `swap_vfo`); Enter/Space is native activation, identical to a click (`selectVfo`). On a rig with `select_vfo` it SELECTS that VFO (operation moves, both boxes keep their contents); without it, it swaps. A no-op for the already-selected box. When CAT lacks both ops the box is a disabled read-only field. | `frontend/app/src/lib/operate/RigPanel.svelte` — VFO `<button onclick={() => onSelectVfo(v)}>` (→ `selectVfo`) |
| SessionPanel row (callsign cell)           | **Enter** / **Space** | Open the QSO edit modal for that QSO                | The callsign cell is a native `<button>`, so Enter/Space is native activation (not a `<tr>` keydown). `sessionEdit.open` hydrates the full QSO (`GET /v1/qso/{uuid}`) and opens the edit modal. While a fetch is in flight or a save is already running, the activation is ignored to prevent double-trigger.                    | `frontend/app/src/lib/operate/SessionPanel.svelte` — callsign-cell `<button onclick={() => sessionEdit.open(q)}>` (state in `frontend/app/src/lib/operate/sessionEdit.svelte.ts`) |
| Callsign field stack icon (`≡`)            | **Click**             | Stack the current callsign (mouse equivalent of Shift+Enter) | Mouse-only affordance — `tabindex={-1}` keeps it out of the Callsign→RST tab order so the keyboard flow is unbroken. Fires the same `stackCall` as Shift+Enter (push + clear, same validate-or-no-op). The `title` ("Stack callsign (Shift+Enter)") exposes the shortcut for discoverability. | `frontend/app/src/lib/operate/LoggingCard.svelte` — `≡` `<button onclick={stackCall}>` |
| Call Stack drawer entry (callsign)         | **Click**             | Pop that callsign into the form                     | Loads the clicked call into the draft and removes it from the stack (pop = load; single action / single effect). Mouse equivalent of Shift+↑ / Shift+↓ for picking by visual position rather than stack order. Focus moves to the Callsign field afterwards. | `frontend/app/src/lib/operate/CallsignStackPanel.svelte` — `pop(index)` (→ `callsignStack.popAt`) |
| Call Stack drawer entry `×`                | **Click**             | Remove that one callsign (discard, don't load)      | Per-row discard — drops just that entry WITHOUT loading it into the form (contrast the row-body click, which loads). | `frontend/app/src/lib/operate/CallsignStackPanel.svelte` — `callsignStack.removeAt(index)` |
| Call Stack drawer "Discard all" button     | **Click**             | Discard the entire stack                            | Drops every entry. No undo. The drawer's header `×` only CLOSES the drawer (`setCallStack(false)`) — it does NOT discard; closing and clearing are separate actions here. | `frontend/app/src/lib/operate/CallsignStackPanel.svelte` — `callsignStack.clear()` (header `×` → `setCallStack(false)`) |
| FT8 Band Activity — a calling-you decode   | **Ctrl/Cmd + click**  | Bag that caller into the pick queue                 | FT8 view only (ADR 0067). On a row that is a station *calling you* (`kind === 'call'`), Ctrl/Cmd+click BAGS the station into the daemon's pick queue — capture-only, no TX — so it works even mid-QSO. A **plain click** instead works the station now, gated on the session's Answer mode ("I pick") and idle. Daemon refusals toast (station not listed / session not in pick mode). The old SPA-curated ctrl-click stack retired with this ADR — the queue lives daemon-side now. | `frontend/app/src/lib/operate/Ft8BandActivity.svelte` — `onRowClick` (Ctrl/Cmd → `bagAnswerer`; plain click → the work/answer path) |
| FT8 pile-up drawer                         | **ESC**               | Close the drawer                                    | Closes the slide-over and leaves the run intact — a slide-over close shouldn't destroy state. (Stopping the run itself lives on the run surface.) | `frontend/app/src/lib/operate/PileupDrawer.svelte` — `onKeydown` (→ `setPileup(false)`) |
| FT8 pile-up drawer "Bag" (listed caller)   | **Click**             | Bag a listed caller into the queue                  | Moves a station from the "Calling you" list into the bagged queue; bagged stations are worked automatically in bag order by the drain. | `frontend/app/src/lib/operate/PileupDrawer.svelte` — `bagAnswerer` |
| FT8 pile-up drawer "Work" (listed caller)  | **Click**             | Work a listed caller now                            | Works the chosen "Calling you" station immediately, ahead of the queue. | `frontend/app/src/lib/operate/PileupDrawer.svelte` — `pickAnswerer` |
| FT8 pile-up drawer bagged entry `×`        | **Click**             | Unbag that caller (back to the listed callers)      | Per-row unbag — returns just that station from the bagged queue to the "Calling you" list. | `frontend/app/src/lib/operate/PileupDrawer.svelte` — `unbagAnswerer` |
| FT8 pile-up drawer header `×`              | **Click**             | Close the drawer                                    | Closes the slide-over only; the run is left intact (same as ESC). | `frontend/app/src/lib/operate/PileupDrawer.svelte` — `setPileup(false)` |
| FT8 pile-up drawer **Resume**              | **Click**             | Resume the paused drain                             | Shown only when the drain is paused with a non-empty queue. Re-enables auto-draining of the bagged queue. | `frontend/app/src/lib/operate/PileupDrawer.svelte` — `resumeDrain` |

## Browser / native (called out for completeness)

These are not project-wired but worth documenting for the user manual:

- **Tab / Shift+Tab** — normal field navigation through the logging card's
  inputs (Callsign → RST Sent → RST Rcvd → Date/Time On → Date/Time Off →
  Name → Comment → Contact-details disclosure → Clear → Log QSO), then on to
  the rest of the Operate surface. Rig fields (frequency / mode / VFO) live in
  the separate Rig panel.
- **ESC** in a `<select>` (e.g. the Rig panel's Mode dropdown) — closes the
  dropdown natively. In Phone/CW the window-level ESC handler still fires the
  Clear action on the same keypress; this is intentional and has not been a
  friction point.
- **ESC** in a date / time picker — varies by browser; native pickers
  typically close on ESC, then the window-level Clear fires on a second
  ESC. Not project-controlled.
