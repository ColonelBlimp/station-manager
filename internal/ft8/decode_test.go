package ft8

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// containsText reports whether want is among the decoded texts.
func containsText(texts []string, want string) bool {
	for _, t := range texts {
		if t == want {
			return true
		}
	}
	return false
}

// TestDecodeFile_20mSlot1 is the integration oracle: it proves go-ft8 is wired
// in and decoding correctly against a known corpus slot. It is not a parity
// gate against jt9 (that lives in go-ft8); it pins a stable lower bound plus a
// handful of high-confidence callsigns so a regression in the wiring or the
// pinned go-ft8 version is caught.
func TestDecodeFile_20mSlot1(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}

	msgs, err := DecodeFile(filepath.Join("testdata", "20m_slot1.wav"), logging.Noop())
	if err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if len(msgs) < 20 {
		t.Fatalf("decoded %d messages, want >= 20", len(msgs))
	}

	got := make([]string, len(msgs))
	for i, m := range msgs {
		got[i] = m.Text
		if m.FreqHz < 0 || m.FreqHz > 3000 {
			t.Errorf("decode %q has implausible freq %.1f Hz", m.Text, m.FreqHz)
		}
	}
	for _, want := range []string{
		"CQ DX S56GD JN65",
		"PA2JFX SV3CNX 73",
		"YM4KF G5MJF -24",
	} {
		if !containsText(got, want) {
			t.Errorf("expected decode %q not found", want)
		}
	}
}

func TestDecodeFile_LiveSlot1(t *testing.T) {
	if testing.Short() {
		t.Skip("full FT8 decode is heavy; skipped under -short")
	}
	msgs, err := DecodeFile(filepath.Join("testdata", "live_slot1.wav"), logging.Noop())
	if err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if len(msgs) < 25 {
		t.Fatalf("decoded %d messages, want >= 25", len(msgs))
	}
}

func TestReadSlotWAV_ReadsFullSlot(t *testing.T) {
	samples, err := readSlotWAV(filepath.Join("testdata", "20m_slot1.wav"))
	if err != nil {
		t.Fatalf("readSlotWAV: %v", err)
	}
	// 15 s * 12 kHz = 180000 samples.
	if len(samples) != 180000 {
		t.Fatalf("read %d samples, want 180000", len(samples))
	}
}

// TestReadSlotWAV_RejectsWrongRate proves the reader enforces go-ft8's input
// contract instead of silently mis-decoding a non-12 kHz file.
func TestReadSlotWAV_RejectsWrongRate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wrongrate.wav")
	writeTestWAV(t, path, 48000, 1, 16, make([]byte, 200))
	if _, err := readSlotWAV(path); err == nil {
		t.Fatal("expected error for 48 kHz file, got nil")
	}
}

// TestDecodeSlot_FailSoft confirms a decode never panics out of the package:
// empty input and a nil logger are both tolerated.
func TestDecodeSlot_FailSoft(t *testing.T) {
	if got := DecodeSlot(nil, nil); len(got) != 0 {
		t.Fatalf("empty input decoded %d messages, want 0", len(got))
	}
	if got := DecodeSlot([]int16{}, logging.Noop()); len(got) != 0 {
		t.Fatalf("empty slice decoded %d messages, want 0", len(got))
	}
}

// TestDecodeSlot_RejectsWrongLength confirms the checked API rejects a slot
// that isn't a full 180000-sample frame: DecodeSlot returns nil (and logs a
// warn) rather than feeding a malformed slot to the decoder. Rejection happens
// before decode work, so this is cheap — no -short gate needed.
func TestDecodeSlot_RejectsWrongLength(t *testing.T) {
	short := make([]int16, 1000)
	if got := DecodeSlot(short, logging.Noop()); got != nil {
		t.Fatalf("wrong-length slot decoded %d messages, want nil", len(got))
	}
}

// writeTestWAV writes a minimal canonical PCM WAV for reader-contract tests.
func writeTestWAV(t *testing.T, path string, rate uint32, channels, bits uint16, data []byte) {
	t.Helper()
	var buf []byte
	put := func(b ...byte) { buf = append(buf, b...) }
	putU32 := func(v uint32) { buf = binary.LittleEndian.AppendUint32(buf, v) }
	putU16 := func(v uint16) { buf = binary.LittleEndian.AppendUint16(buf, v) }

	byteRate := rate * uint32(channels) * uint32(bits/8)
	blockAlign := channels * (bits / 8)

	put('R', 'I', 'F', 'F')
	putU32(uint32(36 + len(data)))
	put('W', 'A', 'V', 'E')
	put('f', 'm', 't', ' ')
	putU32(16)
	putU16(1) // PCM
	putU16(channels)
	putU32(rate)
	putU32(byteRate)
	putU16(blockAlign)
	putU16(bits)
	put('d', 'a', 't', 'a')
	putU32(uint32(len(data)))
	buf = append(buf, data...)

	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatalf("write test wav: %v", err)
	}
}
