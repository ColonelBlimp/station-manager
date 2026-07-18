# SM Cloud — P1 implementation plan (backup + restore)

**Status:** **P1 CODE-COMPLETE — S1–S5 ALL BUILT** (S1 2026-07-08, S2–S5
2026-07-17, every step integration-tested against real Postgres; the S5 gate —
back up → wipe → restore → deep-equal → the restored DB reconciles IN SYNC —
passes end-to-end). Remaining: S6 hosting (deferred ops work) + live dogfood
validation.
**Decision record:** [ADR 0040](../decisions/0040-sm-cloud-p1-backup-restore.md) —
this doc is the long-form implementation plan; the ADR holds the *why* and the
rejected alternatives. When the two disagree, the ADR's decisions win and this
doc is stale.

## Purpose

P1 delivers a **durable, full-fidelity, off-site backup of the operator's
authoritative log, plus restore** — the thing QRZ/ClubLog can't be (lossy, not
the operator's to query). It launches single-tenant (the operator) but is
architected multi-tenant-ready so onboarding 7Q8AC (P2) is data, not a
rearchitecture. Later phases (P3 auto-confirm, P4 community) build on P1's
queryable, UUID-keyed store; they are out of scope here.

## Shape: two sides, one module

- **Daemon client** — `internal/forwarding/smcloud/`, a `Forwarder`
  implementation alongside `qrz`/`clublog`, registered in `cmd/smd`. Pushes QSOs
  over HTTP.
- **Cloud service** — `cmd/smcloud` + a new `internal/cloud/` tree (HTTP server +
  Postgres store). Imports `internal/types` + `internal/adif` (the shared wire
  contract) and nothing daemon-specific, respecting the `cmd/smd/doc.go`
  package-boundary rule.

Same repo / module so the contract *is* `types.Qso` at compile time — the
drift-proof way to share it (ADR 0040 § Codebase).

### The `AdifPrefix() == ""` decision (shapes everything downstream)

The `Forwarder` interface already hands `Submit` a full `types.Qso`, and
`AdifPrefix()` returning `""` is the interface's explicit "SM-private
destination" path: the worker then **skips the QSO-row ADIF stamp and only
updates `qso_upload`.** `smcloud` returns `""` **deliberately** — a stamp write
is a local `UPDATE` that would bump `modified_at`, which would (a) make reconcile
see false drift on every push and (b) risk a re-enqueue loop. So backup status
lives in `qso_upload`, never on the QSO row. This is why reconcile-on-
`modified_at` (below) is sound.

## Sequenced steps

Each step is independently testable. The data path is verified offline
(round-trip) before any live backup, mirroring the FT8 encode→decode gate.

### S1 — Cloud store (Postgres)

`internal/cloud/store`: tables `tenants`, `logbooks`, `qsos` — the last keyed by
`uuid` (PK) with `tenant_id`, `logbook_id`, `modified_at`, `deleted_at`, and
`payload JSONB` (the full `types.Qso`). Migrations. Integration-tested against a
real Postgres (project rule: integration over mocks).

**Canonical `modified_at` precision = microseconds (protocol boundary — pin
BOTH ends).** Postgres `TIMESTAMPTZ` stores microseconds; Go `time.Time` and the
local SQLite side carry nanoseconds. Because reconcile diffs a
`hash-of-sorted-(UUID|modified_at)` (S4 / ADR 0040), a nanosecond local value
that never equals its microsecond-truncated stored form would make the hashes
disagree every cycle and re-push the *whole* logbook — the exact full-payload
waste the flaky-link constraint rules out. The store truncates `modified_at` /
`deleted_at` to microseconds on write (`store.canonicalTime`, so the stored value
is what Postgres would keep anyway); **the S4 reconcile peer MUST apply the same
microsecond truncation to its local values before it hashes/compares**, or the
churn returns from the local side. Pinned by `TestUpsert_PrecisionCanonicalised`.

