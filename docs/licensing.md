# Station Manager — licensing and clean-room policy

This document explains how Station Manager (SM) keeps its MIT licence
defensible while implementing the FT8 protocol — a protocol whose
canonical reference implementation, WSJT-X, is licensed GPL v3. The
short version: SM's FT8 code is clean-room from the published protocol
specification, not translated or copied from WSJT-X source.

It's written for three audiences: a curious user evaluating SM, a
distro packager considering inclusion, and a potential contributor who
wants to know how to keep their PR licence-clean. If you spot something
in the codebase that looks like it might not match the policy below,
opening a GitHub issue is the right next step — see the closing
section.

---

## 1. Station Manager is MIT-licensed

The full text is in `LICENSE` at the repository root. The MIT licence
is permissive: you can use, modify, and redistribute SM (including in
proprietary products) provided you preserve the copyright notice. The
goal of this document is to explain how that permissive licence is
preserved when the project implements a protocol whose reference
implementation is GPL.

SM has no plans to relicense. Anyone using or forking SM today can
rely on it staying MIT for the work they're doing now.

---

## 2. The FT8 clean-room boundary

FT8 was designed by Steve Franke (K9AN), Bill Somerville (G4WJS), and
Joe Taylor (K1JT) — a remarkable protocol that makes reliable QSOs
possible at signal levels far below conventional readability. SM
facilitates logging and forwarding QSOs the operator makes using that
protocol; the protocol itself, and the deep DSP and coding-theory
work behind it, is theirs.

The FT8 protocol is documented in a publicly-available specification
paper. WSJT-X is a *reference implementation* of that protocol, not
the protocol itself. SM implements the protocol from the specification.

### Sources read and used as implementation references

- **Franke, Somerville, Taylor: "The FT4 and FT8 Communication
  Protocols"**, *QEX*, July/August 2020.
  <https://wsjt.sourceforge.io/FT4_FT8_QEX.pdf>
  The canonical FT8 protocol paper. SM's symbol codec, GFSK
  synthesis, sync structure, and message-type framing all derive from
  this paper's sections.

- **`ft4_ft8_protocols.tgz`** — referenced as `[14]` in the QEX paper
  above and downloadable from
  <http://physics.princeton.edu/pulsar/k1jt/>.
  The paper's authors deliberately released this tarball into the
  public domain for reimplementers. It contains the LDPC parity
  matrices, the symbol-to-bit mappings, and seven short reference
  programs that demonstrate how the bit-level constructs in the paper
  combine. SM consumes the public-domain LDPC matrices directly and
  uses the reference programs as derivation aids.

- **Companion academic references** cited inside the QEX paper for
  individual signal-processing techniques (LDPC decoding, OSD, BP
  decoding). These are standard DSP / coding-theory literature and
  are referenced where SM's implementation diverges from a textbook
  approach.

### Sources NOT read and NOT consulted

- **WSJT-X source code** as an implementation reference. Before
  starting SM, the maintainer started to review the WSJT-X source
  tree, recognised it as Fortran (which the maintainer does not
  read), it was closed. No real working knowledge of WSJT-X's
  internals was acquired then or since, and none will be sought
  going forward. This is the load-bearing clean-room boundary.

- **Third-party FT8 implementations** (kgoba/ft8_lib and similar
  permissively-licensed reimplementations). Even though the licences
  on these projects would technically permit reuse, the project
  maintainer chose 2026-05-26 to keep them outside the implementation
  reference set. Convergence with another reimplementation on a
  specific constant or shape would weaken the "derived independently
  from the spec" story; staying out of those trees keeps the story
  simple.

If contributing a PR that touches the FT8 stack, please follow the
same rule: implement from the QEX paper + ref [14] tarball, not from
WSJT-X or other FT8 codebases.

---

## 3. The jt9 oracle policy

SM's test suite invokes WSJT-X's `jt9` binary as a parity oracle —
"given this WAV file, what messages does jt9 decode?" — and compares
SM's decoder output against jt9's. This is **black-box use**: SM reads
jt9's stdout, not its source.

The `jt9` binary is installed from the operating system's WSJT-X
package (Fedora `wsjtx`, Debian `wsjtx`, etc.); it is not bundled
with SM and is not a runtime dependency. Black-box parity testing
against an externally-installed GPL binary does not create a
derivative-work relationship for SM's source.

