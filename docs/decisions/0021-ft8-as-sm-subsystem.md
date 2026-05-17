---
number: 0021
title: FT8 ships as an in-process subsystem inside Station Manager
status: Accepted
date: 2026-05-16
---

# 0021 — FT8 ships as an in-process subsystem inside Station Manager

## Context

FT8 support was originally implemented inside Station Manager (the leftover `internal/ft8/`, `internal/ft8x/`, and `docs/ft8-library-assessment.md` directories in the repo as of 2026-04-14 mark the first attempt). The complexity of porting WSJT-X's Fortran decoder to Go/CGO led to extracting the work into a separate repo — described by the operator as "a rocky road." The path to the extracted repo was never even shared with this assistant; the work has been on its own track since.

The operator has now forked WSJT-X v.3.0.0.1 to `/home/mveary/Development/wsjtx` as a fresh Fortran reference and is starting FT8 over from scratch. The question that surfaced when planning where the code lives: **what does the new FT8 package actually need from SM?**

The honest answer is: a lot.

- It needs to **log decoded QSOs** — direct calls into `internal/qsoservice`, not HTTP roundtrips. FT8 produces multiple QSOs per minute during a heavy run; an in-process API keeps the path tight.
- It needs `internal/errors` for operation-tagged error wrapping, and `internal/logging` for the structured-logging pattern the rest of the daemon uses. Without these, FT8's diagnostics would diverge from everything else's.
- It needs `internal/cat` and `internal/serial` (eventually) for rig control — frequency steering, mode setting, PTT for transmit. FT8 without rig coordination is decode-only and useless for two-way QSOs.
- It needs to read and write **`config.json`** — band settings, audio device, callsign / grid, decode policy, frequency lists, etc. Sharing the daemon's existing config layer beats inventing a parallel one.

That's five SM-internal packages of dependency. A separate library that "depends on station-manager's internal packages" would be inverted — libraries don't depend on specific apps' internals. The integration coupling is meaningfully denser than the isolation benefit that drove the original extraction.

## Decision

The FT8 work returns to Station Manager as `internal/ft8/`, running in-process inside `cmd/smd`. The package follows the same daemon-subsystem pattern as `internal/bridge` (ADR 0013, ADR 0019) — same `Initialize` / `Start(ctx)` / `Stop()` lifecycle, same package-boundary discipline, same enabled-flag gating. The implementation is built **from the FT8 protocol specification** (Joe Taylor's QEX 2020 paper and companion academic references — see the *Licensing constraint* section below for why this matters and what's safe vs not), then validated for decode parity against WSJT-X's `jt9` binary used as a black-box test oracle. Goal: same or better decode results than WSJT-X under the same conditions.

**Note on revision:** an earlier draft of this section read "WSJT-X v.3.0.0.1 ... is the Fortran specification; the porting philosophy is exact port first." That framing was withdrawn 2026-05-17 once the GPL v3 vs MIT licence gap was surfaced — line-by-line translation of GPL source produces derivative work even across languages, which would force-relicense SM. The current wording reflects the corrected position; full reasoning in the *Licensing constraint* section.

The previous extracted repo becomes a reference and stops being the active line of work. The leftover `internal/ft8*` directories in this repo (from the original pre-extraction attempt) are stale and will be replaced.

## Alternatives considered

### Separate Go library, SM imports as `go.mod` dependency

The shape the previous extraction landed in. A pure-Go FT8 library at `github.com/ColonelBlimp/ft8-go` (or similar) reusable by any Go program; SM consumes it as a dependency. Rejected because:

- The library would need to depend on SM's `internal/errors` / `internal/logging` / `internal/cat` / `internal/serial` / config types for the integration to feel native — but those are SM-internal by name and intent. A "library that imports your app's internals" isn't a library, it's a misplaced subsystem.
- Alternative: the library exposes interfaces that SM's subsystem wrapper would implement. Doable, but adds an extra translation layer for every cross-boundary call (QSO submit, log entry, CAT poll, config read). The translation layer adds ceremony without buying isolation that's actually wanted — the operator is the only consumer.
- Cross-repo coordination cost: every behavioural change in FT8 requires a dependency bump in SM, a re-test, and a co-ordinated release. CD pipeline catches breakage but doesn't eliminate the friction.

The "library is reusable by other Go programs" benefit is largely theoretical — there's no second consumer planned. If one ever appears, the in-SM subsystem can be extracted back out at that point with the integration shape already proven.

### Separate process `cmd/ft8d` in this repo, IPC with `cmd/smd`

FT8 ships its own binary; SM and FT8 talk via a Unix socket / SSE / NDJSON / similar. Rejected for v1, but kept as a future option:

