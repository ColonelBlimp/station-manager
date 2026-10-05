---
number: 0087
title: Switch archives only from Settings and state the loss
status: Accepted
date: 2026-10-05
---

# 0087 — Switch archives only from Settings and state the loss

## Context

ADR 0085 and ADR 0086 protect unlogged work during an archive switch. A window that holds a
Phone / CW draft saves it to browser storage before its rebind reload. The draft is offered back
under **Unlogged QSOs**, and the recovered QSO is logged with its original attribution. That
needed browser storage, Web Locks, a Restore path, a guarded submit (`expect_*`), an attribution
read endpoint and an attribution-freshness contract driven by `config.updated`.

No operator had ever lost a QSO to an archive switch; the protection was against an imagined
failure. The RS8 / RS11 drill (2026-10-04 / 05, W-0021) failed twice. Each failure came from the
mechanism's own parts interacting, not from operating:

- A disconnect cleared the saved attribution. `8d50dd01` addressed that.
- The switch's own config write then raised `config.updated`, which cleared it again.

The operator's judgement: "looks clever but is just a pain to debug and support and with little
used feature — really it is protecting against something we only 'think' might be an issue."

One requirement is unchanged. Losing an unfinished entry must never allow logging into the wrong
archive. The archive-binding gate and the forced rebind reload (ADR 0071, W-0021) stay.

## Decision

Accepted records the policy decision; the removal is not yet implemented.

Switch archives only from **Settings → Archives**. Gate the switch on **Activate**. Accept and
state that unlogged work in any other window is lost.

1. **One entry point.** The header archive selector becomes display-only; Settings → Archives is
   the only place a switch starts.
2. **Activate gate.**
   - **Empty form:** if the Phone / CW form in this window is empty, the switch proceeds as
     today.
   - **Unlogged QSO:** if the form holds one, Activate shows it and offers **Discard it and
     switch** or **Cancel**. This replaces the refusal toast (`refuseOverUnloggedWork`).
   - **Navigation is lossless:** opening Settings clears nothing.
   - **Other windows:** the Activate confirmation also states that unlogged work in any other
     window will be lost.
3. **Stated loss, no toast.**
   - **The manual** states it. Switching restarts Station Manager and reloads every open window.
     Unlogged work in any other window is lost: an unlogged Phone / CW QSO, an FT8 / FT4 exchange
     not yet completed, and unsaved Settings changes. Logged QSOs are never affected.
   - **No notification** in the reloaded window. No state is carried across the reload solely
     to announce a loss.
