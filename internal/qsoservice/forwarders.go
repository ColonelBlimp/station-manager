package qsoservice

import (
	"slices"
	"strings"

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

// forwarderNamed reports whether name is in the (case-insensitive) set names.
// Used by the import path to enqueue upload rows only for the forwarders the
// operator explicitly opted into via `smd import --forward`.
func forwarderNamed(names []string, name string) bool {
	return slices.ContainsFunc(names, func(n string) bool {
		return strings.EqualFold(n, name)
	})
}
