# Station Manager — keyboard shortcuts

**Purpose:** running inventory of every keyboard shortcut wired up in the
logging SPA, kept in sync with the code so the user-facing manual can
draw from one source. Each row pins:

- **Where** it fires (window-level, or a specific component / focus
  context).
- **The keys** themselves, including modifier behaviour on macOS where
  it differs.
- **What it does**, in operator language.
- **Source** — the file + the handler function so the next contributor
  can find the gate logic without grep gymnastics.

When a new shortcut is added, append it here and reference this doc in
the user manual update. When one is removed or rebound, update the row
in the same commit as the code change. Treat this doc the same as
`docs/session-handoff.md` — stale rows are worse than no rows.

---

## Global (window-level)

These fire regardless of where focus is, with the documented exceptions.

| Keys                          | Action                            | Notes                                                                                                                                                                                                                                                                                          | Source                                                                                  |
| ----------------------------- | --------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| **ESC**                       | Clear the QSO form                | Same effect as the Clear button. No-op while the QSO Edit Overlay is open — the overlay's own ESC owns that case so dismissing the overlay doesn't also wipe the live draft underneath. After clearing, focus returns to the Callsign field.                                                  | `frontend/logging/src/lib/ui/panels/QsoPanel.svelte` — `handleKeydown`                  |
| **Ctrl + Enter** / **⌘+Enter** | Log the QSO                       | Same effect as the Log QSO button. Gated on `qsoDraft.canSubmit` — the shortcut does not bypass validation. macOS gets ⌘+Enter via `metaKey` for native feel. After a successful log, focus returns to the Callsign field. No-op while the QSO Edit Overlay is open.                            | `frontend/logging/src/lib/ui/panels/QsoPanel.svelte` — `handleKeydown`                  |
| **ESC** (in modal overlay)    | Dismiss the QSO Edit Overlay      | Active only when the overlay is open. Suppressed while a save is in flight (the operator's intent during save is to wait, not cancel). Cancel button + click-outside-the-card are the equivalent mouse paths.                                                                                  | `frontend/logging/src/lib/ui/components/QsoEditOverlay.svelte` — `handleKeydown`        |

## Field-level (require focus in a specific input)

| Context                          | Keys              | Action                                              | Notes                                                                                                                                                                                                                                                       | Source                                                                                |
| -------------------------------- | ----------------- | --------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| Callsign field (focus inside)    | **Tab**           | Trigger enrichment + start the QSO timer            | Only fires when the value is a valid callsign per `isValidCallsign`. `Shift+Tab` is the normal reverse-Tab (no enrichment trigger). On Tab the callsign is normalised to uppercase before being sent to enrichment. Cache hits are silent; slow lookups (>500ms) get a sticky toast. | `frontend/logging/src/lib/ui/components/Callsign.svelte` — `handleKeydown`            |
| VFO frequency input (focus inside) | **Enter**       | Commit the typed frequency                          | Parses the typed value via `parseFrequency` and emits Hz to the parent via `onCommit`. Empty or unparseable input reverts silently — no commit, parent's value stays authoritative.                                                                          | `frontend/logging/src/lib/ui/components/VfoInput.svelte` — `handleKeydown`            |
| VFO frequency input (focus inside) | **ESC**         | Cancel the edit                                     | Reverts the displayed value to the parent's `value` and blurs the input. The parent's authoritative frequency is unchanged.                                                                                                                                  | `frontend/logging/src/lib/ui/components/VfoInput.svelte` — `handleKeydown`            |

## Component activation (button-role keyboard parity)

These mirror what a mouse click on the same element would do — they exist
so a keyboard-only operator never gets stuck.

| Element                                    | Keys                  | Action                                              | Notes                                                                                                                                                                                                                                                                                                                          | Source                                                                                  |
| ------------------------------------------ | --------------------- | --------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| VFO box (rig label "VFO-A" / "VFO-B")     | **Enter** / **Space** | Select that VFO (swap)                              | Equivalent to clicking the box. The box itself is intentionally NOT in the keyboard tab order (`tabindex={-1}`) per ADR 0009 — operators reach the swap action via mouse or the planned Ctrl+\\ shortcut (deferred). Disabled while CAT is live and the box is already selected.                                              | `frontend/logging/src/lib/ui/components/VfoBox.svelte` — `handleKeydown`                |
| SessionPanel row                           | **Enter** / **Space** | Open the QSO Edit Overlay for that QSO              | Equivalent to clicking the row. Fires the same `GET /v1/qso/{uuid}` → populate flow. While a fetch is in flight or another overlay is already open, the click is ignored to prevent double-trigger.                                                                                                                            | `frontend/logging/src/lib/ui/panels/SessionPanel.svelte` — inline `onkeydown` on the `<tr>` |
| QSO Edit Overlay backdrop                  | **Enter** / **Space** | Dismiss the overlay                                 | Only fires when the focus target is the backdrop itself, not a descendant. Equivalent to clicking outside the modal card. Suppressed while a save is in flight.                                                                                                                                                                | `frontend/logging/src/lib/ui/components/QsoEditOverlay.svelte` — inline `onkeydown` on the backdrop `<div>` |

## Browser / native (called out for completeness)

These are not project-wired but worth documenting for the user manual:

- **Tab / Shift+Tab** — normal field navigation. The form's tab order is
  Callsign → RST Sent → RST Rcvd → Mode → VFO (selected one only;
  non-selected VFO is skipped unless split mode is on) → Name → QTH →
  Comment → Date → Time On → Time Off → Clear → Log QSO.
- **ESC** in a `<select>` (e.g. Mode dropdown) — closes the dropdown
  natively. The window-level ESC handler still fires the Clear action
  on the same keypress; this is intentional and the existing behaviour
  hasn't been a friction point.
- **ESC** in a date / time picker — varies by browser; native pickers
  typically close on ESC, then the window-level Clear fires on a second
  ESC. Not project-controlled.
