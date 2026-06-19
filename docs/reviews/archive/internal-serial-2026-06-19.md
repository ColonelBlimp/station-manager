# internal/serial code review - 2026-06-19

## Scope

Fresh review of `internal/serial` at `f77d6bd3`, approached as a new package
review. I read the package implementation, tests, direct production consumers,
diagnostic consumers, rig-definition serial contracts, relevant docs, and the
local `go.bug.st/serial v1.6.4` dependency source used by this module.

Focus areas: correctness, performance, security, test coverage, and
documentation. This is review-only; no source fixes were made.

Primary files reviewed:

- `internal/serial/serial.go`
- `internal/serial/config.go`
- `internal/serial/doc.go`
- `internal/serial/transport.go`
- `internal/serial/errors.go`
- `internal/serial/buffer_pool.go`
- tests under `internal/serial`
- `internal/bridge/pipeline.go`
- `internal/bridge/service.go`
- `internal/bridge/command.go`
- `cmd/catcli/main.go`
- `cmd/civ-probe/main.go`
- `internal/cat/rig.go`
- `internal/cat/rigs/icom-ic7300.json`
- `internal/types/rig.go`
- `internal/config/config.go`
- `docs/install.md`
- `docs/decisions/0034-civ-codec-protocol-seam.md`
- local dependency source under `go.bug.st/serial@v1.6.4`

## Summary

The core package is compact and generally well engineered. It keeps
`internal/serial` at the byte-transport layer, serializes concurrent writes,
centralizes reads through one background reader, copies framed responses before
delivery, bounds oversized frames, preserves caller slices on delimiter append,
and has a write watchdog for stuck OS writes. The direct test suite is broad,
race-clean, and currently reports 86.2% statement coverage.

The highest risks sit at the hardware-safety and integration boundaries. The
package comments currently promise that DTR/RTS can be de-asserted at open
without any assert-then-clear window, but the underlying dependency explicitly
documents that Unix-like systems cannot provide that guarantee. The bridge
steady state still benefits from `InitialStatusBits=false`, but an IC-7300 with
USB SEND mapped to RTS or DTR can still see an unintended PTT pulse on open.
The diagnostic `cmd/catcli -rig icom-ic7300` path is worse: it drops the rigdef
RTS/DTR fields entirely and mis-parses the `0xFD` delimiter, so the manual live
test path can both assert the control lines and frame CI-V incorrectly.

## Findings

### H1. Unix opens can still pulse DTR/RTS despite the package's no-window contract

**Area:** correctness / hardware safety / security / documentation  
**Files:** `internal/serial/serial.go:148-163`,
`internal/serial/config.go:53-61`,
`internal/cat/rigs/icom-ic7300.json:10-20`,
`docs/install.md:124-127`,
`docs/decisions/0034-civ-codec-protocol-seam.md:286-297`,
`docs/decisions/0034-civ-codec-protocol-seam.md:546-548`,
`go.bug.st/serial@v1.6.4/serial.go:71-75`,
`go.bug.st/serial@v1.6.4/serial_unix.go:249-268`

`Open` sets `serial.Mode.InitialStatusBits` when `Config.RTS` or `Config.DTR`
is non-nil (`serial.go:154-163`). The comments around that code say this sets
the line state "AT OPEN" and avoids an assert-then-clear window
(`serial.go:148-153`). `Config` repeats the same stronger guarantee: a non-nil
false is "Applied via serial.Mode.InitialStatusBits so the line is never
asserted even momentarily" (`config.go:53-61`).

That guarantee does not hold on the target Unix-like deployments. The local
`go.bug.st/serial v1.6.4` dependency documents that Linux, macOS, and other
Unix systems cannot set modem output bits before opening the port; even a false
initial bit can go true for a few milliseconds (`serial.go:71-75`). The Unix
implementation confirms the ordering: after the port is opened and termios is
configured, it reads modem status and then clears or sets DTR/RTS
(`serial_unix.go:249-268`).

