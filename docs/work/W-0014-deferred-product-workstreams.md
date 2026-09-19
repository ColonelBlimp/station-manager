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

## Design exchanges

- **2026-09-19 — logbooks (create/edit/delete) and database files (create/delete), operator
  thinking aloud after the alpha.3 freeze.** Facts checked: the daemon already serves the whole
  logbook lifecycle — `POST /v1/logbook` (name, callsign, description), `PATCH` (name/description,
  callsign immutable per ADR 0055: the logbook *is* the station identity its QSOs carry), `DELETE`
  refusing `has_qsos` and `default_logbook` — but since the legacy logbook SPA was retired
  (2026-08-19, W-0003) no operator surface calls them: `lib/api/logbooks.ts` only lists, counts and
  pages, and the first-run card creates the single default logbook. Creating a second logbook is
  API-only today. All logbooks live in one authoritative store (`db/station-manager.db`, the `log`
  migration set) beside `reference.db` (catalogues and enrichment caches) and `evidence.db`; there is
  no per-logbook file, and the invariants make `logbook`/`qso`/`session`/`qso_upload` one
  transactional set — QSO plus upload-queue writes are atomic across it. Assessment recorded:
  (1) logbook management is a real gap left by the SPA retirement, cheap to close as a
  Settings → Logbooks section (create; rename/description; set default; delete surfacing the two
  daemon refusals; nearest confusable outcomes: deleting the logbook the header has selected, a
  delete that appears to work while its QSOs remain, an edit that seems to change the callsign);
  (2) "database files" per logbook is not recommended — it would split the transactional set,
  multiply migrations and break the cross-logbook queue; if the intent is backup / restore /
  integrity of the existing files, that is the DB backup/restore/integrity UI already in this
  inventory and a separate item (today: a stopped-daemon copy of the working directory, per the
  acceptance records). Rulings wanted before either becomes a dossier: which of the two the
  operator means by "database files"; where logbook management lives (Settings section versus an
  action in the Logbook view); whether a delete should offer export-then-delete for a logbook with
  QSOs or keep the refusal; whether the default logbook is set from that section (it is a
  `config.json` selector, `default_logbook_id`). Post-freeze; not part of alpha.3.
  Follow-up the same day, operator: the thought was N1MM+-style — create a new database file for a
  new log — and the operator already sees the problem: QSO history is lost to the new file.
  Assessment: agreed, and Station Manager already has the two mechanisms that give what a fresh file
  gives without the loss — a **new logbook row** in the same store (its own name, callsign, count,
  export and per-logbook contest-dupe scope, invariants §contest dupe) and the **contest mode ruled
  2026-09-12 in W-0011** (a declared id and window; contacts inside it stamped `CONTEST_ID`, so a
  contest stays queryable and exportable afterwards without leaving the main log). Either keeps
  enrichment caches, worked-before history and the forwarding queue whole. Direction recorded, not
  selected: no per-logbook or per-contest database files; if a "start a fresh log" affordance is
  wanted, it is the Settings → Logbooks create action above, and contest logs are the W-0011 contest
  mode.

## Gates

Each workstream needs an operator go-ahead and its own dossier or ADR before implementation. Network
services require authoritative protocol/security evidence; TX-capable phone/CW work explicitly
crosses the present narrow-daemon boundary and needs a new safety decision. This inventory does not
rank its members and must not become a second roadmap.

## References

- [`ADR 0053`](../decisions/0053-inbound-dx-cluster-spot-alerts.md)
- [`docs/reviews/oss-maintainability-plan.md`](../reviews/oss-maintainability-plan.md)
- Expanded historical inventory: `d0391ed7:docs/backlog.md`.
