# internal/pskreporter code review - 2026-06-19

## Scope

Fresh review of `internal/pskreporter` and the adjacent contracts that make the
uploader correct in production:

- `internal/pskreporter/service.go`
- `internal/pskreporter/ipfix.go`
- package tests under `internal/pskreporter`
- FT8 decode-sink boundary in `internal/ft8`
- daemon wiring and shutdown ordering in `cmd/smd`
- PSK Reporter config/documentation in `internal/types`, `docs/ft8.md`, and
  `cmd/ft8-psk-probe`

Focus areas: correctness, performance, security, test coverage, and
documentation. This is review-only; no source fixes were made.

## Summary

The core implementation is compact and mostly well-shaped for an optional,
best-effort UDP uploader. The IPFIX encoder is byte-tested against the documented
wire shape, `AddSpot` does not perform UDP I/O on the FT8 decode goroutine, the
service keeps one long-lived UDP socket, and the daemon gates upload on explicit
`psk_reporter.enabled` plus a configured receiver callsign.

The main risks are lifecycle edges rather than the steady-state encoder:
shutdown can cancel the uploader's flush loop before FT8 has finished draining,
the advertised buffer limit is a wake-up threshold rather than a cap, and the
flush loop can retain long-lived timers when full-buffer wakes happen repeatedly.
There are also a few comments that now contradict the fixed 4-field receiver
template and service API shape.

## Findings

### M1. Shutdown can drop decode reports after the uploader's final flush

**Area:** correctness / lifecycle  
**Files:** `cmd/smd/main.go:370-380`, `cmd/smd/main.go:612-621`,
`cmd/smd/main.go:662-686`, `internal/pskreporter/service.go:136-145`,
`internal/pskreporter/service.go:149-163`,
`internal/pskreporter/service.go:172-178`

`cmd/smd` starts the FT8 subsystem and the PSK Reporter uploader with the shared
`workerCtx`:

- `ft8Svc.Start(workerCtx)` at `cmd/smd/main.go:612`.
- `pskSvc.Start(workerCtx)` at `cmd/smd/main.go:618`.

On graceful shutdown, `workerCancel()` runs before FT8 is stopped:

- `workerCancel()` at `cmd/smd/main.go:666`.
- `ft8Svc.Stop()` at `cmd/smd/main.go:682`.
- `pskSvc.Stop()` at `cmd/smd/main.go:686`.

The uploader's flush loop exits as soon as its context is canceled, performing
one best-effort final flush at `internal/pskreporter/service.go:176-178`. The
socket is not closed until `Stop()` later waits for the goroutine and sets
`s.conn = nil` at `service.go:157-162`.

That leaves a shutdown window where FT8 may still be draining an in-flight
decode, but the PSK Reporter flush loop has already exited. `AddSpot` uses
`s.conn != nil` as the "live" check (`service.go:101-104`), so a late decode in
that window can buffer spots into `s.buf`. Nothing flushes them afterward:
`Stop()` waits for the already-exited loop and closes the socket, but does not
perform another `flush()` after the wait.

This is best-effort telemetry, so losing the final shutdown slot is not
operator-data loss. It does, however, contradict the "final flush" behavior that
the service and `cmd/smd` comments promise, and it makes the shared context
ordering fragile.

**Recommendation:** decouple the uploader's lifetime from the early
`workerCancel()` path, or make `Stop()` the single owner of final flush. Practical
options:

- start `pskSvc` with its own context and only cancel it inside `pskSvc.Stop()`
  after `ft8Svc.Stop()` has drained, or
- have `Stop()` set a stopped/live flag under the lock, cancel the loop, wait,
  then run one last `flush()` before closing the socket, while ensuring
  post-stop `AddSpot` drops.

Add a regression test that cancels the parent context, adds a spot before
`Stop()`, then proves either the spot is dropped intentionally or included in the
final datagram. The current code would buffer it and lose it silently.

### M2. `maxBufferedRows` is not an actual buffer/datagram cap

**Area:** correctness / performance / availability  
**Files:** `internal/pskreporter/service.go:34-38`,
`internal/pskreporter/service.go:101-116`,
`internal/pskreporter/service.go:190-213`,
`internal/pskreporter/ipfix.go:148-155`,
`internal/pskreporter/ipfix.go:189-207`

