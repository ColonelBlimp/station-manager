package ft8

import (
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// BenchmarkDecodeSlot times a full single-slot decode. Run it with each FFT
// backend to compare:
//
//	CGO_ENABLED=0 go test -run=^$ -bench=DecodeSlot ./internal/ft8/            # gonum (pure Go)
//	CGO_ENABLED=1 go test -tags pocketfft -run=^$ -bench=DecodeSlot ./internal/ft8/  # PocketFFT (C)
//
// The WAV read is outside the timed loop so the benchmark measures decode work,
// not file I/O.
func BenchmarkDecodeSlot(b *testing.B) {
	samples, err := readSlotWAV(filepath.Join("testdata", "20m_slot1.wav"))
	if err != nil {
		b.Fatalf("readSlotWAV: %v", err)
	}
	log := logging.Noop()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DecodeSlot(samples, log)
	}
}
