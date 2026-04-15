//go:build !logging_debug

package logging

// debugEnabled reports whether logging debug tracking is compiled in.
// Always false in this file; always true when the `logging_debug` build
// tag is set (see debug_tracking.go).
//
// This pair of files exists so the activeOpLocations feature (which
// captures runtime.Caller info and updates a shared map under a mutex
// on every log event when ShutdownTimeoutWarning is enabled) does not
// impose any overhead on release builds. See docs/reviews/internal-logging.md
// finding 4.3 for the rationale and the contention issue the previous
// always-compiled version suffered from.
const debugEnabled = false

// trackLocation is a release-build no-op. Always returns "".
func trackLocation(_ *Service, _ int) string { return "" }

// untrackLocation is a release-build no-op.
func untrackLocation(_ *Service, _ string) {}

// snapshotLocations is a release-build no-op. Always returns nil.
func snapshotLocations(_ *Service) map[string]int { return nil }
