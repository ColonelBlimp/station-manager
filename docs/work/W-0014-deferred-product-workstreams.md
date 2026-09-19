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
  Same day, operator: where does the management page go, and can two or more logbooks share a
  callsign? Facts: `logbook.name` is UNIQUE (409 `duplicate_name`); `callsign` is only validated
  (3–32 chars, one digit, uppercased) and the schema comment defines it as ADIF `STATION_CALLSIGN`
  — so **yes, several logbooks may carry the same callsign today**, and that is the ordinary way to
  run a contest or a season under one call. The QSO dedupe index is per logbook. Forwarders are
  station-global in `config.json`, so every logbook uploads to the same QRZ/ClubLog/SM Cloud accounts
  — right for logbooks sharing the operator's call, and the hazard for a logbook under a *different*
  call (club, /P): the per-logbook service bindings ADR 0056 describes are not something found built.
  Recommendation recorded: a **Settings → Logbooks** section mirroring Settings → Rigs (list; Add with
  name, callsign, description; Edit name/description; Set as default; Delete with the daemon's
  `has_qsos` / `default_logbook` refusals shown as reasons, the default undeletable exactly as the
  default rig is since the 2026-09-09 ruling), because it is lifecycle configuration with the same
  shape as rigs and the Logbook view stays the QSO browsing/editing surface; the header keeps naming
  the selected logbook (a switcher there is a separate ADR 0055 follow-up). Nearest confusable
  outcomes: two similarly named logbooks under one call and a session logging into the wrong one; a
  different-call logbook forwarding into the personal accounts. Awaiting the operator's ruling on the
  placement and on whether a different-call logbook needs the ADR 0056 bindings first.
  **Ruled 2026-09-19:** placement accepted — Settings → Logbooks mirroring Settings → Rigs; the
  Logbook view remains the QSO surface; a header switcher stays separate under ADR 0055.
  Different-call logbooks are OUT of the first slice: ADR 0056's per-logbook bindings must land before
  the UI can create or select one. First slice: **Add uses the normalised callsign of the current
  default logbook**; existing different-call rows may remain visible but cannot be set as default, with
  the explanation that service bindings are required. **Correction (operator):** shared callsigns make
  QRZ and ClubLog routing safe, but SM Cloud has ONE configured cloud-logbook name
  (`smcloud.CloudLogbookName`, `internal/forwarding/smcloud/smcloud.go`) and its reconciler is built at
  boot for `default_logbook_id` only (`cmd/smd/lifecycle_adapters.go`, `smcloud.NewReconciler(fc,
  d.cfg.DefaultLogbookID, …)`) — so **Set as default cannot be copied mechanically from rigs** and must
  not claim independent cloud backup for a non-default logbook; the dossier resolves that ADR 0056
  dependency before implementation.
  Same day, operator: how would a user run a different callsign for a contest, given contesting is not
  implemented, and contesting is another design thread. Facts: under ADR 0055 the logbook IS the
  station identity — `STATION_CALLSIGN` comes from the logbook, never per QSO — so a different call
  means a different logbook; the W-0011 contest mode (ruled 2026-09-12) stamps `CONTEST_ID` inside a
  window but changes no callsign; forwarders are station-global, so a different-call logbook's live
  submits would upload under the operator's personal QRZ/ClubLog accounts (imports alone carry an
  explicit `forwardTo`, `qsoservice.SubmitImport`), and SM Cloud would neither back it up nor reconcile
  it. Today's only path is manual and fragile: create the logbook by API, make it the default and
  restart, disable QRZ/ClubLog for the duration, export ADIF afterwards. Assessment: the contest-under-
  another-call case is exactly the ADR 0056 bindings plus a **per-logbook forwarding policy** (which
  destinations, or none, a logbook feeds — the club/portable call typically forwards nowhere or to its
  own accounts) plus SM Cloud's one-logbook limit; it should be designed as one thread with the W-0011
  contest mode (contest id and window, dupe scope, scoring/export already in this inventory) rather
  than bolted onto the Logbooks section. Recorded as the **contesting design thread**: W-0011 owns the
  contest-mode ruling, this inventory owns definitions/scoring/export, and the different-call case
  binds the two through ADR 0056. Not selected; needs its own dossier or ADR before code.
  **Correction, same day (coder):** the morning assessment "no per-logbook or per-contest database
  files" was written without two records that exist: **ADR 0071** (Proposed, 2026-08-17: physical
  QSO archives, one SQLite file each, UUID-identified, catalogued in `config.json`, create online,
  restart-to-switch through `POST /v1/restart`, forwarding decided per archive) and the **operator
  requirement of 2026-09-12 in W-0012** ("selectable and creatable data files … logical logbooks
  inside one file are not the ask"; forwarding OFF for a new file, SM Cloud must name its own cloud
  logbook or stay disabled, enrichment cache shared in `reference.db`, alternatives A restart-switch /
  B in-process swap, A recommended). Those records answer the objections raised here (queue and
  transaction integrity are per file, not split; the cache is shared). What the operator conceded
  today — history is lost to a new file — is the trade ADR 0071 makes deliberately for isolation and
  portability, and W-0012's entry notes "files give isolation and portability, logbooks give callsign
  identity — both, files first". The 2026-09-19 rulings above (Settings → Logbooks first slice) stand
  as ruled, but the operator should re-decide the ORDER with ADR 0071 in view: files first as the
  2026-09-12 entry recommends, or logbooks first as ruled today. Unresolved; flagged to the operator.
  **Ruled 2026-09-19 (operator, correction accepted): FILES FIRST.** ADR 0071 moved to Accepted with a
  dated files-first decision (acceptance does not select implementation). Physical archives and stable
  archive/logbook identity precede the Settings → Logbooks implementation, so `default_logbook_id`,
  the SM Cloud identity and the ADR 0056 bindings are built once against the archive model. The
  earlier Logbooks rulings stand: when built, Settings → Logbooks manages logical logbooks inside the
  active archive; different-call creation stays gated by ADR 0056, which is implemented in the
  archive-aware shape (bindings on logical logbooks within an archive; SM Cloud addressing stable
  archive and logbook UUIDs; a new archive inherits no bindings — ADR 0056 dated update). Contesting
  gets its own ADR after those identities and routing boundaries are settled. **Execution order:**
  (1) complete alpha.3 acceptance and retire B1-01; (2) W-0010 outcome 9 — its failed-versus-waiting
  behaviour and the preserved QRZ fixture remain valid within an archive-local queue; (3) the ADR
  0071 archive programme — identity/adoption, safe catalogue and provisioning, then attended restart
  activation; (4) Settings → Logbooks against the active-archive model; (5) contest mode, scoring,
  export and different-call operation on that foundation. The three ruled W-0012 slices (landing
  preference, "CQ run" header, Excel export) remain independent post-freeze commits outside the
  archive/data-model chain; W-0002 and W-0020 remain validation-only. `docs/backlog.md` carries the
  order; each of (3)–(5) opens its own dossier when selected.

## Gates

Each workstream needs an operator go-ahead and its own dossier or ADR before implementation. Network
services require authoritative protocol/security evidence; TX-capable phone/CW work explicitly
crosses the present narrow-daemon boundary and needs a new safety decision. This inventory does not
rank its members and must not become a second roadmap.

## References

- [`ADR 0053`](../decisions/0053-inbound-dx-cluster-spot-alerts.md)
- [`docs/reviews/oss-maintainability-plan.md`](../reviews/oss-maintainability-plan.md)
- Expanded historical inventory: `d0391ed7:docs/backlog.md`.
