---
number: 0049
title: Daemon-owned operating sessions — session_id on every QSO
status: Rejected
date: 2026-07-14
---

# 0049 — Daemon-owned operating sessions — session_id on every QSO

> **Rejected 2026-07-16, before any implementation.** Working the boundary semantics
> through with the operator surfaced the flaw: every scenario (dinner-break misfire,
> daemon restart, threshold choice) demanded new machinery — a merge-correction UI,
> restart-safe lifecycle state, a configurable idle gap — to defend a structure whose
> only live tenant was a display feature, with the stamping sitting on `prepareQso`,
> the most load-bearing write path in the system. The dissolving observation: **the
> map needs a time-filtered read, not a session entity.** "This sitting" is derivable
> at read time from `qso_date`/`time_on` (a duration picker — last 60 min / 5 h /
> 10 days), and a derived boundary is *recomputable* — change the threshold and
> nothing was ever stored wrong — where a stamped one is frozen at write time and
> needs correction tooling. Live update needs no new plumbing either: `qso.stored`
> on `GET /v1/events` already fires for every logging path (`submit.go`). The
> claimed independent value thinned on inspection: contests group better by
> `CONTEST_ID` than by sittings, and the multi-op operator-profiles tenant hasn't
> arrived. **Revisit trigger:** a session that must carry data *not derivable from
> its QSOs* — operator identity per shift in a real multi-op, labels. If that fires,
> the analysis below is the starting point. Current map design: `docs/backlog.md`
> → "QSO contacts map".

## Context

