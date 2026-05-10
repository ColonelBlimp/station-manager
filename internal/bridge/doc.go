// Package bridge is the daemon's serial/CAT bridge subsystem (per
// ADR 0013 + ADR 0019). It owns the serial-port connection to the
// rig, decodes AUTO-mode CAT pushes via internal/cat, and fans the
// resulting state events out to SPA subscribers via SSE on
// /v1/rig/events.
//
// The v1 shape is intentionally narrow: read-only, stateless filter,
// one frontend (SSE), no PTT awareness, no inbound command path. See
// ADR 0019 for the full v1 design + the trigger-list for what would
// expand it (FT8 stack, click-to-tune from SPA, third-party app
// needing rigctld-compat TCP, multi-rig hardware, etc.).
//
// Package boundary discipline (ADR 0013): internal/storage and
// internal/forwarder MUST NOT import internal/bridge, and vice
// versa. The boundary is enforced by a static-import test in this
// package's test files; CI catches violations.
//
// Lifetime: constructed by cmd/smd, started after Initialize, drained
// on Stop. The Service exposes Subscribe() for SSE handlers and
// Enabled() for callers that need to gate on whether the subsystem is
// actually running. See Service for the full lifecycle contract.
package bridge
