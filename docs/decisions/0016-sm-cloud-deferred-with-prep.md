---
number: 0016
title: SM Cloud (multi-tenant hosted service) deferred — two cheap-now schema decisions captured
status: Accepted
date: 2026-05-06
---

# 0016 — SM Cloud deferred; two cheap-now schema decisions captured

> **Update 2026-07-02:** the deferral is lifted for a first phase. SM Cloud P1
> (full-fidelity backup + restore, single-tenant launch) is designed in **ADR
> 0040**. This ADR's prep decisions (UUIDv7, `qso_history`) remain load-bearing;
> the full multi-tenant SaaS is now *sequenced* (single-tenant first), not
> indefinitely deferred.

## Context

The operator surfaced a future shape they have always had in mind: an **SM
Cloud** hosted service — multi-tenant, multi-user, multi-logbook. Operators
would run a local (or networked) `smd` as today, and the daemon would forward
QSOs upstream to a hosted SM Cloud service that holds the canonical
multi-operator log, supports edit / delete / upload from any browser, and
serves as off-site backup. This is **distinct from ADR 0014** (upstream
forwarding among the operator's *own* daemons): SM Cloud is a public-facing
hosted service shared across unrelated operators.

The forcing question: should we plan for SM Cloud now, or defer until the
local-PC / networked-daemon system is solid?

What is true at the time of writing:

- The operator runs a single local daemon today. SM Cloud has zero current
  drivers — there are no users, no hosting, no operational story.
- The codebase has shipped milestones 1, 1b, 1c (audited 2026-05-02). The
  v2 SPA is under active iteration. Fundamental UX shape (sub-tabs, station
  identity, ADIF MY_* end-to-end) is settling into place but is not finished.
- ADR 0014 already established the *deferred-with-prep* pattern for
  upstream forwarding: defer the build, but accept four prep items that are
  independently justified by today's scope. SM Cloud benefits from the same
  treatment but adds **schema-shaped** concerns that ADR 0014's items don't
  cover.
- Two specific decisions are cheap to make now and expensive to retrofit
  once a local QSO log exists with thousands of rows: how QSOs are
  identified, and whether edit/delete history is recoverable.

The operator is not asking to plan SM Cloud. They are asking to identify
the **minimum non-deferrable prep work** so that "build SM Cloud later"
remains a one-feature change rather than a data-migration nightmare, and to
capture the answer in a place that future-us reads before re-opening the
question.

## Decision

**SM Cloud is deferred.** No multi-tenant schema, no user/account model, no
Postgres choice, no auth flows, no public-facing API surface, no hosted
infrastructure is built in v1. The daemon stays single-operator, single-host
in scope. Where SM Cloud comes up in design discussions, the answer is "ADR
0016 — out of scope until a real driver appears."

**Two prep-work items are accepted as standing requirements** because each
is justified by v1 scope independently *and* is the kind of decision that is
nearly free now and expensive to retrofit once a populated QSO log exists.
If/when SM Cloud becomes real, the schema is ready; the build is forwarder-
driver-shaped per ADR 0014.

The two prep items:

### 1. Globally-unique, time-ordered QSO identifier

Every QSO row carries a globally-unique identifier in addition to whatever
local primary key the database uses. The expected shape is **UUIDv7** (RFC
9562, time-ordered, 128-bit) or **ULID** (26-char base32, time-ordered,
functionally equivalent for this purpose) — the implementation session
picks one. The identifier is generated locally by the daemon at QSO-create
time, never reused, never regenerated, and round-trips through every API
response.

**Justified today by:** even on a single daemon, a local-int autoincrement
PK is an internal database detail, not a stable external identifier. Once
ADIF exports leave the daemon, once the SPA caches QSO lists, once a future
edit endpoint references a QSO by URL, "QSO #42 in this database" stops
being a meaningful name. A daemon-generated globally-unique ID **is** the
stable external identifier; the local int can stay as the storage PK if
sqlboiler-generated code wants it.

**Cloud-readiness payoff:** when SM Cloud arrives, two operators uploading
their logs cannot collide on QSO identity. The local daemon's QSO ID is
the canonical name; the cloud stores it as the natural key for the cloud-
side row. Re-uploads, deletes, and edits route by the same ID. No
re-identification, no "translate local-id 42 to cloud-id 8819374" mapping
table, no merge-conflict resolution at upload time.

**Implementation outcome (session 38, 2026-05-06): SHIPPED — UUIDv7 chosen.**
ULID was rejected for ecosystem familiarity and RFC 9562 standardisation;
both options had equivalent k-sortability + index-locality properties.
Generation lives in `internal/utils/uuid.go` (hand-rolled from
`crypto/rand` to avoid an external dependency). Persistence: `qso.uuid`
column, `NOT NULL UNIQUE` with a strict format CHECK (length=36, dash
positions, version-7 nibble), added to `0001_init.up.sql` in place rather
than via a backfill migration because the project hasn't gone to
production. External surface: every QSO API path (`GET/PATCH/DELETE
/v1/qso/{uuid}`, `GET /v1/qso/{uuid}/uploads`) routes by UUID;
`/v1/contact-history` items carry `uuid`. ADIF emission carries the UUID
as `APP_SM_QSO_ID` on every record (omit-when-empty). The internal int
PK stays as a storage detail; the submit response carries a transitional
`id` field for one release alongside the `uuid` field, then disappears.
Known wire-shape gap: `internal/events` SSE payloads still carry only int
`qso_id` because no live consumer yet exists; they grow `qso_uuid` when
the SPA wires up event consumption.

### 2. Edit/delete provenance trail (audit table from day one)

Any time a QSO row is edited or deleted, the daemon writes a row to a
`qso_history` (name TBD at implementation) audit table capturing **what
changed, when, and the source** (e.g. "SPA submit", "ADIF re-import",
"manual edit", "delete"). The audit row is a separate table — not a column
on `qso` — and it is append-only: history is never deleted, even when the
QSO row itself is.

The implementation can lean on `additional_data` provenance fields per ADR
0014 prep #4 for *origin* metadata (`received_from`, `originated_by`), but
edit/delete history specifically needs query-friendly columns (timestamp,
qso_id, op, before-image / diff) so that "show me what I changed last
Tuesday" is a real query.

**Justified today by:** even a single operator wants "what did I change
and when?" auditing the moment they make their first manual edit. ADIF
re-imports are notorious for silent overwrites; a known-good record can be
clobbered by a stale export with no way to detect it after the fact. The
audit table makes that recoverable.

**Cloud-readiness payoff:** when an edit/delete is uploaded to SM Cloud, the
cloud has a complete history to replicate, not just the latest state. Two
operators editing the same QSO from different devices have a true conflict-
detection story (compare timestamps + before-images), not "last write
wins." A cloud-side undo is possible because the local already has the
history.

**Implementation outcome (session 40, 2026-05-07): SHIPPED — `qso_history`
table, audit scope = update + delete only.** INSERT was deliberately
omitted from the audit scope: initial-insert provenance already lives on
`qso.additional_data` per ADR 0014 prep #4, and a row in `qso_history`
with `op='insert'` and a "before" snapshot is semantically empty. The
table is keyed on the QSO's `uuid` (not the int PK) so audit rows survive
any future renumbering or cross-daemon sync — same canonical-identifier
shape as prep #1. The full pre-edit `types.Qso` is stored as
`json.Marshal()` in `before_image` rather than a diff: at personal-
operator scale the storage is negligible and a complete snapshot is
trivial to replay. Append-only is enforced by `BEFORE UPDATE` /
`BEFORE DELETE` triggers (`RAISE(ABORT, 'qso_history is append-only…')`)
on top of "the daemon never UPDATEs/DELETEs this table", so a manual
sqlite3 session can't silently tamper with history. The audit-row insert
shares the QSO mutation's transaction under one-fails-all-fail (committed
mutation with no audit row, or audit row with no mutation, both rejected).
A new `internal/enums/source/` enum carries the originating subsystem;
only `source.API = "api"` is declared today (PATCH/DELETE on
`/v1/qso/{uuid}`); future sources are added one constant at a time.
Migration was amended into `0001_init.up.sql` in place rather than chained
as `0002_*.sql` because the project hasn't gone to production. Helper:
`sqlite.Service.InsertQsoHistoryTx` (write); `FetchQsoHistoryByUUIDWithContext`
(read, ordered by `at ASC, id ASC`). DTO: `types.QsoHistory`. Tests cover
the happy path, append-only triggers, multi-edit accumulation, and the
op/source/before-image guards.

