package main

// ADR 0070 phase 3b — formatting the orchestrated shutdown.
//
// orchestrator.Shutdown is logging-free: it calls an observer with each NodeOutcome as it settles and
// returns the ordered ShutdownReport. cmd/smd supplies the observer (shutdownObserver) that emits the
// per-outcome records WHILE the logger is still open — the exceptional records only, preserved
// verbatim from the pre-orchestrator gracefulShutdown so operator log parsing / alerts are unchanged.
//
// The logging node drains LAST (DrainAfter every other node) and its Stop closes the logger, so its
// OWN outcome is handled separately, AFTER Shutdown returns (reportLoggingOutcome): a Failed/TimedOut
// logging close goes to stderr (the logger is gone), a Skipped logging (an earlier node did not drain,
// so the logger was deliberately left open) is noted through the still-open logger.

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/lifecycle/orchestrator"
)

// The shutdown log record messages, preserved verbatim from gracefulShutdown so existing operator
// monitoring keys off the same strings across the cutover.
const (
	msgShutdownExceededRecord = "graceful shutdown exceeded budget; remaining dependent teardown abandoned"
	msgShutdownSkippedRecord  = "graceful shutdown stage skipped; prerequisite did not stop"
)

// shutdownObserver returns the orchestrator outcome observer. It logs the exceptional records — a
// Failed node's Stop error, the ONE first-timeout budget warning, and each Skipped node naming its
// unmet prerequisites — the moment each settles, while the logger is still open. A Drained node logs
// nothing (matching gracefulShutdown's clean-run silence). It skips the logging node: that node's
// Stop is closing the logger, so its own outcome is reported by reportLoggingOutcome instead.
func (d *daemon) shutdownObserver() func(orchestrator.NodeOutcome) {
	budget := time.Duration(d.cfg.Server.ShutdownTimeoutSec) * time.Second
	if budget <= 0 {
		budget = 10 * time.Second
	}
	firstTimeout := true
	return func(oc orchestrator.NodeOutcome) {
		if oc.Node == nodeLogging {
			return // the logger is closing under this outcome; reportLoggingOutcome handles it
		}
		switch oc.Result {
		case orchestrator.Failed:
			d.logger.ErrorWith().Err(oc.Err).Str("stage", oc.Node).Msg("shutdown: node Stop error")
		case orchestrator.TimedOut:
			if firstTimeout {
				firstTimeout = false
				d.logger.WarnWith().Str("stage", oc.Node).Dur("budget", budget).
					Msg(msgShutdownExceededRecord)
			}
		case orchestrator.Skipped:
			d.logger.WarnWith().Str("stage", oc.Node).Str("prerequisite", strings.Join(oc.BlockedBy, ",")).
				Msg(msgShutdownSkippedRecord)
		}
	}
}

// reportLoggingOutcome reports the logging node's OWN shutdown outcome, AFTER Shutdown returns:
//   - Drained: nothing — the clean-run "smd stopped" was logged by stopLogging before it closed.
//   - Failed / TimedOut: the logger is closed or mid-close, so report to stderr.
//   - Skipped: an earlier node did not drain, so the logger was left open (not closed beneath a
//     possibly-live user) — note it through the still-open logger and leave it for process reclamation.
func (d *daemon) reportLoggingOutcome(report orchestrator.ShutdownReport) {
	for _, oc := range report.Outcomes {
		if oc.Node != nodeLogging {
			continue
		}
		switch oc.Result {
		case orchestrator.Failed:
			_, _ = fmt.Fprintf(os.Stderr, "smd: logging Stop failed: %v\n", oc.Err)
		case orchestrator.TimedOut:
			_, _ = fmt.Fprintf(os.Stderr, "smd: logging Stop timed out; the logger was not cleanly closed\n")
		case orchestrator.Skipped:
			d.logger.WarnWith().Str("prerequisite", strings.Join(oc.BlockedBy, ",")).
				Msg("shutdown: logger left open (an earlier node did not drain); reclaimed at process exit")
		}
		return
	}
}
