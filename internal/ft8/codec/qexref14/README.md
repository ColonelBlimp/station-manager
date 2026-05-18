# QEX Paper Reference [14] — Vendored Files

This directory contains files vendored from the public-domain
reference tarball published alongside the QEX paper:

> Franke (K9AN), Somerville (G4WJS), Taylor (K1JT),
> **"The FT4 and FT8 Communication Protocols,"**
> QEX July/August 2020.
> Paper PDF: https://wsjt.sourceforge.io/FT4_FT8_QEX.pdf
> Reference [14] tarball: `ft4_ft8_protocols.tgz` from
> http://physics.princeton.edu/pulsar/k1jt/

## Files

| File | Description |
|---|---|
| `generator.dat` | LDPC(174,91) generator matrix — 83 rows × 91 columns, ASCII '0'/'1' format. Row *i* specifies which message bits contribute (XOR'd modulo 2) to parity bit *i*. |
| `parity.dat` | LDPC(174,91) sparse parity-check matrix — 83 rows × 174 columns. The data section lists 174 lines (one per column), each containing 3 1-based row indices where that column has a 1. |
| `arrl_rac_sections.txt` | ARRL / RAC Field Day section abbreviations (84 entries: 83 sections + `"DX "` catch-all). Fortran-90 `data csec/ ... /` statement format with 3-character right-padded strings; consumed by Type 0.3 / 0.4 (Field Day) message packing per QEX Table 1. |
| `states_provinces.txt` | US states + Canadian provinces + DC (64 entries). Fortran-90 `data cmult/ ... /` statement format with 3-character right-padded strings; consumed by Type 3 (RTTY Roundup) message packing per QEX Table 1. |

## Licensing

Section 9 of the QEX paper explicitly carves these files out from the
GPL that covers the main WSJT-X source distribution:

> *"With the exception of code contained in reference [14], source
> code for our implementations of FT4, FT8, and MSK144 is not in the
> public domain. Rather, all code in WSJT-X is copyrighted and licensed
> under the terms of Version 3 of the GNU General Public License
> (GPLv3) ..."*

The files in this directory are therefore **public domain** per the
paper authors' declaration. Vendoring them into Station Manager's
MIT-licensed source tree is explicitly permitted.

The GPL WSJT-X source tree (`*.f90`, `*.cpp`, `*.h` files in the main
WSJT-X distribution) is a separate matter and is **off-limits** as an
implementation reference for Station Manager — see ADR 0021's
*Licensing constraint* section and `docs/v2-design/milestones.md`
§ Milestone 4 design preamble.

## Why vendored

The matrices are load-bearing data: the LDPC encoder/decoder cannot
function without them, and they are not algorithmically reconstructible
from a brief textual description (they're hand-crafted by the FT8
designers for good distance properties at this code rate).

The two lookup tables (`arrl_rac_sections.txt`, `states_provinces.txt`)
are similarly load-bearing for Phase 4 message types — without them,
Type 0.3 / 0.4 (Field Day) and Type 3 (RTTY Roundup) packing cannot
emit the section / state code. They're vendored ahead of Phase 4 to
keep the QEX ref [14] bucket complete in one place.

Vendoring all four files avoids any runtime dependency on the operator
having downloaded reference [14] separately.

## Provenance verification

Per QEX paper §3 the matrices have these properties — pinned as test
invariants in `../ldpc_test.go`:

- `generator.dat` is 83 rows × 91 columns of '0'/'1' characters.
- `parity.dat` is sparse 83×174: each column has exactly 3 ones; each
  row has 6 or 7 ones.

The lookup tables are vendored verbatim from the tarball (Fortran-90
`data` statement format). They have no Go consumer yet — `//go:embed`
loaders + parsers land alongside the Type 0.3 / 0.4 and Type 3
encoders in Phase 4 of the Layer 2 plan.
