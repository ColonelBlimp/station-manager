# W-0014 — Preserve deferred product workstreams without scheduling them

**Status:** Parked inventory — not queued for implementation
**Selected:** Not selected
**Outcome:** Product ideas remain discoverable without inflating the ranked backlog or implying an
implementation commitment.

## Inventory requiring a future go-ahead or design

- LoTW and eQSL forwarding; awards tracking; logbook statistics and analytics;
- the accepted inbound DX-cluster direction in ADR 0053;
- multi-logbook management, DB backup/restore/integrity UI, and contest definitions/scoring/export;
- POTA/activation workflow and predictive callsign assistance;
- operator profiles, whole-log Dashboard map, propagation/conditions panel, movable/dockable
  navigation, voice keyer/phone-CW auto-CQ/QSO copilot, and a community pile-up status site;
- SM Cloud P1 beyond the current dogfood phase and the DB-manager SPA/data-validation surface.

## Gates

Each workstream needs an operator go-ahead and its own dossier or ADR before implementation. Network
services require authoritative protocol/security evidence; TX-capable phone/CW work explicitly
crosses the present narrow-daemon boundary and needs a new safety decision. This inventory does not
rank its members and must not become a second roadmap.

## References

- [`ADR 0053`](../decisions/0053-inbound-dx-cluster-spot-alerts.md)
- [`docs/reviews/oss-maintainability-plan.md`](../reviews/oss-maintainability-plan.md)
- Expanded historical inventory: `d0391ed7:docs/backlog.md`.
