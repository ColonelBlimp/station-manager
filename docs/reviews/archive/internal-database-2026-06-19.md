# Code Review: internal/database

Date: 2026-06-19  
Scope: `internal/database`, which is currently the SQLite persistence layer under `internal/database/sqlite`, including migrations, adapters, service lifecycle, public helper methods, generated-model boundaries, and direct consumers in `internal/qsoservice`, `internal/forwarding/worker`, and database-facing API handlers.

## Summary

I reviewed the current tree as a fresh codebase. I did not use archived review documents as evidence for the findings below.

The core QSO, logbook update, upload queue, audit-history, cursor pagination, and ADIF-stamp paths are in good shape. The important write paths use transactions where they need atomicity, raw SQL is parameterized, upload claiming is deterministic and scoped, and the main HTTP list path caps page size before reaching SQLite.

The remaining risks are mostly around older public helpers and startup verification. Some helper methods still call generated full-row `Update` / `Upsert` methods directly, which bypasses the active-row and not-found contracts added to the newer QSO/logbook paths. Separately, migration verification only checks two tables, so a partially damaged v1 schema can pass startup and fail later when forwarding, enrichment, or history code first touches its tables.

## Findings

### M1 - Correctness: legacy public update/upsert helpers bypass active-row and not-found contracts

Evidence:
- `UpdateContactedStationWithContext` converts the DTO to a generated model, sets `ModifiedAt`, calls `model.Update(ctx, h, boil.Infer())`, ignores the returned row count, and returns `nil` on zero affected rows (`internal/database/sqlite/api_context.go:795-822`).
- `UpdateCountryWithContext` has the same shape after prefix validation: generated update, ignored row count, `nil` on zero affected rows (`internal/database/sqlite/api_context.go:961-988`).
- The generated `ContactedStation.Update` and `Country.Update` statements match by primary key only (`WHERE id = ?`), not `deleted_at IS NULL`, and return `RowsAffected` to the caller (`internal/database/sqlite/models/contacted_station.go:462-486`, `internal/database/sqlite/models/country.go:375-399`).
- The newer `UpdateLogbookWithContext` shows the intended safer pattern: explicit column map, `id = ? AND deleted_at IS NULL`, and zero rows -> `ErrNotFound` (`internal/database/sqlite/api_context.go:1503-1552`).
- `UpsertLogbookWithContext` still delegates to generated `model.Upsert(..., boil.Infer(), boil.Infer())` (`internal/database/sqlite/api_context.go:1554-1584`). SQLBoiler `Infer` updates all non-primary-key columns (`github.com/aarondl/sqlboiler/v4@v4.19.7/boil/columns.go:170-187`), and the generated SQLite upsert emits `SET column = EXCLUDED.column` for every inferred update column (`internal/database/sqlite/models/sqlite_upsert.go:37-52`, `internal/database/sqlite/models/logbook.go:744-768`).
- Current tests cover happy paths for these helpers (`internal/database/sqlite/service_test.go:551-583`, `internal/database/sqlite/service_test.go:835-889`), while missing/soft-deleted coverage exists only for the newer QSO and logbook update paths (`internal/database/sqlite/review_findings_test.go:16-115`).

Impact:
- Updating a missing contacted-station or country ID reports success instead of `ErrNotFound`.
- If a row is soft-deleted by generated or future code, these helpers can write through the tombstone because the generated update has no active-row predicate.
- `UpsertLogbookWithContext` can conflict on a soft-deleted logbook primary key and clear/reset columns through `EXCLUDED` defaults, including `deleted_at`/audit-style fields, instead of preserving the active-row contract.
- The current application blast radius is limited: I did not find production callers for `UpdateContactedStationWithContext`, `UpdateCountryWithContext`, or `UpsertLogbookWithContext` beyond tests. The risk is still real because these methods are exported inside the package used by the rest of the daemon.

Recommendation:
Either remove/deprecate the unused legacy helpers, or rewrite them to match the current handwritten pattern:
- validate positive IDs and required natural keys before opening the DB handle,
- use explicit `models.M` column whitelists instead of `boil.Infer`,
- include `deleted_at IS NULL` on update predicates,
- translate `RowsAffected == 0` to `errors.ErrNotFound`,
- add focused tests for missing IDs and soft-deleted rows.

For `UpsertLogbookWithContext`, prefer a transaction that explicitly chooses insert versus active-row update, or delete the helper if no caller needs caller-supplied primary-key upsert semantics.

