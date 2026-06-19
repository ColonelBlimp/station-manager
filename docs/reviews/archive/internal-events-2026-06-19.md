# internal/events review - 2026-06-19

## Scope

Fresh review of `internal/events` as if the codebase were new to me, with
adjacent inspection of the live publishers and the `/v1/events` SSE consumer:

- `internal/events`
- `internal/api/handler_events.go`, event handler tests, and event E2E tests
- `internal/api/server.go` and subscriber limiting
- `internal/qsoservice` publish sites for QSO lifecycle events
- `internal/forwarding/worker` publish sites for forwarding outcome events
- current non-archive docs that describe the event contract

I did not use archived review documents as evidence. The worktree was clean
before creating this review artifact.

## Findings

### M1 - `/v1/events` can miss events after the client observes the stream as open

Category: correctness

`handleEvents` sends `200 OK` and flushes the SSE response before it subscribes
to the hub:

- `internal/api/handler_events.go:79-81` arms the write deadline, writes the
  status, and flushes.
- `internal/api/handler_events.go:83-84` subscribes to the hub only after that.
- The handler comment at `internal/api/handler_events.go:35-37` and the design
  doc at `docs/v2-design/api.md:216-220` rely on the client contract "open the
  stream first, then fetch current state" to avoid missing events.

Because the hub intentionally has no replay, a client that receives the open
SSE response and then immediately fetches state or triggers a write can still
lose any event published in the small window before `Subscribe()` runs. The
tests avoid this race by waiting on the internal hub count before publishing
(`internal/api/handler_events_test.go:124`, `internal/api/handler_events_test.go:190`,
and `internal/api/handler_events_e2e_test.go:92`), but real clients cannot
observe that internal barrier.

Recommendation: subscribe before writing/flushing the `200 OK` response, then
defer `unsub()` immediately. Add a regression test with an instrumented response
writer that proves the hub subscription exists before the first observable
flush.

### M2 - `qso.updated` and `qso.deleted` publish paths are not covered through the stream

Category: test coverage

The five-event vocabulary includes `qso.updated` and `qso.deleted`, and the
service publishes them after successful transactions:

- `internal/qsoservice/update.go:280-283`
- `internal/qsoservice/delete.go:92-95`

However, the current event E2E coverage exercises `qso.stored`,
`forward.succeeded`, and `forward.failed` only:

- `internal/api/handler_events_e2e_test.go:59-151`
- `internal/api/handler_events_e2e_test.go:154-222`

I found no test references to `NameQsoUpdated`, `NameQsoDeleted`,
`qso.updated`, or `qso.deleted` under `internal/*_test.go`. That leaves two of
the documented five event types unpinned at the API boundary. A future change
could rename the event, publish the wrong payload, publish before commit, or
break SSE serialization for update/delete without failing the focused event
tests.

Recommendation: add SSE tests that open `/v1/events`, perform `PATCH
/v1/qso/{uuid}` and `DELETE /v1/qso/{uuid}`, and assert the event name plus
`qso_id`/`logbook_id` payload. These can be API-level tests; full forwarding
E2E is not needed for this gap.

### L1 - The API slow-reader eviction test has no assertion

Category: test coverage

`TestHandleEvents_SlowReaderIsEvictedAndStreamEnds` documents the expected
behavior, publishes 500 events, then discards the response and hub values:

- `internal/api/handler_events_test.go:291-322`

There is no assertion on `SubscriberCount`, handler completion, EOF, or write
failure. As written, this test cannot fail if the handler stops exiting on slow
readers. The lower-level hub test does cover channel-buffer eviction
(`internal/events/hub_test.go:136-167`), but it does not exercise the HTTP
handler's write/flush/deadline path.

Recommendation: make the API test deterministic with a custom
`http.ResponseWriter` that blocks or returns an error after a controlled number
of writes, then assert the handler returns and unsubscribes. If that is too
artificial, remove the no-op test and rely on the hub unit test plus the
per-write-deadline regression test.

### L2 - Reconnect documentation still points clients at retired `/v1/qsos`

Category: documentation

The event package docs and the event design section both say reconnecting
clients should re-query via `GET /v1/qsos`:

- `internal/events/doc.go:10-13`
- `docs/v2-design/api.md:216-220`

The current router does not register that route. The relevant live routes are
`GET /v1/qso/{uuid}` and `GET /v1/logbook/{id}/qso`:

