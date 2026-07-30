// Package ft8 is Station Manager's FT8 subsystem: it wires the go-ft8 library
// into the daemon and owns everything from live receive audio through decode,
// occupancy analysis, the SSE wire, and — operator-initiated — transmit, sequencing,
// and completed-QSO assembly. It is no longer the thin decode-only wrapper the
// original scaffold was; the responsibilities below all live here now.
//
// # Audio + decode (RX)
//
// The int16 seam (12 kHz, mono, signed-16-bit PCM — the format go-ft8 and its
// WSJT-X/jt9 lineage expect) is the package's contract. The DSP, LDPC, and
// message unpacking all live in go-ft8; this package does not reimplement any
// of it.
//
//   - DecodeSlot turns a slot of int16 samples into decoded messages via
//     go-ft8's checked API (a malformed slot is rejected with a typed error,
//     not silently zero-padded), logging each message.
//   - DecodeFile reads a WAV fixture into a slot and hands it to DecodeSlot —
//     the offline/test path.
//   - Live capture (CGO build only; a !cgo stub leaves the subsystem idle) feeds
//     a sample ring; a UTC slot Scheduler snapshots it on each 15-second
//     boundary and drives a decode worker. Capture is demand-driven: the device
//     is acquired on the first /v1/ft8/events subscriber and released a short
//     linger after the last leaves.
//   - The occupancy detector ranks clear TX offsets per slot.
//   - Decodes, occupancy, TX state, QSO status, and completed-QSO events fan out
//     on the ft8-* SSE events (Service.HTTPHandler).
//
// # Transmit + sequencing (TX, operator-initiated)
//
// Transmit is operator-initiated and auto-advancing — the daemon starts no run of
// its own (the QEX FT8 spec forbids automatic operation). The operator arms TX,
// then either answers a CQ (ADR 0031) or calls CQ to work a pile-up (ADR 0033);
// the sequencer walks the message ladder, transmitting each rung on the
// synchronised timebase (ADR 0032).
//
// "Never unattended" would overstate it, and this comment said so until
// 2026-07-30. A Call-CQ run works answerers, and an ADR 0059 auto-work run works
// callers, until Abandon — with nobody at the desk. The one presence check that IS
// enforced is the open view: Service.onLingerExpired disarms TX and abandons any
// active QSO once the last /v1/ft8/events subscriber is gone past captureLinger.
// Closing the browser stops a run; walking away does not.
//
//   - TxKeyer is the PTT seam: the package keys the rig WITHOUT importing
//     internal/bridge (the bridge implements TxKeyer; injected like the capture
//     source). tx_on/tx_off are never exposed on the generic command path.
//   - TxController orchestrates one slot: encode → wait for the boundary → key →
//     play → unkey, with PTT guaranteed down on every return path.
//   - A completed exchange (73/RR73 actually transmitted — never merely queued)
//     is assembled by the pure BuildQso into a types.Qso and handed to an
//     injected sink (SetQsoLogger); submit, enrichment, and forwarding stay in
//     cmd/smd + qsoservice, so this package does NOT import qsoservice.
//     Narrow-daemon-scope holds by import graph; dependency injection gives the
//     one-way direction.
//
// # Invariants
//
// Fail-soft is load-bearing: a decode (or capture, or TX) failure logs and
// returns — it must never take down the daemon, the same principle as
// "enrichment never blocks logging". A bare decode is NOT a QSO; only a
// completed exchange is, and it is logged only after the final rung truly
// transmits.
//
// go-ft8 is licensed GPL-3.0-only (a WSJT-X/jt9 derivative); linking it is why
// Station Manager is GPL-3.0-only. See docs/licensing.md and ADR 0023.
package ft8
