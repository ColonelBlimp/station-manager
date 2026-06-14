# `internal/database` - code review (2026-06-14)

> **Resolution (2026-06-14): all three findings fixed.**
> - **M2** — QSO updates now go through `updateActiveQso`, an explicit active-row
>   update: `models.Qsos(ID.EQ, DeletedAt.IsNull()).UpdateAll(...)` with a column
>   map that omits `id`/`created_at`/`deleted_at` and refreshes `modified_at`,
>   returning `errors.ErrNotFound` on zero rows affected. The
>   `deleted_at IS NULL` predicate is in the UPDATE itself (not a separate
>   exists-check), so a DELETE landing between a stale PATCH's read and the write
>   can't be written through — no resurrection, no mutate-the-tombstone, no
>   spurious update uploads/history (the tx rolls back on `ErrNotFound`). Both
>   `UpdateQsoWithContext` + `UpdateQsoTx` use it; the PATCH handler maps the
>   race-case `ErrNotFound` → 404. (`created_at` was *not* actually clobbered —
>   SQLBoiler trims it for non-whitelist updates — but the explicit map omits it
>   regardless.) Tests: `TestUpdateQso_RejectsSoftDeletedAndMissing`.
> - **M1** — `Initialize` no longer uses `sync.Once` (which consumed the guard on
>   the first failure); it's mutex-guarded, re-checkable, and sets
>   `isInitialized` only after every step succeeds, so a failed first call can be
>   retried once the dependency/config is fixed. `Close` drops the (now-gone)
>   once-reset. Test: `TestInitialize_RetryableAfterFailure`.
> - **L1** — the country prefix-wildcard validation is centralised in
>   `validateCountryPrefix` and called from **all** durable writers (Insert /
>   Update / Upsert), not just Upsert. Test: `TestCountryWriters_RejectWildcardPrefix`.
>
> **Follow-up filed (`docs/backlog.md`):** the same generated `Update(Infer)`
> pattern on the **logbook** + **contacted-station** helpers (which also have
> `deleted_at`). QSO was the highest-risk (API-reachable PATCH/DELETE + upload/
> history side effects) and is done; the siblings are filed for a later
> active-predicate sweep per the review's M2 note.
>
> Verified: `go test ./internal/database/sqlite ./internal/qsoservice
> ./internal/forwarding/worker ./internal/api ./cmd/smd`; `go test -race` on the
> first three; `go vet` — all green.

## Scope

Read-only review of the SQLite persistence layer under
`internal/database/sqlite`, focused on lifecycle behavior, generated
SQLBoiler model use, soft-delete semantics, migration/schema contracts,
upload queue state transitions, and adjacent QSO/API/forwarder call
paths.

Covered:

- `internal/database/sqlite/*.go`
- `internal/database/sqlite/adapters/*.go`
- `internal/database/sqlite/models/*.go`
- `internal/database/sqlite/migrations/*.sql`
- `internal/database/sqlite/*_test.go`

Context checked:

- `internal/qsoservice/*`
- `internal/api/handler_qso.go`
- `internal/api/handler_logbook.go`
- `internal/api/handler_uploads.go`
- `internal/forwarding/worker/worker.go`
- `cmd/smd/main.go`
- `cmd/smd/import.go`

No production code changes were applied during this review.

## Headline verdict

The package is in good overall shape: migrations are embedded cleanly,
the tx-only QSO/upload helpers support the one-fails-all-fail contract,
the forwarder claim path uses an atomic `UPDATE ... RETURNING`, and the
read paths mostly respect soft deletes through generated SQLBoiler
filters. The remaining risks are concentrated around places where the
handwritten layer trusts generated `Update` behavior too broadly, plus a
lifecycle retry edge in `Initialize`.

Headline counts: **0 Critical**, **0 High**, **2 Medium**, **1 Low**.

## Medium findings

### M1. `Initialize` cannot be retried after a failed first call

**Files:**

