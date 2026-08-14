package bridge

import (
	"sync"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// commandCoalesceWindow is the quiet window after which a run of rapid identical
// freq-steps is summarised (operator decision 2026-08-14: 1 s). Package var so tests
// can dial it.
var commandCoalesceWindow = time.Second

// isCoalesceOp reports the rapid VFO-step ops whose per-command outcome is coalesced
// into ONE summary rather than one Info line per key-repeat (operator decision
// 2026-08-14). It is an ALLOW-LIST, not a deny-list: a new op logs immediately
// (default-visible) until it is deliberately added here, so a future high-frequency
// op fails safe (verbose) rather than silently swallowed.
func isCoalesceOp(op string) bool {
	switch op {
	case "set_freq", "set_freq_b":
		return true
	}
	return false
}

// commandOutcome is the structured record of one SendCommands call that reached the
// wire (both protocols). applied == batch on full success; on a mid-batch CI-V
// failure applied < batch and failedIndex/failedOp name where it stopped. failedIndex
// is -1 when the whole batch applied.
type commandOutcome struct {
	opID      string
	protocol  string
	ops       []string
	values    []string
	batch     int
	applied   int
	failedIdx int
	failedOp  string
}

// commandLog owns L4 rig-command outcome logging at the durable bridge boundary.
// Rapid identical single-op freq-steps are coalesced into one Info summary after a
// quiet window; a DIFFERENT op or a FAILURE flushes any pending run immediately
// (preserving order), and everything non-coalescable logs its outcome at once. It is
// pure logging: record() never blocks the command path (a brief mutex + at most a
// timer re-arm), and the async flush touches nothing but the logger.
type commandLog struct {
	log    logging.Logger
	now    func() time.Time
	window time.Duration

	mu    sync.Mutex
	run   *coalRun
	timer *time.Timer
}

// coalRun is an in-progress coalesced run of one repeated freq-step op.
type coalRun struct {
	opID     string
	protocol string
	op       string
	first    string
	last     string
	count    int
	start    time.Time
}

func newCommandLog(log logging.Logger, window time.Duration) *commandLog {
	if log == nil {
		log = logging.Noop()
	}
	return &commandLog{log: log, now: time.Now, window: window}
}

// record logs one command outcome. A fully-applied single coalesce-op extends (or
// opens) a coalesced run; anything else — a batch, a non-coalesce op, or any failure —
// flushes a pending run first (so the log stays chronological) then logs immediately.
func (c *commandLog) record(o commandOutcome) {
	c.mu.Lock()
	defer c.mu.Unlock()
	coalescable := o.failedIdx < 0 && o.batch == 1 && o.applied == 1 && isCoalesceOp(o.ops[0])
	if !coalescable {
		c.flushLocked()
		c.logImmediateLocked(o)
		return
	}
	c.mergeLocked(o)
}

func (c *commandLog) mergeLocked(o commandOutcome) {
	op, val := o.ops[0], o.values[0]
	if c.run != nil && c.run.op == op {
		c.run.last = val
		c.run.count++
	} else {
		c.flushLocked() // a different coalesce-op ends the previous run
		c.run = &coalRun{
			opID: o.opID, protocol: o.protocol, op: op,
			first: val, last: val, count: 1, start: c.now(),
		}
	}
	if c.timer != nil {
		c.timer.Stop()
	}
	c.timer = time.AfterFunc(c.window, c.flush)
}

// flush emits any pending coalesced run. Safe to call unconditionally (no-op when no
// run is open); called by the quiet-window timer, by record() before an immediate
// log, and on bridge shutdown.
func (c *commandLog) flush() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flushLocked()
}

func (c *commandLog) flushLocked() {
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	if c.run == nil {
		return
	}
	r := c.run
	c.run = nil
	c.log.InfoWith().
		Str("op_id", r.opID).
		Str("protocol", r.protocol).
		Str("op", r.op).
		Int("count", r.count).
		Str("first_value", r.first).
		Str("last_value", r.last).
		Int64("duration_ms", int64(c.now().Sub(r.start)/time.Millisecond)).
		Msg("bridge: rig command applied (coalesced VFO step)")
}

func (c *commandLog) logImmediateLocked(o commandOutcome) {
	ev := c.log.InfoWith()
	msg := "bridge: rig command applied"
	if o.failedIdx >= 0 {
		ev = c.log.WarnWith()
		msg = "bridge: rig command partially applied"
	}
	ev = ev.
		Str("op_id", o.opID).
		Str("protocol", o.protocol).
		Strs("ops", o.ops).
		Strs("values", o.values).
		Int("batch", o.batch).
		Int("applied", o.applied)
	if o.failedIdx >= 0 {
		ev = ev.Int("failed_index", o.failedIdx).Str("failed_op", o.failedOp)
	}
	ev.Msg(msg)
}