- `internal/api/server.go:99-112`
- `docs/v2-design/api-endpoints.md:57-80`
- `docs/v2-design/api-endpoints.md:105-110`

This matters because the event payloads are intentionally minimal; correct
client reconciliation depends on fetching the authoritative state from the
right endpoint.

Recommendation: update the reconnect docs to name the logbook-scoped list for
baseline reconciliation, and the singular QSO endpoint for follow-up detail
fetches.

## Security

No direct security defect found in the current event package. The production
event names are constants, payloads are JSON-marshaled, `/v1/events` is behind
the shared SSE subscriber cap, and the handler bounds each write with a deadline.

One hardening note: `writeSSEEvent` writes `evt.Name` directly into the SSE
`event:` line (`internal/api/handler_events.go:151`). Current production callers
only use trusted constants, so this is not an active injection path. If the hub
ever accepts event names derived from input or plugin-style code, make the event
name a typed/validated value before it reaches the SSE writer.

## Performance

No performance defect found for the documented scale. `Hub.Publish` holds a
mutex while it walks subscribers, but sends are non-blocking and the server caps
SSE subscribers at `Server.MaxEventSubscribers` (default 16). That makes the
fan-out cost bounded and appropriate for the package's stated personal-operator
scale.

## Documentation

The package docs are useful about no-replay semantics, slow-reader eviction, and
scale assumptions. The actionable documentation issue is the stale reconnect
route in L2.

## Verification

Passed:

```sh
GOCACHE=/tmp/go-build go test ./internal/events ./internal/qsoservice ./internal/forwarding/worker
GOCACHE=/tmp/go-build go test ./internal/api
GOCACHE=/tmp/go-build go test -race ./internal/events
GOCACHE=/tmp/go-build go test -race ./internal/api
GOCACHE=/tmp/go-build go vet ./internal/events ./internal/api ./internal/qsoservice ./internal/forwarding/worker
```

The first combined attempt including `./internal/api` hit the sandbox's
localhost-listener restriction in `httptest.NewServer`:
`listen tcp6 [::1]:0: socket: operation not permitted`. I reran
`GOCACHE=/tmp/go-build go test ./internal/api` with listener permissions and it
passed.

## Resolution (2026-06-19)

All four findings fixed.

- **M1 (fixed).** `handleEvents` now calls `s.hub.Subscribe()` (+ `defer unsub()`)
  BEFORE setting headers / writing the `200 OK` / flushing, so the subscription
  exists by the time the client can observe the open stream — closing the
  no-replay gap. Regression test `TestHandleEvents_SubscribesBeforeStreamObservable`
  drives the handler with an instrumented writer (`probeWriter`) and asserts the
  hub subscriber count is 1 at the first flush.
- **M2 (fixed).** New e2e `TestE2E_SSE_UpdateAndDeleteFlow` opens `/v1/events`,
  submits → `PATCH` → `DELETE` a QSO (no forwarder configured, so only the qso
  lifecycle events flow), and asserts the `qso.updated` and `qso.deleted` frames
  with their `qso_id`/`logbook_id` payloads.
- **L1 (fixed).** `TestHandleEvents_SlowReaderIsEvictedAndStreamEnds` rewritten
  to be deterministic + assert: it parks the handler in its open-stream flush
  (via `probeWriter.release`), overflows the buffer so the hub evicts, releases
  the flush, then asserts the handler exits and `SubscriberCount` drops to 0. The
  old version discarded results and could not fail.
- **L2 (fixed).** Reconnect docs in `internal/events/doc.go` and
  `docs/v2-design/api.md` now point at the live routes — `GET /v1/logbook/{id}/qso`
  (baseline) + `GET /v1/qso/{uuid}` (detail) — instead of the retired `/v1/qsos`;
  the api.md note also records the M1 server-side fix.

The security hardening note (`writeSSEEvent` writing `evt.Name` raw) needs no
change today — all production event names are trusted constants; left as a
documented caveat for any future input-derived event name.

Verified: `gofmt`/`go vet` clean; `go build ./...`; `internal/events`,
`internal/api`, `internal/qsoservice`, `internal/forwarding/worker` pass;
`go test -race ./internal/events ./internal/api` clean.

## Residual risk

I did not run frontend tests or `go test ./...`; this review focused on
`internal/events` and the adjacent server/publisher packages that define its
runtime contract.
