# internal/forwarding review - 2026-06-19

## Scope

Fresh review of the current `internal/forwarding` tree as if the codebase were
new to me, including:

- `internal/forwarding` root interface and registry
- `internal/forwarding/qrz`
- `internal/forwarding/stub`
- `internal/forwarding/worker`
- adjacent enqueue, worker-spawn, upload-state, event, and documentation
  contracts in `internal/qsoservice`, `internal/database/sqlite`, `cmd/smd`,
  `internal/api`, and current non-archive docs

I did not use archived review documents as evidence. The worktree already had
an untracked `docs/reviews/internal-events-2026-06-19.md` before this review; I
left it untouched.

## Findings

### M1 - Delete LOGID lookup can select a stale upstream id after row re-arm

Category: correctness

Delete forwarding depends on `FetchPriorUpstreamIDWithContext` choosing the
current upstream id from the last successful upstream-creating action. The
method filters uploaded insert/update rows, then orders only by `created_at
DESC`:

- `internal/database/sqlite/api_context.go:2247-2265`
- `internal/database/sqlite/api_context.go:2295-2302`

That is not the same as "last successful upload". `qso_upload.created_at` is
second-resolution and immutable (`internal/database/sqlite/migrations/0001_init.up.sql:180-193`).
`InsertQsoUploadTx` explicitly re-arms an existing row on conflict without
changing its id or `created_at` (`internal/database/sqlite/api_context.go:2440-2454`).
The later success transition updates the existing row and relies on
`modified_at` for freshness (`internal/database/sqlite/api_context.go:1830-1841`,
`internal/database/sqlite/migrations/0001_init.up.sql:197-205`).

So a re-armed insert or update row can be the most recent successful upstream
write while still carrying an older `created_at` than the other uploaded row.
A fast insert/update sequence can also tie at second resolution. In both cases
the delete path can pass an older LOGID to QRZ or a future forwarder, making the
delete target stale or nondeterministic.

Recommendation: select by successful-transition freshness, not row creation
freshness. Ordering by `modified_at DESC, id DESC` for uploaded insert/update
rows is closer to the worker's real state transition. Add a regression that
creates both insert and update rows, re-arms the older row, marks it uploaded
with a new upstream id, and proves the delete lookup returns the re-armed row's
id.

### M2 - QRZ update success can be recorded without the LOGID needed for later delete

Category: correctness, test coverage

`classifyUpdate` treats `RESULT=OK` as success even when `LOGID` is empty:

- `internal/forwarding/qrz/response.go:186-193`

The QRZ delete path requires `priorUpstreamID`, and the worker fails delete
rows before calling `Submit` when no upstream id exists:

- `internal/forwarding/worker/worker.go:249-281`
- `internal/forwarding/qrz/qrz.go:277-287`

The implementation guide also says a delete with no stored upstream id fails
before submission (`docs/v2-design/forwarding-implementation.md:887-891`).
The live QRZ test expects update success to include a LOGID
(`internal/forwarding/qrz/live_test.go:130-136`), while the unit test only
covers the populated-LOGID case (`internal/forwarding/qrz/response_test.go:260-270`).

This leaves a hole: a QRZ update that returns `RESULT=OK` without `LOGID` is
marked uploaded, gets an ADIF upload-status stamp, and emits
`forward.succeeded`, but a later delete cannot identify the remote record and
settles as `forward.failed`.

Recommendation: require `LOGID` for QRZ update success unless there is a
documented QRZ response shape that truly omits it and a separate delete
strategy exists. Add `RESULT=OK` without `LOGID` tests at both classifier and
HTTP-submit layers.

### M3 - The test stub forwarder is registered in the production daemon

Category: correctness, security/hardening

`internal/forwarding/stub` describes itself as a test-only forwarder:

- `internal/forwarding/stub/stub.go:1-16`

But `cmd/smd` blank-imports it in the production daemon:

- `cmd/smd/main.go:30-31`

The default stub mode is `always_success` when credentials are omitted
(`internal/forwarding/stub/stub.go:75-88`), and successful submits return
`UpstreamID: "stub-ok"` without making any upstream call
(`internal/forwarding/stub/stub.go:129-149`). A real `config.json` can
therefore choose `type: "stub"` and the daemon will mark rows uploaded and emit
`forward.succeeded` without uploading anywhere. The install docs tell operators
to add forwarders for online services but do not warn that `stub` is a fake
destination (`docs/install.md:128-132`).

