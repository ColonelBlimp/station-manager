package server

// L10 — /v1/health DB-ping logging must be TRANSITION-ONLY (operator 2026-08-15): the
// endpoint previously logged EVERY failed probe at Warn, flooding under frequent
// monitoring. Now: one Warn on the unhealthy edge (with cause), repeated failures
// silenced at the default level (Debug), one recovery Info with elapsed duration.
// Concurrency-safe (probes overlap).

import (
	"bytes"
	"encoding/json"
	stderrors "errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeHealthClock struct{ t time.Time }

func (c *fakeHealthClock) now() time.Time { return c.t }

func slogByMsg(t *testing.T, buf *bytes.Buffer, msg string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		if m["msg"] == msg {
			out = append(out, m)
		}
	}
	return out
}

func TestDBHealthLog_TransitionsOnly(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	clk := &fakeHealthClock{t: time.Unix(1_000_000, 0)}
	h := &dbHealthLog{log: log, now: clk.now}

	h.fail(stderrors.New("connection refused")) // unhealthy edge
	h.fail(stderrors.New("connection refused")) // repeated → Debug, not a 2nd Warn
	clk.t = clk.t.Add(5 * time.Second)
	h.ok() // recovery edge

	edge := slogByMsg(t, &buf, "health: db ping failing")
	if len(edge) != 1 {
		t.Fatalf("unhealthy-edge Warn lines = %d, want 1 (transition-only); log:\n%s", len(edge), buf.String())
	}
	if edge[0]["level"] != "WARN" {
		t.Errorf("edge level = %v, want WARN", edge[0]["level"])
	}
	if edge[0]["err"] != "connection refused" {
		t.Errorf("edge missing the cause; err = %v", edge[0]["err"])
	}

	repeat := slogByMsg(t, &buf, "health: db ping still failing")
	if len(repeat) != 1 {
		t.Fatalf("repeated-failure lines = %d, want 1; log:\n%s", len(repeat), buf.String())
	}
	if repeat[0]["level"] != "DEBUG" {
		t.Errorf("repeated-failure level = %v, want DEBUG (silenced at default level)", repeat[0]["level"])
	}

	rec := slogByMsg(t, &buf, "health: db ping recovered")
	if len(rec) != 1 {
		t.Fatalf("recovery lines = %d, want 1; log:\n%s", len(rec), buf.String())
	}
	if rec[0]["level"] != "INFO" {
		t.Errorf("recovery level = %v, want INFO", rec[0]["level"])
	}
	if ms, _ := rec[0]["unhealthy_ms"].(float64); int64(ms) != 5000 {
		t.Errorf("unhealthy_ms = %v, want 5000", rec[0]["unhealthy_ms"])
	}
}

func TestDBHealthLog_ConcurrentProbes_NoRace(t *testing.T) {
	h := newDBHealthLog(slog.New(slog.NewJSONHandler(io.Discard, nil)))
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
