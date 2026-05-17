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
// subsystem, not a library. The implementation is built from the
// FT8 protocol specification (Franke / Somerville / Taylor, "The
// FT4 and FT8 Communication Protocols," QEX July/August 2020 —
// https://wsjt.sourceforge.io/FT4_FT8_QEX.pdf), NOT translated
// from the GPL WSJT-X Fortran source tree, which is off-limits as
// implementation reference per ADR 0021's Licensing constraint
// (GPL v3 vs MIT incompatibility). WSJT-X binaries (`jt9`,
// `ft8sim`) are used only as black-box parity oracles during
// corpus prep — never at runtime, never at test time. Goal: same
// or better decode results than WSJT-X under the same conditions.
//
// v1 status (2026-05-16): SCAFFOLD ONLY. The Service has the right
// lifecycle shape (Initialize / Start / Stop, all are idempotent), the
// boundary tests defend the import graph (mirrors internal/bridge's
// pattern), and DI wiring is in cmd/smd. No decode pipeline yet —
// that lands as spec-implementation commits over subsequent
// sessions, per the M4 milestone breakdown in
// docs/v2-design/milestones.md. Enabled defaults to false in
// config; the subsystem stays dormant until the implementation
// lands and the operator opts in.
//
// Package boundary discipline (ADR 0021 inherits ADR 0013): the
// storage package (internal/database/sqlite), the forwarder package
// (internal/forwarding), and internal/bridge MUST NOT import
// internal/ft8 — FT8 is a sibling subsystem of bridge, both
// dependents on cat/serial. Conversely, internal/ft8 may import
// internal/errors, internal/logging, internal/types, internal/cat,
// internal/serial, and internal/qsoservice (FT8 is a CONSUMER of
// qsoservice — it submits decoded QSOs through it). The boundary
// is enforced by static-import tests in boundary_test.go; CI
// catches violations.
//
// Lifetime: constructed by cmd/smd, started after Initialize,
// drained on Stop. See Service for the full lifecycle contract.
package ft8
