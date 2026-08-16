package main

import (
	"context"
	"sync"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// teardownDeps holds the graceful-shutdown teardown operations as injectable
// values so the ordered, budget-bounded teardown (LC-2) can be exercised with
// blocking seams. run() builds this from the live services; tests build it from
// stubs that block or record.
//
// Method values are captured directly (bridgeSvc.Stop, hub.Close, ...); the
// WaitGroups are shared with run() and drained here.
type teardownDeps struct {
	log    *logging.Service
	budget time.Duration // cfg.Server.ShutdownTimeoutSec, as a Duration (0 ⇒ 10s floor)

	stopBridge   func() error                // RF-unkey path — runs FIRST, with the full budget
	stopFt8      func() error                // prerequisite for evidence, the FT8 QSO-log drain, and hub close
	stopPsk      func() error                // independent best-effort flush
	stopEvidence func()                      // depends on ft8 (the decode loop is its only producer)
	shutdownHTTP func(context.Context) error // graceful HTTP drain — independent, ctx-bounded
	workerWG     *sync.WaitGroup             // forwarder workers (cancelled before the budget starts)
	qsoLogWG     *sync.WaitGroup             // FT8 completed-QSO log goroutines — launched by the ft8 decode loop
	closeHub     func()                      // daemon events hub — unsafe to close while any publisher is live
}

// shutdownCoord bounds every teardown stage by ONE shared budget (LC-2). The
// budget is a single context.WithTimeout started right after the accept listener
// is closed; each stage runs against it. The FIRST stage still running when the
// budget expires is named in a SINGLE structured warning (later generic deadline
// errors are suppressed). Dependent stages whose prerequisite did not stop are
// recorded as skipped and NOT attempted — because attempting them is the actual
// hazard: closing the evidence archive under a live producer, Wait()ing a
// WaitGroup an ft8 goroutine may still Add() to, or closing hub channels a live
// publisher may still send on, are races / panics, not merely untidy.
type shutdownCoord struct {
	log          *logging.Service
	ctx          context.Context
	start        time.Time
	budget       time.Duration
	expired      bool   // latched: emit the deadline warning exactly once
	expiredStage string // the stage that exhausted the budget (set with expired)
}

// run launches fn in a goroutine and bounds it by the shared budget. The done
// channel is buffered so fn can complete and its goroutine exit cleanly even
// after run() has stopped waiting (a stage that overran the budget is left to
// finish against the exiting process). It returns true iff fn completed before
// the budget expired.
func (sc *shutdownCoord) run(stage string, fn func()) bool {
	stageStart := time.Now()
	done := make(chan struct{}, 1)
	go func() {
		fn()
		done <- struct{}{}
	}()
	select {
	case <-done:
		return true
	case <-sc.ctx.Done():
		// Both channels can be ready at once (Go selects at random); a stage that
		// finished in the same instant the budget expired completed — prefer that.
		select {
		case <-done:
			return true
		default:
		}
		sc.reportExpiry(stage, time.Since(stageStart))
		return false
	}
}

// reportExpiry emits the ONE deadline warning, for the stage active when the
// budget ran out. Subsequent expiries are silent — a stage that starts after the
// budget is already spent returns immediately without re-logging a generic
// timeout error.
func (sc *shutdownCoord) reportExpiry(stage string, stageElapsed time.Duration) {
	if sc.expired {
		return
	}
	sc.expired = true
	sc.expiredStage = stage
	sc.log.WarnWith().
		Str("stage", stage).
		Dur("stage_elapsed", stageElapsed).
		Dur("total_elapsed", time.Since(sc.start)).
		Dur("budget", sc.budget).
		Msg("graceful shutdown exceeded budget; remaining dependent teardown abandoned")
}

// skip records a dependent stage that was not attempted because its prerequisite
// did not stop within the budget.
func (sc *shutdownCoord) skip(stage, prerequisite string) {
	sc.log.WarnWith().
		Str("stage", stage).
		Str("prerequisite", prerequisite).
		Msg("graceful shutdown stage skipped; prerequisite did not stop")
}

// gracefulShutdown runs the ordered, budget-bounded teardown (LC-2). It MUST be
// called after the accept listener is closed (server.StopAccepting) and the
// forwarder workers cancelled (workerCancel); it owns everything from budget
// creation onward. The invariant it enforces: after the accept gate closes,
// either every ordered stage completes or the daemon records the stage that
// exhausted the one configured grace period — and the initial RF-unkey attempt
// is never skipped by a timeout.
func gracefulShutdown(d teardownDeps) {
	budget := d.budget
	if budget <= 0 {
		// A hand-edited config.json with server.shutdown_timeout_sec at 0 must not
		// make the budget fire immediately. Match config.applyDefaults' 10s floor.
		budget = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	sc := &shutdownCoord{log: d.log, ctx: ctx, start: time.Now(), budget: budget}

	// 1. Bridge FIRST — this drives the guaranteed-stop RF unkey. Running it as the
	//    first stage, before anything else can consume the budget, is what
	//    guarantees the unkey is attempted under budget pressure (LC-2 ruling: no
	//    separate RF reserve; bridge-first with the full budget is sufficient).
	sc.run("bridge", func() {
		if err := d.stopBridge(); err != nil {
			d.log.ErrorWith().Err(err).Msg("bridge: Stop error")
		}
	})

	// 2. FT8 — its decode loop is the sole producer into the evidence writer, the
	//    launcher of the FT8 QSO-log goroutines, and a publisher into the hub, so it
	//    is the prerequisite for stages 4, 7 and 8.
	ft8Stopped := sc.run("ft8", func() {
		if err := d.stopFt8(); err != nil {
			d.log.ErrorWith().Err(err).Msg("ft8: Stop error")
		}
	})

	// 3. PSK Reporter — independent best-effort final flush.
	sc.run("pskreporter", func() {
		if err := d.stopPsk(); err != nil {
			d.log.ErrorWith().Err(err).Msg("pskreporter: Stop error")
		}
	})

	// 4. Evidence writer — DEPENDS on ft8: the decode loop is its only producer, so
	//    closing the archive while ft8 still runs races the writer. Skip (do not
	//    attempt) if ft8 hung.
	if ft8Stopped {
		sc.run("evidence", d.stopEvidence)
	} else {
		sc.skip("evidence", "ft8")
	}

	// 5. HTTP graceful shutdown — independent; signalled even under budget pressure
	//    (Shutdown returns promptly once the shared ctx expires).
	sc.run("http", func() {
		if err := d.shutdownHTTP(ctx); err != nil {
			d.log.ErrorWith().Err(err).Msg("HTTP server shutdown error")
		}
	})

	// 6. Forwarder-worker drain — independent (workers were cancelled before the
	//    budget started). They publish status into the hub, so they gate hub close.
	sc.run("forwarder-workers", d.workerWG.Wait)

	// 7. FT8 completed-QSO log drain — DEPENDS on ft8: while the decode loop runs it
	//    can still Add() to this WaitGroup, and Wait() racing Add() panics. Skip if
	//    ft8 hung; those goroutines' late Submits error safely against the closing DB.
	if ft8Stopped {
		sc.run("ft8-qso-log", d.qsoLogWG.Wait)
	} else {
		sc.skip("ft8-qso-log", "ft8")
	}

	// 8. Hub close — DEPENDS on every publisher having stopped: HTTP handlers
	//    (qsoservice) drained under stage 5, forwarder workers under stage 6, and the
	//    FT8 QSO loggers under stage 7. Closing subscriber channels while any is still
	//    live is a send-on-closed-channel panic. If the budget never expired, every
	//    stage above completed within it (a stage that overran WOULD have tripped the
	//    deadline), so all publishers are drained and the hub is safe to close. If the
	//    budget expired at all, some publisher may still be live: leave the hub open
	//    (the exiting process reclaims it) and record the skip against the stage that
	//    exhausted the budget.
	if sc.expired {
		sc.skip("hub", sc.expiredStage)
	} else {
		sc.run("hub", d.closeHub)
	}
}

// safetyNetStop is the error-path teardown net for a subsystem whose Stop is also
// driven by gracefulShutdown on the happy path. It runs stop() ONLY when graceful
// teardown did not (gracefulDone == false) — i.e. an early error return that never
// reached gracefulShutdown.
//
// On the happy path gracefulShutdown already owns the Stop, so re-calling it from a
// defer is not merely redundant: a Stop that gracefulShutdown ABANDONED at the
// budget (bridge/ft8's sync.Once body still in-flight so <-stopDone never returns,
// or psk's WaitGroup still draining) would re-block on that same wedged operation
// when run() returns, re-introducing the unbounded hang LC-2 removed (codex P1 on
// d8e0eee9). Idempotency does not save us — the second caller blocks on the shared
// completion signal, it does not no-op.
func safetyNetStop(gracefulDone bool, stop func() error) error {
	if gracefulDone {
		return nil
	}
	return stop()
}