4. **Recovery removed.**
   - **Browser side:**
     - The rebind save (the preserver and the held-save gate's Retry / Discard and reload).
     - The **Unlogged QSOs** control and panel, Restore, the recovered form and its submit.
     - The Web Locks claim, the cross-tab draft channel and the attribution source.
   - **Daemon side:** `GET /v1/submit-attribution` and the recovered-submit expectation check.
   - **What stays:** the daemon's ordinary attribution stamping (MY_RIG, OPERATOR, MY_NAME).
5. **Old pages are refused, not ignored.**
   - **Which submits:** a `POST /v1/qso` that carries any `expect_*` query key.
   - **The refusal:** `409 reload_required`, storing nothing. The message tells the page to
     reload. The expectation is never silently dropped.
6. **Existing browser records are orphaned.**
   - Records already in a browser's `saved-qso-drafts` IndexedDB store stay where they are.
   - The application no longer reads, writes, lists, migrates or deletes them.
   - There is no automatic deletion or migration.
7. **The rebind stays.**
   - The archive-switch gate stays: Phone / CW and FT8 / TX intents are refused while a switch is
     in flight or unresolved.
   - So does the instance-compare reload after a switch. Only the draft-saving step before the
     reload goes.
8. **The Activate prompt (operator, 2026-10-05).**
   - **Mechanism:** the browser's `window.confirm`, as Activate already uses. A styled dialog with
     named buttons stays its own item.
   - **Content:** the prompt names the destination archive.
   - **Unlogged entry:** when this window holds one, including a partial one, the prompt names
     it and states that OK discards this window's entry and switches.
   - **Other windows:** it always warns that unlogged work in any other window will be lost.
   - **Cancel** sends nothing and preserves everything.
9. **A Log in flight (operator, 2026-10-05).**
   - **Refused:** Activate is refused while this window's Log is in flight, checked both before
     the confirmation and again before the activation starts.
   - **Allowed:** once the outcome is unknown, switching is allowed. The prompt then says the
     QSO **may already be logged** and should be checked in the original archive's Logbook.

## Alternatives considered

### Keep ADR 0085 / 0086 and fix the attribution interaction

Two fixes were open:

- Stop the switch's own config writes publishing `config.updated`.
- Or keep the attribution across `config.updated` as unconfirmed.

These were candidate fixes for the traced second failure, not proven by tests. Rejected because
the first fix exposed another interaction, and the feature answers no observed loss. Its support
and debug cost is not justified.

### Clear the logging space when navigating to Settings

Proposed by the operator as point 1. Rejected in discussion (2026-10-05):

- It ties the loss to a navigation rather than to the switch, and Settings is opened
  mid-operation for unrelated reasons.
- It does not reach the other-window case.

The gate on Activate gives the same outcome, with an explicit choice.

### Notify the reloaded window of a discarded draft

A toast in the reloaded window, for example "The archive changed in another window; any unlogged
QSO here was discarded". Rejected: it carries state across the reload only to announce a loss
that the Activate warning and the manual already state.

### Ignore `expect_*` from old pages

Rejected. A page loaded before the upgrade would believe its QSO was attribution-checked when it
was not. An explicit refusal tells it to reload.

### Delete or migrate existing browser records

Rejected. It would need a migration path in code that otherwise disappears. The only known
records are drill records. Orphaned records are inert.

## Consequences

- **Removed:**
  - ADR 0085's RS1–RS25 behaviour and ADR 0086's indicator.
  - The browser-local recovery path, the attribution endpoint and the expectation check.
  - The tests that prove them.
  - The SPA loses most of `frontend/app/src/lib/drafts/`.
- **Accepted:**
  - An unlogged Phone / CW QSO or an unfinished FT8 / FT4 exchange in another window is lost on
    a switch, without notice in that window.
  - The operator is told at Activate and in the manual.
- **Old pages:** a page loaded before the upgrade, submitting a recovered QSO, gets `409
  reload_required`. Its record stays in its browser storage, unreachable after the reload.
- **Orphaned storage:** browser storage may keep orphaned `saved-qso-drafts` records
  indefinitely.
- **Not decided here:**
  - `config.updated` has no consumer other than the attribution source. Whether the daemon keeps
    publishing it is decided in the removal change.
  - The same goes for the rig snapshot kept for saving (`rigReadingForSave`).
- **Order of work:**
  - The removal is its own change, after this ADR.
  - `docs/v2-design/api-endpoints.md` changes with it: the endpoint is removed, and
    `reload_required` replaces `attribution_changed`.
  - The manual changes with it as well.

## Triggers to revisit

- An operator reports losing a QSO worth logging to an archive switch in another window. Then
  reconsider a narrow recovery, designed from that incident.
- Archive switching becomes frequent (for example, per contest or per operator). The loss window
  then matters more than it does for a rare, drastic switch.
- Multi-operator use with several stations sharing one daemon.

## References

- ADR 0085 (superseded), ADR 0086 (superseded), ADR 0071 (archive programme).
- `docs/work/W-0021-qso-archives.md`: drill results, the first and second causes, and the
  2026-10-05 redesign discussion and rulings.
- Commits `c551c828`…`152fb36b` (Restore commit 4, option A), `8d50dd01` (attribution across a
  disconnect).
