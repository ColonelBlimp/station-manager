---
number: 0024
title: FT8 via the external go-ft8 library, an in-tree wrapper, and a CGO-gated live pipeline
status: Accepted
date: 2026-05-31
---

# 0024 — FT8 via the external go-ft8 library, an in-tree wrapper, and a CGO-gated live pipeline

## Context

ADR 0021 decided FT8 would ship as an in-process subsystem with the decoder
*implemented in-tree*, clean-room from the QEX 2020 specification so Station
Manager could stay MIT. That clean-room decoder never reached jt9 parity and
hit a Go-runtime decode-depth wall (see memory `project_sm_ft8_language_question`);
FT8 was removed from the tree on 2026-05-30 (tag `ft8-snapshot-2026-05-30`) and
continued out-of-tree.

The out-of-tree result is **go-ft8** (`github.com/ColonelBlimp/go-ft8`, first
tagged `v0.1.0`), an explicit WSJT-X/jt9-derivative — therefore GPL-3.0-only. SM
relicensed MIT → GPL-3.0-only (ADR 0023) to link it. With the decoder now an
external GPL library, two questions remained: how SM consumes it, and how live
receive audio reaches it. This ADR records both, plus the build-time consequence
(CGO) and how it's contained.

## Decision

SM consumes FT8 decode by **linking the external go-ft8 library** — the
"separate Go library, imported as a go.mod dependency" option ADR 0021 weighed
and rejected, now chosen. `internal/ft8` is a thin wrapper: `DecodeSlot` (the
per-slot decode + structured logging, fail-soft) plus a live pipeline — int16
`sampleRing` → UTC-aligned slot `Scheduler` → `Service` → safego decode worker.

Live audio capture is **miniaudio via `gen2brain/malgo` (CGO)**, gated behind
`//go:build cgo` with a `!cgo` stub. The project ships **two build flavours**:

- **Static, CGO-free default** — offline/file FT8 decode + everything else.
- **CGO "live" build** — malgo capture (and, for free, the pocketfft FFT backend).

## Alternatives considered

### In-tree clean-room decoder (ADR 0021's decision)

Rejected: it never reached jt9 parity after months of work and hit a runtime
wall. The shipping decoder is the external WSJT-X-derived go-ft8 instead.

### Capture B — pure-Go PulseAudio/PipeWire client

A pure-Go client (e.g. `jfreymuth/pulse`) would keep the live build CGO-free.
Rejected: it assumes a PulseAudio/PipeWire server is present and running.
That's an assumption about the host, and "assumption is the mother of disaster."

### Capture C — exec an external recorder (`pw-record`/`parecord`)

Read 12 kHz mono PCM off a subprocess's stdout — zero CGO, zero Go deps.
Rejected for the same reason as B, one level worse: it assumes a specific binary
is installed on the host.

### Capture A — miniaudio via malgo (chosen)

miniaudio is a backend *abstraction*: it probes PipeWire/PulseAudio/JACK and
falls back to raw ALSA (universal on Linux), adapting at runtime rather than
pinning one audio system. It therefore makes the **fewest** host assumptions of
the three — the opposite of the risk B and C carry. The cost is CGO, which we
were likely to take on anyway for decode performance (pocketfft), and which is
contained behind a build tag.

### Make CGO the default build

Rejected: the static, CGO-free binary is a real operational asset (no glibc
version coupling, no shared-lib RPM deps, cross-platform by default, gcc-less
contributors and the CI fast lane keep working). Keep it the default; make live
FT8 the CGO build. If live capture needs CGO, the live build is a CGO build
regardless — at which point pocketfft's ~2× decode speedup is free in it.

## Consequences

- Two build flavours. Live FT8 requires the CGO build; the static default has no
  live capture. The `!cgo` capture stub makes that honest — `Service.Start` on
  the static build logs "capture unavailable; subsystem idle" and stays up.
- Capture-won't-start is **fail-soft** everywhere (no device, busy, or the static
  build): a warning, never an error that aborts daemon startup.
- A decode is **not a QSO** — `internal/ft8` logs "heard this" lines only; it does
  not write the `qso` table or the upload queue. So FT8 does not touch the
  log/forward subsystems and the narrow-daemon-scope invariant holds by the import
  graph, exactly as with `internal/bridge`. The legitimate future integration
  point (operator turns a decode into a QSO) would be `qsoservice`, not coupling.
- go-ft8 is a go.mod dependency; it is GPL-3.0-only, which is why SM is
  GPL-3.0-only (ADR 0023). gonum (default FFT) and malgo are both GPL-compatible.
- pocketfft and capture both ride the CGO build, gated by build tags so the static
  default is untouched and CI gates the CGO path so it can't rot.

## Triggers to revisit

- If a pure-Go capture path matures to the point it removes the CGO need without
  reintroducing a host-server assumption, the live build could go CGO-free.
- If the live-path decode budget (deep mode, multi-stream contest topology, weak
  hardware) shows the default gonum FFT is too slow, pocketfft could become the
  shipped default — flipping the two-build default, not the mechanism.
- If FT8 ever needs to write QSOs directly rather than just log decodes, the
  `qsoservice` integration point and its transaction boundaries need a conscious
  decision (and probably an ADR of their own).

## References

- ADR 0021 — the prior in-process-subsystem decision this supersedes (architecture);
  its licensing was already superseded by ADR 0023.
- ADR 0023 — MIT → GPL-3.0-only relicensing (the reason SM can link go-ft8).
- ADR 0013 — narrow-daemon-scope / import-graph discipline this preserves.
- go-ft8: `github.com/ColonelBlimp/go-ft8` (`v0.1.0`).
- `docs/session-handoff.md` Sessions 111–114 (offline path, checked API, live steps
  1–2) + memory `project_sm_ft8_integration`.