For the bridge daemon, the IC-7300 rigdef correctly carries `rts:false` and
`dtr:false` (`icom-ic7300.json:17-20`), so the steady state after open should be
de-asserted. The unsafe part is the open-time pulse. ADR 0034 records the real
bench consequence: the IC-7300's USB SEND function can map PTT to RTS or DTR,
and merely opening the port keyed the rig during probing
(`0034-civ-codec-protocol-seam.md:286-297`). `docs/install.md` lists the
IC-7300 as a tested CAT target but does not list the required IC-7300 menu
settings, including `USB SEND = OFF` (`install.md:124-127`). The ADR still marks
that install-doc work as future (`0034-civ-codec-protocol-seam.md:546-548`).

The result is a hardware-safety bug in the documented operational contract. A
user can follow the current install docs, connect an IC-7300, start the daemon
on Linux, and still get an unintended transmit pulse if USB SEND is mapped to a
control line. This is not a credential/security exposure, but it is an
unauthorized RF-transmit hazard and should be treated with comparable care.

**Recommendation:** remove the no-assert-window claim from `internal/serial`
comments and document the true platform behavior. Promote the IC-7300
prerequisites into `docs/install.md`: CI-V Transceive ON, CI-V USB Port linked
to REMOTE, baud matching CI-V Baud Rate, and USB SEND OFF. Consider a prominent
startup warning or refusal for Unix deployments when a rigdef requests
`RTS=false` or `DTR=false` until the operator has acknowledged the hardware
prerequisite. If a true no-pulse guarantee is required, this wrapper cannot
provide it with the current dependency on Unix; that would require an
OS/device-specific lower-level open strategy or a hardware/radio setting that
removes RTS/DTR from the PTT path.

### H2. `cmd/catcli -rig icom-ic7300` drops CI-V serial safety and framing settings

**Area:** correctness / hardware safety / diagnostic tooling / test coverage  
**Files:** `cmd/catcli/main.go:57-80`,
`cmd/catcli/main.go:213-248`,
`internal/serial/serial.go:230-234`,
`internal/cat/rigs/icom-ic7300.json:17-20`,
`internal/bridge/pipeline.go:1007-1035`,
`internal/bridge/pipeline_test.go:1058-1087`,
`docs/v2-design/cat-serial-reuse.md:274-285`

The bridge path and the diagnostic CLI now disagree on how to compose a
`cat.RigSerial` into `serial.Config`.

The bridge helper handles both CI-V-specific fields:

- it preserves the rigdef's `RTS` and `DTR` pointers in the returned
  `serial.Config` (`pipeline.go:886-896`);
- it parses hex byte delimiters such as `0xFD` (`pipeline.go:1007-1035`), with
  tests pinning the IC-7300 path (`pipeline_test.go:1058-1087`).

`cmd/catcli` reimplements that composition in `serialConfigFromRig` and misses
both details. It sets `LineDelimiter` with `firstByteOrZero(rs.LineDelimiter)`,
so the IC-7300 rigdef value `"0xFD"` becomes ASCII `'0'`, not byte `0xFD`
(`main.go:213-248`, `icom-ic7300.json:17`). It also never copies `rs.RTS` or
`rs.DTR` into `serial.Config` (`main.go:219-240`).

This has two concrete effects:

1. Opening `catcli -rig icom-ic7300` passes nil `RTS`/`DTR` to `serial.Open`,
   which leaves `InitialStatusBits` nil. `go.bug.st/serial` then uses its
   default asserted DTR/RTS state. On an IC-7300 with USB SEND mapped to a
   control line, the diagnostic command can hold PTT asserted, not just pulse it.
2. CI-V commands encoded by `cat.Encode` already end in `0xFD`, but
   `WriteCommandBytes` appends the configured delimiter whenever the final byte
   differs (`serial.go:230-234`). With the mistaken delimiter `'0'`, `catcli`
   appends byte `0x30` to CI-V frames and reads responses split on the wrong
   byte.

That makes the documented live-verification path in
`docs/v2-design/cat-serial-reuse.md:274-285` unsafe and unreliable for the first
binary CAT rig.

