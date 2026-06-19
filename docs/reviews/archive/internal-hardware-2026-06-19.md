# internal/hardware code review - 2026-06-19

Scope: fresh review of `internal/hardware` as a new codebase, plus the adjacent
`/v1/hardware` API handler, audio-device resolution paths, rig-profile config
contract, and endpoint documentation. Reviewed at `efcb6dff`.

Focus areas: correctness, performance, security, test coverage, and
documentation. This is a review artifact only; no production code was changed.

## Summary

`internal/hardware` is intentionally small and mostly side-effect free: serial
enumeration is read-only, audio enumeration only lists miniaudio devices, and the
API handler degrades to empty lists instead of failing the whole request. The
static `!cgo` path is also clean: `AudioDevices` returns the sentinel
`ErrAudioUnavailable` and `/v1/hardware` can still respond.

The main correctness risk is identity. Rig profiles now persist audio device
names, and runtime capture/playback resolves by the first matching name. That is
safe only when names are unique per direction. The package also has a few
resilience and coverage gaps around live OS enumeration.

## Findings

### M1 - Audio device names are treated as unique IDs, but duplicate names select the first matching device

The hardware endpoint exposes audio devices as `{name, is_default}` only
(`internal/hardware/hardware.go:47-54`), and the CGO enumerator fills `Name` from
`malgo.DeviceInfo.Name()` with no backend ID or duplicate marker
(`internal/hardware/audio_cgo.go:39-47`). Rig audio config stores only those names
(`internal/types/rig.go:88-106`), and the design docs explicitly say each
direction is resolved by matching the saved name against the live enumeration
(`docs/v2-design/config.md:458-475`).

The runtime match is first-hit. Capture scans `ctx.Devices(malgo.Capture)` and
breaks on the first `devices[i].Name() == DeviceName`
(`internal/audio/capture/capture.go:218-234`). Playback does the same for
`malgo.Playback` (`internal/audio/playback/playback.go:183-200`). If two attached
USB audio codecs expose the same miniaudio name, the operator cannot distinguish
them through `/v1/hardware`, and the daemon can bind capture or playback to the
wrong physical codec without an explicit error.

Impact: a multi-rig station with duplicate USB codec names can decode from or
transmit audio to the wrong rig. For FT8 TX this is especially confusing because
PTT can key the selected CAT rig while the audio stream is sent to the first
same-named playback device.

Recommendation: do not silently first-match duplicate names. At minimum, detect
duplicate names per direction in the audio layer and fail with a clear
`device name is ambiguous` error instead of picking the first. Prefer extending
`/v1/hardware` with a stable or semi-stable backend identifier/disambiguator when
malgo exposes one safely, plus a display label that can distinguish same-name
devices. The config UI should mark duplicates as ambiguous until the persisted
identifier model can represent them.

### M2 - A failure in one audio direction hides the other direction

`AudioDevices` enumerates capture first, then playback, and returns an error for
either failure (`internal/hardware/audio_cgo.go:27-36`). The API handler treats any
audio error as `audio.available=false` and leaves both capture and playback empty
(`internal/api/handler_hardware.go:64-73`).

That means a playback enumeration failure suppresses an otherwise valid capture
list, and a capture enumeration failure suppresses an otherwise valid playback
list. The rig-profile model is per-direction (`audio.rx` and `audio.tx`), and the
runtime audio paths fail soft per direction, so the discovery endpoint is stricter
than its consumers need it to be.

Impact: an operator can lose the ability to configure a working RX or TX audio
path because the opposite direction failed to enumerate. It also makes support
harder: the response says only `available=false`, not which direction failed.

Recommendation: split the enumeration result by direction. Either return
`capture_available` / `playback_available` with independent lists and errors in
logs, or keep the current wire shape but populate the successful direction and add
direction-specific diagnostics for the UI. Add tests that simulate capture-only
and playback-only failures.

### M3 - `/v1/hardware` performs uncached live OS/audio enumeration on every request

The endpoint is always registered (`internal/api/server.go:141-143`) and each
request synchronously calls `hardware.SerialPorts()` and `hardware.AudioDevices()`
(`internal/api/handler_hardware.go:58-73`). The audio path creates and frees a
fresh malgo context on every call (`internal/hardware/audio_cgo.go:18-25`) and
queries both directions (`internal/hardware/audio_cgo.go:27-34`). The general
non-SSE request limiter allows up to 128 concurrent requests by default
(`internal/config/defaults.go:45-55`, `internal/api/middleware.go:74-96`).

Live enumeration is useful for hot-plugging, but no cache, singleflight, timeout,
or per-route limit means repeated requests can create many audio contexts and
probe the host audio stack concurrently. If the daemon is deliberately bound
beyond loopback, this also becomes an unauthenticated hardware-probing endpoint;
the repo documents loopback as the trust boundary and warns on non-loopback binds
(`docs/v2-design/api-endpoints.md:15-17`, `internal/config/config.go:988-996`).

Impact: a buggy config SPA tab, browser retry loop, or reachable remote client can
tie up API workers and the host audio backend doing repeated enumeration. This is
not the hottest normal path, but it is an unnecessary performance and local-DoS
risk.

Recommendation: add a short TTL cache or singleflight around hardware
enumeration, with an explicit refresh path if the config UI needs immediate
hot-plug detection. Consider a lower per-route concurrency/rate cap for
`/v1/hardware`. Keep the non-loopback warning, but call out hardware inventory
exposure explicitly in the endpoint docs if this endpoint remains always-on.