**Upsert guards (reconcile soundness).** The `ON CONFLICT (uuid) DO UPDATE`
applies only `WHERE EXCLUDED.modified_at >= qsos.modified_at` (a stale/reordered
push can't clobber a newer row; `>=` keeps an identical re-push idempotent) `AND
qsos.tenant_id = EXCLUDED.tenant_id` (a push carrying another tenant's client-
generated UUID can't hijack the row once multi-tenant — a no-op today, one line
now vs a semantics migration later). A newer non-tombstone over a tombstone
resurrects by recency (edit-after-delete wins, local authoritative); a stale
missed-delete is rejected by the modified_at guard, so the tombstone holds.
`Upsert` returns the applied-row count (pushed − stale) for sync telemetry.

### S2 — Cloud HTTP API

`cmd/smcloud` + `internal/cloud/server`:

- `PUT /v1/qsos` — batch **upsert-by-UUID** (insert/update). A body with
  `deleted_at` set is a **tombstone** (soft-delete; the cloud is a retentive
  superset — ADR 0040 § Delete).
- `GET /v1/logbooks/{id}/reconcile` — `{count, hash}` over the sorted
  `(UUID|modified_at)` of live rows.
- `GET /v1/logbooks/{id}/manifest` — the `(UUID, modified_at)` list for diffing.
- `GET /v1/export` — full-JSON dump for restore.
- Bearer-token auth → tenant; `GET /v1/health`, `GET /v1/version`.

**Gate:** an offline round-trip test — `types.Qso` → `PUT` → store → `export` →
unmarshal → deep-equal, including UUID, HH:MM:SS seconds, and `additional_data`
— lands *before* anything real flows.

**BUILT 2026-07-17 (gate passing).** `internal/cloud/server` (stdlib + store +
types only — no daemon imports; `log/slog` to stderr for systemd/journald) +
`cmd/smcloud` (flag/env config: `SMCLOUD_DSN` / `SMCLOUD_TOKEN` (env-only) /
`SMCLOUD_CALLSIGN`, `-listen` default `127.0.0.1:8091` (loopback since the
2026-07-18 hardening batch; was `:8091`); boot = ping → embedded-migration
apply (`store.Migrate`, golang-migrate over the same files + tracking table as
`task migrate:cloud:up`) → `EnsureTenant` → serve; graceful shutdown; TLS is the
S6 reverse proxy's job). Wire detail in `internal/cloud/server/doc.go`: the QSO
payload is stored and exported **verbatim** (`json.RawMessage` end to end,
unmarshalled only to validate + extract the UUID — which is what makes the gate
byte-faithful); `modified_at`/`deleted_at` ride an envelope beside `qso`;
`PUT /v1/qsos` takes the logbook by NAME (the server ensures it, so the client
never pre-provisions); `GET /v1/logbooks` added for id discovery; a per-logbook
read on another tenant's logbook is a 404 (existence not leaked). The
2026-07-18 review-3 batch tightened the contract: PUT bodies must be a single
JSON document, uploaded UUIDs must be **UUIDv7** (restore's admission rule —
an accepted backup must be restorable) with validation running before the
`EnsureLogbook` side effect, `/v1/export` reads from one repeatable-read
snapshot (`store.ExportSnapshot`), and migration 0002 adds a composite
`(logbook_id, tenant_id)` FK so the schema itself refuses cross-tenant
logbook filing. **ADR 0050 (built 2026-07-18, same day):** the sync version
marker is now a per-row monotonic `revision` counter — local SQLite
migration 0005 (column + combined stamp trigger), envelope + export wire
field, cloud migration 0003, upsert guard `revision > OR (= AND
modified_at >=)`, and the reconcile manifest + summary hash line
(`uuid|unixmicro|revision`) — because second-precision `modified_at` cannot
order same-second edits (both ends must deploy together; hash formulas
differ across the skew, which flags full drift and re-pushes harmlessly). The reconcile
hash lives in the shared **`internal/cloud/reconcile`** package (`Summary`:
sort by lowercased UUID, µs-truncate, hash `uuid|unixmicro` lines with SHA-256)
— **S4's daemon side must import this same package**, which discharges the
µs-truncation obligation by construction. Tests: the gate + auth + tombstone +
stale-push-telemetry + ownership in `internal/cloud/server/server_test.go`
(same Postgres skip-gate as the store; the two suites serialise via a
`pg_advisory_lock` because `go test ./...` runs their packages in parallel
against the one dev DB).

### S3 — `smcloud` forwarder (daemon client)

`internal/forwarding/smcloud/`: `Type()="smcloud"`, `AdifPrefix()=""`, `Submit`
POSTs the full-QSO JSON; delete sends a tombstone (UUID is the key, so
`priorUpstreamID` is ignored). Blank-import registration in `cmd/smd` (pattern:
`forwarder_stub_dev.go`). Config entry under `forwarders[]` — non-sparse per ADR
0039, so the daemon seeds it disabled; credential fields = cloud URL + bearer
token; the data-driven config-SPA `ForwardingTab` renders it with no code change.
Reuses the queue + ADR-0038 forever-retry (built for a flaky link). Integration
test: submit → worker pushes → cloud stores; assert the QSO's `modified_at` is
untouched.

**BUILT 2026-07-17, two deltas from the sketch above (both deliberate, from the
code):** (1) **NOT auto-seeded** into the non-sparse config — `DefaultForwarderConfigs`
deliberately excludes operator-must-supply-URL types (there is no canonical
smcloud endpoint to seed), so the operator adds it via the config SPA's
data-driven add-forwarder form (descriptor registered: url text / token
password / logbook text, default `"main"`). (2) The wire needs the row's
`modified_at`, which `types.Qso` didn't carry — **`types.Qso` gained
`ModifiedAt`/`DeletedAt` with `json:"-"`** (the `LastRefreshedAt` column-only
pattern: overlaid from the promoted columns in `adapters.QsoModelToType`, never
in the additional_data blob or payload; they ride the wire ENVELOPE beside the
`qso`). Submit guards: zero `modified_at` or empty UUID = Terminal (a silent
`now()` substitute would poison the reconcile hash forever); `applied: 0`
(stale re-push — the cloud holds a newer copy) = Success for a backup. Outcome
matrix mirrors qrz (no response → Unreachable/forever-retry; 408/429/5xx →
Transient; other 4xx → Terminal). The `modified_at`-untouched property is held
by the worker's existing `AdifPrefix()==""` plain-mark path (no QSO-row write —
pinned in `TestAdifPrefix_EmptyByDesign` + worker suite). End-to-end wire test
`TestSubmit_AgainstRealCloudServer` runs Submit against the REAL
`internal/cloud/server` over Postgres (insert deep-equal payload round-trip,
stale-push no-clobber, delete tombstone) — a test-only import that pins the
locally-declared envelope against the server's.

### S4 — Reconcile

A daemon routine (periodic + on-demand): compute the local per-logbook
`{count, hash}`, `GET` the cloud's, compare. On mismatch, `GET` the manifest,
diff against local, and **re-enqueue the diverged UUIDs** through the existing
forwarder queue — detect + self-heal, no separate repair path. `modified_at`
(trigger-bumped on every edit) is the drift signal; the dedupe key is not (it's
an identity key, blind to edits — ADR 0040 § Reconcile signal).

**BUILT 2026-07-17.** `smcloud.Reconciler` (`internal/forwarding/smcloud/reconcile.go`)
built from the SAME ForwarderConfig as the forwarder, guarding the default
logbook: hourly loop (2-min startup delay) under the worker context in cmd/smd
+ on-demand **`POST /v1/smcloud/reconcile`** (503 until an enabled smcloud
forwarder exists). Local hash comes from the new
`sqlite.FetchQsoManifestWithContext` through the shared `internal/cloud/reconcile`
Summary — the µs obligation discharged by construction. Heal paths: upserts via
`qsoservice.EnqueueUploads` (force; smcloud has no stamp so nothing is skipped),
missed tombstones via the NEW `qsoservice.EnqueueDeleteUploads` (refuses live
rows per-row). Direction of trust: **local is authoritative** — cloud-only rows
(a previous DB generation) and cloud-newer rows are counted + logged, never
touched (S5 restore pulls; reconcile never deletes cloud data). Batches cap at
5000/run (`truncated`, next run continues). **Two protocol bugs found by the
tests and fixed:** (a) local `modified_at` is NULL until the update trigger
first fires → both readers (`adapters.QsoModelToType` + the manifest query)
fall back to `created_at`; (b) `created_at` carries sub-second digits while the
trigger writes whole seconds → both readers truncate to SECONDS (the trigger
defines the local precision), else a same-second edit/delete pushes a value the
cloud's `>=` guard rejects as stale forever. Tests: 11 pure diff-table cases +
`TestReconciler_EndToEnd` (real sqlite+qsoservice vs real cloud server+Postgres:
empty-cloud first backfill → drain → in-sync → missed delete → tombstone heal →
in-sync) + `EnqueueDeleteUploads` / manifest integration tests + the 503/200/500
handler test.

### S5 — Restore

`smd restore` (or `smd import --from-cloud`): pull `GET /v1/export` and insert
**preserving UUID + `additional_data` + `modified_at`** — never an ADIF
re-import (which mints new UUIDs and flattens `additional_data`). Needs a
`qsoservice` insert path that *accepts* an existing UUID rather than minting one:
verify whether `SubmitImport` can, or add a restore-specific path. Test: back up
→ wipe local → restore → local == original (UUID, seconds, `additional_data`
intact).

**BUILT 2026-07-17 (gate passing).** The verify came back "SubmitImport
preserves UUIDs but is ADIF-shaped" → a restore-specific JSON-native path:
**`qsoservice.Restore`** (valid-UUIDv7 + non-zero modified_at required; existing
rows — tombstones included — SKIP, so re-runs are idempotent and repairing a
diverged row stays reconcile's job; no validation gauntlet, no upload rows —
re-pushing a restore would be circular — no enrichment; dedupe key reused from
the payload or recomputed; time_off defaults to time_on like prepareQso, the one
schema-level normalisation) over **`sqlite.InsertRestoredQsoWithContext`** (the
ONE writer that sets modified_at/deleted_at explicitly — `QsoTypeToModel`
deliberately leaves them unmapped because the UPDATE path round-trips fetched
QSOs and an explicit write would defeat the bump trigger on never-edited rows).
**`smd restore`** (cmd/smd, daemon stopped): credentials come from the config's
smcloud forwarder entry (enabled or not — restore only reads), flags
`-forwarder/-cloud-logbook/-logbook/-config/-dry-run`; `smcloud.FetchExport` +
`CloudLogbookName` are the client half. Tombstones restore as soft-deleted rows
with their original recency. Tests: qsoservice round-trip (seconds, app-ext
fields, enrichment, dedupe-recompute, tombstone via manifest, no-uploads,
guards) + **`TestRestore_FullCycle`** — two real local stacks around the real
cloud server/Postgres: log+delete+push on machine 1, restore onto fresh
machine 2, deep-equal the live QSO, tombstone back deleted, and machine 2
**reconciles IN SYNC** (the modified_at-survival proof), then an idempotent
re-run all-skips.

### S6 — Deferred (not part of the P1 build)

Hosting (VPS + managed Postgres, TLS, systemd unit / container) — a build-time
ops call (well-connected region, not Malawi). P2 multi-tenant onboarding of
7Q8AC *after* the security assessment (auth model: trust-on-provisioning,
per-tenant tokens — ADR 0040 § Identity).

**ARTIFACTS BUILT 2026-07-17; provisioning is the operator's remaining step.**
`task build:smcloud` produces a fully STATIC linux binary (pure Go — no CGO, no
glibc floor, scp to any VPS); `deploy/smcloud/` holds the hardened systemd unit
(DynamicUser — smcloud keeps no local state), the env-file template (loopback
listen, DSN, callsign, token), and a Caddyfile example (automatic Let's Encrypt;
smcloud never terminates TLS). The step-by-step runbook —
decisions → build → Postgres → unit → TLS → daemon wiring → first-backfill
verify → operations (pg_dump cron, upgrades, restore drill, token rotation) —
is **`docs/smcloud-deploy.md`** (Tier 1). Migrations self-apply at service
boot, so a VPS deploy never runs a migration CLI.

## Invariants held

- **Narrow daemon scope** — `smcloud` is just a `Forwarder`; log/forward code is
  untouched, and the cloud service imports only `types`/`adif`.
- **Backup never blocks logging** — it's downstream and best-effort, the
  enrichment-never-blocks analogue.
- **Soft-delete both ends** — local `deleted_at` already exists and matches the
  cloud tombstone.
- **No `modified_at` pollution** — `AdifPrefix()==""`.
- **UUID identity survives restore** — full-JSON round-trip, not ADIF.

## To verify at build time

Not fully confirmed during design; check before coding the affected step:

- `forwarding.Result` struct fields (S3).
- Whether `qsoservice` has, or needs, a UUID-preserving insert for restore (S5).
- The forwarder factory / registration signature (S3).
- Config `credential_fields` wiring for a plain endpoint-URL field (S3).