**Recommendation:** stop duplicating serial rig composition in `cmd/catcli`.
Extract the bridge parser/composer into a shared package, or make `catcli` call
a shared helper that preserves `RTS`/`DTR` and supports `0xNN` delimiters. Add a
`cmd/catcli` regression for `icom-ic7300` that asserts `LineDelimiter == 0xFD`
and both modem-output pointers are present and false. Also assert that a CI-V
command ending in `0xFD` is not given an extra delimiter byte by the CLI config.

### M1. Recoverable oversized-line warnings can hide a later terminal reader error

**Area:** correctness / API contract / observability  
**Files:** `internal/serial/serial.go:80-96`,
`internal/serial/serial.go:124-126`,
`internal/serial/serial.go:357-374`,
`internal/serial/serial.go:442-447`,
`internal/serial/serial.go:463-471`,
`internal/serial/doc.go:31-48`,
`internal/serial/serial_test.go:354-428`,
`internal/serial/serial_test.go:780-828`,
`internal/serial/serial_test.go:1075-1106`

The public `Errors()` contract says the channel yields at most one terminal
reader-loop error and is then closed (`serial.go:80-96`, `serial.go:357-374`,
`doc.go:39-48`). The implementation also sends recoverable oversized-line
notifications on the same one-slot `errCh` while the reader loop continues
(`serial.go:463-471`).

That conflation creates two bad behaviors:

- If an oversized-line warning fills `errCh` and the caller does not drain it
  immediately, a later non-timeout read error takes the best-effort send path
  and can be dropped (`serial.go:442-447`).
- If the caller does drain the warning immediately and follows the documented
  supervisor pattern, it can treat a recoverable oversized line as a terminal
  reconnect signal even though the reader keeps running.

The current tests cover oversized-line warning and recovery, and they cover
terminal read errors separately (`serial_test.go:354-428`,
`serial_test.go:780-828`, `serial_test.go:1075-1106`). They do not cover the
combined sequence where an oversized warning occupies `errCh` before a terminal
read error arrives. The current bridge consumer does not rely on `Errors()` for
normal serial teardown, so this is mostly a package API and diagnostic-consumer
risk today.

**Recommendation:** separate recoverable reader diagnostics from terminal
reader errors. Options include logging oversized-line drops at the caller's
logging layer, exposing a distinct warning/metrics hook, or changing
`Errors()` to be explicitly documented as a mixed warning/error stream with
buffering semantics that cannot hide terminal errors. Add a regression that
injects an oversized line followed by a non-timeout read error without draining
`Errors()` between them, and assert the chosen terminal-error contract.

### M2. Invalid numeric serial overrides are classified as transient open failures

**Area:** correctness / operability / configuration validation  
**Files:** `internal/types/rig.go:108-120`,
`internal/config/config.go:316-338`,
`internal/bridge/pipeline.go:176-185`,
`internal/bridge/pipeline.go:225-237`,
`internal/bridge/pipeline.go:844-896`,
`internal/serial/config.go:66-85`,
`go.bug.st/serial@v1.6.4/serial.go:97-103`

The bridge already treats malformed parity, stop bits, and delimiters as
permanent configuration errors because `buildSerialConfig` parses those values
before opening the port (`pipeline.go:176-185`, `pipeline.go:874-885`). Numeric
serial fields do not get the same treatment.

Per-rig overrides are projected from config into `BridgeSerialConfig`
(`config.go:316-338`) and overlaid in `buildSerialConfig` without validating
the resulting baud rate or data bits (`pipeline.go:844-896`). `serial.Open`
does reject non-positive baud rates (`config.go:66-85`), and the dependency
documents that data bits must be 5, 6, 7, or 8 (`serial.go:97-103`). But any
error from `s.openClient(serialCfg)` is published as
`BridgeErrCodeSerialOpenFailed` and returned as `exitTransient`
(`pipeline.go:225-237`), with comments saying the port may appear later.

That means an operator typo such as `baud_rate:-1` or `data_bits:9` can be
reported and retried as if it were a missing USB device. The supervisor will
keep backing off and retrying even though the only fix is changing config. This
also weakens documentation for the per-rig override schema, because the current
type comments describe the composition step but not validation ownership
(`types/rig.go:108-120`).