## What is NOT built (foreclosed)

Each of the following has zero drivers today and is explicitly out of scope
until a real driver appears. **Do not build any of these speculatively.**

- **Postgres choice or any non-SQLite migration.** SQLite stays the local
  daemon's storage. The "Postgres for the cloud" question is decided when
  SM Cloud has a build driver, not now. Don't pre-emptively make schema
  decisions Postgres-friendly at SQLite-unfriendly cost.
- **User / account model in the local daemon.** No `users` table, no
  `tenant_id` column, no per-user filtering on QSO queries. The local
  daemon is single-operator; SM Cloud will introduce its own account model
  when it exists.
- **Auth flows for cloud login.** OAuth, magic links, password resets,
  email verification — all SM-Cloud-side concerns when the cloud daemon is
  built. The local daemon's auth (per ADR 0014 prep #2) is the bearer-
  token-to-daemon shape, unrelated.
- **Multi-tenant aware code paths.** No "filter by current user", no
  per-user rate limiting, no row-level security plumbing. The local daemon
  has one tenant: the operator running it.
- **Public-facing API surface.** No `/v1/cloud/*` endpoints, no rate
  limiting middleware, no abuse-detection scaffolding. The daemon's HTTP
  server stays LAN-scoped per the existing topology.
- **Cloud-aware UI in the SPA.** No "log in to SM Cloud" button, no cloud-
  vs-local logbook picker, no cloud-sync status indicator. The SPA targets
  one daemon at a time per ADR 0014's foreclosure.
