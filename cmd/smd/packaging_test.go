package main

import (
	"os"
	"strings"
	"testing"
)

// The packaged systemd unit declares an absolute TimeoutStopSec backstop (20s per the LC-2 ruling)
// above the default 10s application shutdown budget, so a wedged teardown is always reaped by systemd
// even if the orchestrated drain overruns its own budget. (Moved from shutdown_test.go in ADR 0070
// phase 3c — this invariant is unrelated to the deleted gracefulShutdown.)
func TestSmdService_DeclaresTimeoutStopSecBackstop(t *testing.T) {
	b, err := os.ReadFile("../../packaging/smd.service")
	if err != nil {
		t.Fatalf("read smd.service: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "TimeoutStopSec=20") {
		t.Errorf("smd.service must declare TimeoutStopSec=20 (absolute systemd cap); got:\n%s", s)
	}
}
