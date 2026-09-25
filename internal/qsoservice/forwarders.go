package qsoservice

import (
	"fmt"
	"slices"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// shouldEnqueue reports whether a configured forwarder should receive a
// qso_upload row for the given QSO lifecycle action. Used at each
// ingest site (submit, update, delete).
//
// Per ADR 0039, `enabled` GATES enqueue: a disabled forwarder gets no rows at
// all (and the daemon discards any queued rows it still holds at startup). This
// reverses ADR 0022's presence-gating — once ADR 0038 made connectivity outages
// retry forever while enabled, the queued-but-not-uploaded "suspended" state
// lost its only use case. QSOs not queued (logged while a forwarder was
// disabled, or pre-dating it) are uploaded later via the logbook SPA's manual
// backfill, never automatically.
//
// See docs/decisions/0039-forwarder-enabled-gates-enqueue-config-driven.md
// (and ADR 0038, ADR 0022).
func shouldEnqueue(fc types.ForwarderConfig, act action.Action) bool {
	return fc.Enabled && slices.Contains(fc.ActionFilter, act.String())
}

// forwarderNamed reports whether name is in the (case-insensitive) set names.
// Used by the import path to enqueue upload rows only for the forwarders the
// operator explicitly opted into via `smd import --forward`.
func forwarderNamed(names []string, name string) bool {
	return slices.ContainsFunc(names, func(n string) bool {
		return strings.EqualFold(n, name)
	})
}

// refuseBulkBackfillImport gates an import's forwardTo selection (review
// 2026-08-07 #1): an import IS a bulk catch-up batch by definition, so naming
// a destination that forbids bulk backfill (forwarding.NoBulkBackfill —
// ClubLog's realtime.php rule, a written commitment whose violation gets the
// application API key blocked) refuses the whole import UP FRONT, before
// anything is stored. Refusal, not silent narrowing, per the force_unsupported
// posture (enqueue.go review round 2 #1): an operator who asked for clublog
// must not be told "imported" while nothing was queued. Matches names the way
// forwarderNamed does, since the CLI passes the operator's own typing. The
// LIVE submit path never calls this — as-you-log realtime is the sanctioned
// use of these destinations.
func refuseBulkBackfillImport(forwardTo []string, forwarders []types.ForwarderConfig) error {
	for _, fwd := range forwarders {
		if forwarding.NoBulkBackfill(fwd.Type) && forwarderNamed(forwardTo, fwd.Name) {
			return &SubmitError{
				Code: "backfill_unsupported",
				Message: fmt.Sprintf("forwarder %q accepts retries of failed uploads only; an "+
					"import is a bulk catch-up batch — upload an ADIF export on the destination's "+
					"website instead", fwd.Name),
			}
		}
	}
	return nil
}

// SetDestinationRoutes installs the start-time snapshot of the active
// archive's bindings resolved against their station accounts (ADR 0082). The
// daemon sets it once after the seed; the same set builds the workers. Every
// binding is present, enabled or not — routesFor filters.
func (s *Service) SetDestinationRoutes(routes []forwarding.BoundForwarder) {
	s.routesMu.Lock()
	defer s.routesMu.Unlock()
	s.routes = append([]forwarding.BoundForwarder(nil), routes...)
}

// DestinationRoutes returns a copy of the installed snapshot.
func (s *Service) DestinationRoutes() []forwarding.BoundForwarder {
	s.routesMu.RLock()
	defer s.routesMu.RUnlock()
	return append([]forwarding.BoundForwarder(nil), s.routes...)
}

// routesFor is the destination list every enqueue path iterates for a QSO of
// logbookID: the ENABLED bindings of that logbook, in snapshot order. A logbook
// with no enabled binding queues nowhere — no gate, no error, `forwarded_to: []`.
func (s *Service) routesFor(logbookID int64) []types.ForwarderConfig {
	s.routesMu.RLock()
	defer s.routesMu.RUnlock()
	var out []types.ForwarderConfig
	for _, r := range s.routes {
		if r.LogbookID == logbookID && r.Config.Enabled {
			out = append(out, r.Config)
		}
	}
	return out
}

// routeByName resolves a binding by its forwarder_name (case-insensitive) and
// returns it only if it is enabled and its action_filter covers act — the
// eligibility gate for a manual backfill, which also learns the logbook the
// binding serves.
func (s *Service) routeByName(name string, act action.Action) (forwarding.BoundForwarder, bool) {
	name = strings.TrimSpace(name)
	s.routesMu.RLock()
	defer s.routesMu.RUnlock()
	for _, r := range s.routes {
		if strings.EqualFold(r.Config.Name, name) && shouldEnqueue(r.Config, act) {
			return r, true
		}
	}
	return forwarding.BoundForwarder{}, false
}
