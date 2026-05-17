package qsoservice

import (
	"slices"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// shouldEnqueue reports whether a configured forwarder should receive a
// qso_upload row for the given QSO lifecycle action. Used at each
// ingest site (submit, update, delete) to filter out destinations whose
// action_filter excludes this action.
//
// Per ADR 0022, the Enabled flag is purely a worker-lifecycle signal —
// disabled forwarders accumulate `pending` rows that drain when the
// operator re-enables them and restarts. Presence in config.json is
// what gates enqueue; the caller's `s.Config.Forwarders()` loop
// already only iterates defined forwarders.
//
// See docs/v2-design/forwarding.md §6, §8, and ADR 0022.
func shouldEnqueue(fc types.ForwarderConfig, act action.Action) bool {
	return slices.Contains(fc.ActionFilter, act.String())
}