- **Pro:** CPU + crash isolation. FT8's signal-processing CPU load (FFTs, LDPC, Kalman) can run hot during decode windows. A runaway decoder can't starve SM's log/forward path if it's a separate process. Same logic applies to crash isolation — an FT8 panic doesn't take down logging.
- **Con:** Two binaries to ship, an IPC contract to design, latency on the QSO-submit path, deployment complexity. Inconsistent with the operator's stated preference for the single-binary default deployment (mirrors the bridge's "default in-process, opt-in split-host" pattern per ADR 0013).
- **Decision:** start in-process; build `cmd/ft8d` if CPU starvation actually bites in real operating use. Multi-rig and contest scenarios may force the split eventually; v1 doesn't need it.

### Stay extracted

Continue the work in the separate repo, paper over the integration coupling with explicit interfaces. Rejected: the previous extraction was tried for an extended period and described as rocky; the integration-coupling reframing makes the cost of staying extracted clearer than it was at extraction time. Reversing now is cheaper than continuing to fight the same friction.

## Consequences

**Gained:**

- **Direct call paths.** FT8 calls `qsoservice.Submit(ctx, qso)` directly. No HTTP serialisation, no JSON marshal/unmarshal, no extra goroutine hop. QSO write latency stays in microseconds.
- **Native error / logging / config story.** FT8's diagnostics integrate with SM's existing journalctl output, structured logging, and `internal/errors` op-tagged paths. Operator gets a single pane of glass.
- **Single-binary deployment.** No second daemon to install, no IPC to configure, no version skew between FT8 and SM. The RPM stays one package.
- **CD gate covers FT8 from commit #1.** Per session 66's CD pipeline work — any FT8 commit that breaks the bridge subsystem tests (e.g. via shared use of `internal/cat`), or breaks the SPA build (e.g. via config-shape changes that the SPA reads via `/v1/config`), is caught immediately. Cross-strand work doesn't silently corrupt the other side.
- **Future SET-side CAT work serves both.** When the inbound CAT command path lands (parked, see ADR 0021's predecessor parked-follow-up in `docs/session-handoff.md`), both the bridge and FT8 use the same write methods on `internal/cat`. No duplication.

**Accepted costs:**

- **Single-process CPU profile.** FT8 decoding is heavyweight (FFTs, LDPC, Kalman) — a runaway decode can starve log/forward of CPU on a constrained host. Mitigation: bounded goroutine pool inside `internal/ft8`, careful work-queue design. Hard process isolation deferred until measured friction surfaces.
- **Single-process crash blast radius.** An FT8 panic takes the daemon down, including logging and the bridge. Mitigation: panic recovery at the goroutine boundaries inside `internal/ft8` (same pattern `internal/safego` already provides for other subsystems).
- **Daemon binary size grows.** WSJT-X's Fortran is large; the Go port plus any LDPC tables, codebooks, and CGO bridges will add MB to the binary. Acceptable — the RPM is already operator-installed, not network-distributed at scale.
- **CGO surface introduced (likely).** A pure-Go FT8 decoder is possible but the Fortran reference relies on `libfftw3`, `portaudio`, and similar — easier to bind via CGO than reimplement. CGO complicates cross-compilation and increases build-time dependencies. Caught early: the CI pipeline will fail loudly on CGO build issues.
- **`internal/safego` may need extending** if FT8's worker pattern doesn't fit the existing helpers. Cheap follow-up.
- **Reverses a prior decision.** Future contributors reading the leftover `internal/ft8*` directories and the still-extant separate repo will be confused. This ADR is the disambiguation point; the leftover directories and memory entries get rewritten in the same commit set that this ADR lands in.

## Licensing constraint

**Added 2026-05-17 (session 68 continuation), after WSJT-X's GPL v3 vs
Station Manager's MIT licence gap was surfaced as a load-bearing
constraint that this ADR's original text understated.**

WSJT-X is GPL v3. Station Manager is MIT. The two licences are
incompatible: any work derivative of WSJT-X must itself be GPL v3,
which would force-relicense Station Manager. The original framing of
"exact port first" in this ADR's *Decision* section, taken literally,
would have produced a derivative work — line-by-line translations from
Fortran to Go are derivative under standard copyright doctrine even
though the target language differs. That framing is hereby revised.

The implementation must be **GPL-clean**:

- **Implementation source is the protocol specification, not the
  Fortran sources.** Joe Taylor's QEX July/August 2020 paper "The FT4
  and FT8 Communication Protocols" describes the LDPC(174,91)
  generator matrix, the Costas sync sequence, the symbol mapping, and
  the frame structure with enough fidelity to implement from. Steve
  Franke's companion QEX papers cover the demodulator. The WSJT-X user
  docs cover sequencing and message packing. These are the canonical
  references.
- **WSJT-X source files (`*.f90`, `*.cpp`, `*.h`) are not consulted
  for implementation guidance.** Reading them for general orientation
  isn't strictly prohibited but is discouraged — the line between
  "understood the structure" and "copied the expression" is the kind
  of grey area that's safest to avoid entirely on a permissively-
  licensed project.
- **WSJT-X binaries (`jt9`, `ft8sim`) are tools we exec for testing.**
  Tool use does not create derivative works (same legal shape as
  compiling code with GCC). The M4.1 parity gate operates entirely at
  this level — feed a WAV through both decoders, diff the output.
- **WSJT-X assets (sample WAVs, dictionaries, message files) are not
  bundled in this repo.** They're part of the GPL distribution.
  Operator-recorded WAVs (own copyright, MIT/CC0-released) are the
  long-term clean test-corpus source; in the interim, the parity gate
  reads from `$FT8_TEST_CORPUS` pointing at the operator's local
  WSJT-X install.
- **CGO dependencies must be permissively licensed.** This excludes
  **FFTW3** (GPL v2) despite it being the WSJT-X reference; KissFFT
  or PocketFFT (both BSD-3) replace it. PortAudio is MIT-equivalent
  and stays in scope. Any future CGO dependency gets its licence
  checked before vendoring.
- **Mathematical constants (Costas array, LDPC parity matrix, CRC14
  polynomial)** are facts and not copyrightable, so citing them from
  the paper is fine. Don't copy a specific Fortran array literal
  byte-for-byte — derive from the spec.

When in doubt the rule is: the only WSJT-X artifacts that touch this
codebase are the binaries we exec for testing and the academic papers
we cite. Source files in the fork are off-limits as implementation
reference material.

This constraint is incorporated by reference into every M4 sub-
milestone — see `docs/v2-design/milestones.md` § Milestone 4 design
preamble, *Licensing constraint*.

## Triggers to revisit

- **CPU starvation in real operating use.** If a heavy FT8 decode window measurably slows log/forward or makes the SPA feel unresponsive, the `cmd/ft8d` split-process variant becomes the right answer. Build it whole, mirror the bridge's `cmd/bridge` shape, ship the option as opt-in just like ADR 0013 did for the bridge.
- **A second Go consumer of FT8 appears.** WSJT-X-style standalone decoder, fldigi-style bridge tool, anything other than SM that wants Go-native FT8. At that point extract a pure-Go library from `internal/ft8/` (the integration code stays in SM) and version it independently.
- **Multi-host operation.** SO2R contest, master/slave field-day topology with FT8 running on a different host than the writer daemon. Same answer as CPU starvation: `cmd/ft8d` becomes the deployment shape.
- **CGO becomes too costly.** If cross-compilation or build-time dependency management gets painful (e.g. trying to ship a Windows or macOS binary), the trigger is "evaluate a pure-Go FFTW alternative or accept that we're Linux-only for FT8." Not foreseen for v1 — operator's target is Linux.
- **Exact-port philosophy bites.** If a Fortran-to-Go faithful port produces materially worse decode results than the reference (rounding differences in critical signal-processing chains, FP precision mismatches, etc.), the trigger is "characterize the deviation, then either fix-to-parity or accept the difference and document it." Goal stays "same or better than WSJT-X."

## References

- ADR 0013 — `daemon-owns-bridge-as-subsystem.md` (the subsystem-pattern precedent and the package-boundary discipline; same shape applies here)
- ADR 0019 — `bridge-subsystem-v1-design.md` (the in-process-default + opt-in-split-host pattern this ADR mirrors)
- ADR 0020 — `bridge-pipeline-supervisor.md` (the lifecycle pattern any long-running goroutine subsystem follows)
- `docs/v2-design/milestones.md` § Milestone 4 — the concrete six-sub-milestone breakdown of this ADR's work (M4.1 WAV-decode parity → M4.6 SPA panel), with CGO commitment formalised in the design preamble.
- **Joe Taylor (K1JT), Steven Franke (K9AN), Bill Somerville (G4WJS): "The FT4 and FT8 Communication Protocols," QEX July/August 2020.** The canonical protocol specification — implementation source-of-truth per the *Licensing constraint* section. Open-access via ARRL's QEX archive.
- WSJT-X user documentation — sequencing, message-packing, and on-air-practice reference. Distributed with the WSJT-X package.
- `docs/v1-analysis/invariants.md` — "Narrow daemon scope": FT8 decoding must not couple to log/forward subsystems. Satisfied here by package-boundary import discipline (same as the bridge).
- `docs/session-handoff.md` parked follow-ups — inbound CAT command path is the trigger that unblocks FT8's TX side; until that ships, FT8 is decode-only.
- Memory `project_ft8_library.md` — updated in the same commit as this ADR landing; reflects the in-SM reversal and the WSJTX fork as Fortran reference.
- Memory `project_sm_cd_pipeline_planned.md` — CD pipeline shipped session 66; gates FT8 commits from commit #1.
- WSJT-X v.3.0.0.1 fork at `/home/mveary/Development/wsjtx` — the Fortran specification.
