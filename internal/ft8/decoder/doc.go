// Package decoder contains Station Manager's FT8 decoder
// implementation — algorithmic primitives (CRC14, LDPC encode/decode,
// callsign/locator/hash packing, message unpacking) plus the
// signal-processing pipeline (downsample, sync, demod) that turns an
// FT8 WAV file into decoded message strings.
//
// # Implementation source
//
// Built from the FT8 protocol specification: Franke (K9AN),
// Somerville (G4WJS), Taylor (K1JT), "The FT4 and FT8 Communication
// Protocols," QEX July/August 2020 —
// https://wsjt.sourceforge.io/FT4_FT8_QEX.pdf — plus the public-domain
// reference programs and data files in the QEX paper's reference [14]
// tarball (`ft4_ft8_protocols.tgz` from
// http://physics.princeton.edu/pulsar/k1jt/), which Section 9 of the
// paper explicitly carves out from the GPL. The GPL WSJT-X main
// source tree is NOT consulted as an implementation reference per
// ADR 0021's Licensing constraint.
//
// # Test architecture (M4.1 layered model)
//
// Layer 1 (this package) — spec-vector tests for each algorithmic
// primitive. Tests pin known inputs to known outputs derived from
// the paper and/or the public-domain reference programs in QEX
// ref [14]. Always runs in CI; no external state, no fixtures, no
// skipping. Catches any algorithmic regression the moment it lands.
//
// Layer 2 — encoder ↔ decoder round-trip tests (will land alongside
// the encoder package). Layers 3+4 — synthetic and real-signal WAV
// tests against operator-supplied fixtures. Layer 5 — the
// `cmd/ft8-corpus-prep` developer tool. Full detail in
// `docs/v2-design/milestones.md` § Milestone 4 design preamble.
//
// # Scope
//
// FT8 only. Other digital modes (FT4, JT9, JT65, MSK144, FST4, Q65)
// are out of scope per ADR 0021 and the M4 explicit-non-scope list.
package decoder
