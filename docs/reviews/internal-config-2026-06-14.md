# internal/config Code Review - 2026-06-14

## Scope

Reviewed `internal/config` and the adjacent contracts that make its behavior
observable:

- `internal/config/{config.go,validate.go,migrations.go,defaults.go,doc.go}`
- `internal/config/{config_test.go,lookup_test.go}`
- `/v1/config` GET/PUT handling in `internal/api/handler_config.go`
- runtime consumers in `internal/bridge`, `internal/serial`, `internal/qsoservice`,
  and the logging SPA config/QSO submit paths

This is review-only. No source fixes were made.

## Findings

### H1 - `bridge.timeouts.write_watchdog_ms` bypasses config validation

`types.BridgeTimeoutsConfig` includes `WriteWatchdogMs`, documented as the
serial-write backstop that closes the port if a write hangs:

- `internal/types/bridge.go:119-130`

`validateBridge` range-checks the other timeout fields through `checkTimeout`,
but omits `WriteWatchdogMs`:

- `internal/config/config.go:1116-1149`

The value is still live. `bridge.New` resolves it directly into
`s.writeWatchdog`, `runPipeline` copies it into `serial.Config.WriteTimeoutMS`,
and the serial layer treats that value as the watchdog for a blocking port
write:

- `internal/bridge/service.go:208-212`
- `internal/bridge/pipeline.go:136-139`
- `internal/serial/config.go:42-50`
- `internal/serial/serial.go:222-230`

Impact:

- A typo such as `write_watchdog_ms: 1` can make legitimate CAT writes close the
  port on normal scheduling jitter.
- A very large value can leave command/tune stop writes blocked far longer than
  the intended fault backstop; sufficiently large durations can also overflow
  before they reach the serial layer.
- This is specifically hardware-write-path relevant: the watchdog was added to
  keep blocked writes from hanging bridge commands and tune/FT8 unkey paths.

Recommendation:

- Run `checkTimeout("bridge.timeouts.write_watchdog_ms", b.Timeouts.WriteWatchdogMs)`
  in `validateBridge`.
- Add focused tests for below-min and above-max `write_watchdog_ms`, alongside
  the existing timeout tests.

### M1 - `config.Service` accessors bypass the mutex and expose shared mutable data

The service-level contract says reads should use `Snapshot()` and writes should
use `Update()` so `Cfg` and `Path` do not race:

- `internal/config/config.go:1175-1182`
- `internal/config/config.go:1211-1214`
- `internal/config/config.go:1227-1245`

Several public accessors read `s.Cfg` directly without an `RLock`, and
`Forwarders()` returns the live slice header:

- `internal/config/config.go:1303-1369`
- `internal/config/config.go:1320-1323`

That slice is read during QSO submit/update/delete:

- `internal/qsoservice/submit.go:286-297`
- `internal/qsoservice/update.go:235-245`
- `internal/qsoservice/delete.go:61-65`

At the same time, `/v1/config` writes a whole candidate config through
`Service.Update`:

- `internal/api/handler_config.go:147-153`
- `internal/api/handler_config.go:231-233`

Impact:

- A config PUT concurrent with a QSO submit can race on `s.Cfg` even when the
  PUT edits unrelated fields, because `Update` assigns the whole struct while
  `Forwarders()` reads one field outside the lock.
- Returning shared slices/maps/pointers also means a caller can accidentally
  mutate nested live config after taking what looks like a safe value snapshot.
  The current PUT path is close to this hazard: it builds `candidate := current`
  from a shallow `Snapshot()` and then `Normalize(&candidate)` mutates
  `candidate.Rigs` entries when removing redundant rig overrides
  (`internal/config/config.go:641-653`).

The focused `-race` suite did not report a race, but there is no test that runs
`Update` concurrently with `Forwarders()` or QSO submit.

Recommendation:

- Make every service accessor take the read lock, or implement each through
  `Snapshot()`.
- Return defensive copies for slices/maps that callers range over or could
  mutate.
- Add a race test that loops `Update` while another goroutine calls
  `Forwarders()` or submits QSOs.
- Consider a `Config.Clone()` helper if `Snapshot()` is meant to be safe for
  candidate editing, not just read-only inspection.

### M2 - `operator` and `owner_callsign` are callsign fields in the UI contract but are not validated by `internal/config`

The logging SPA treats all three identity fields as callsigns. The config API
type says the daemon enforces the fallback chain, and the My Station panel uses
the same callsign validator for `station_callsign`, `owner_callsign`, and
`operator`:

- `frontend/logging/src/lib/api/config.ts:143-147`
- `frontend/logging/src/lib/ui/panels/MyStationPanel.svelte:333-359`

The normal QSO submit path sends all three from daemon-authoritative config into
ADIF, and the ADIF formatter emits `OPERATOR` and `OWNER_CALLSIGN` when present:

- `frontend/logging/src/lib/ui/panels/QsoPanel.svelte:361-380`
- `frontend/logging/src/lib/utils/adif.ts:236-244`

`validateLoggingStation` only validates `StationCallsign`; it does not trim,
uppercase, or validate `Operator` or `OwnerCallsign`:

- `internal/config/validate.go:125-149`

The FT8 API also falls back to `Operator` when `StationCallsign` is empty, then
passes that value to FT8 sequencing with only a non-empty check:

- `internal/api/handler_ft8_qso.go:43-55`
- `internal/api/handler_ft8_qso.go:87-99`
- `internal/ft8/caller_sequencer.go:27-35`

Impact:

- A direct API client or hand-edited config can persist malformed or
  non-canonical `operator` / `owner_callsign` values that the SPA itself would
  reject.
- Those values can be emitted into logged ADIF.
- In the station-empty/operator-present FT8 fallback case, malformed `operator`
  can reach the FT8 sequencer instead of being rejected at config PUT/load.

Recommendation:

- Extend `Normalize` to trim and uppercase `LoggingStation.Operator` and
  `LoggingStation.OwnerCallsign`.
- Extend `validateLoggingStation` to apply the same non-empty callsign rule to
  all three callsign identity fields.
- Add PUT/load tests for invalid operator and owner callsign, plus a first-setup
  test proving provided values are canonicalized.

## Notes

- Load/default/migration structure is generally coherent: parse -> migrate raw
  document -> unmarshal -> defaults -> legacy folds -> normalize -> validate.
- The rig-profile projection helpers (`ActiveBridge`, `ActiveFt8`,
  `ResolveMyRig`) match the current ADR 0028 single-active-rig model and have
  useful focused coverage.
- Existing tests cover many migration and normalization cases, but not
  concurrent config reads/writes.

## Verification

Commands run:

- `GOCACHE=/tmp/go-build go test -count=1 ./internal/config ./internal/api ./internal/bridge ./internal/qsoservice`
- `GOCACHE=/tmp/go-build go test -race -count=1 ./internal/config ./internal/api ./internal/bridge ./internal/qsoservice`

The first sandboxed adjacent-package run failed because `httptest.NewServer`
could not bind localhost sockets (`operation not permitted`). The same focused
suite and the race suite passed when rerun with localhost binding allowed.
