// Package sandbox is the research-stage ground-up candidate-detection
// experiment ground. Started 2026-05-26 to explore alternative
// approaches to FT8 candidate detection (windowing geometry, FFT
// configuration, sync mechanisms, etc.) without disturbing the
// parity-pinned pipeline in research/candidates/.
//
// # Scope
//
// "Candidate detection from the ground up" — anything from raw
// samples up to the point where a (freq, dt) hit is emitted for the
// demodulator stage. Multiple experiments may coexist here; once one
// matures into a serious contender it gets its own sibling package
// or is promoted into research/candidates/ behind a Gates-style A/B.
//
// # Constraints
//
//   - Imports: stdlib + internal/audio only. No imports from
//     research/candidates/, research/demod/, research/synth/, or
//     internal/ft8/*. The research-tree firewall applies in full —
//     FT8 protocol constants are re-declared here.
//
//   - Reference sources: the QEX 2020 paper (Franke/Somerville/Taylor)
//     and the public-domain ref [14] tarball (LDPC matrices + short
//     reference programs the paper authors deliberately released).
//     WSJT-X source is OFF-LIMITS as implementation reference (GPL
//     v3 incompatible with SM's MIT licence). Third-party FT8
//     implementations (kgoba/ft8_lib etc.) are off-limits per the
//     2026-05-26 operator directive even when permissively licensed.
//
// # Test fixtures
//
// Truth-tagged WAVs live at research/*.wav with matching truth
// manifests at research/*.truth.json (load via research/truth.Read).
// Real-capture fixtures live elsewhere in the research tree and are
// referenced by relative path from each test file. Synthetic
// single-tone inputs (where the expected outcome is exactly known)
// are the right starting point for ground-up experiments — they
// surface algorithm bugs that real captures would hide under noise.
package sandbox
