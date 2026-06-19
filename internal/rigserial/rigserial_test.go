package rigserial

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/serial"
)

func boolPtr(b bool) *bool { return &b }

// TestCompose_IcomCIV is the H2 regression: composing the IC-7300 rigdef carries
// RTS/DTR through as explicit false (de-assert, so opening can't key USB SEND
// PTT) and parses the "0xFD" CI-V delimiter — the two things catcli used to get
// wrong by duplicating this composition.
func TestCompose_IcomCIV(t *testing.T) {
	def, ok := cat.Lookup("icom-ic7300")
	if !ok {
		t.Fatal("icom-ic7300 rigdef not found")
	}
	cfg, err := Compose(def.Serial, "/dev/ttyUSB0")
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if cfg.PortName != "/dev/ttyUSB0" {
		t.Errorf("PortName = %q, want /dev/ttyUSB0", cfg.PortName)
	}
	if cfg.LineDelimiter != 0xFD {
		t.Errorf("LineDelimiter = %#x, want 0xFD (CI-V frame terminator)", cfg.LineDelimiter)
	}
	if cfg.RTS == nil || *cfg.RTS {
		t.Errorf("RTS = %v, want explicit false (de-assert)", cfg.RTS)
	}
	if cfg.DTR == nil || *cfg.DTR {
		t.Errorf("DTR = %v, want explicit false (de-assert)", cfg.DTR)
	}
}

// TestCompose_RejectsBadNumeric is the M2 regression: a bad baud rate or data
// bits is a config error here (→ the bridge classifies it permanent), not a
// transient open failure.
func TestCompose_RejectsBadNumeric(t *testing.T) {
	base := cat.RigSerial{BaudRate: 19200, DataBits: 8, StopBits: 1, Parity: "none", LineDelimiter: ";"}

	if _, err := Compose(base, "/dev/ttyUSB0"); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	bad := base
	bad.BaudRate = -1
	if _, err := Compose(bad, "/dev/ttyUSB0"); err == nil {
		t.Error("negative baud_rate should be rejected")
	}

	bad = base
	bad.DataBits = 9
	if _, err := Compose(bad, "/dev/ttyUSB0"); err == nil {
		t.Error("data_bits 9 should be rejected")
	}
}

// TestDelimiterFromString covers the literal-char and 0xNN hex forms plus the
// rejection paths (moved here from the bridge — review 2026-06-19 H2).
func TestDelimiterFromString(t *testing.T) {
	ok := map[string]byte{";": ';', "\r": '\r', "0xFD": 0xFD, "0XFD": 0xFD, "0x3b": ';'}
	for in, want := range ok {
		got, err := DelimiterFromString(in)
		if err != nil {
			t.Errorf("DelimiterFromString(%q): unexpected error %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("DelimiterFromString(%q) = %#x, want %#x", in, got, want)
		}
	}
	for _, bad := range []string{"", "ab", "0xFFF", "0x", "0xGG", "ý"} {
		if _, err := DelimiterFromString(bad); err == nil {
			t.Errorf("DelimiterFromString(%q) = nil error, want error", bad)
		}
	}
}

// TestOpenMayPulseLines covers the H1 pulse-warning predicate. On a non-Windows
// build (the CI/dev target) an explicit-false RTS/DTR is pulse-prone; nil and
// explicit-true are not (the line is asserted anyway).
func TestOpenMayPulseLines(t *testing.T) {
	deassert := serial.Config{RTS: boolPtr(false), DTR: boolPtr(false)}
	if !OpenMayPulseLines(deassert) {
		t.Error("a de-asserted RTS/DTR config should warn on a Unix build")
	}
	if OpenMayPulseLines(serial.Config{}) {
		t.Error("nil RTS/DTR (unmanaged) should not warn")
	}
	if OpenMayPulseLines(serial.Config{RTS: boolPtr(true), DTR: boolPtr(true)}) {
		t.Error("explicit-true lines are asserted anyway — no pulse hazard")
	}
}
