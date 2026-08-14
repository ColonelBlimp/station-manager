package bridge

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

func clLines(s string, needle string) []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		if l != "" && strings.Contains(l, needle) {
			out = append(out, l)
		}
	}
	return out
}

// newDetCommandLog builds a commandLog with a fake clock and a window long enough that
// the timer never fires during the test — flush() is driven explicitly for
// determinism. Returns the log, its buffer, and a pointer to advance the clock.
func newDetCommandLog(buf *bytes.Buffer, clk *time.Time) *commandLog {
	c := newCommandLog(logging.NewForWriter(buf), time.Hour)
	c.now = func() time.Time { return *clk }
	return c
}

func applied(opID, proto, op, val string) commandOutcome {
	return commandOutcome{opID: opID, protocol: proto, ops: []string{op}, values: []string{val}, batch: 1, applied: 1, failedIdx: -1}
}

// L4: a non-coalesce op logs its outcome immediately with op-id, protocol, op, value,
// batch and applied — the default-visible record the operator can tell "written" by.
func TestCommandLog_ImmediateOutcome_Applied(t *testing.T) {
	var buf bytes.Buffer
	clk := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	c := newDetCommandLog(&buf, &clk)

	c.record(applied("op1", "yaesu", "set_mode", "USB"))

	got := clLines(buf.String(), "rig command applied")
	if len(got) != 1 {
		t.Fatalf("want one immediate outcome, got %d: %q", len(got), buf.String())
	}
	for _, want := range []string{`"op_id":"op1"`, `"protocol":"yaesu"`, `"ops":["set_mode"]`, `"values":["USB"]`, `"batch":1`, `"applied":1`} {
		if !strings.Contains(got[0], want) {
			t.Errorf("outcome missing %s: %s", want, got[0])
		}
	}
	// A non-coalesce op must NOT be buffered as a run.
	if strings.Contains(buf.String(), "coalesced") {
		t.Errorf("a non-coalesce op must not be coalesced: %s", buf.String())
	}
}

// L4 confusable state: a partially-applied batch is distinguishable from a full one —
// it warns and names the applied count, the failed index and the failed op.
func TestCommandLog_PartialApply_Warns(t *testing.T) {
	var buf bytes.Buffer
	clk := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	c := newDetCommandLog(&buf, &clk)

	c.record(commandOutcome{
		opID: "op2", protocol: "icom_civ",
		ops: []string{"set_freq", "set_mode"}, values: []string{"14074000", "USB"},
		batch: 2, applied: 1, failedIdx: 1, failedOp: "set_mode",
	})

	got := clLines(buf.String(), "partially applied")
	if len(got) != 1 || !strings.Contains(got[0], `"level":"warn"`) {
		t.Fatalf("a partial apply must warn: %q", buf.String())
	}
	for _, want := range []string{`"applied":1`, `"batch":2`, `"failed_index":1`, `"failed_op":"set_mode"`} {
		if !strings.Contains(got[0], want) {
			t.Errorf("partial outcome missing %s: %s", want, got[0])
		}
	}
}

// L4 flood-control: rapid identical freq-steps are coalesced into ONE summary carrying
// the count, first and last value, and the run duration — not one line per step.
func TestCommandLog_CoalescesFreqSteps(t *testing.T) {
	var buf bytes.Buffer
	clk := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	c := newDetCommandLog(&buf, &clk)

	ids := []string{"b-1", "b-2", "b-3", "b-4", "b-5"}
	vals := []string{"14074000", "14074100", "14074200", "14074300", "14074400"}
	for i := range vals {
		c.record(applied(ids[i], "icom_civ", "set_freq", vals[i]))
		if buf.Len() != 0 {
			t.Fatalf("step %d must not log until flush: %q", i, buf.String())
		}
	}
	clk = clk.Add(600 * time.Millisecond)
	c.flush()

	got := clLines(buf.String(), "coalesced VFO step")
	if len(got) != 1 {
		t.Fatalf("want one coalesced summary, got %d: %q", len(got), buf.String())
	}
	for _, want := range []string{
		`"op":"set_freq"`, `"count":5`, `"first_value":"14074000"`, `"last_value":"14074400"`, `"duration_ms":600`,
		// P1: EVERY coalesced request's op-id is carried, so any one is findable.
		`"op_ids":["b-1","b-2","b-3","b-4","b-5"]`,
	} {
		if !strings.Contains(got[0], want) {
			t.Errorf("summary missing %s: %s", want, got[0])
		}
	}
}