`maxBufferedRows` is documented as the point where a datagram would fill
(`service.go:38`). In practice, it only sends a non-blocking wake to the flush
loop:

```go
full := len(s.buf) >= maxBufferedRows
...
select {
case s.flushNow <- struct{}{}:
default:
}
```

There is no cap on `s.buf` while the wake is pending. Additional unique calls
continue to be added until the flush goroutine gets scheduled and takes the
snapshot. Under normal FT8 receive rates this is unlikely to become large, but
the package API itself allows a bursty caller or future source to enqueue far
past the intended datagram size.

That matters because `flush()` snapshots every buffered spot into one datagram
(`service.go:196-211`), while `encodeDatagram` writes the IPFIX length as
`uint16(16+len(body))` (`ipfix.go:203`). Long variable-length fields are
truncated to 254 bytes each, but a sufficiently large burst can still exceed the
UDP/IPFIX practical size. The likely outcome is an oversized UDP write error or
a wrapped IPFIX length field, after the buffer has already been cleared.

**Recommendation:** make the threshold an enforced bound, not only a wake-up
hint. Prefer one of these approaches:

- rotate a full snapshot synchronously under the lock into a queue consumed by
  the flush goroutine, while keeping UDP I/O off the caller goroutine;
- drop additional lower-priority spots once `len(s.buf) >= maxBufferedRows` and
  a flush is already pending; or
- split snapshots into chunks of at most `maxBufferedRows` before encoding.

Add tests that burst more than `maxBufferedRows` unique calls and assert that
the service emits bounded datagrams or intentionally drops overflow with a
counter/log, instead of encoding one unbounded packet.

### L1. Repeated early flush wakes can retain long-lived timers

**Area:** performance  
**File:** `internal/pskreporter/service.go:172-182`

`flushLoop` creates a fresh `time.After(flushInterval + jitter)` every loop.
When the `<-s.flushNow` case wins, that timer cannot be stopped and remains
allocated until its five-minute-plus-jitter deadline expires. Full-buffer wakes
should be rare for normal FT8 receive rates, but this is still an avoidable
timer retention pattern in a long-running daemon and becomes visible if a busy
band or future source causes repeated early flushes.

**Recommendation:** use `time.NewTimer`, stop and drain it on the `ctx.Done()`
and `flushNow` branches, then create the next timer after each flush. This
matches the timer hygiene used elsewhere in the daemon.

**Test gap:** no test currently exercises repeated `flushNow` wake-ups or timer
behavior. This can be covered indirectly by a deterministic flush-loop test with
a shortened interval, or simply guarded in review once the implementation uses a
stoppable timer.

### L2. Lifecycle/API comments are stale

**Area:** documentation / maintainability  
**Files:** `internal/pskreporter/ipfix.go:62-68`,
`internal/pskreporter/service.go:49-50`

Two comments contradict the current implementation:

- `Receiver.Antenna` says an empty antenna is "omitted" and that a 3-field
  template is used. The current encoder intentionally always emits a 4-field
  receiver template and sends antenna as an empty variable-length field when
  unset.
- `Service` says to construct with `New`, then `Initialize -> Start(ctx) ->
  Stop`, but the service has no `Initialize` method.

The tests and surrounding comments correctly describe the fixed 4-field receiver
shape, so this is documentation drift rather than a runtime bug.

**Recommendation:** update the comments to match the current contract. While
there, document whether `Start` is intended to be single-use or idempotent; today
a second `Start` call would open another socket and flush loop without closing
the first.

## Test Coverage Notes

Strong coverage observed:

- `ipfix_test.go` covers variable-length strings, receiver and sender template
  bytes, receiver and sender record bytes, header length, alignment, and session
  identifier placement.
- `service_test.go` covers the real UDP send path through a local listener,
  strongest-SNR deduplication, disabled no-op behavior, and dropping spots before
  a successful `Start`.
- `internal/ft8/sequence_test.go` covers `SpotFrom` for CQ, directed report,
  directed grid, `/P`, hashed, bare CQ, and free-text inputs.
- `cmd/ft8-psk-probe` compiles and uses the same `SpotFrom` parser as the daemon.

Coverage gaps worth adding:

- Shutdown ordering: parent context canceled before `Stop`, late `AddSpot`, and
  final flush behavior.
