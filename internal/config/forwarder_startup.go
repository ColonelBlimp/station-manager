package config

import (
	"fmt"

	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ForwarderStartupFinding rejects a forwarder list the daemon could not start
// with. It mirrors only spawnForwarderWorkers' enabled-forwarder Build gate —
// skip disabled, Build each enabled one; startup additionally resolves retry
// defaults and constructs workers, which are outside this probe — because that
// gate is precisely the failure it exists to pre-empt: Validate never inspects
// credentials, so a bad value used to surface only at the next restart, as a
// daemon that refuses to come up long after the operator believed the save had
// worked (three separate review findings were instances of that one gap). Both
// the config PUT handler and `smd config-check` share it (W-0008 CC-6), so the
// API's 400 and the preflight refuse the same files.
//
// Disabled forwarders are skipped because startup skips them — a destination must
// stay saveable while half-configured, before the operator switches it on.
//
// Build is side-effect-free (the constructors assemble a struct and an
// http.Client; no network, no files), so probing is cheap and safe.
//
// The returned Finding carries a STABLE, sanitised message — the forwarder's
// operator-chosen name and the fault class, nothing else — and that is the
// public contract for every surface that reports it (400 body, preflight output,
// automation logs). The constructor's own error comes back separately as cause,
// for protected daemon logging only. Constructors are themselves bound never to
// embed a credential value (forwarding.Build's contract), so the split is
// defense in depth, not a workaround for a known leak: a stable message cannot
// regress when a constructor's wording changes, and the stored value survives
// merging when an operator enables a previously-disabled entry without retyping
// it. Same split the 5xx path uses: generic on the wire, real cause in the log.
func ForwarderStartupFinding(fwds []types.ForwarderConfig) (*Finding, error) {
	for _, fc := range fwds {
		if !fc.Enabled {
			continue
		}
		if _, err := forwarding.Build(fc); err != nil {
			return &Finding{
				Field: "forwarders",
				Code:  "forwarder_unusable",
				Message: fmt.Sprintf(
					"forwarder %q is enabled but its credentials are incomplete or invalid; check its settings",
					fc.Name),
			}, err
		}
	}
	return nil, nil
}
