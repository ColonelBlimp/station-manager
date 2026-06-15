package bridge

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// the IC-7300 READ snapshot blob: read-freq (03) + read-mode (04), each a
// FE FE 94 E0 <cmd> FD frame, concatenated — exactly what cat.Encode(def,"READ")
// produces for the icom_civ rigdef.
var civReadBlob = []byte{
	0xFE, 0xFE, 0x94, 0xE0, 0x03, 0xFD,
	0xFE, 0xFE, 0x94, 0xE0, 0x04, 0xFD,
}

func TestSplitCIVFrames(t *testing.T) {
	got := splitCIVFrames(civReadBlob)
	want := [][]byte{
		{0xFE, 0xFE, 0x94, 0xE0, 0x03},
		{0xFE, 0xFE, 0x94, 0xE0, 0x04},
	}
	if len(got) != len(want) {
		t.Fatalf("splitCIVFrames = %d frames, want %d: % X", len(got), len(want), got)
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Errorf("frame %d = % X, want % X", i, got[i], want[i])
		}
	}

	// A buffer with no delimiter is one frame, unchanged.
	one := splitCIVFrames([]byte{0xFE, 0xFE, 0x94, 0xE0, 0x03})
	if len(one) != 1 {
		t.Errorf("no-delimiter buffer split into %d frames, want 1", len(one))
	}
}

// TestWriteSnapshotReads_CIVSpacesFrames: a CI-V snapshot is written as one
// write PER read frame (not a single back-to-back blast), so the half-duplex
// rig can answer each before the next arrives — the fix for the lost freq reply
// (bench 2026-06-15).
func TestWriteSnapshotReads_CIVSpacesFrames(t *testing.T) {
	s := &Service{civReadGap: 0} // 0 gap: assert the splitting, no real delay
	fake := newFakeSerial()

	if err := s.writeSnapshotReads(context.Background(), fake, true, civReadBlob); err != nil {
		t.Fatalf("writeSnapshotReads: %v", err)
	}
	w := fake.recordedWrites()
	if len(w) != 2 {
		t.Fatalf("CI-V snapshot = %d writes, want 2 (one per read frame): % X", len(w), w)
	}
	if !bytes.Equal(w[0], []byte{0xFE, 0xFE, 0x94, 0xE0, 0x03}) {
		t.Errorf("write 0 = % X, want read-freq frame", w[0])
	}
	if !bytes.Equal(w[1], []byte{0xFE, 0xFE, 0x94, 0xE0, 0x04}) {
		t.Errorf("write 1 = % X, want read-mode frame", w[1])
	}
}

// TestWriteSnapshotReads_KenwoodSingleWrite: the Kenwood path is unchanged — the
// ;-delimited burst goes out in one write (the rig queues it fine).
func TestWriteSnapshotReads_KenwoodSingleWrite(t *testing.T) {
	s := &Service{}
	fake := newFakeSerial()

	blob := []byte("ID;FA;FB;ST;VS;MD0;MD1;PC;")
	if err := s.writeSnapshotReads(context.Background(), fake, false, blob); err != nil {
		t.Fatalf("writeSnapshotReads: %v", err)
	}
	w := fake.recordedWrites()
	if len(w) != 1 || !bytes.Equal(w[0], blob) {
		t.Fatalf("Kenwood snapshot = %d writes %q, want one blob %q", len(w), w, blob)
	}
}

// TestWriteSnapshotReads_GapHonoursContext: with a gap configured, a cancelled
// context stops the snapshot between frames (so shutdown isn't delayed and a
// dead port doesn't spin).
func TestWriteSnapshotReads_GapHonoursContext(t *testing.T) {
	s := &Service{civReadGap: 10 * time.Second} // long gap so the cancel wins
	fake := newFakeSerial()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	err := s.writeSnapshotReads(ctx, fake, true, civReadBlob)
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
	// First frame writes before the (i>0) gap; the gap then sees ctx.Done.
	if w := fake.recordedWrites(); len(w) != 1 {
		t.Errorf("writes = %d, want 1 (first frame, then cancelled in the gap)", len(w))
	}
}
