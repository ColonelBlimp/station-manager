---
number: 0056
title: Per-logbook config lives in the database; station-global config stays in config.json
status: Accepted
date: 2026-07-22
---

# 0056 — Per-logbook config lives in the database; station-global config stays in config.json

## Context

`config.json` is the daemon's single global config file. Forwarding destinations
(`forwarders: [...]`) and enrichment providers (`lookup`) are declared there
GLOBALLY — one flat list, applied to every QSO regardless of logbook. Each
carries a credential (QRZ API key, ClubLog email+password, SMTP) that is bound to
ONE station callsign / online account.

ADR 0055 made `STATION_CALLSIGN` a per-logbook property (the logbook is the
operating identity), which exposed the mismatch: a QSO logged under a *different*
logbook (7Q1 event, 7Q8AC) would still be uploaded to the **first callsign's**
QRZ/ClubLog account, and the SMC reconciler — constructed once at boot with
`cfg.DefaultLogbookID` — only backs up the boot-time logbook. That is ADR 0055's
gap #2, and it is the hard gate on switching the current logbook mid-session.

The service surface is *growing*: QRZ + ClubLog today; **HamQTH** planned as both
a forwarder destination AND an enrichment source; **LoTW** not yet started. With
N logbooks (callsigns) and M online services, the routing is an N×M matrix of
"which credential/account this logbook uploads to and enriches from" — inherently
relational data that does not fit a flat global file, and that grows every time a
callsign or a service is added. The cardinality is also **variable per logbook** —
one logbook uploads to nothing, another to a single service, another to several,
depending on the need — which is precisely the one-to-many shape a join table
expresses as zero-to-many rows.

## Decision

Draw the config boundary at **per-logbook vs station-global**:

- **Per-logbook, relational config lives in the DATABASE** — on the `logbook`
  row or child tables. This covers `owner_callsign` (ADR 0055) and, the driving
  case, **per-logbook service bindings**: which forwarder credential/account each
  logbook uploads to (`logbook_forwarders`-style: `logbook_id × forwarder config`)
  and, where credentials are per-callsign, which enrichment account it queries.
- **Station-global, human-editable config stays in `config.json`** — the `MY_*`
  shack fields, the operator roster (ADR 0055), and all server / bridge / ft8 /
  datastore settings.

The rule, stated once: **per-logbook or relational → database; one-physical-station
or startup-critical or hand-edited → `config.json`.**

## Alternatives considered

### Keep everything in `config.json` (per-logbook forwarders as nested blocks)

Rejected: an N×M nested structure (each logbook carrying its own forwarder list +
credentials) is unwieldy and drift-prone; adding one service means hand-editing
every logbook's block; there is no referential integrity between a logbook and
its bindings; and it duplicates the relational shape the DB already models for
logbooks and QSOs. The per-logbook cardinality is variable (zero, one, or many
services), so each block is a sparse variable-length list — exactly the case a
join table handles as zero-to-many rows and flat config handles badly. It gets
worse with every callsign and every new service — the opposite of what the growth
(HamQTH, LoTW, …) demands.

### Move ALL config into the database (including station-global + server settings)

Rejected: `config.json` is loaded BEFORE the DB opens (it holds the datastore path
and the server bind), so startup-critical settings cannot live in the DB. It is
also human-readable, version-controllable, and backup-friendly — properties worth
keeping for the shack description and daemon tuning. Moving non-relational,
station-global settings buys nothing and loses those properties.

### Global forwarder list + per-logbook enable/disable flags

Rejected: enable/disable does not solve the real problem — the *credential* is
per-callsign. A QRZ key logs into ONE QRZ account; you need per-logbook
credentials, not merely per-logbook on/off of a shared (wrong-account) one.

## Consequences

- **New DB tables + migrations + sqlboiler models** for the per-logbook service
  bindings (forwarder credential/account per logbook; enrichment where per-call).
  `owner_callsign` joins the `logbook` row.
- The **forwarder worker** resolves the destination account/credential by the
  QSO's `logbook_id`, not global config; the **enrichment orchestrator** likewise
  where credentials are per-callsign. The **SMC reconciler** becomes per-logbook
  (follows each logbook rather than the boot-time default).
