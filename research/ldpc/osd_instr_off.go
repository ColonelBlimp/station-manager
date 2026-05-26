//go:build !osdinstr

package ldpc

// osdInstrEnabled gates the per-candidate CRC-pass tracking inside
// osd's scoreOne closure. When false (the default, no build tag set),
// the CRC check runs ONCE at end of enumeration on the ML candidate
// only — matching production behaviour. When true (build with
// `-tags osdinstr`), every one of the 4187 enumerated candidates is
// CRC-checked so the diagnostic Stats fields (OSDCRCPassingCount /
// OSDBestCRCMetric / OSDBestCRCNormMetric / OSDBestCRCHamming) can
// be populated for the Finding-2 calibration question.
//
// Cost of `osdinstr`: ~12.7% of total decode-eval runtime on the
// real-capture corpus (2.89 s of crc14 calls × 4187 iterations ×
// ~500 OSD invocations per corpus). Measured 2026-05-26 — see
// docs/session-handoff.md for the profile that motivated the gate.
//
// Default off is the production-perf setting. The Stats fields
// remain in the public type so callers don't have to conditionally
// reference them; when off, the values are zero / +Inf sentinels.
const osdInstrEnabled = false
