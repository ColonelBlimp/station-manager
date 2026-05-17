// Package codec contains Station Manager's FT8 message-codec
// primitives — the bidirectional building blocks shared between
// encoding (TX-side) and decoding (RX-side) of the FT8 protocol.
//
// What's here today (Layer 1 primitives — see "Test architecture"
// below):
//
//   - CRC14: 14-bit cyclic-redundancy check over the 77-bit message
//   - CallsignC28: standard amateur callsign → 28-bit code
//   - Grid4ToG15: 4-char Maidenhead grid / reserved token / signed
//     report → 15-bit code
//   - LDPC(174,91): the dense generator matrix, sparse parity-check
//     matrix (both vendored from QEX ref [14]), and LDPCEncode for
//     the encode-side
//   - Pack / Unpack: bit-per-byte ↔ packed-bytes conversion
//
// What's coming (encode-side message-packing helpers, the LDPC
// decoder, signal-processing primitives like demod + Costas sync)
// lives in this package too — the unifying principle is "FT8
// protocol-level primitives that do not need audio or rig I/O."
// Audio capture and tone synthesis live elsewhere (a separate
// internal/ft8/audio sub-package or similar, TBD when M4.2 lands).
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
// Layer 2 — encoder ↔ decoder round-trip tests. Land in this
// package when both directions are present (the encoder side is
// well underway; the decoder side starts with LDPC belief-
// propagation decoding). Layers 3+4 — synthetic and real-signal
// WAV tests against operator-supplied fixtures. Layer 5 — the
// `cmd/ft8-corpus-prep` developer tool. Full detail in
// `docs/v2-design/milestones.md` § Milestone 4 design preamble.
//
// # Scope
//
// FT8 only. Other digital modes (FT4, JT9, JT65, MSK144, FST4, Q65)
// are out of scope per ADR 0021 and the M4 explicit-non-scope list.
//
// # Slot widths and machine-word fits (Section 9 of QEX paper)
//
// The FT8 designers picked alphabet sizes and message lengths so
// the resulting per-slot integers land in well-behaved widths:
//
//	Slot   Bits  Algorithm                              Machine fit
//	c28    28    base-37/36/10/27/27/27 (mixed-radix)   uint32 (4 spare bits)
//	g15    15    base-(18,18,10,10) + reserved tokens   uint16 (1 spare bit)
//	g25    25    base-(18,18,10,10,24,24)               uint32 (7 spare bits)
//	h10/12/22  N base-38 × magic prime, top N bits      uint32
//	c58    58    base-38^11                             uint64 (6 spare bits)
//	f71    71    base-42^13                             uint64 + 7 high bits
//
// f71 is the only one that doesn't fit cleanly in a single machine
// word (42^13 ≈ 9.3 × 10^20 > 2^64). The implementation carries a
// two-word (hi:lo) accumulator and uses math/bits.Mul64 for the
// 64×64→128 multiplication. c58 is the closest near-miss the
// other way: 38^11 ≈ 2.39 × 10^17 fits in uint64 with ~17% headroom
// (uint64 max ≈ 1.84 × 10^19). Reading "why is c58 one-line of
// arithmetic and f71 multi-precision?" — that's because the spec
// happens to put one above and one below the 64-bit boundary.
//
// # Naming note
//
// The Go package name "codec" emphasises that primitives here serve
// both encode and decode paths — most of the bit-packing and FEC
// arithmetic is symmetric. The directory was previously named
// "decoder" while only encode-side primitives existed; the rename
// to "codec" landed when it became clear the same package would
// host both directions. The `cmd/ft8-corpus-prep` developer tool
// (which IS decode-specific — it shells out to WSJT-X's `jt9 -8`
// as the parity oracle) is a separate `cmd/...` binary, not part
// of this package.
package codec