**Recommendation:** validate numeric serial fields before `openClient` and
classify failures as `BridgeErrCodeSerialConfigInvalid` / `exitPermanent`.
Either move all runtime serial validation into `buildSerialConfig`, expose a
small validation helper from `internal/serial`, or make the bridge distinguish
`serial.validateConfig` failures from true OS open failures. Add config or
bridge regressions for negative baud and unsupported data bits in per-rig
overrides, and assert no transient retry loop is started.

## Security Review

The main security-relevant issue is hardware safety: unintended transmitter
keying via DTR/RTS. `internal/serial` itself does not handle secrets or remote
network input. It reads from a local serial device selected by operator config,
frames bytes by a delimiter, and caps buffered line length at 4096 bytes
(`serial.go:19-21`). Oversized frames are discarded, which protects the daemon
from unbounded memory growth on a noisy or hostile serial line.

The write watchdog is a useful safety control for the hardware-facing paths:
when configured, a stuck OS write closes the port and returns `ErrWriteTimeout`
instead of wedging the bridge's serialized write path indefinitely
(`serial.go:245-268`). The H1/H2 DTR/RTS issues should be fixed before relying
on IC-7300 serial-open behavior as an RF-safe invariant.

## Performance Review

No hot-path performance defect was found in the core package.

The reader loop uses a pooled read buffer, scans chunks with `bytes.IndexByte`,
copies only completed framed responses before publishing them, and caps
individual line accumulation at 4096 bytes (`serial.go:409-499`). The response
channel is bounded at 64 entries (`serial.go:14-21`, `serial.go:190-199`), so a
slow consumer applies backpressure rather than allowing unbounded queue growth.

The watchdog path starts one goroutine per write only when `WriteTimeoutMS > 0`
(`serial.go:245-268`). That is acceptable for current CAT traffic, which is low
rate and hardware-latency dominated. If future code uses this package for
high-rate binary streaming, the per-write goroutine and per-response copy should
be revisited, but they are the right tradeoff for rig control.

## Test Coverage Notes

Strong coverage observed:

- `internal/serial` covers config defaults and failures, concurrent writes,
  partial writes, zero-byte writes, context cancellation, close behavior,
  delimiter append/preserve behavior, binary byte responses, oversized-line
  discard and recovery, timeout read recovery, terminal read errors, response
  channel close behavior, caller-slice immutability, and the write watchdog.
- `internal/bridge` covers the IC-7300 serial config path and the `0xNN`
  delimiter parser.
- Race testing for `internal/serial` and `internal/bridge` passes on the current
  tree.

Coverage gaps to close:

- No test proves the `Errors()` behavior when an oversized-line warning is
  followed by a terminal read error before the warning is drained.
- No `cmd/catcli` tests cover rigdef serial composition, so the IC-7300
  `RTS`/`DTR` and `0xFD` regression was not caught.
- No bridge/config test covers invalid numeric per-rig serial overrides being
  treated as permanent config errors.
- Unit tests cannot prove OS-level DTR/RTS pulse behavior. That needs docs,
  runtime warnings/acknowledgement, and hardware validation on supported
  platforms.

## Documentation Notes

- `internal/serial` comments currently overstate `InitialStatusBits` on Unix.
  H1 should be fixed in code comments, package docs, and install docs together.
- `docs/install.md` should include the IC-7300 operator prerequisites already
  recorded in the rigdef and ADR 0034.
- `internal/types/rig.go:110-113` describes `LineDelimiter` as a
  single-character string, but the supported IC-7300 rigdef uses the documented
  bridge hex form `"0xFD"`. That comment should be widened to "single byte or
  0xNN hex form".
- `cmd/catcli` still says the inline serial composition is the future
  `internal/rigconfig` shape (`main.go:213-217`), while the bridge now has the
  real production composition logic. A shared helper would remove both code and
  documentation drift.

## Verification

Commands run on the current tree:

```text
GOCACHE=/tmp/go-build go test ./internal/serial
GOCACHE=/tmp/go-build go test -race ./internal/serial
GOCACHE=/tmp/go-build go test -cover ./internal/serial
GOCACHE=/tmp/go-build go vet ./internal/serial ./internal/bridge ./cmd/catcli
GOCACHE=/tmp/go-build go test ./cmd/catcli ./internal/cat
GOCACHE=/tmp/go-build go test ./internal/bridge
GOCACHE=/tmp/go-build go test -race ./internal/bridge
```

