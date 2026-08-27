# W-0012 — Complete routed operator-experience follow-ups

**Status:** Open — staged after release correctness gates
**Selected:** Not selected
**Outcome:** Remaining UI, map, onboarding, and diagnostic improvements have one routed home and do
not compete with the app-shell, notification-history, or UI-cohesion dossiers.

## Routed outcomes

- [W-0001](../archive/work/W-0001-durable-notifications.md) owns durable notification history; ADR 0060 owns alert
  placement and ADR 0061 the proposed structured event store. Never expose `smd.log` as the event
  history.
- [W-0003](../archive/work/W-0003-retire-legacy-operator-spas.md) owns canonical app routing and remaining shell
  consolidation; [W-0004](../archive/work/W-0004-complete-app-ui-cohesion.md) owns themes, occupancy colors, and
  ambient build identity.
- **Operate workflow:** reorganize the contact view; decide whether QTH belongs on the Phone/CW
  card; surface active-versus-configured rig state; revive only dead SSE clients on visibility
  changes; retain focused fixes for session email/filter/toolbar feedback and card layout.
- **Maps and tables:** dogfood-validate shipped map catch-up/zoom behavior; decide solar-time overlay
  versus a world-time widget, map band-source policy, and session column resizing/sorting before
  implementation. The whole-log Dashboard map remains separate from the shipped time-window map.
- **Onboarding/preferences:** reduce non-Linux first-run friction; add download-site install content
  from the canonical install guide; keep beginner help, profiles, and `default_logbook.id` wiring
  deferred until their consuming workflow exists.

## Verification boundary

Every slice states its operator-visible outcome and nearest confusable state first. Frontend work
runs the affected SPA's lint, format check, Svelte check, and Vitest suite. Layout fixtures must make
overflow, hidden-tab recovery, focus ownership, or stale state observable; screenshots alone are not
the acceptance test.

## References

- [`ADR 0060`](../decisions/0060-operator-alert-surfaces-and-stuck-tx-overlay.md)
- [`ADR 0061`](../decisions/0061-consolidated-operator-event-log.md)
- Expanded pre-decomposition inventory: `d0391ed7:docs/backlog.md`.
