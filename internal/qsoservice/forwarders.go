package qsoservice

import (
	"slices"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// shouldEnqueue reports whether a configured forwarder should receive a
// qso_upload row for the given QSO lifecycle action. Used at each
// ingest site (submit, update, delete) to filter out disabled
// destinations and those whose action_filter excludes this action.
//
// See docs/v2-design/forwarding.md §6.
func shouldEnqueue(fc types.ForwarderConfig, act action.Action) bool {
	if !fc.Enabled {
		return false
	}
	return slices.Contains(fc.ActionFilter, act.String())
}
