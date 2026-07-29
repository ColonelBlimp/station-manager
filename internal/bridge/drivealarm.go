package bridge

import "time"

// Drive-collapse detection — follow-up (1) of the 2026-07-29 meter arc.
//
// NOT YET IMPLEMENTED. The behaviour is pinned first in drivealarm_test.go,
// which carries the operator-approved acceptance criterion and the reasoning
// for each rule.

// driveSilenceTimeout is how long the meter stream may be silent inside a keyed
// transmission before the drive is called dead. THE OPERATOR'S NUMBER, not a
// derived one: the healthy stream measured ~12 Hz during FT8 TX (normal gaps
// ~80 ms) and the observed collapse left a ~10 s gap, so anything from ~1 s to
// ~3 s separates them; 3 s was chosen because it still leaves ~9 s of warning
// inside a 12.6 s slot.
//
// A var rather than a const so tests can shorten it — the same reason
// txConfirmTimeout is one.
var driveSilenceTimeout = 3 * time.Second