If you maintain a fork or downstream package and want to drop the
oracle entirely, the test that depends on it is gated and skips
cleanly when `jt9` isn't on PATH.

---

## 4. The AI-consultation rule

SM is developed with AI assistance. This includes Claude (in the
editor) and occasional cross-referencing of ideas with other AI
systems. Anyone working on a non-trivial codebase in 2026 is going to
be in this position; the question is how to do it without
compromising the clean-room story.

The rule SM applies:

- **AIs are idea sources, not value sources.** An AI suggesting an
  algorithm shape ("try a 1920-sample window into a 3840 zero-padded
  FFT") is fine — that's textbook DSP, unencumbered. An AI suggesting
  a specific numeric constant ("scale by 1/300") is treated as raw
  material that has to pass through the derivation gate before it
  lands in code.

- **The derivation gate.** A committed magic number must trace back to
  one of: (a) a sweep against measured data with the sweep output
  preserved, (b) a first-principles derivation in the QEX paper or
  ref [14] reference, or (c) a documented "no-op, value is
  conventionally X for clarity" comment with the no-op property
  verifiable.

- **Independent convergence is fine.** If a sweep lands on a constant
  that WSJT-X also uses, that's not contamination — that's two
  independent derivations reaching the same numerical optimum because
  the underlying mathematics has one. The hygiene is in the
  derivation record, not in arbitrary avoidance.

The recent practical example (2026-05-27) was a `1.0/300.0` input
scale factor suggested by an AI consultation. We declined to adopt it
because (a) we couldn't derive it independently — it's a no-op for
SM's matched-filter detector, so no sweep would have an optimum to
find, and (b) it matches WSJT-X's `fac=1./300.` line exactly, so
adopting unattributed would have created an apparent-copying problem
for zero detection benefit.

---

## 5. CGO dependency licensing (FFTW3 etc.)

ADR 0021 (in `docs/decisions/`) covers the dependency-licensing rules
in detail. The short version for this document's purpose:

- **Source-side**: SM's source code can legitimately include CGO
  bindings against GPL'd system libraries like FFTW3 (`libfftw3f`).
  The binding code is the project's own work, MIT-able, and copies no
  GPL source.

- **Binary-side**: The GPL trigger fires when a binary linked against
  a GPL'd library is distributed. SM's binary releases therefore avoid
  GPL'd CGO dependencies. The current FT8 codepath uses pure-Go FFT
  (`internal/audio/realfft.go`) precisely so the binary stays
  unencumbered.

- **If CGO acceleration is ever wanted**, the preferred path is a
  permissively-licensed FFT library — KissFFT or PocketFFT (both
  BSD-3-Clause). FFTW3 is a documented option for forks willing to
  inherit GPL on their binaries; SM's upstream releases will not.

---

## 6. Constants and derivation footprint

Going forward, non-trivial constants landing in the FT8 stack carry a
doc comment pointing to their derivation source: a paper section, a
sweep output, or a "no-op, see analysis" reference. Existing
constants are updated when touched. This is audit-as-you-go, not a
backfill project.

The measurement infrastructure that makes this discipline cheap
already exists:

- `research/cmd/decode-eval/` — end-to-end decoder evaluation against
  truth-tagged captures
- `research/cmd/sweep-gates/` — parameter sweep harness for the
  candidate-detection gate constants
- Truth manifests at `research/*.truth.json` — pin the expected
  signals in each test capture
- ADR log at `docs/decisions/` — the reasoning trail for choices that
  get revisited

A reviewer interested in any committed constant should be able to find
either the comment that explains it or the harness that would
reproduce it.

---

## 7. If you find something that looks wrong

The policy above is what SM intends to follow, but a project this size
will have rough edges. If you find:

- A constant in the FT8 code that looks like it came from WSJT-X
  source and has no derivation footprint
- An algorithm shape that goes beyond "standard DSP / coding theory"
  and matches WSJT-X's particular implementation choice without an
  independent reference
- A test that reads jt9 source rather than treating it as a black box
- Any other policy gap

… please open a GitHub issue. The fix is one of: add the missing
derivation evidence, replace the construct with an independently-
derived equivalent, or update this document if the policy needs to
evolve. Either way, surfacing it is more useful than letting it sit.

---

*Maintained by Marc Veary. Last updated 2026-05-27.*
