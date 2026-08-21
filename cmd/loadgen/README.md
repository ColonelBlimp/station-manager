# cmd/loadgen — daemon stress / profiling harness

Drives the daemon's hot-path endpoints under load so pprof captures
have something to sample. Built to find the kind of bottleneck that
only shows up at concurrency, not in unit tests. The 2026-05-09
profiling expedition used it to find five bugs; the full historical chain is
recoverable through `docs/session-handoff.md`'s retirement route.

## When to use it

- A code change is suspected to affect throughput / latency on a
  read or write path.
- A new endpoint is shipped and you want a baseline before regressions
  can creep in.
- Something feels slow during normal operation and you want to confirm
  the bottleneck before optimising.

Not a regression-test framework — runs are operator-triggered, results
are interpreted, no pass/fail gates. The harness emits per-endpoint
p50/p95/p99 latency and throughput so you can compare runs by eye.

## Pre-flight: daemon config

The harness pushes well past the daemon's production-default
self-DoS guards. Bump these in `build/config.json` (or wherever the
daemon's config lives) before starting the daemon:

```json
"server": {
    "enable_profiling": true,
    "max_concurrent_requests": 1024,
    "submit_rate_per_sec": 5000,
    "submit_rate_burst": 10000
}
```

Restore them after stress runs — the production defaults
(`max_concurrent_requests=128`, `submit_rate_per_sec=20`,
`submit_rate_burst=40`) are operator-protective floors, not perf
ceilings.

`enable_profiling=true` mounts pprof at `/debug/pprof/*` so you can
capture profiles in a third terminal while a run is in flight. Off by
default for security (pprof exposes goroutine/heap state and
`/debug/pprof/profile?seconds=N` is a DoS vector); flip it back off
when done.

## Two run shapes

### Submit-only — pure write-path stress

```sh
go run ./cmd/loadgen \
    -count 10000 \
    -concurrency 100 \
    -station 7Q5MLV \
    -prefix 7Q9
```

Each iteration POSTs one ADIF record. Sequential callsigns
(`<prefix><suffix>`) keep dedupe miss-rate at 100% so every record
exercises the full insert + qso_history audit + events broadcast path.
3-letter suffix caps at 17576 unique callsigns per prefix; the harness
auto-extends to 4 or 5 letters as needed.

`-station` must match the target logbook's callsign — the daemon
rejects mismatches at submit time. `-logbook` defaults to 1 (the
seeded default).

### Mixed mode — operator's actual flow

```sh
go run ./cmd/loadgen \
    -count 10000 \
    -concurrency 100 \
    -station 7Q5MLV \
    -prefix 7Q9 \
    -mode mixed
```

Each iteration fires three sequential requests: GET
`/v1/enrich/callsign` → GET `/v1/contact-history` → POST `/v1/qso`.
Mirrors the SPA's Tab → submit cycle so contention between read and
write paths surfaces.

**Cold-cache warning:** the first time a callsign is enriched the
daemon synchronously calls hamnut + QRZ. With unique callsigns at
concurrency 100, that's a lot of upstream pressure. Either:

1. **Pre-warm the cache** with a submit-only run on the same prefix
   (the contacted_station rows it creates serve as stale-hits so the
   subsequent mixed run returns from cache and queues bounded async
   refreshes).
2. Use the `-skip-enrich` shape (TODO if/when added) for pure
   DB-side stress.
3. Accept ~5 s/cold-miss latency and bounded run.

### SSE subscriber flag

Pass `-sse` to open one `/v1/events` connection in the background.
Tests the events Hub broadcast cost when an SPA tab is "open" against
the daemon. The subscriber doesn't emit results — it's a daemon-side
load contributor only.

## Capturing profiles

In a third terminal while a run is in flight:

```sh
mkdir -p /tmp/sm-profiles

# 20-second CPU profile (the 30s default exceeds the daemon's WriteTimeout)
curl -sS -o /tmp/sm-profiles/cpu.pb.gz \
    "http://localhost:8080/debug/pprof/profile?seconds=20"

# Heap snapshot (instant)
curl -sS -o /tmp/sm-profiles/heap.pb.gz \
    "http://localhost:8080/debug/pprof/heap"

# Allocation profile (counts every alloc since startup; useful for GC-pressure detection)
curl -sS -o /tmp/sm-profiles/allocs.pb.gz \
    "http://localhost:8080/debug/pprof/allocs"

# Goroutine snapshot (single moment)
curl -sS -o /tmp/sm-profiles/goroutine.pb.gz \
    "http://localhost:8080/debug/pprof/goroutine"
```

Read with `go tool pprof`:

```sh
go tool pprof -top -cum /tmp/sm-profiles/cpu.pb.gz | head -25
go tool pprof -top      /tmp/sm-profiles/allocs.pb.gz | head -25
```

Cumulative view shows where time is going through the call stack;
flat view shows where time lands at the leaf. Both are useful — the
2026-05-09 expedition found the ADIF parser allocation hotspot in the
allocs flat view, then the planner-stats issue from the goroutine
snapshot showing workers parked on `database/sql.(*DB).conn`.

If a CPU profile shows zero samples in a 20s window, the daemon was
mostly blocked in syscalls (fsync, network) — Go's pprof CPU only
samples while goroutines are executing Go code. That's a finding in
itself, not a tool failure.

## Flag reference

| Flag | Default | Purpose |
|---|---|---|
| `-target` | `http://localhost:8080` | Daemon base URL |
| `-concurrency` | 50 | Concurrent workers |
| `-count` | 5000 | Total iterations (= QSOs in submit mode; = Tab cycles in mixed mode) |
| `-prefix` | `7Q1` | Callsign prefix; suffix appended automatically |
| `-station` | `G4ABC` | STATION_CALLSIGN value (must match logbook callsign) |
| `-logbook` | 1 | Logbook ID |
| `-request-timeout-sec` | 10 | Per-request timeout |
| `-mode` | `submit` | `submit` or `mixed` |
| `-sse` | false | Open one `/v1/events` subscriber in background |

## Reading the output

The summary block at the end shows per-endpoint counts, status-code
breakdown, and latency percentiles. A `429` count above zero means the
daemon's rate limiter is throttling; either bump the rate or accept
the noise. An `error` count above zero means transport failures
(daemon refused, timed out, crashed) — check the daemon log.

The throughput number is `count / (sum(latency) / concurrency)` — an
approximation of wallclock req/s based on per-request latency
totals. For order-of-magnitude reading, not for SLA reporting.
