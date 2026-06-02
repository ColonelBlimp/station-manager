//go:build cgo

package ft8

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Constructing the source must not touch audio hardware — only Start does.
// These assert the device-string → capture.Config.DeviceIndex resolution.
func TestNewCaptureSource_DeviceResolution(t *testing.T) {
	cases := []struct {
		name   string
		device string
		want   int
	}{
		{"empty is system default", "", -1},
		{"integer index", "2", 2},
		{"zero index", "0", 0},
		{"non-numeric falls back to default", "USB Audio CODEC", -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := newCaptureSource(types.Ft8Config{Enabled: true, Device: c.device}, logging.Noop())
			ms, ok := src.(*malgoSource)
			if !ok {
				t.Fatalf("expected *malgoSource, got %T", src)
			}
			if ms.cfg.DeviceIndex != c.want {
				t.Errorf("DeviceIndex = %d, want %d", ms.cfg.DeviceIndex, c.want)
			}
		})
	}
}

// Stop before Start must be a safe no-op (captureSource contract).
func TestMalgoSource_StopBeforeStart(t *testing.T) {
	src := newCaptureSource(types.Ft8Config{Enabled: true}, logging.Noop())
	if err := src.Stop(); err != nil {
		t.Errorf("Stop before Start: unexpected error %v", err)
	}
}