// L4 review P2: a continuous freq-step stream (which never hits the quiet-window flush,
// since the timer resets each step) must not grow the run without limit. At the cap the
// run flushes a chunk and a fresh run opens — bounding memory + log-line size while
// every op-id still lands in exactly one summary.
func TestCommandLog_CapsRunLength(t *testing.T) {
	old := commandCoalesceMaxRun
	commandCoalesceMaxRun = 3
	defer func() { commandCoalesceMaxRun = old }()

	var buf bytes.Buffer
	clk := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	c := newDetCommandLog(&buf, &clk)

	for i := 1; i <= 7; i++ { // continuous run of 7, cap 3
		c.record(applied(fmt.Sprintf("b-%d", i), "icom_civ", "set_freq", fmt.Sprintf("140740%02d", i)))
	}
	// Chunks flush automatically at 3 and 6; the 7th stays pending until flush.
	if got := clLines(buf.String(), "coalesced VFO step"); len(got) != 2 {
		t.Fatalf("cap 3 over 7 steps must auto-flush 2 chunks, got %d: %q", len(got), buf.String())
	}
	c.flush()
	got := clLines(buf.String(), "coalesced VFO step")
	if len(got) != 3 {
		t.Fatalf("want 3 chunks after the final flush, got %d: %q", len(got), buf.String())
	}
	// Coverage + bounding: ids partition into ≤cap chunks, none lost or duplicated.
	if !strings.Contains(got[0], `"op_ids":["b-1","b-2","b-3"]`) || !strings.Contains(got[0], `"count":3`) {
		t.Errorf("chunk 1 must carry ids b-1..b-3: %s", got[0])
	}
	if !strings.Contains(got[1], `"op_ids":["b-4","b-5","b-6"]`) {
		t.Errorf("chunk 2 must carry ids b-4..b-6: %s", got[1])
	}
	if !strings.Contains(got[2], `"op_ids":["b-7"]`) || !strings.Contains(got[2], `"count":1`) {
		t.Errorf("final chunk must carry id b-7: %s", got[2])
	}
}

// L4 P2: op-ids carry a per-boot random prefix + an incrementing counter, so they are
// unique within a session and do not collide with a prior session's ids after a
// restart (a bare counter would reset to 1 and reuse ids).
func TestNextOpID_BootPrefixedAndOrdered(t *testing.T) {
	s, _ := newCommandTestService(t)
	a, b := s.nextOpID(), s.nextOpID()
	if a == b {
		t.Fatalf("op-ids must be unique within a session: %q %q", a, b)
	}
	pa, na, okA := strings.Cut(a, "-")
	pb, nb, okB := strings.Cut(b, "-")
	if !okA || !okB || pa == "" || pa != pb {
		t.Fatalf("op-ids must carry a stable, non-empty boot prefix: %q %q", a, b)
	}
	if na != "1" || nb != "2" {
		t.Fatalf("the counter must increment after the prefix: %q %q", a, b)
	}
}

// A different op flushes the pending run BEFORE it logs, so the coalesced summary and
// the new outcome stay in chronological order.
func TestCommandLog_DifferentOpFlushesRunInOrder(t *testing.T) {
	var buf bytes.Buffer
	clk := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	c := newDetCommandLog(&buf, &clk)

	c.record(applied("s", "yaesu", "set_freq", "14074000"))
	c.record(applied("s", "yaesu", "set_freq", "14074100"))
	c.record(applied("s", "yaesu", "set_freq", "14074200"))
	c.record(applied("modeop", "yaesu", "set_mode", "USB")) // different op → flush then log

	out := buf.String()
	summaryAt := strings.Index(out, "coalesced VFO step")
	modeAt := strings.Index(out, `"ops":["set_mode"]`)
	if summaryAt < 0 || modeAt < 0 {
		t.Fatalf("want both the freq summary and the mode outcome: %q", out)
	}
	if summaryAt > modeAt {
		t.Fatalf("coalesced summary must be logged BEFORE the following op: %q", out)
	}
	if !strings.Contains(clLines(out, "coalesced VFO step")[0], `"count":3`) {
		t.Errorf("run must have counted 3 steps: %s", out)
	}
}

// A failure never coalesces: it flushes any pending run then warns immediately.
func TestCommandLog_FailureFlushesPendingRun(t *testing.T) {
	var buf bytes.Buffer
	clk := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	c := newDetCommandLog(&buf, &clk)

	c.record(applied("s", "icom_civ", "set_freq", "14074000"))
	c.record(applied("s", "icom_civ", "set_freq", "14074100"))
	c.record(commandOutcome{ // a set_freq that FAILED at the wire
		opID: "fail", protocol: "icom_civ", ops: []string{"set_freq"}, values: []string{"99999999"},
		batch: 1, applied: 0, failedIdx: 0, failedOp: "set_freq",
	})

	if got := clLines(buf.String(), "coalesced VFO step"); len(got) != 1 || !strings.Contains(got[0], `"count":2`) {
		t.Fatalf("the pending run must flush (count 2) before the failure: %q", buf.String())
	}
	if got := clLines(buf.String(), "partially applied"); len(got) != 1 || !strings.Contains(got[0], `"applied":0`) {
		t.Fatalf("the failed step must warn with applied 0: %q", buf.String())
	}
}

// The quiet-window timer flushes a run with no explicit flush and no following op —
// otherwise a trailing step run would never surface.
func TestCommandLog_TimerAutoFlushes(t *testing.T) {
	sb := &syncBuf{}
	c := newCommandLog(logging.NewForWriter(sb), 20*time.Millisecond)

	c.record(applied("t", "yaesu", "set_freq", "14074000"))
	c.record(applied("t", "yaesu", "set_freq", "14074100"))

	deadline := time.Now().Add(2 * time.Second)
	for len(clLines(sb.String(), "coalesced VFO step")) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("timer did not auto-flush the run: %q", sb.String())
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !strings.Contains(clLines(sb.String(), "coalesced VFO step")[0], `"count":2`) {
		t.Errorf("auto-flushed run must count 2: %s", sb.String())
	}
}
