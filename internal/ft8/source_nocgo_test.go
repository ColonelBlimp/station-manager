//go:build !cgo

package ft8

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// On the CGO-free build the source must report capture unavailable (so the
// Service logs "idle" rather than failing) and Stop must be a safe no-op.
func TestNewCaptureSource_Unavailable(t *testing.T) {
	src := newCaptureSource(types.Ft8Config{Enabled: true}, logging.Noop())

	ch, err := src.Start(context.Background())
	if err == nil {
		t.Fatal("expected capture-unavailable error on CGO-free build, got nil")
	}
	if ch != nil {
		t.Errorf("expected nil channel on error, got %v", ch)
	}
	if err := src.Stop(); err != nil {
		t.Errorf("Stop: unexpected error %v", err)
	}
}