This is not an external attacker path, but it is a production footgun:
misconfigured or copied test config can produce false-positive upload status.

Recommendation: either remove the stub registration from the production
binary, gate it behind a dev/test build tag, or explicitly document and validate
it as a development-only destination. If keeping it available in production is
intentional, rename the package comments away from "test-only" and add a clear
operator warning in install/config docs.

### L1 - Forwarder cadence and retry config have lower-bound validation but no sane upper bounds

Category: performance, security/hardening

Config validation rejects negative or zero-invalid values, but it does not cap
oversized positive values:

- `internal/config/config.go:1140-1155`

Those values later become `time.Duration` values:

- `cmd/smd/main.go:906-910`
- `internal/forwarding/worker/backoff.go:37-41`

`computeBackoff` caps the exponential shift, but the initial `int seconds ->
time.Duration` multiplication can overflow before that guard runs. On overflow,
`initial <= 0` returns a zero delay, which can collapse retry backoff instead
of applying the configured cap. Similarly, very large `max_attempts`,
`batch_size`, or cadence values can create accidental self-DoS behavior from a
hand-edited config.

Recommendation: add explicit sane ceilings for forwarder `tick_interval_sec`,
`batch_size`, `retry.max_attempts`, `retry.initial_backoff_sec`, and
`retry.max_backoff_sec`, matching the existing config validation style for
bridge timeouts and other bounded knobs. Add tests for rejected overflow-scale
values.

### L2 - Forwarding docs still contain stale status and old delete-lookup names

Category: documentation

`docs/v2-design/forwarding.md` still says the QRZ port and SSE subsystem are
pending:

- `docs/v2-design/forwarding.md:866-919`

Current code and the milestone doc show both have landed:

- `internal/forwarding/qrz/qrz.go:1-4`
- `internal/api/server.go:129-133`
- `docs/v2-design/milestones.md:238-244`

`docs/v2-design/forwarding-implementation.md` also still describes the delete
lookup as `FetchInsertUpstreamIDWithContext` and "prior insert" only:

- `docs/v2-design/forwarding-implementation.md:346-351`
- `docs/v2-design/forwarding-implementation.md:734-746`

The implementation now uses `FetchPriorUpstreamIDWithContext` and considers
both insert and update rows:

- `internal/forwarding/worker/worker.go:249-281`
- `internal/database/sqlite/api_context.go:2247-2260`

Recommendation: refresh the forwarding design status section and update the
implementation guide's delete-lookup text so future forwarder work follows the
current insert-or-update contract.

## Security

No credential leakage or remote injection path found in the current QRZ
forwarder. API keys are parsed from the credentials blob, sent only in the form
body, and not logged by the forwarding code. QRZ response bodies are capped to
1 MiB before parsing.

The main hardening concern is M3: a fake success provider is registered in the
production daemon. L1 is the config-side self-DoS concern.

## Performance

Normal-path performance is appropriate for the package's design. The worker
claims bounded batches, processes rows sequentially per destination, and relies
on per-forwarder goroutines so one upstream does not block another. The queue
claim path is a single `UPDATE ... RETURNING` scoped by `forwarder_name`, which
is the right shape for SQLite.

The actionable performance issue is L1: unbounded operator values can defeat
the intended retry/cadence bounds.

## Test Coverage

Strong coverage exists for:

- registry validation and default retry registration
- stub modes
- QRZ HTTP transport classification, response classification, request shape,
  delete behavior, and manual live QRZ flows behind the `manual` tag
- worker success, transient retry, retry exhaustion, terminal failure, panic
  recovery, soft-delete handling, delete LOGID lookup, ADIF stamping, and
  future `next_attempt_at` gating
- adjacent API upload/event paths

Gaps tied to findings:

- no regression for delete LOGID lookup freshness after row re-arm or
  same-second insert/update rows (M1)
- no QRZ classifier/submit test for `RESULT=OK` update without `LOGID` (M2)
- no production guard test that prevents the test stub from being configured in
  real daemon builds (M3)
- no overflow-scale config validation tests for forwarding retry/cadence values
  (L1)

## Verification

Passed:

```sh
GOCACHE=/tmp/go-build go test ./internal/forwarding/... ./internal/qsoservice ./internal/database/sqlite ./cmd/smd
GOCACHE=/tmp/go-build go test -race ./internal/forwarding/...
GOCACHE=/tmp/go-build go test ./internal/api
GOCACHE=/tmp/go-build go test -race ./internal/api
GOCACHE=/tmp/go-build go vet ./internal/forwarding/... ./internal/qsoservice ./internal/database/sqlite ./cmd/smd
```

The first sandboxed `go test` and `go test -race` attempts for
`./internal/forwarding/...` failed only because QRZ tests use
`httptest.NewServer` and the sandbox blocked localhost listener creation:
`listen tcp6 [::1]:0: socket: operation not permitted`. I reran the listener
backed commands with listener permissions and they passed.

## Resolution (2026-06-19)

All five findings addressed.

- **M1 (fixed).** `FetchPriorUpstreamIDWithContext` now orders by `modified_at
  DESC, id DESC` (success freshness), not `created_at DESC` (immutable creation)
  — so a re-armed insert/update row that holds the live upstream id wins over a
  later-created but stale one. Regression
  `TestFetchPriorUpstreamID_OrdersBySuccessFreshnessNotCreation` pins it (drops
  the `modified_at` auto-touch trigger to set distinct second-resolution
  timestamps deterministically).
- **M2 (fixed).** `classifyUpdate`'s `RESULT=OK` branch now requires a `LOGID`
  (terminal otherwise), matching the insert path + the REPLACE branch — an update
  can no longer be marked uploaded with no upstream id, which would strand a later
  delete. Tests: `TestClassify_Update_OK_NoLogID_Terminal` (classifier) +
  `TestSubmit_Update_OK_MissingLOGID_IsTerminal` (HTTP submit).
- **M3 (fixed — build-tag, operator's choice).** The test-only `stub` forwarder's
  blank-import moved from `cmd/smd/main.go` to `cmd/smd/forwarder_stub_dev.go`
  (`//go:build dev`). The dev Taskfile targets + `dev-rpm.sh` (→ `deploy:local:dev`)
  build with `-tags dev` and keep stub for local forwarding tests; the release
  (`release-rpm.sh`) + the CGO-free `build:smd:static` (CI releasability gate) do
  NOT — so a shipped binary rejects `type:"stub"` as "unknown forwarder type".
  Verified: `go tool nm` shows 0 stub symbols in the release-shape binary, 11 in
  the dev build. (No dedicated guard *test* — `cmd/smd`'s suite uses stub heavily
  and unconditionally, so a `!dev` guard would force the whole suite into a
  tag-split; the build-tag + the registry's existing fail-loud on unknown types
  is the guarantee.)
- **L1 (fixed).** `validateForwarders` now caps `tick_interval_sec` (≤ 86400),
  `batch_size` (≤ 1000), `retry.max_attempts` (≤ 100), and the backoff seconds
  (≤ 86400 — also keeps `secs * time.Second` well inside int64, closing the
  overflow that could collapse backoff to zero). Tests added to
  `TestLoad_Forwarders_ValidationErrors` (over-bound tick/batch/initial_backoff).
- **L2 (fixed — per the new doc-map regime).** `forwarding.md` and
  `forwarding-implementation.md` are Tier-2 historical design briefs (demoted in
  session 186's doc-map), so rather than freshen their stale status text, each got
  the **historical banner** stamped at its top pointing at the current truth
  (code + ADRs); the implementation-guide banner also calls out the specific drift
  (delete lookup is `FetchPriorUpstreamIDWithContext`, insert-or-update, not the
  old `FetchInsertUpstreamIDWithContext`). This is the little-by-little
  "banner-as-touched" process from `docs/README.md`.

Verified: `gofmt`/`go vet` clean; default (CGO-free release-shape) `go build
./...`; `internal/forwarding/...`, `internal/config`, `internal/database/sqlite`,
`cmd/smd` pass; `go test -race ./internal/forwarding/...` clean; dev-tagged
`cmd/smd` tests clean (no double-registration).

Not run:

- QRZ manual live tests (`task test:qrz-live`,
  `task test:qrz-live-interactive`) because they require real QRZ credentials
  and make real upstream writes.
- `go test ./...`; this review stayed focused on `internal/forwarding` and its
  closest contracts.