- **Conflict-resolution UX between local and cloud edits.** Until the cloud
  exists, "what should the SPA show when an edit on machine A and a delete
  on machine B race?" has no answer because there's no race. When SM Cloud
  is built, an ADR will pick the rule (likely last-writer-wins on
  timestamp, with the audit history as the recovery path).

If a real driver appears, write a new ADR proposing the specific shape.
**Do not pre-emptively scaffold any of these.**

## Alternatives considered

### Build SM Cloud now (or in parallel)

Design the multi-tenant schema, choose Postgres, build the auth flows, ship
the cloud daemon alongside the local daemon. "If we know we want it, why
wait?"

Rejected for the same reason ADR 0014 rejected pre-emptive cluster
infrastructure. The cautionary tale (`internal/adapters/`) applies
identically: 30+ test files of speculative framework abandoned as too
complex once reality arrived. SM Cloud is a bigger surface than upstream
forwarding (account model, billing, abuse, hosting cost, security patching)
— building speculatively is even more dangerous here. The local system is
not yet finished; spending design budget on the cloud while the SPA is
still iterating its station-identity shape would freeze v1 details around
hypothetical cloud requirements.

### Defer everything; retrofit when needed

Don't accept the two prep items. If SM Cloud ever becomes real, write a
schema migration that retroactively assigns UUIDs to existing rows and
backfills audit history from `additional_data` / git-log heuristics.

Rejected: backfilling globally-unique IDs across an existing populated
log is doable but error-prone (every external reference to "QSO #42" gets
re-pointed; the SPA's local cache invalidates; ADIF exports done before
the migration carry the old ID and never the new). Backfilling audit
history is **not** doable — once an edit happens without being recorded,
the before-image is lost. The two prep items are the two costs that
*cannot* be paid retroactively. Everything else can be deferred safely.

### Halfway-house: pick Postgres for the local DB now, "for cloud parity"

Switch the local daemon's storage from SQLite to embedded Postgres now, on
the theory that SM Cloud will use Postgres and "now's the time."

Rejected: SQLite is the right choice for a single-operator local daemon
(zero-config, file-based backups, embeds in the binary, stable across
plug-pulls). Postgres adds operational burden (run a server, manage users,
handle upgrades) for zero v1 benefit. The "cloud parity" argument is the
exact pre-anticipation pattern this ADR is rejecting — SM Cloud's storage
choice is SM Cloud's problem, made when SM Cloud is real. Two databases
with similar SQL shapes is fine; coupling the local daemon's storage choice
to a hypothetical cloud's choice is not.

### Pick UUIDv4 instead of UUIDv7/ULID

Use random UUIDs (UUIDv4) — no time-ordering required.

Rejected as default: UUIDv4 has poor B-tree index locality (random inserts
fragment the index) and is harder to debug ("which QSO was created first?"
is no longer answerable from the ID). UUIDv7 / ULID give the same
uniqueness guarantee with better index behaviour and a usable creation-time
hint baked in. The implementation session can override this if there's a
specific reason, but the default is time-ordered.

## Consequences

**Signed up for:**

- **Every QSO row gets a `uuid` (or equivalent) column.** The column is
  populated by the daemon at create time using a time-ordered generator.
  It is unique-indexed. ADIF exports include it (likely under
  `APP_SM_QSO_ID` or similar app-defined ADIF tag — implementation picks).
  API responses use it as the canonical QSO identifier; local int PK stays
  as a sqlboiler/storage detail.
