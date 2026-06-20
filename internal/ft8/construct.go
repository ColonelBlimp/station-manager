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
//
// workingDir is the daemon's RESOLVED working dir (cfgSvc.WorkingDir(), which
// honours --config / data_dir) — used as the default decode-log location so it
// lands next to smd.log rather than wherever a bare utils.WorkingDir() resolves.
func NewService(cfg types.Ft8Config, log logging.Logger, workingDir string) *Service {
	if log == nil {
		log = logging.Noop()
	}
	s := newService(cfg, log, newCaptureSource(cfg, log))
	s.workingDir = workingDir
	return s
}
