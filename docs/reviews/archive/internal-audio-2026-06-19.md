# `internal/audio` - code review (2026-06-19)

## Scope

Review-only pass over `internal/audio` as a fresh package: WAV file I/O, complex
FFT and real FFT primitives, the CGO-backed `capture` and `playback`
subpackages, package tests, build-tag behavior, and adjacent FT8 contracts that
consume live capture, transmit playback, and occupancy spectra.

No production code changes were applied during this review. This document is the
only artifact from the pass.

Headline counts: **0 Critical**, **0 High**, **1 Medium**, **3 Low**.

## Medium findings

### M1. `ReadWAV` accepts truncated `data` chunks as valid audio

**Files:**

- `internal/audio/wav.go:71`
- `internal/audio/wav.go:99`
- `internal/audio/wav.go:174`
- `internal/audio/wav.go:175`
- `internal/audio/wav.go:179`
- `internal/audio/wav.go:199`
- `internal/audio/wav_test.go:531`
- `internal/audio/wav_test.go:543`

`ReadWAV` deliberately ignores the RIFF file-size field and bounds the data read
with `io.LimitReader`, which is the right OOM defense for hostile chunk sizes.
However, after reading the first `data` chunk it does not verify that the file
actually supplied `chunkSize` bytes:

```go
pcmData, readErr = io.ReadAll(io.LimitReader(f, int64(chunkSize)))
```

`io.ReadAll` returns `nil` when the underlying file reaches EOF before the
limit. If a PCM16 chunk declares four bytes but only contains two, the current
code converts one valid sample and returns success. PCM8 truncation is even
easier to accept because every byte is sample-aligned. Only some truncations are
caught later as sample-alignment errors.

Impact:

- Corrupt or partially copied WAV fixtures can be treated as shorter valid
  recordings, causing misleading decode or occupancy results instead of a clear
  `ErrWAVInvalidHeader`.
- The package documentation says malformed RIFF/WAVE chunk structure returns
  `ErrWAVInvalidHeader`; a short data chunk is malformed structure.
- The existing malformed-file regressions cover truncated `fmt` chunks and
  `data` before `fmt`, but not truncated `data` payloads.

Suggested fix:

- After the bounded read, require `uint64(len(pcmData)) == uint64(chunkSize)`.
- Return `ErrWAVInvalidHeader` on a short payload, preserving the package's
  sentinel-error contract.
- Add tests for at least PCM16 declared-size-four/actual-size-two and PCM8
  declared-size-three/actual-size-two.

## Low findings

### L1. `playback.Player.Close` is not terminal even though the API exposes `ErrClosed`

**Files:**

- `internal/audio/playback/playback.go:30`
- `internal/audio/playback/playback.go:34`
- `internal/audio/playback/playback.go:98`
- `internal/audio/playback/playback.go:106`
- `internal/audio/playback/playback.go:275`
- `internal/audio/playback/playback.go:286`
- `internal/audio/playback/doc.go:21`
- `internal/audio/playback/doc.go:23`
- `internal/audio/capture/capture.go:144`
- `internal/audio/capture/capture.go:153`
- `internal/audio/capture/capture_test.go:100`

The playback package declares `ErrClosed` and its package documentation says
terminal-state calls return sentinel errors mirroring `internal/audio/capture`.
The implementation never sets or checks a closed state. `Close` stops playback,
uninitializes the malgo context, sets `p.ctx = nil`, and returns. A later
`Init` succeeds because it only checks whether `p.ctx` is already non-nil.

Current FT8 callers reduce the blast radius: arming creates a new player,
disarming closes it, and the transmit controller calls `Stop` after natural
completion or cancellation. Still, the package-level lifecycle contract is
inconsistent and the dead `ErrClosed` sentinel can mislead future callers.

Suggested fix:

- If playback should match capture, add a `closed atomic.Bool`, set it in
  `Close`, and return `ErrClosed` from `Init`, `ListDevices`, and `Play` after
  closure.
- If reusable-after-close is intentional, remove `ErrClosed` and update
  `doc.go` so playback's lifecycle is explicitly different from capture.
- Add a non-integration regression either way.

### L2. Playback lifecycle behavior has almost no non-integration coverage

**Files:**

- `internal/audio/playback/buffer_test.go:9`
- `internal/audio/playback/buffer_test.go:69`
- `internal/audio/playback/playback.go:61`
- `internal/audio/playback/playback.go:98`
- `internal/audio/playback/playback.go:118`
- `internal/audio/playback/playback.go:145`
- `internal/audio/playback/playback.go:256`
- `internal/audio/playback/playback_integration_test.go:16`
- `internal/audio/capture/capture_test.go:24`
- `internal/audio/capture/capture_test.go:93`

`internal/audio/playback` has good pure tests for `fillFrame` and
`bytesAsInt16`, but the lifecycle surface is mostly covered only by
`//go:build integration` tests that require a real output device. In contrast,
capture has non-hardware tests for defaults, construction, sentinel strings,
idempotent `Init`, `Init` after `Close`, not-initialized errors, already-running
state, close concurrency, and callback/channel safety.

That gap is why the `ErrClosed` mismatch above is not pinned by the normal
focused package suite or the race pass.

Suggested fix:

