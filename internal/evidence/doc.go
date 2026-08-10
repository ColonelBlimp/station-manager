// Package evidence implements the local half of the FT8 evidence archive —
// design §4.1 in docs/v2-design/spot-network/spot-network-design.md (first
// capture slice, operator decisions 2026-08-10).
//
// Why a separate SQLite file: SQLite WAL permits one writer per database,
// and the log database's writer serves the QSO commit — the sacred path. A
// shared file could only promise "evidence housekeeping never delays a QSO"
// by discipline; evidence.db promises it by structure. The package-import
// graph enforces the same separation upward: internal/ft8 never imports this
// package (cmd/smd injects an adapter, the QSO-logger pattern), and this
// package never imports internal/ft8 — go-ft8 is the shared vocabulary.
//
// What is stored: the RICH decode stream — every parse status, own
// transmissions included, with payload, provenance, metrics and decoder
// build — plus one coverage row per physical slot (what makes an empty
// stretch of archive interpretable) and loss intervals (what makes a missing
// stretch honest). profile_uuid is NULL this slice: "no profile was
// recorded", never "pending" (§5.4 amendment); profiles ship as their own
// slice before sync does.
//
// Bounded by construction: capture is a default-off consent layer (§8); the
// writer is a bounded non-blocking queue that drops and counts rather than
// ever stalling the decode loop; and disk use is capped physically
// (evidence.db + WAL + shm) with drop-new at a soft watermark below the hard
// cap — reserved headroom covers WAL/checkpoint churn and the one coalesced
// loss-interval row that records the dropping itself. No old evidence is
// ever deleted by this slice; the purge/acked-first machinery arrives with
// sync.
package evidence