### L1 - The graceful-degradation contract is not tested deterministically

The handler test verifies shape against the current host
(`internal/api/handler_hardware_test.go:10-59`), but it does not simulate serial
enumeration failure, `ErrAudioUnavailable`, generic audio failure, or partial
capture/playback failure. The package functions call `enumerator.GetDetailedPortsList`,
`malgo.InitContext`, and `ctx.Devices` directly (`internal/hardware/hardware.go:59-63`,
`internal/hardware/audio_cgo.go:18-34`), so tests have no seam to force those
branches.

Impact: the most important `/v1/hardware` API promise - always 200 with non-null
lists and correct audio availability - can regress without a focused test
catching it. The current tests also cannot cover M1 duplicate-name handling if it
is added later.

Recommendation: introduce a narrow internal enumerator interface or package-level
function variables used only by the handler/package tests. Add table tests for:
serial failure, static-build `ErrAudioUnavailable`, generic audio failure,
capture-only success, playback-only success, duplicate audio names, and empty but
successful lists.

### L2 - Serial labels fall back to bare paths even when other USB metadata is available

`SerialPort` carries VID, PID, serial number, and product metadata
(`internal/hardware/hardware.go:37-45`), but `serialLabel` uses only `Product` and
then falls back to the raw device path (`internal/hardware/hardware.go:83-90`).
The design says the endpoint should provide friendly labels so the operator does
not hand-type device paths (`docs/v2-design/frontend-spa.md:171-179`), while the
API docs acknowledge that labels may fall back to the bare path
(`docs/v2-design/api-endpoints.md:171-176`).

Impact: on hosts where udev does not populate `Product`, the picker can show
only `/dev/ttyUSB0` even though the response already includes metadata that could
help distinguish adapters. That weakens the KISS goal and makes the future config
SPA responsible for reconstructing labels that the daemon could provide once.

Recommendation: extend `serialLabel` to use the best available metadata:
product, then VID/PID, then serial number, always including the path for
disambiguation. If the API intentionally leaves label construction to the SPA,
document `label` as a fallback display string rather than the authoritative
friendly label.

## Security notes

I did not find a new hardware-write security bug in `internal/hardware`; it does
not key PTT, open serial ports, or open audio devices for streaming. The residual
security concern is information disclosure: `/v1/hardware` exposes serial paths,
USB IDs/serials, and audio device names. That is consistent with the repo's
documented no-auth, loopback-default model, but it should remain part of the
non-loopback bind threat model.

## Test coverage notes

Current coverage is thin but useful: `internal/hardware` verifies serial label
construction and host enumeration shape; `internal/api` verifies the endpoint's
basic 200/JSON/non-null response shape. Missing coverage is mostly around
forced-failure branches and ambiguous device identity.

## Verification

Commands run:

- `GOCACHE=/tmp/go-build go test ./internal/hardware`
- `CGO_ENABLED=0 GOCACHE=/tmp/go-build go test ./internal/hardware`
- `GOCACHE=/tmp/go-build go vet ./internal/hardware ./internal/api`
- `GOCACHE=/tmp/go-build go test ./internal/api`
- `GOCACHE=/tmp/go-build go test -race ./internal/hardware ./internal/api`
- `CGO_ENABLED=0 GOCACHE=/tmp/go-build go test ./internal/api`

The first sandboxed race/static API runs failed only because an unrelated
`httptest.NewServer` enrichment test could not bind localhost
(`socket: operation not permitted`). The same focused commands passed when rerun
with localhost binding allowed.

## Resolution (2026-06-19)

Operator scoped this to the real today-risks: **M1 + L2 fixed now; M2 + M3
deferred** to the config-SPA workstream (they shape/harden `/v1/hardware`, whose
consumer isn't built yet) — logged in `docs/backlog.md`.

- **M1 (fixed — safety).** New shared `audio.MatchDeviceName(names, want, dir)`
  (CGO-free, pure, in `internal/audio`) resolves a configured device name to its
  index and returns an **ambiguity error** when two devices share the name,
  instead of silently first-matching. `capture.Start` and `playback.Play` now use
  it (replacing their first-hit loops), so a duplicate USB-codec name fails soft
  (that direction idle + logged) rather than binding the wrong physical codec —
  the multi-rig wrong-codec / wrong-PTT-vs-audio risk. The durable per-device
  identifier (so duplicates can be *distinguished* rather than just rejected)
  stays a config-SPA item. Test: `TestMatchDeviceName`.
- **L2 (fixed).** `serialLabel` now uses the best available metadata —
  product → `VID:PID` (+ serial number) → serial number → bare path — always
  appending the device path for disambiguation, so a host where udev didn't
  populate `Product` still gets a useful label. Test cases added to
  `TestSerialLabel`.
- **M2 + M3 (deferred).** Per-direction audio availability (M2) and enumeration
  caching / per-route cap (M3) harden `/v1/hardware` for the future config SPA;
  shaping the wire change + cache policy belongs with that workstream. Backlogged.
- **L1 (partially addressed).** The new `MatchDeviceName` + `serialLabel` are pure
  and directly unit-tested; the broader handler failure-branch seam (forced
  serial/audio failures) rides with the deferred M2/M3 handler work.

Verified: `gofmt`/`go vet` clean; CGO build of `internal/audio/...` +
`internal/hardware`; CGO-free `go build ./...`; CGO `go test` of audio (incl.
capture/playback), hardware, and `internal/api` pass; `go test -race
./internal/audio/...` clean.