- Add non-integration playback tests for `DefaultConfig`, `New`, sentinel
  strings, `ListDevices` before `Init`, `Play` before `Init`,
  `ErrAlreadyPlaying` via the atomic flag, `Stop` while idle, `Close`
  idempotence, and the chosen `Init` after `Close` contract.
- Keep real-device start/stop/audio-drain assertions in the integration tests.

### L3. Several package comments describe older tree states

**Files:**

- `internal/audio/wav.go:1`
- `internal/audio/wav.go:7`
- `internal/audio/wav.go:9`
- `internal/audio/playback/doc.go:3`
- `internal/audio/playback/doc.go:17`
- `internal/audio/playback/doc.go:18`
- `internal/audio/itoa.go:3`
- `internal/audio/itoa.go:5`
- `internal/audio/fft.go:47`
- `internal/audio/realfft.go:22`
- `internal/audio/realfft.go:47`

The code and tests are ahead of a few comments:

- The root package comment says the old `audio/capture` CGO subpackage went
  away with the FT8 extraction, but `internal/audio/capture` and
  `internal/audio/playback` are present and used by the FT8 subsystem.
- `playback/doc.go` still says PTT and guaranteed-stop sequencing "arrive in
  step (d)", while the current FT8 service already owns key/unkey and stop
  sequencing around playback.
- `itoa.go` says the formatter is shared with `fft_cgo.go`, but no CGO FFT file
  exists in this package.
- FFT comments still point to `dsp.Spectrogram`; the current in-tree hot caller
  is FT8 occupancy's `audio.NewRealPlan(3840)` path.

Impact is documentation drift rather than runtime behavior, but these comments
sit on lifecycle and signal-chain code where future changes need accurate
context.

Suggested fix:

- Refresh the root package, playback, FFT, RealPlan, and `itoa` comments to
  describe the current capture/playback/FT8 occupancy shape.

## Positive notes

- The prior Start/Close lifecycle race class is not present in the current
  capture/playback code: both `capture.Start` and `playback.Play` hold their
  lifecycle mutex across `InitDevice` and `device.Start`.
- WAV parsing now has focused regressions for odd-sized `fmt` chunk padding,
  truncated `fmt` sentinel preservation, and `data` before `fmt`.
- Capture's normal test suite covers closed-channel sends, concurrent close
  stress, and `Init` after `Close`; the race pass was clean.
- The CGO boundary is cleanly isolated. `CGO_ENABLED=0 go test
  ./internal/audio/...` still exercises the pure WAV/FFT code and the pure
  playback helpers without requiring live audio.
- The FT8 transmit caller waits for playback completion, calls `Stop` after the
  device-buffer tail, and calls `Stop` on cancellation before returning through
  the deferred unkey path.

## Verification

Commands run:

```text
go test ./internal/audio/... -count=1
go vet ./internal/audio/...
CGO_ENABLED=0 go test ./internal/audio/... -count=1
go test -race ./internal/audio/... -count=1
go test ./internal/ft8 -count=1
```

All commands passed. The first sandboxed `internal/ft8` run failed before
testing product code because the sandbox blocks `httptest` localhost listeners
with `listen tcp6 [::1]:0: socket: operation not permitted`; the same command
passed when rerun outside that sandbox.

Hardware-backed `//go:build integration` tests for real capture and playback
devices were not run.

## Resolution (2026-06-19)

All four findings addressed.

- **M1 (fixed).** `ReadWAV` now requires the `data` chunk to actually supply
  `chunkSize` bytes after the bounded read; a short payload (file EOF before the
  declared size) returns `ErrWAVInvalidHeader` instead of silently decoding a
  shorter recording. The `io.LimitReader` stays as the oversized-declared-size
  OOM defense. Tests: `TestReadWAV_TruncatedData_IsInvalidHeader` (PCM16
  declared-4/actual-2 and PCM8 declared-3/actual-2).
- **L1 (fixed — playback made terminal, matching capture).** Chose
  consistency with `capture` over reusable-after-close, since the `ErrClosed`
  sentinel and `doc.go` already promised terminal semantics and FT8 arms a fresh
  `Player` per session. Added a `closed atomic.Bool` set under lock in `Close`;
  `Init` / `ListDevices` / `Play` now return `ErrClosed` after close.
- **L2 (fixed).** New `playback_lifecycle_test.go` (`//go:build cgo`, not
  `integration`) pins `DefaultConfig`, nil-logger→noop, sentinel strings,
  `ListDevices`/`Play` before `Init`, `Stop` while idle, `ErrAlreadyPlaying` via
  the atomic flag, and `Close` idempotence + the terminal `ErrClosed` contract —
  all without a real device, so they run in the normal CGO-on suite and the race
  pass.
- **L3 (fixed).** Refreshed the stale comments: the root `audio` package doc now
  describes the re-added capture/playback CGO subpackages; `itoa.go` no longer
  references a non-existent `fft_cgo.go`; `fft.go` / `realfft.go` point at the
  FT8 occupancy detector's `NewRealPlan(3840)` path instead of the removed
  `dsp.Spectrogram`; and `playback/doc.go` states PTT + guaranteed-stop are
  owned above this layer (ADR 0030, shipped) rather than "arriving in step (d)".

Verified: `gofmt`/`go vet` clean; `go test ./internal/audio/...` passes CGO-on
and CGO-off; `go test -race ./internal/audio/...` clean; the FT8 consumer
(`go test ./internal/ft8`) passes.
