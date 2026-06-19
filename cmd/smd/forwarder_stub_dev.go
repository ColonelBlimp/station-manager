//go:build dev

package main

// The "stub" forwarder is a fake destination that marks QSOs uploaded without
// sending anywhere — useful for exercising the worker / SSE / upload-status path
// locally without hitting QRZ. It is registered ONLY in dev builds (built with
// `-tags dev`: the dev Taskfile targets and the dogfood `deploy:local:dev`), so a
// shipped release binary cannot select it (review 2026-06-19 M3). Tests register
// the stub themselves; they don't depend on this file.
import _ "github.com/ColonelBlimp/station-manager/internal/forwarding/stub"