- More than `maxBufferedRows` unique calls in one burst.
- `Start` called with an already-canceled context and `Start` called twice.
- Timer behavior under repeated full-buffer wake-ups.
- Daemon wiring test or helper-level test for `cmd/smd` PSK Reporter ordering,
  if that wiring gets factored.

## Security Notes

The uploader is opt-in and publishes receive reports to a public service only
when `psk_reporter.enabled` is true and a receiver callsign is configured. It
does not handle credentials or accept untrusted inbound network data. The main
security-relevant property is availability: malformed config or oversized bursts
should not block the daemon or cause unbounded memory growth. Findings M1/M2 are
therefore primarily availability concerns.

## Verification

Commands run:

```sh
GOCACHE=/tmp/go-build go test ./internal/pskreporter
GOCACHE=/tmp/go-build go test -race ./internal/pskreporter
GOCACHE=/tmp/go-build go test ./internal/ft8
GOCACHE=/tmp/go-build go test -race ./internal/ft8
GOCACHE=/tmp/go-build go test ./cmd/ft8-psk-probe
GOCACHE=/tmp/go-build go test ./cmd/smd
GOCACHE=/tmp/go-build go test -race ./cmd/smd
GOCACHE=/tmp/go-build go vet ./internal/pskreporter ./cmd/ft8-psk-probe
GOCACHE=/tmp/go-build go vet ./cmd/smd
```

Results:

- `internal/pskreporter`: pass.
- `internal/pskreporter` under `-race`: pass.
- `internal/ft8`: pass.
- `internal/ft8` under `-race`: pass.
- `cmd/ft8-psk-probe`: pass / no test files.
- `cmd/smd`: pass.
- `cmd/smd` under `-race`: pass.
- `go vet` over the reviewed package/probe/startup surface: pass.

The first sandboxed runs of UDP/HTTP listener-backed tests failed with
`socket: operation not permitted`; rerunning the same focused commands with
localhost binding allowed passed.

## Resolution (2026-06-19)

All four findings fixed (lifecycle/availability/docs in the best-effort uploader;
nothing deferred). All changes are contained in `internal/pskreporter` — no
`cmd/smd` shutdown-ordering change was needed (see M1).

- **M1 (fixed).** `Stop()` now OWNS the authoritative final flush instead of
  relying on the loop's ctx.Done flush. It waits for the loop, sets a new
  `stopped` flag (so further `AddSpot` drops rather than buffering into a
  closing socket), runs one `flush()`, then closes. Because `cmd/smd` stops FT8
  (`ft8Svc.Stop()`) before `pskSvc.Stop()`, a last decode that buffers a spot
  after `workerCancel()` is now sent by Stop's final flush. Chose this (the
  review's option b, fully self-contained in the service) over giving the
  uploader its own context — no daemon-wiring change. Test:
  `TestService_StopFlushesLateSpot` (cancel parent → late AddSpot → Stop sends it;
  post-Stop AddSpot drops).
- **M2 (fixed).** `flush()` now splits the snapshot into datagrams of at most
  `maxBufferedRows` spots each, so a bursty buffer (the threshold is only a
  wake-up hint, not a hard cap) can no longer produce a single oversized UDP
  packet / wrapped IPFIX length. Per-datagram header state (seq/templates/sent)
  is computed up-front under the one lock; UDP I/O stays off the lock. Test:
  `TestService_FlushChunksLargeBuffer` (165 spots → 3 bounded datagrams, each
  with a self-consistent length field).
- **L1 (fixed).** `flushLoop` uses a single `time.NewTimer` with a `resetTimer`
  helper (stop + non-blocking drain + reset) on the flushNow / timer branches,
  instead of allocating a fresh `time.After` each iteration — a flushNow wake no
  longer leaks the old 5-min-plus-jitter timer until its deadline.
- **L2 (fixed, docs).** Corrected the two stale comments: `Receiver.Antenna` now
  says an empty antenna is sent as an empty variable-length field (the encoder
  always uses the fixed 4-field receiver template), and the `Service` doc drops
  the non-existent `Initialize` step and notes `Start` is single-use.

Verified: `gofmt`/`go vet` clean; `internal/pskreporter` passes normal + `-race`;
`cmd/ft8-psk-probe` + CGO-free `cmd/smd` build. The `AddSpot` signature is
unchanged, so the `internal/ft8` decode-sink boundary is unaffected.