- **A `qso_history` audit table exists from day one.** Shipped session 40
  (2026-05-07). Schema: `id`, `qso_uuid` (FK to QSO via the UUID — not
  the int PK), `op` (`'update'` or `'delete'` — INSERT is not audited
  because origin is already in `additional_data` per ADR 0014 prep #4),
  `at` (timestamp, SQL DEFAULT), `source` (freetext label, daemon writes
  values from `internal/enums/source/`), `before_image` (full JSON
  snapshot of the pre-mutation `types.Qso`, not a diff). Append-only is
  enforced both by code path (daemon never issues UPDATE/DELETE on this
  table) and by `BEFORE UPDATE`/`BEFORE DELETE` triggers (`RAISE(ABORT,
  …)`) — belt-and-braces against manual sqlite3 sessions.
- **The decisions are pinned in this ADR before any code is written.** The
  next session implementing the migration starts from this ADR, not from
  re-litigating the choice.

**Accepted costs:**

- **Marginal storage growth from UUIDs.** 16 bytes per QSO row plus index
  overhead. Negligible at personal-operator scale (thousands of QSOs).
- **Audit-table writes on every edit / delete.** A second insert per
  mutation. At personal-operator write rates this is invisible.
- **Slight schema-migration ceremony for the existing log.** Whatever QSOs
  already exist at migration time get backfilled UUIDs (sortable by their
  existing `created_at` so time-ordering is preserved for the historical
  rows). The audit table is empty for pre-migration history — that's fine,
  history starts when the audit table starts.

**Gained:**

- **No speculative cloud code.** The codebase stays focused on what's
  actually used. v1 milestones don't drift into "but for the cloud..."
  conversations.
- **Adding SM Cloud later is a forwarder-driver-shaped change** (per ADR
  0014's prep work), with the schema already cloud-ready. No log-wide
  re-identification, no audit-history backfill from heuristics.
- **The two prep items are independently valuable for v1.** UUIDs make
  external references stable (ADIF exports, future rest endpoints, log
  sharing); the audit table makes "what did I change?" recoverable. Each
  pays for itself before SM Cloud exists.
- **Foreclosure is explicit.** Future contributors (future-us included)
  see "SM Cloud is deferred; only these two prep items are active." When
  someone says "wouldn't it be cool if we added cloud sync", the answer
  is "yes, write an ADR proposing the specific shape; do not start
  coding."

## Triggers to revisit

- **A specific cloud driver appears.** Examples: the operator wants to
  share a logbook with a friend; the operator wants browser access to
  their log from anywhere; the operator wants off-site backup beyond
  ClubLog/QRZ. When this happens, write an ADR proposing the SM Cloud
  shape (account model, schema, auth, hosting). The two prep items above
  mean that ADR can focus on the cloud surface itself, not on a local-
  schema overhaul.
- **The "SQLite locally, Postgres in the cloud" assumption turns out
  wrong.** If the cloud's schema cannot be expressed cleanly on top of the
  local schema, revisit — possibly by promoting the local daemon to
  embedded Postgres for parity, possibly by accepting the schema
  divergence and translating at the forwarder. Today this is hypothetical.
- **UUIDv7/ULID specifically turn out to be the wrong choice.** E.g. if a
  Go ecosystem standardisation pushes hard the other way, or a debugging
  pain forces a switch. Easy enough to migrate within the daemon (regen
  IDs and back-reference), painful only if external systems already cache
  the old IDs. Today UUIDv7/ULID is the obvious time-ordered choice.
- **Audit table grows unbounded.** Personal-operator scale, this is fine
  for years. If it ever bloats, add a retention policy ADR — keep the most
  recent N years of history, archive the rest. Not a redesign; a
  maintenance trigger.

## References

- ADR 0013 (`0013-daemon-owns-bridge-as-subsystem.md`) — establishes the
  package-boundary model that the daemon stays narrow within. SM Cloud is
  a separate process / separate codebase by definition; the local daemon's
  scope does not expand.
- ADR 0014 (`0014-upstream-forwarding-deferred.md`) — the canonical
  deferred-with-prep pattern this ADR mirrors. The forwarder driver layer,
  threaded auth, and `additional_data` provenance from ADR 0014 carry
  forward as the SM Cloud upload mechanism. This ADR adds the schema-
  shaped prep that ADR 0014 didn't cover.
- ADR 0015 (`0015-additional-data-omits-empty-fields.md`) — `additional_data`
  conventions; provenance fields under ADR 0014 prep #4 use this blob.
  The audit table in this ADR is a separate concern — query-friendly
  history vs free-form blob metadata.
- `docs/v1-analysis/lessons-for-v2.md` — "Build specific, not generic"
  lesson. SM Cloud is the maximally-tempting place to over-build; this
  ADR is the explicit anti-momentum protection.
- `docs/v1-analysis/invariants.md` — `additional_data` absorbs spec
  evolution; this ADR's audit table is intentionally NOT in
  `additional_data` because edit history needs to be queryable, not
  free-form.
- Memory `project_sm_overview.md` — personal-project status is load-
  bearing. SM Cloud changes that, which is precisely why the build is
  deferred until the operator commits to the operational burden.
