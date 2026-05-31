# Station Manager — licensing

This document explains Station Manager's (SM) licence and how it relates
to the FT8 protocol implementation it builds on. It is written for three
audiences: a curious user evaluating SM, a distro packager considering
inclusion, and a contributor who wants to know the licence their PR lands
under. If you spot something that looks inconsistent with the policy
below, opening a GitHub issue is the right next step — see the closing
section.

> **History.** SM was MIT-licensed from its first commit until 2026-05-31,
> with an elaborate clean-room policy designed to *keep* it MIT while
> implementing FT8 from the published specification. That effort is over.
> SM is now GPL-3.0-only. The reasoning is in
> `docs/decisions/0023-relicense-to-gplv3.md`. Earlier revisions of this
> file described the MIT/clean-room position; git history preserves them.

---

## 1. Station Manager is GPL-3.0-only

The full text is in `LICENSE` at the repository root; `NOTICE` records the
copyright and the derivative chain. The GNU General Public License,
version 3 only (`GPL-3.0-only`) is a strong copyleft licence: you may use,
modify, and redistribute SM, but any distributed work that incorporates
SM's code must itself be licensed GPL-3.0-only and ship with (an offer of)
corresponding source.

The "version 3 **only**" — not "version 3 or later" — wording is
deliberate and inherited; see §3.

Copyright on SM's own code is held by Marc L. Veary (7Q5MLV), the sole
author, which is what made the MIT → GPL relicensing possible.

### What changed for users and packagers

- **Source up to 2026-05-31 stays MIT for anyone already relying on it.**
  Relicensing is not retroactive on copies already published under MIT —
  any commit at or before that date, and the `v1` branch / `v1.0.0` tag,
  remains usable under MIT terms. From 2026-05-31 onward, `main` is
  GPL-3.0-only.
- **Binary distribution carries a source obligation.** The RPM and any
  other binary you redistribute must be accompanied by, or offer,
  corresponding GPLv3 source. The public GitHub repository satisfies this
  for tagged source builds.
- **Forks and downstream work must stay GPL-3.0-only.** You can no longer
  fold SM into a proprietary or permissively-licensed product.

---

## 2. Why GPL-3.0-only — the FT8 decode path

SM's reason for existing is logging and forwarding QSOs; FT8 decoding is
one capability among several. That capability is provided by a companion
library, **go-ft8** (`github.com/ColonelBlimp/go-ft8`), which SM links in.

go-ft8 is an explicit **WSJT-X/jt9-derivative** — it descends from the
GPL-licensed reference implementation of FT8 — and is therefore
GPL-3.0-only. A derivative of GPLv3 code is a derivative regardless of the
implementation language or who read the source; linking it into `smd`
makes the distributed daemon a derivative work, and GPL copyleft requires
the combined work to be distributed under the same licence. SM could not
link go-ft8 and remain MIT, so SM adopted GPL-3.0-only.

This reverses years of effort that went the other way. SM previously
maintained a clean-room boundary (implement FT8 from the QEX 2020
specification paper + the public-domain ref [14] tarball, never from the
WSJT-X source) precisely to keep the project MIT. That clean-room decoder
never reached jt9 parity and hit a runtime/decode-depth wall; the operator
chose the WSJT-X-derived go-ft8 as the shipping decoder instead. With the
project now GPL, the clean-room boundary is retired — see §4.

---

## 3. version-3-only, not "or later"

go-ft8 is licensed version-3-**only**. A work that links version-3-only
code cannot offer the recipient the GPL's usual "or, at your option, any
later version" choice, because part of the combined work forbids it. SM
therefore inherits `-only`. The SPDX identifier is `GPL-3.0-only`
everywhere it appears (`LICENSE`/`NOTICE`, `nfpm.yaml`, `package.json`).

---

## 4. The clean-room boundary is retired

The former policy forbade reading WSJT-X source, ran an "AI-consultation
derivation gate" for FT8 constants, and preferred BSD/MIT FFT libraries
over FFTW3 to keep the binary unencumbered. **None of that applies any
more.** Now that the project is GPL-3.0-only:

- Building FT8 on WSJT-X/jt9-derived code is permitted — that is exactly
  what go-ft8 does.
- `jt9` and the WSJT-X source may be used freely as references, oracles, or
  derivation sources for GPL code in the SM/go-ft8 family.
- A GPL'd CGO dependency such as FFTW3 no longer raises a binary-
  distribution concern, since the binary is GPL anyway. (go-ft8 currently
  vendors BSD-3-Clause PocketFFT regardless, for its own reasons.)

Contributors touching FT8 should work in the **go-ft8** repository under
its GPL terms and its `docs/WSJTX_DERIVATIVE.md` conventions; SM itself
links go-ft8 rather than carrying the decoder.

---

## 5. Dependency compatibility

GPL-3.0-only can only be combined with GPL-compatible dependencies. As of
this relicensing every Go and JavaScript dependency SM pulls in is under a
permissive licence — MIT, BSD-2/3-Clause, Apache-2.0, or ISC — all of
which are GPL-3.0-compatible, so the combined binary is distributable.

The one thing to check on any *new* dependency is GPL compatibility. A
GPLv2-only, CDDL, or proprietary dependency would be incompatible and is
the thing to reject — the licence stays put. (Apache-2.0 is compatible
with GPLv3, though not with GPLv2 — another reason SM is on v3.)

---

## 6. go-ft8 notices to carry

When a binary that links go-ft8 is redistributed, preserve go-ft8's own
`LICENSE` (GPLv3), its `NOTICE`, its `docs/WSJTX_DERIVATIVE.md` derivative-
status notice, and its third-party notices (notably the BSD-3-Clause
PocketFFT it vendors). SM's own `NOTICE` points at these.

---

## 7. If you find something that looks wrong

If you find a dependency that looks GPL-incompatible, a binary release that
ships without a source offer, a stale "MIT" reference left over from the
old policy, or any other licensing gap — please open a GitHub issue. The
fix is usually one of: replace the incompatible dependency, add the missing
source offer, or update this document. Surfacing it is more useful than
letting it sit.

---

*Maintained by Marc Veary. Last updated 2026-05-31.*
