//go:build logging_debug

package logging

import (
	"fmt"
	"runtime"
)

// debugEnabled reports whether logging debug tracking is compiled in.
// Used by helper.go to decide whether to capture the caller location
// of a new log event. Always true in this file; always false in
// debug_tracking_nop.go.
const debugEnabled = true

// trackLocation captures a file:line identifier for the calling code
// site (skipping the requested number of stack frames) and increments
// that site's counter in s.activeOpLocations. It returns the location
// string so the caller can store it on the constructed event for later
// decrement in finish().
//
// This is a debug-only helper compiled in with the `logging_debug`
// build tag. In the default (release) build it is a no-op that returns
// an empty string — see debug_tracking_nop.go.
func trackLocation(s *Service, skip int) string {
	if s == nil || !s.LoggingConfig.ShutdownTimeoutWarning {
		return ""
	}
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return ""
	}
	location := fmt.Sprintf("%s:%d", file, line)
	s.mu.Lock()
	if s.activeOpLocations == nil {
		s.activeOpLocations = make(map[string]int)
	}
	s.activeOpLocations[location]++
	s.mu.Unlock()
	return location
}

// untrackLocation decrements the counter for a previously-captured
// location. Called from logEvent.finish() when a tracked event
// completes. No-op in the release build.
func untrackLocation(s *Service, location string) {
	if s == nil || location == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeOpLocations == nil {
		return
	}
	s.activeOpLocations[location]--
	if s.activeOpLocations[location] <= 0 {
		delete(s.activeOpLocations, location)
	}
}

// snapshotLocations returns a copy of the current location counters for
// inclusion in shutdown-timeout warning log entries. Returns nil if the
// map is empty or if the build does not include debug tracking.
func snapshotLocations(s *Service) map[string]int {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.activeOpLocations) == 0 {
		return nil
	}
	out := make(map[string]int, len(s.activeOpLocations))
	for k, v := range s.activeOpLocations {
		out[k] = v
	}
	return out
}