- **This unblocks multi-callsign operation and ADR 0055's shell current-logbook
  selector** (gap #2's guardrail): once routing is per-logbook, switching the
  current logbook no longer misroutes uploads.
- **New services slot in as rows, not config rewrites** — HamQTH, LoTW, and the
  next one are a binding row per logbook, not a global-config schema change per
  service.
- **Two config sources** (`config.json` + DB). This ADR is the "what lives where"
  rule; `config.md` and the config SPA must state it so it does not confuse.
- **Credentials move from `config.json` to the DB — this is NOT secret-neutral.**
  `config.WriteJSON` forces `config.json` to `0600`, but SQLite creates its DB (and
  WAL/SHM sidecars) at `0644` and the containing dir may be `0755`, so moving QRZ
  keys / ClubLog passwords into the DB as-is exposes them to other local users. The
  DB, its sidecars, and its directory MUST be tightened to owner-only before any
  credential is stored (see Implementation requirements). The build-injected ClubLog
  key (ADR 0054) is a separate mechanism, unaffected. Per-logbook credential entry
  needs a config-SPA surface (the logbook editor), since the DB isn't hand-edited.
- Enrichment is a *softer* per-logbook case than forwarding: a lookup's RESULT
  (the contacted station's data) is the same whichever of your accounts you query;
  only the CREDENTIAL may be per-callsign. Forwarding (which uploads to a specific
  remote log) is the hard driver; enrichment bindings can follow the same table
  but are optional per provider.

## Triggers to revisit

- If a service appears whose credential is genuinely **station-global** (one
  account for all your calls), that binding can stay in `config.json` — the rule
  keys on per-logbook-ness, not on "it's a service."
- If the two-source split confuses operators in practice (e.g. "why is my QRZ key
  not in config.json anymore?"), reconsider the boundary or the SPA's framing.
- If per-logbook config ever needs frequent **hand-editing** before a UI exists,
  the DB's opacity bites — but the config SPA's logbook editor is the intended
  path, so this is a sequencing note, not a design flaw.

## Implementation requirements (surfaced by review 2026-07-22)

The clean-room review flagged four things the implementation MUST handle;
recorded here so they are not lost:

1. **Enrichment must carry the logbook_id — IF enrichment credentials are made
   per-logbook.** Enrichment runs *before* a QSO exists, and the current logbook is
   client state (ADR 0055), so the daemon cannot infer it: `/v1/enrich/callsign`
   and `Orchestrator.Enrich` would need to take the logbook_id from the caller (the
   SPA already sends `?logbook` on submit and knows the current logbook; the FT8 e4
   sink holds the pinned logbook_id from gap 6). BUT — enrichment RESULTS are
   callsign-agnostic (the contacted station's data is the same whichever of your
   accounts you query), so keeping enrichment on a **global/default account is a
   valid v1**; this requirement binds only if per-logbook enrichment *credentials*
   are actually wanted. Forwarding (which uploads to a specific remote log) has no
   such escape — it is per-logbook or it misroutes.

2. **A crash-safe STARTUP migration must seed bindings from the existing config
   BEFORE the old fields are removed.** Existing installs hold forwarder
   enabled/credentials/endpoints/retry only in `config.json`, and SQL migrations
   cannot read it. A Go-side, idempotent startup step must seed the per-logbook
   binding tables (for the default logbook) from the current global config; the
   removal/ignore of the old config fields must be **gated on that seeding having
   run**. Otherwise an upgrade starts with no bindings — new QSOs are not queued,
   existing `qso_upload` rows have no worker, and lookup providers vanish.

3. **The database must be owner-only (`0600`) before credentials move in** — the DB
   file, its WAL/SHM sidecars, and the containing directory. See the corrected
   Consequences bullet above; my original "secret-neutral" claim was wrong.

4. **`POST /v1/smcloud/reconcile` must become multi-logbook aware.** It currently
   takes no logbook and returns one scalar summary (one `cloud_logbook_id`); once
   reconciliation is per-logbook it must accept a logbook_id, or reconcile + report
   **every** bound logbook in an aggregate response — otherwise the "back up/check
   now" action can only surface one reconciler while periodic reconcile covers
   several.

## References

- ADR 0055 — station identity by logbook + operator roster (defines the
  per-logbook identity; gap #2 is the forwarding/SMC routing this ADR resolves,
  and its mid-session-switch guardrail depends on this landing).
- ADR 0054 — ClubLog API key build-injection (a distinct secret mechanism).
- `internal/forwarding/` (worker + registry + destinations), `internal/lookup/`
  (enrichment orchestrator), `internal/database/sqlite` (logbook table, migrations).
- `docs/v2-design/config.md` — must document the config/DB boundary once built.
