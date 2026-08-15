package api

// L10 — /v1/healthz DB-ping logging must be TRANSITION-ONLY (operator 2026-08-15): the
// endpoint previously DISCARDED the ping cause. Now: one Warn on the unhealthy edge
// carrying the cause, repeated failures silenced at the default level (Debug), one
// recovery Info with the elapsed unhealthy duration. Concurrency-safe (probes overlap).

import (
	stderrors "errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

type fakeHealthClock struct{ t time.Time }

func (c *fakeHealthClock) now() time.Time { return c.t }

func TestDBHealthLog_TransitionsOnly(t *testing.T) {
	var buf strings.Builder
	clk := &fakeHealthClock{t: time.Unix(1_000_000, 0)}
	h := &dbHealthLog{logger: logging.NewForWriter(&buf), now: clk.now}

	h.fail(stderrors.New("disk I/O error")) // unhealthy edge
	h.fail(stderrors.New("disk I/O error")) // repeated → Debug, not a 2nd Warn
	clk.t = clk.t.Add(5 * time.Second)
	h.ok() // recovery edge

	// Exactly ONE unhealthy-edge Warn, carrying the cause.
	edge := credWarnRecords(t, &buf, "database ping is failing")
	if len(edge) != 1 {
		t.Fatalf("unhealthy-edge Warn lines = %d, want 1 (transition-only); log:\n%s", len(edge), buf.String())
	}
	if lvl, _ := edge[0]["level"].(string); lvl != "warn" {
		t.Errorf("edge level = %q, want warn", lvl)
	}
	if !strings.Contains(buf.String(), "disk I/O error") {
		t.Errorf("unhealthy edge missing the ping cause; log:\n%s", buf.String())
	}

	// The repeated failure is Debug (silenced at the default level).
	rep := credWarnRecords(t, &buf, "still failing")
	if len(rep) != 1 {
		t.Fatalf("repeated-failure lines = %d, want 1; log:\n%s", len(rep), buf.String())
	}
	if lvl, _ := rep[0]["level"].(string); lvl != "debug" {
		t.Errorf("repeated-failure level = %q, want debug (silenced at default level)", lvl)
	}

	// Exactly ONE recovery Info carrying the elapsed unhealthy duration.
	rec := credWarnRecords(t, &buf, "ping recovered")
	if len(rec) != 1 {
		t.Fatalf("recovery lines = %d, want 1; log:\n%s", len(rec), buf.String())
	}
	if lvl, _ := rec[0]["level"].(string); lvl != "info" {
		t.Errorf("recovery level = %q, want info", lvl)
	}
	if ms, _ := rec[0]["unhealthy_ms"].(float64); int64(ms) != 5000 {
		t.Errorf("unhealthy_ms = %v, want 5000", rec[0]["unhealthy_ms"])
	}
}

func TestDBHealthLog_ConcurrentProbes_NoRace(t *testing.T) {
	h := newDBHealthLog(logging.NewForWriter(io.Discard))
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				h.fail(stderrors.New("x"))
			} else {
				h.ok()
			}
		}(i)
	}
	wg.Wait() // the -race detector is the assertion: overlapping probes must not race
}
