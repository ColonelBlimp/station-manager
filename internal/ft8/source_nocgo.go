//go:build !cgo

package ft8

import (
	"context"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// newCaptureSource on the CGO-free (static default) build returns a source
// that cannot capture. Service.Start treats a source whose Start errors as
// "capture unavailable; subsystem idle" — so a static daemon with
// ft8.enabled=true stays up and logs the reason rather than failing startup.
// Live FT8 requires the CGO build (ADR 0024).
func newCaptureSource(_ types.Ft8Config, _ logging.Logger) captureSource {
	return unavailableSource{}
}

type unavailableSource struct{}

func (unavailableSource) Start(context.Context) (<-chan []int16, error) {
	const op errors.Op = "ft8.unavailableSource.Start"
	return nil, errors.New(op).
		WithMsg("live audio capture is unavailable: this daemon was built without CGO (ADR 0024)")
}

func (unavailableSource) Stop() error { return nil }