"A session" — one sitting at the radio — is today a purely **client-side, ephemeral**
notion. The Operate SPA keeps `session.qsos` in `$state`, persisted to
`sessionStorage` (`frontend/app/.../session.svelte.ts`); the daemon has no idea a
session exists. The two existing `POST /v1/session/{email,export}` endpoints reinforce
this: they take a **client-supplied list of QSO UUIDs** ("the SPA owns the session
list", `server.go`) and rebuild ADIF from live rows — "session" there means *whatever
UUIDs the SPA hands me*, nothing durable.

The trigger was a QSO-contacts **map**. A session-scoped great-circle map (arcs from the
QTH to each station worked this sitting) is the genuinely useful, sellable version — the
whole-log map is slow on a large log and reads as a blur. But a decent map wants the
**whole working space**, which an in-app overlay can't give without blocking operating
(you can't log a running pile-up while a full-screen overlay covers the log). The natural
answer is a **separate browser tab / second monitor** — but a separate tab is a separate
browsing context: it cannot see the operating tab's in-memory `session.qsos`, and
`sessionStorage` is per-tab. So the map tab has to get its data from **the daemon**, which
means the daemon must know what "this session" is.

Making the daemon session-aware is a structural change, but it is independently valuable
well beyond the map: it enables **session reconstruction** ("show me last Saturday's
run") and is the missing carrier for the backlogged **operator/contest profiles** item
(multi-op contests rotate operators mid-event; each shift is a session with its own
operator identity). The map is simply the first visible tenant.

## Decision

Add **daemon-owned operating sessions**. A first-class `sessions` table (UUIDv7 id,
`started_at`, `ended_at`, `operator_call`, `operator_name`, `label`) and a promoted,
indexed `session_id` column on `qso`. The daemon owns "the current open session",
**auto-opens** it on the first QSO of a sitting and **auto-closes** it after an idle gap,
with an **explicit start/name/end** control for named contest sessions. Every QSO is
stamped with the current `session_id` **server-side in `prepareQso`** — the client never
supplies it. New QSOs only; the existing ~5,400 rows stay session-less. The
session-scoped map is delivered as a **separate daemon-served tab** fed by
`GET /v1/session/{id}/qsos`.

## Alternatives considered

### Map delivery — separate daemon-fed tab (chosen) vs in-app overlay vs BroadcastChannel

**In-app full-screen overlay** (a lightbox over the Operate view, reusing the live
`session.qsos`, zero daemon change) was the cheapest path to a big, live map — and was
**rejected because it blocks operating**: the overlay steals the working space, so you
cannot log while watching the map draw, which defeats the "watch the pile-up build"
appeal. **A separate tab synced via `BroadcastChannel`** (same-origin tabs share live
state, still no daemon change) was rejected because the map tab would then depend on the
operating tab staying open and could never show a *past* session — it is fragile and
strictly less capable than reading from the daemon. The **daemon-fed separate tab** wins:
the map opens on a second monitor, operating in the main tab is untouched, and because the
daemon is the source the map tab stands alone (no cross-tab coupling) and gains
past-session reconstruction for free.

### Map scope — session-first (chosen) vs whole-log

A **whole-log** map (every station ever worked) was rejected as the *first* build: it is
slow to render on a large log, pushes toward a bespoke aggregate endpoint, and "everywhere
over years" is a blur, not a story. The **session** map is the interesting artifact — a
single sitting drawing itself arc by arc. The whole-log dashboard map is not foreclosed;
it becomes "swap the data source" on the same render engine if ever wanted, but it is not
part of this decision.

### Boundary semantics — auto-managed with override (chosen) vs purely explicit vs coarse

**Purely explicit** (the operator must start every session) was rejected as friction for
casual operating — the daily driver would forget, and the map/reconstruction would silently
have no data. **Per-daemon-run** (one session per process) is too coarse (the dogfood
daemon runs for days) and **per-SPA-load** too fine (an F5 redeploy would split a sitting).
**Auto-open on first QSO + auto-close after an idle gap, with explicit start/name/end**
gives zero-friction sessions by default and full control when it matters (a named contest
session held open across the event). The idle-gap threshold is a daemon config value.

### Table shape — operator-identity columns now (chosen) vs bare {id, started, ended}

The **bare** table was rejected because it saves nothing meaningful (the columns are free)
and forecloses the payoff that justifies doing this over a cheaper map hack:
`operator_call` + `operator_name` on the session are the bridge to the contest/multi-op
**operator-profiles** backlog item — "start a session as operator X" *is* the profile
pick, and each rotated shift becomes a session with its operator. We add the columns now
but **do not yet wire ADIF `OPERATOR`-from-session** (QSOs still take operator identity
from config); that is the obvious next tenant, left for when profiles are built.

### session_id storage — promoted indexed column (chosen) vs additional_data blob-only

The `additional_data` **blob-only** path (add a struct field, zero migration) was rejected
because the whole point is the query "give me this session's QSOs" — a filtered lookup,
exactly the case the codebase promotes to a real indexed column (`type_to_model.go` doc).
A `0005` migration adds `session_id TEXT` + index and the `sessions` table together;
sqlboiler regen + two one-line adapter edits carry it through. The column is authoritative
on read and still mirrored into the blob per the existing pattern.

### Who assigns session_id — daemon server-side (chosen) vs client-supplied

**Client-supplied** (the SPA mints and sends it with each QSO) was rejected: every logging
path (Phone/CW submit, FT8 `ft8-logged`, anything future) would re-implement session
tracking and could disagree, and it would trust client identity data. The daemon stamping
the current `session_id` in `prepareQso` — right beside `resolveSubmitUUID` — means **every
path is tagged for free from one source of truth**, and it mirrors the existing H1
identity-spoof guard (the public submit path already assigns `uuid` and refuses a
client-supplied canonical identity; only trusted import preserves supplied ids).

## Consequences

- The daemon gains durable session knowledge: `GET /v1/session/{id}/qsos` (and later a
  session list) can serve any tab or tool, and email/export "this session" can eventually
  resolve **by session_id** instead of a trusted client UUID list — a coherence win for the
  two existing `/v1/session/*` handlers.
- **Naming care required:** `/v1/session/*` and the `Session*Request` type names in package
  `api` are already used by the *client-list* email/export endpoints. The new endpoints must
  mean the *durable* session unambiguously and not collide with `SessionEmailRequest` /
  `SessionExportRequest`.
- A schema migration (`0005`), a sqlboiler regen, two adapter edits, and the new table added
  to `coreTablesBySet[MigrationSetLog]`. Small, but it is the first `qso` schema change since
  the `0004` UTC rebuild.
- **The existing ~5,400 QSOs have no session** — historical sittings can't be mapped or
  reconstructed until (if ever) a one-shot time-gap backfill script is written. Accepted:
  sessions are going-forward only.
- The map itself stays **SPA + one read endpoint** — no whole-log aggregate, fully offline
  (basemap bundled, session QSOs from localhost), which fits the flaky-link operator.
- Session boundary logic (auto-open/auto-close/idle-gap) is **new daemon state** that must
  survive restart sensibly: a daemon restart mid-sitting should not silently strand the open
  session (resolve on the next QSO, or persist the "current session" pointer). This is the
  main net-new complexity the decision buys.
- Stays inside the **narrow log subsystem** (ADR 0013): a session is a property of stored
  QSOs, not rig/CAT/audio, so no boundary is crossed.

## Triggers to revisit

- **A second operator / real multi-op contest** — the moment two operators share a station,
  wiring ADIF `OPERATOR` (and `STATION_CALLSIGN`/`OWNER_CALLSIGN`) from the session's
  operator identity stops being optional; build the profile pick then.
- **Operators want historical sessions mapped** — write the time-gap backfill to
  retroactively sessionize the existing log.
- **The whole-log map is wanted** — reuse the same render engine, add the aggregate endpoint;
  record the ADR 0043/0044 aggregate-exception if built.
- **Auto-boundary heuristics misfire on air** (a sitting split across an idle gap, or two
  sittings merged) — revisit the idle threshold or move toward explicit-only.
- **Flip status Proposed → Accepted** once the migration lands, sessions stamp correctly on
  new QSOs, and the session map renders end-to-end from a real sitting.

## References

- ADR 0013 (narrow daemon scope), 0015 (`omitempty` additional_data), 0016 (UUIDv7 ids),
  0017 (promoted-column precedent — `last_refreshed_at`), 0043/0044 (coupling / aggregate
  guidance), 0046 (Operate tile layout — why an overlay fights the working space).
- `internal/types/qso.go` + `internal/types/doc.go` (ADIF-alignment + additional_data mirror);
  `internal/qsoservice/submit.go` (`prepareQso`, `resolveSubmitUUID`, one-fails-all-fail tx);
  `internal/database/sqlite/migrations/` (golang-migrate; `0004` rebuild as the model);
  `internal/database/sqlite/adapters/` (`type_to_model.go` / `model_to_type.go`);
  `internal/api/server.go` (route registration; existing `/v1/session/*` client-list
  handlers); `internal/utils/uuid.go` (`NewUUIDv7`).
- `frontend/app/src/lib/operate/session.svelte.ts` (the client-side session this makes
  durable); `docs/backlog.md` → "QSO contacts map" + "Operator / user profiles".
- Related feature memory: `sm-frontend-app-consolidation`.
