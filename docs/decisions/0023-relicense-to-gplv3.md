---
number: 0023
title: Relicense Station Manager from MIT to GPL-3.0-only
status: Accepted
date: 2026-05-31
---

# 0023 — Relicense Station Manager from MIT to GPL-3.0-only

## Context

Station Manager was MIT-licensed from its first commit (both `main` and
the frozen `v1` branch). A large body of work — ADR 0021, `docs/licensing.md`,
the `research/` clean-room decoder tree, the "don't read WSJT-X source"
boundary — existed specifically to let SM implement the FT8 protocol while
*staying* MIT: implement from the QEX 2020 paper + the public-domain ref [14]
tarball, never from the GPL WSJT-X/jt9 Fortran source.

That clean-room effort never reached jt9 parity. Across the 2026-05 sessions
the sandbox decoder plateaued well short of the oracle and hit a runtime/
depth wall (see memory `project_sm_ft8_language_question`); FT8 was removed
from the SM tree on 2026-05-30 (tag `ft8-snapshot-2026-05-30`) and continued
out-of-tree. The out-of-tree result is the companion library **go-ft8**
(`github.com/ColonelBlimp/go-ft8`), which the operator built as an explicit
**WSJT-X/jt9-derivative** — i.e. GPL-3.0-only. It is the FT8 decode path SM
will actually ship.

A port/derivative of GPLv3 code is a derivative work regardless of the
implementation language or who viewed the source. Linking go-ft8 into `smd`
makes the distributed daemon a derivative of GPLv3-only code, and the GPL's
copyleft requires the combined work to be distributed under the same licence.
SM cannot link go-ft8 and remain MIT.

## Decision

Relicense Station Manager from MIT to **GPL-3.0-only**, effective 2026-05-31,
ahead of integrating the go-ft8 FT8 decode library. The full GPLv3 text lives
in `LICENSE`; a `NOTICE` file records the copyright, the version-3-only
constraint, and the linked-derivative chain.

## Alternatives considered

### Stay MIT, keep the clean-room decoder

The original plan. Rejected: the clean-room decoder did not reach parity after
months of work and hit a Go-runtime depth wall; the operator chose the
WSJT-X-derived go-ft8 as the shipping decoder. Keeping MIT would mean shipping
no usable FT8, or shipping a derivative of GPL code under MIT — which is not
permitted.

### Keep go-ft8 at arm's length to avoid copyleft

Run go-ft8 as a separate process and talk to it over IPC, or otherwise argue
the daemon and the decoder are "mere aggregation." Rejected: the intended
integration is in-process (import as a `go.mod` dependency, per the shape ADR
0021 sketched), the FSF treats a single program built from GPL and non-GPL
parts as one work, and contorting the architecture purely to dodge copyleft is
exactly the kind of cleverness this project avoids. The operator owns both
codebases and is content for SM to be GPL.

### GPL-3.0-or-later instead of -only

Rejected because it is not available: go-ft8 is version-3-**only**, and a work
linking version-3-only code cannot offer the "or any later version" option.
SM inherits `-only`.

## Consequences

- `LICENSE` now holds the GPLv3 text; `nfpm.yaml` ships `license: GPL-3.0-only`;
  `frontend/logging/package.json` declares `GPL-3.0-only`; a `NOTICE` file and a
  README licence section are added.
- The "no plans to relicense" promise in the old `docs/licensing.md` is void.
  Code distributed under MIT *before* 2026-05-31 (any commit up to that point,
  and the `v1` branch / `v1.0.0` tag) remains MIT for anyone relying on it —
  relicensing is not retroactive on already-published copies. Going forward,
  `main` is GPL-3.0-only.
- The clean-room boundary is **dropped**. Building FT8 on WSJT-X/jt9-derived
  code is now permitted; the "don't read WSJT-X source", "AI-consultation
  derivation gate", and "prefer BSD/MIT FFT to keep the binary unencumbered"
  rules in the old licensing doc and ADR 0021 no longer apply. `jt9` may be
  used freely (as oracle or otherwise); FFTW3/CGO no longer carries a binary-
  distribution licence concern, since the binary is GPL anyway.
- Dependency check: every current Go and JS dependency is permissively licensed
  (MIT / BSD-2/3-Clause / Apache-2.0 / ISC), all GPL-3.0-only-compatible, so the
  combined binary is distributable. Re-check on any new dependency: a GPLv2-only,
  CDDL, or proprietary dependency would be incompatible.
- The operator holds copyright on all of SM's own code (sole author), so the
  relicensing is theirs to make.
- Distribution obligations: binary releases (the RPM) must offer corresponding
  source under GPLv3. The repo being public on GitHub satisfies this for the
  tagged source; the `NOTICE` documents the go-ft8 + PocketFFT notices to carry.

## Triggers to revisit

- If the FT8 decode path is ever replaced by a genuinely clean-room or
  permissively-licensed implementation that SM no longer links any GPL code,
  the copyleft obligation falls away and MIT (or another permissive licence)
  becomes available again.
- If a future dependency is GPL-incompatible (GPLv2-only, CDDL, proprietary),
  that dependency — not the licence — is the thing to reconsider.

## References

- `LICENSE` — GPLv3 full text. `NOTICE` — copyright + derivative chain.
- `docs/licensing.md` — operator/packager/contributor-facing explanation.
- `docs/decisions/0021-ft8-as-sm-subsystem.md` — the (parked) in-process FT8
  integration shape this licence change clears the way for.
- go-ft8: `github.com/ColonelBlimp/go-ft8` — its `LICENSE`, `NOTICE`, and
  `docs/WSJTX_DERIVATIVE.md`.
- Memory `project_sm_ft8_language_question` — why the clean-room Go decoder
  stalled and the operator leaned to the WSJT-X-derived path.
