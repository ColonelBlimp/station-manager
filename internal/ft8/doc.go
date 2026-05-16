// Package ft8 is the daemon's FT8 (and related digital-mode)
// subsystem (per ADR 0021). It runs in-process inside `cmd/smd`,
// owns the audio-capture-to-decode pipeline, and integrates with
// the rest of the daemon for QSO logging, rig steering, error
// handling, and structured logging.
//
// ADR 0021 reversed the prior extraction of FT8 to a separate Go
// library. The integration coupling (logging, errors, config, CAT,
// qsoservice) is dense enough that "library that depends on a
// specific app's internals" was the wrong shape — it's a misplaced
// subsystem, not a library. The WSJT-X v.3.0.0.1 fork at
// /home/mveary/Development/wsjtx is the Fortran specification; the
// porting philosophy is "exact port first, then layered
// improvements once decode parity is established." Goal: same or
// better results than WSJT-X.
//
// v1 status (2026-05-16): SCAFFOLD ONLY. The Service has the right
// lifecycle shape (Initialize / Start / Stop, all idempotent), the
// boundary tests defend the import graph (mirrors internal/bridge's
// pattern), and DI wiring is in cmd/smd. No decode pipeline yet —
// that lands as Fortran-port work commits over subsequent sessions.
// Enabled defaults to false in config; the subsystem stays dormant
// until the implementation lands and the operator opts in.
//
// Package boundary discipline (ADR 0021 inherits ADR 0013): the
// storage package (internal/database/sqlite), the forwarder package
// (internal/forwarding), and internal/bridge MUST NOT import
// internal/ft8 — FT8 is a sibling subsystem of bridge, both
// dependents on cat/serial. Conversely internal/ft8 may import
// internal/errors, internal/logging, internal/types, internal/cat,
// internal/serial, and internal/qsoservice (FT8 is a CONSUMER of
// qsoservice — it submits decoded QSOs through it). The boundary
// is enforced by static-import tests in boundary_test.go; CI
// catches violations.
//
// Lifetime: constructed by cmd/smd, started after Initialize,
// drained on Stop. See Service for the full lifecycle contract.
package ft8