### M2 - Correctness: migration verification can pass an incomplete schema

Evidence:
- `doMigrations` runs `m.Up()` and then calls `missingCoreTables`; if no tables are reported missing, startup logs schema verified and returns success (`internal/database/sqlite/migrations.go:55-80`).
- `missingCoreTables` only requires `logbook` and `qso` (`internal/database/sqlite/internal.go:174-219`).
- The initial migration creates additional required runtime tables and triggers: `contacted_station`, `country`, `qso_upload`, and `qso_history` (`internal/database/sqlite/migrations/0001_init.up.sql:127-169`, `internal/database/sqlite/migrations/0001_init.up.sql:173-219`, `internal/database/sqlite/migrations/0001_init.up.sql:239-269`).

Impact:
A database with `schema_migrations` already at the current version, plus `logbook` and `qso` present but `qso_upload`, `qso_history`, or enrichment-cache tables missing, will pass `Migrate()`. The daemon can then fail later in forwarding, session upload status reads, audit-history reads, or enrichment refreshes. That is exactly the class of problem the post-migration schema verification is meant to catch early.

Recommendation:
Expand schema verification to include all tables that current runtime code depends on: at minimum `logbook`, `qso`, `contacted_station`, `country`, `qso_upload`, and `qso_history`. Consider also checking critical indexes/triggers such as `idx_qso_upload_pending`, `trg_qso_upload_set_updated_at`, and the `qso_history` append-only triggers, or add a separate integrity check for those. Add a regression test that opens a deliberately incomplete "version current" database and asserts `Migrate()` fails before normal use begins.

### L1 - Documentation: lifecycle and UUID comments overstate the actual contract

Evidence:
- The package doc says "All lifecycle methods are idempotent" (`internal/database/sqlite/doc.go:6-13`), but `Open` returns `Database service is already open` when called while open (`internal/database/sqlite/service.go:86-110`).
- `FetchQsoByUUIDWithContext` says format validation quickly rejects malformed UUIDs (`internal/database/sqlite/api_context.go:432-435`), but the implementation only checks for an empty string before running the parameterized lookup (`internal/database/sqlite/api_context.go:436-453`).

Impact:
This is not a direct security issue because the UUID lookup is parameterized, and API route handlers validate path UUIDv7 values before calling into the database. It is still misleading for new package callers and reviewers: they may expect a second `Open()` to be a no-op, or expect malformed UUID strings to fail before hitting SQLite.

Recommendation:
Update the comments to match the code, or change the code if idempotent `Open` / DB-layer UUID validation is the intended contract. If UUID validation is added here, use the same UUIDv7 utility used by the API path parser so the API and DB contracts do not drift.

## Security Review

No high or medium security findings surfaced in this pass.

The raw SQL paths I checked bind caller-controlled values rather than interpolating them. `MarkUploadSuccessWithAdifStampWithContext` validates `adifPrefix` before deriving JSON paths, binds both JSON paths and values, checks row counts, and wraps the upload/QSO stamp in one transaction (`internal/database/sqlite/api_context.go:1869-1957`). `MarkSessionEmailedWithContext` dynamically builds only the placeholder list from typed `int64` IDs and binds the values (`internal/database/sqlite/api_context.go:1986-2030`). Country prefix validation prevents LIKE metacharacters from entering the longest-prefix cache lookup path (`internal/database/sqlite/api_context.go:918-930`).

## Performance Review

No performance findings surfaced in current primary paths.

The main QSO list path uses cursor pagination and the HTTP handler caps `limit` before calling SQLite (`internal/api/handler_qso_list.go:70-101`). The database query orders by `(qso_date, time_on, id)` and fetches `limit+1` for cursor detection (`internal/database/sqlite/api_context.go:604-675`), with a matching composite partial index in the migration (`internal/database/sqlite/migrations/0001_init.up.sql:102-115`). `Migrate()` runs `ANALYZE` after migrations to keep planner stats fresh, and `Close()` runs `PRAGMA optimize`.

The older offset-paging helper remains present, but I did not find it on the main HTTP list path. If it becomes externally reachable again, it should get the same limit ceiling and large-offset review as the cursor path.

## Coverage Notes