- `internal/database/sqlite/service.go:42`
- `internal/database/sqlite/service.go:43`
- `internal/database/sqlite/service.go:44`
- `internal/database/sqlite/service.go:49`
- `internal/database/sqlite/service.go:54`
- `internal/database/sqlite/service.go:60`
- `internal/database/sqlite/service.go:68`
- `internal/database/sqlite/service.go:74`
- `internal/database/sqlite/doc.go:6`

`Initialize` wraps all dependency/config validation in `s.initOnce.Do`.
Any early failure, such as missing `LoggerService`, missing
`ConfigService`, invalid config, or an initial directory creation error,
still consumes the `sync.Once`. Since `isInitialized` is only set on the
success path, a later call after the dependency/config problem is fixed
skips the body, sees the fresh local `initErr == nil`, and returns
success while the service remains uninitialized.

That violates the package lifecycle contract that initialization is
idempotent and creates a bad retry state:

- first `Initialize()` returns an error;
- caller fixes injection/config and calls `Initialize()` again;
- second `Initialize()` returns nil without setting `DatabaseConfig` or
  `isInitialized`;
- `Open()` still fails with "not initialized".

The existing close/reopen regression test covers resetting `initOnce`
after a successful lifecycle, but not failure-then-retry.

Suggested fix:

Avoid consuming the one-shot guard on failed initialization. A simple
pattern is to protect initialization with `s.mu`, re-check
`isInitialized`, run validation/config setup normally, and store
`isInitialized=true` only after all steps succeed. If keeping
`sync.Once`, it needs an explicit reset on failure, but a mutex is less
surprising for retryable initialization.

Add a regression test that calls `Initialize` with missing config, then
injects config and logger, calls `Initialize` again, and verifies `Open`
can proceed.

### M2. QSO updates can write through a soft-deleted row and clear `deleted_at`

**Files:**

- `internal/api/handler_qso.go:82`
- `internal/qsoservice/update.go:215`
- `internal/database/sqlite/api_context.go:326`
- `internal/database/sqlite/api_context.go:344`
- `internal/database/sqlite/api_context.go:351`
- `internal/database/sqlite/api_context.go:2276`
- `internal/database/sqlite/api_context.go:2283`
- `internal/database/sqlite/adapters/type_to_model.go:61`
- `internal/database/sqlite/models/qso.go:248`
- `internal/database/sqlite/models/qso.go:986`
- `internal/database/sqlite/models/qso.go:994`
- `internal/database/sqlite/models/qso.go:1007`
- `internal/database/sqlite/models/qso.go:1041`

`UpdateQsoWithContext` and `UpdateQsoTx` convert `types.Qso` to a fresh
SQLBoiler model and call `model.Update(..., boil.Infer())`. The generated
`Qso.Update` infers every non-primary-key column, and `qsoAllColumns`
includes `deleted_at`. The adapter does not populate `DeletedAt` from
`types.Qso`, so the generated update writes `deleted_at = NULL`.

The generated SQL predicate is primary-key-only:

```sql
UPDATE "qso" SET ... WHERE "id" = ?
```

It does not require `deleted_at IS NULL`, and the handwritten wrapper
ignores the returned rows-affected count. That creates a real stale
snapshot race:

1. `PATCH /v1/qso/{uuid}` fetches an active row in the handler.
2. A concurrent `DELETE /v1/qso/{uuid}` commits the soft delete.
3. The patch path continues and calls `UpdateQsoTx` with the pre-delete
   snapshot.
4. The generated update matches by `id`, writes the patch, and clears
   `deleted_at`, making the deleted QSO active again.

Even outside the HTTP path, direct callers of `UpdateQsoWithContext` get
the same behavior against a non-existent or soft-deleted ID: zero rows
affected is not translated into `errors.ErrNotFound`, and soft-deleted
rows can be updated by primary key.

Suggested fix:

Do not use generated full-row `Update(..., boil.Infer())` for QSO
updates. Use an explicit active-row update that either:

- whitelists only intended mutable columns and adds
  `WHERE id = ? AND deleted_at IS NULL`, or
- fetches the generated model inside the same transaction, mutates only
  allowed fields, and still checks the rows-affected result.

In either case, return `errors.ErrNotFound` when rows affected is zero.
Keep `deleted_at` out of normal update whitelists.

Add regression coverage for:

- updating a soft-deleted QSO returns `ErrNotFound` and leaves it hidden;
- stale update after delete does not resurrect the row;
- `UpdateQsoWithContext` on a missing ID returns `ErrNotFound`.

The same generated-update pattern also exists on logbook,
country, and contacted-station helpers. The QSO path is the most
important to fix first because it is API-reachable through PATCH/DELETE
interleavings and also controls upload/history side effects.

## Low findings

### L1. Country prefix wildcard invariant is enforced only on one write path

**Files:**

- `internal/database/sqlite/api_context.go:807`
- `internal/database/sqlite/api_context.go:870`
- `internal/database/sqlite/api_context.go:895`
- `internal/database/sqlite/api_context.go:974`
- `internal/database/sqlite/api_context.go:992`
- `internal/database/sqlite/enrichment_cache_test.go:121`
- `internal/database/sqlite/enrichment_cache_test.go:125`

`FetchCountryByCallsignWithContext` implements longest-prefix matching
with:

```sql
? LIKE country.prefix || '%'
```

The read-side comment says `UpsertCountryWithContext` rejects LIKE
wildcards and the test says the upsert path is the single chokepoint
that prevents wildcard rows from landing. That is true for the current
hamnut/orchestrator write path, but it is not true for the package API:
`InsertCountryWithContext` and `UpdateCountryWithContext` are exported
helpers and do not apply the same prefix validation. A direct caller can
insert `M%`, `_`, or `M\A`; after that, callsign lookups can over-match.

Suggested fix:

Centralize country prefix validation and call it from insert, update,
and upsert, or remove/deprecate the direct insert/update helpers if
upsert is intended to be the only durable country writer. Add tests for
`InsertCountry` and `UpdateCountry` rejecting the same wildcard cases as
`UpsertCountry`.

## Verified safe / checked

- SQLite DSN PRAGMAs are applied per connection through repeated
  `_pragma=` query parameters, and the explicit runtime PRAGMAs in
  `Open` are harmless reinforcement for the opened connection.
- `ClaimPendingUploadsWithContext` uses one atomic
  `UPDATE ... RETURNING` claim and re-sorts returned rows in Go, avoiding
  the old select-then-update race and nondeterministic lifecycle order.
- `MarkUploadSuccessWithAdifStampWithContext` updates only
  `qso.additional_data`; it does not touch `deleted_at`, so it cannot
  resurrect a soft-deleted QSO by itself.
- The "including deleted" QSO fetch helpers are intentionally scoped to
  forwarding/upload views and do not leak into normal GET/list paths.

## Verification

Commands run:

```sh
GOCACHE=/tmp/go-build go test ./internal/database/sqlite ./internal/qsoservice ./internal/forwarding/worker
GOCACHE=/tmp/go-build go test ./internal/api
GOCACHE=/tmp/go-build go test ./cmd/smd
GOCACHE=/tmp/go-build go test -race ./internal/database/sqlite ./internal/qsoservice ./internal/forwarding/worker
GOCACHE=/tmp/go-build go vet ./internal/database/sqlite ./internal/qsoservice ./internal/forwarding/worker ./internal/api ./cmd/smd
GOCACHE=/tmp/go-build go test ./internal/database/...
```

Results:

- `internal/database/sqlite`, `internal/qsoservice`, and
  `internal/forwarding/worker` passed.
- `internal/api` initially failed in the sandbox because
  `httptest.NewServer` could not bind `localhost`; rerun outside the
  sandbox passed.
- `cmd/smd` passed.
- Race test passed for the focused database/QSO/worker packages.
- Vet passed for the reviewed package and adjacent call paths.
- `internal/database/...` passed.
