// Package ft8 wires the go-ft8 decode library into Station Manager.
//
// It is deliberately thin: it owns the int16 audio seam (12 kHz, mono,
// signed-16-bit PCM — the format go-ft8 and its WSJT-X/jt9 lineage expect)
// and the fail-soft + structured-logging policy around a decode. The DSP,
// LDPC, and message unpacking all live in go-ft8; this package does not
// reimplement any of it.
//
// Two entry points share one path:
//
//   - DecodeSlot turns a slot of int16 samples into decoded messages via
//     go-ft8's checked API — a malformed slot is rejected with a typed error
//     rather than silently zero-padded — logging each message as a structured
//     line (plus a debug-level per-slot diagnostics line) and returning the
//     slice. This is the call the daemon will make per 15-second slot once
//     live capture exists; the live path only has to produce the int16 slot.
//   - DecodeFile reads a WAV fixture into an int16 slot and hands it to
//     DecodeSlot — the offline/test path, and the first integration proof
//     against go-ft8's deterministic corpus.
//
// Fail-soft is load-bearing: a decode that errors or panics logs and
// returns nothing. An FT8 failure must never take down the daemon, the
// same principle as "enrichment never blocks logging".
//
// go-ft8 is licensed GPL-3.0-only (a WSJT-X/jt9 derivative); linking it is
// why Station Manager is GPL-3.0-only. See docs/licensing.md and ADR 0023.
package ft8
