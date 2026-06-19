// Package bridge is the daemon's serial/CAT bridge subsystem (per
// ADR 0013 + ADR 0019). It owns the serial-port connection to the
// rig, decodes AUTO-mode CAT pushes via internal/cat, and fans the
// resulting state events out to SPA subscribers via SSE on
// /v1/rig/events.
//
// State display is read-only and (almost) stateless: the rig pushes,
// the bridge decodes + filters + forwards, the SPA displays. But the
// subsystem is no longer read-only overall — two narrow write paths
// have since landed:
//
//   - Inbound commands (ADR 0026): POST /v1/rig/command drives freq /
//     mode / VFO / band via data-driven `cat` commands. SendCommands
//     gates on a command's Exposed flag, so only commands the rigdef
//     explicitly exposes are reachable from the HTTP surface.
//   - Tune-carrier control (ADR 0027): POST /v1/rig/tune keys a
//     reduced-power RTTY carrier for tuning an external amp — the first
//     feature that transmits. The daemon owns a guaranteed stop (hard
//     auto-off + release-on-disconnect + single-flight) and snapshots /
//     restores mode+power. The TX-keying commands (tx_on/tx_off) are
//     deliberately NOT Exposed, so only the tune controller can key TX.
//
// Both write paths are gated on rig-identity verification: SendCommands
// and StartTune refuse (ErrRigIdentityUnverified) until the rig has
// positively identified as the configured driver, and a definite
// identity mismatch halts the pipeline outright — so a wrong / unverified
// rig can never be commanded or keyed (review 2026-06-04 H2).
//
// The tune controller's mode+power restore snapshot is the one scoped
// exception to "no persistent rig-state cache" (alongside the hub's
// one-slot event-replay caches). See ADR 0019 for the original v1
// design. Since then the bridge has gained an inbound command path
// (`/v1/rig/command`, ADR 0026) and two daemon-owned keyed-transmission
// features — the tune carrier (ADR 0027) and FT8 TX keying (this
// package's ft8tx.go, ADR 0030). Still NOT in v1: phone/CW
// PTT-for-operating (QSO keying), rigctld-compat TCP, and multi-rig
// hardware.
//
// Package boundary discipline (ADR 0013): the storage package
// (internal/database/sqlite), the forwarder package
// (internal/forwarding), and the QSO log-write orchestration
// package (internal/qsoservice) MUST NOT import internal/bridge,
// and vice versa. The boundary is enforced by a static-import test
// in this package's test files (boundary_test.go); CI catches
// violations.
//
// Lifetime: constructed by cmd/smd, started after Initialize, drained
// on Stop. The Service exposes Subscribe() for SSE handlers and
// Enabled() for callers that need to gate on whether the subsystem is
// actually running. See Service for the full lifecycle contract.
package bridge