Results:

- `internal/serial`: pass
- `internal/serial` race: pass
- `internal/serial` coverage: 86.2% of statements
- vet for `internal/serial`, `internal/bridge`, and `cmd/catcli`: pass
- `cmd/catcli`: pass, no test files
- `internal/cat`: pass
- `internal/bridge`: pass after rerunning outside the sandbox so
  `httptest.NewServer` could bind localhost
- `internal/bridge` race: pass outside the sandbox

## Resolution (2026-06-19)

All four findings fixed. Operator decisions: H2 → a small shared package
(`internal/rigserial`); H1 → a one-time runtime WARN at open (plus the docs).

- **H1 (fixed — hardware safety, code can't guarantee no-pulse on Unix).**
  Removed the false "never asserted even momentarily / no assert-then-clear
  window" claims from `serial.Open`'s comment and `serial.Config.RTS/DTR` doc,
  replacing them with the true platform behaviour (Unix can't set modem bits
  before open). New `rigserial.OpenMayPulseLines` flags a Unix open that
  de-asserts RTS/DTR; the bridge and catcli log a one-time WARN at connect
  naming the rig-side fix. `docs/install.md` now carries the IC-7300
  prerequisites — **USB SEND = OFF**, CI-V Transceive ON, CI-V USB Port → REMOTE,
  baud = CI-V Baud Rate. (ADR 0034 is append-only history; the install-doc work
  it deferred is now done in install.md.)
- **H2 (fixed).** New `internal/rigserial` package owns the single
  `cat.RigSerial → serial.Config` composition (RTS/DTR carry-through + parity /
  stop-bits / `0xNN` delimiter parsing). The bridge's `buildSerialConfig` overlays
  its per-rig overrides then calls `rigserial.Compose`; `cmd/catcli` calls it
  directly (fixing the dropped RTS/DTR — which had let go.bug.st default to
  *asserted*, holding PTT — and the `0xFD`→`'0'` mis-parse). The bridge's
  duplicate parsers were removed; the delimiter round-trip test moved to
  rigserial. catcli's manual `-delim` now also accepts the `0xNN` form. Tests:
  `rigserial.TestCompose_IcomCIV`, `TestDelimiterFromString`.
- **M1 (fixed).** The recoverable oversized-line drop no longer rides the
  one-slot terminal `errCh` (where it could fill the slot and hide a later
  terminal read error). It's counted in a new `Port.DroppedLines()` atomic
  instead, so `Errors()` matches its documented "at most one terminal error"
  contract. Test: `TestOversizedLineDoesNotHideTerminalError` (oversized then a
  terminal read error → the terminal error is still delivered) +
  `TestOversizedLineCountedAndDropped`; three existing oversized tests updated to
  assert via `DroppedLines()`.
- **M2 (fixed).** `rigserial.Compose` validates numeric fields (baud > 0,
  data bits ∈ {5,6,7,8}) as it composes — and the bridge calls it from
  `buildSerialConfig`, whose errors are already classified `exitPermanent`. So a
  bad per-rig override (`baud_rate:-1`, `data_bits:9`) is now a permanent config
  error, not a transient open failure the supervisor retries forever. Tests:
  `rigserial.TestCompose_RejectsBadNumeric`,
  `bridge.TestBuildSerialConfig_RejectsInvalidOverride`.

Also updated: `types.RigOverrides` comment (LineDelimiter is a single byte OR
`0xNN`; composition is `internal/rigserial`, which validates). `cmd/civ-probe`
needs no change — it opens via the raw go.bug.st API with its own post-open
DTR/RTS de-assert backstop, not the rigserial path.

Verified: `gofmt`/`go vet` clean; `internal/serial`, `internal/rigserial`,
`internal/bridge`, `internal/cat`, `cmd/catcli` build + pass; `-race` on serial /
rigserial / bridge clean; CGO-free `go build ./...` + CGO `cmd/...` build.