Strong current coverage:
- QSO and logbook active-row update behavior, including soft-deleted and missing-row cases (`internal/database/sqlite/review_findings_test.go:16-115`).
- Modernc SQLite uniqueness detection and typed-error drift (`internal/database/sqlite/unique_error_test.go:12-102`).
- Upload claim ordering, forwarder scoping, retry/reset behavior, prior-upstream lookup, ADIF stamping, and rollback on partial stamp failure (`internal/database/sqlite/service_test.go:941-1117`, `internal/database/sqlite/service_test.go:1519-1702`).
- QSO history append-only triggers and ordering (`internal/database/sqlite/qso_history_test.go`).

Missing focused coverage:
- `UpdateContactedStationWithContext` and `UpdateCountryWithContext` returning `ErrNotFound` for missing IDs and refusing soft-deleted rows.
- `UpsertLogbookWithContext` behavior against missing IDs, duplicate names, and soft-deleted rows.
- Post-migration verification of all runtime-required tables, indexes, and triggers.
- Documentation/contract tests only if the intended behavior is to make `Open()` idempotent or add DB-layer UUID validation.

## Verification

Commands run:

```text
GOCACHE=/tmp/go-build go test ./internal/database/sqlite -count=1
GOCACHE=/tmp/go-build go test ./internal/database/sqlite ./internal/qsoservice ./internal/forwarding/worker -count=1
GOCACHE=/tmp/go-build go vet ./internal/database/sqlite ./internal/qsoservice ./internal/forwarding/worker
GOCACHE=/tmp/go-build go test -race ./internal/database/sqlite -count=1
GOCACHE=/tmp/go-build go test ./internal/api -run 'Test(CreateLogbook|UpdateLogbook|SubmitQso|FetchQso|QsoList|FetchUploads|QsoHistory|SessionEmail)' -count=1
```

Result:
- The focused database, adjacent `qsoservice`, adjacent forwarding worker, race, and vet checks passed.
- The selected API handler command first failed in the sandbox because `httptest` could not bind `127.0.0.1:0` (`socket: operation not permitted`). Rerunning the same focused command outside the sandbox passed.

## Resolution (2026-06-19)

All three findings fixed.

- **M1 (fixed).** `UpdateContactedStationWithContext` and
  `UpdateCountryWithContext` were rewritten to the safe pattern: validate a
  positive id, explicit column map with `UpdateAll(id = ? AND deleted_at IS
  NULL)`, refresh `modified_at`, and translate `RowsAffected == 0` →
  `ErrNotFound` (matching `updateActiveQso`/`UpdateLogbook`).
  `UpsertLogbookWithContext` (and its `UpsertLogbook` wrapper + tests) was
  **removed** rather than rewritten: it was entirely dead, and its only safe
  semantic ("update the active logbook with this id, else error") is exactly
  what `UpdateLogbook` already provides — caller-supplied-PK insert is
  nonsensical for an AUTOINCREMENT key (sqlboiler's generated Insert even drops
  the PK). Tests: `TestUpdateContactedStation_RejectsSoftDeletedAndMissing`,
  `TestUpdateCountry_RejectsSoftDeletedAndMissing`. This also closes the
  2026-06-14 contacted-station backlog item (removed from `docs/backlog.md`).
- **M2 (fixed).** `missingCoreTables` now requires `logbook`, `qso`,
  `contacted_station`, `country`, `qso_upload`, and `qso_history`, so a
  "version current" DB missing a runtime table fails `Migrate()` at startup
  instead of erroring later in forwarding/history/enrichment. Test:
  `TestMissingCoreTables_FlagsIncompleteSchema`.
- **L1 (fixed — docs match code).** `doc.go` now states Open is NOT idempotent
  (a deliberate double-open guard) while Initialize is, and notes Migrate
  verifies the runtime tables; the `FetchQsoByUUIDWithContext` comment now says
  the guard is an empty-string reject only (the lookup is parameterized and the
  API validates the path UUIDv7). Behaviour unchanged.

Verified: `gofmt`/`go vet` clean; `go build ./...`; `internal/database/sqlite`,
`internal/qsoservice`, `internal/forwarding/worker`, `internal/api` pass;
`go test -race ./internal/database/sqlite` clean.

## Worktree Note

I did not modify production code. The worktree already contained unrelated `internal/config` and `docs/v2-design/config.md` edits plus `docs/reviews/internal-config-2026-06-19.md`; this review adds only `docs/reviews/internal-database-2026-06-19.md`.
