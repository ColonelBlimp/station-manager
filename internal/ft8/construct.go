package ft8

import (
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// NewService constructs the FT8 subsystem with the build-flavour-appropriate
// capture source: the CGO build wires miniaudio (live capture); the static
// CGO-free build wires a stub that reports capture unavailable, leaving the
// subsystem idle (ADR 0024). cmd/smd calls this; the captureSource seam stays
// internal so tests inject fakes via newService.
//
// Config is snapshotted at construction — an operator restart picks up edits,
// matching internal/bridge.
func NewService(cfg types.Ft8Config, log logging.Logger) *Service {
	if log == nil {
		log = logging.Noop()
	}
	return newService(cfg, log, newCaptureSource(cfg, log))
}
