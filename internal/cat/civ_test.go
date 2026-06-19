package cat

import (
	stderr "errors"
	"testing"
)

// civTestDef is an IC-7300-shaped CI-V rigdef used across the engine tests.
// It mirrors the schema ADR 0034 settled on: hex command bytes in Cmd, a
// per-literal mode_seq with one frame for non-data modes and two for the data
// mode, and BCD-freq / byte markers on the decode states. The exact bytes are
// the ones bench-validated on the rig (addr 0x94, freq LE BCD, 06 self-clears
// the data flag).
func civTestDef() RigDefinition {
	modeMaps := []ValueMapping{
		{Key: "00", Value: "LSB"},
		{Key: "01", Value: "USB"},
		{Key: "03", Value: "CW"},
	}
	return RigDefinition{
		Protocol:   ProtocolIcomCIV,
		CivAddress: "94",
		Terminator: "\xfd",
		Commands: []Command{
			{Name: "READ", Cmd: "03", Encoding: EncodingNone},
			{Name: "set_freq", Cmd: "05", Encoding: EncodingBCDFreq, Exposed: true},
			{Name: "set_mode", Cmd: "06", Encoding: EncodingModeSeq, Exposed: true, ModeSeq: []ModeSequence{
				{Mode: "LSB", Frames: []string{"0600:01"}},
				{Mode: "USB", Frames: []string{"0601:01"}},
				{Mode: "CW", Frames: []string{"0603:01"}},
				{Mode: "USB-D", Frames: []string{"0601:01", "1A06:0101"}},
			}},
			{Name: "set_power", Cmd: "140A", Encoding: EncodingBCDPower, Exposed: true},
			{Name: "tx_on", Cmd: "1C0001", Encoding: EncodingNone},
			{Name: "tx_off", Cmd: "1C0000", Encoding: EncodingNone},
		},
		States: []State{
			{Prefix: "00", Markers: []Marker{{Tag: "VFOAFREQ", Kind: MarkerKindBCDFreq, Index: 0, Length: 5}}},
			{Prefix: "03", Markers: []Marker{{Tag: "VFOAFREQ", Kind: MarkerKindBCDFreq, Index: 0, Length: 5}}},
			{Prefix: "01", Markers: []Marker{{Tag: "MAINMODE", Kind: MarkerKindByte, Index: 0, Length: 1, ValueMappings: modeMaps}}},
			{Prefix: "04", Markers: []Marker{{Tag: "MAINMODE", Kind: MarkerKindByte, Index: 0, Length: 1, ValueMappings: modeMaps}}},
		},
	}
}

func eqBytes(t *testing.T, got, want []byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length: got % X (%d), want % X (%d)", got, len(got), want, len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("byte %d: got % X, want % X", i, got, want)
		}
	}
}

func TestFreqToBCD_RoundTrip(t *testing.T) {
	cases := []struct {
		hz  string
		bcd []byte
	}{
		{"14074000", []byte{0x00, 0x40, 0x07, 0x14, 0x00}},  // bench-validated 20m FT8
		{"7074000", []byte{0x00, 0x40, 0x07, 0x07, 0x00}},   // 40m
		{"146520000", []byte{0x00, 0x00, 0x52, 0x46, 0x01}}, // 2m
		{"0", []byte{0x00, 0x00, 0x00, 0x00, 0x00}},
	}
	for _, c := range cases {
		got, err := freqToBCD(c.hz)
		if err != nil {
			t.Fatalf("freqToBCD(%q): %v", c.hz, err)
		}
		eqBytes(t, got, c.bcd)

		back, err := bcdToFreq(c.bcd)
		if err != nil {
			t.Fatalf("bcdToFreq(% X): %v", c.bcd, err)
		}
		want := c.hz
		if want == "0" {
			want = "0"
		}
		if back != want {
			t.Errorf("bcdToFreq(% X) = %q, want %q", c.bcd, back, want)
		}
	}
}

func TestFreqToBCD_Rejects(t *testing.T) {
	for _, bad := range []string{"", "abc", "14.074", "-100", "123456789012"} {
		if _, err := freqToBCD(bad); err == nil {
			t.Errorf("freqToBCD(%q) = nil error, want error", bad)
		}
	}
}

func TestBCDToFreq_RejectsBadNibble(t *testing.T) {
	// 0xAB is not a valid BCD pair (nibbles > 9).
	if _, err := bcdToFreq([]byte{0xAB, 0x00, 0x00, 0x00, 0x00}); err == nil {
		t.Errorf("bcdToFreq(invalid BCD) = nil error, want error")
	}
}

func TestLevelToBCD(t *testing.T) {
	cases := []struct {
		dec string
		bcd []byte
	}{
		{"0", []byte{0x00, 0x00}},
		{"100", []byte{0x01, 0x00}},
		{"255", []byte{0x02, 0x55}},
	}
	for _, c := range cases {
		got, err := levelToBCD(c.dec, civPowerBCDLen)
		if err != nil {
			t.Fatalf("levelToBCD(%q): %v", c.dec, err)
		}
		eqBytes(t, got, c.bcd)
	}
}

func TestEncodeCIV_None(t *testing.T) {
	def := civTestDef()

	read, err := Encode(def, "READ")
	if err != nil {
		t.Fatalf("Encode READ: %v", err)
	}
	eqBytes(t, read, []byte{0xFE, 0xFE, 0x94, 0xE0, 0x03, 0xFD})

	on, err := Encode(def, "tx_on")
	if err != nil {
		t.Fatalf("Encode tx_on: %v", err)
	}
	eqBytes(t, on, []byte{0xFE, 0xFE, 0x94, 0xE0, 0x1C, 0x00, 0x01, 0xFD})

	off, err := Encode(def, "tx_off")
	if err != nil {
		t.Fatalf("Encode tx_off: %v", err)
	}
	eqBytes(t, off, []byte{0xFE, 0xFE, 0x94, 0xE0, 0x1C, 0x00, 0x00, 0xFD})
}

func TestEncodeCIV_UnknownCommand(t *testing.T) {
	_, err := Encode(civTestDef(), "nope")
	if !stderr.Is(err, ErrUnknownCommand) {
		t.Errorf("Encode unknown: err = %v, want ErrUnknownCommand", err)
	}
}

func TestEncodeCommandCIV_Freq(t *testing.T) {
	got, err := EncodeCommand(civTestDef(), "set_freq", "14074000")
	if err != nil {
		t.Fatalf("EncodeCommand set_freq: %v", err)
	}
	eqBytes(t, got, []byte{0xFE, 0xFE, 0x94, 0xE0, 0x05, 0x00, 0x40, 0x07, 0x14, 0x00, 0xFD})
}

func TestEncodeCommandCIV_ModeSeq(t *testing.T) {
	def := civTestDef()

	// Non-data mode: one frame, 06 self-clears the data flag (bench-validated).
	usb, err := EncodeCommand(def, "set_mode", "USB")
	if err != nil {
		t.Fatalf("EncodeCommand set_mode USB: %v", err)
	}
	eqBytes(t, usb, []byte{0xFE, 0xFE, 0x94, 0xE0, 0x06, 0x01, 0x01, 0xFD})

	// Data mode: two frames concatenated — base mode then 1A 06 data ON.
	usbD, err := EncodeCommand(def, "set_mode", "USB-D")
	if err != nil {
		t.Fatalf("EncodeCommand set_mode USB-D: %v", err)
	}
	eqBytes(t, usbD, []byte{
		0xFE, 0xFE, 0x94, 0xE0, 0x06, 0x01, 0x01, 0xFD,
		0xFE, 0xFE, 0x94, 0xE0, 0x1A, 0x06, 0x01, 0x01, 0xFD,
	})
}

func TestEncodeCommandCIV_Power(t *testing.T) {
	got, err := EncodeCommand(civTestDef(), "set_power", "100")
	if err != nil {
		t.Fatalf("EncodeCommand set_power: %v", err)
	}
	// No scale_max_watts on civTestDef's set_power → value is the raw 0–255 level.
	eqBytes(t, got, []byte{0xFE, 0xFE, 0x94, 0xE0, 0x14, 0x0A, 0x01, 0x00, 0xFD})
}

// TestEmbeddedIC7300_PowerScaling exercises the real rigdef's scale_max_watts=100
// on both directions: watts↔0–255 level (set_power encode) and level→watts
// (14 0A decode → TXPWR), so the SPA/QSO sees watts uniformly across rigs.
func TestEmbeddedIC7300_PowerScaling(t *testing.T) {
	def, ok := Lookup("icom-ic7300")
	if !ok {
		t.Fatal("icom-ic7300 not embedded")
	}

	// Encode: watts → level (round(watts×255/100)), big-endian BCD.
	for _, tc := range []struct {
		watts string
		bcd   []byte // the 2-byte level after the 14 0A command bytes
	}{
		{"0", []byte{0x00, 0x00}},   // 0 → 0
		{"50", []byte{0x01, 0x28}},  // round(127.5) = 128 → 01 28
		{"100", []byte{0x02, 0x55}}, // 255 → 02 55
	} {
		got, err := EncodeCommand(def, "set_power", tc.watts)
		if err != nil {
			t.Fatalf("EncodeCommand set_power %sW: %v", tc.watts, err)
		}
		want := append([]byte{0xFE, 0xFE, 0x94, 0xE0, 0x14, 0x0A}, append(tc.bcd, 0xFD)...)
		eqBytes(t, got, want)
	}

	// Decode: level → watts (round(level×100/255)). Reply FE FE E0 94 14 0A <2 BCD>.
	for _, tc := range []struct {
		bcd   []byte
		watts string
	}{
		{[]byte{0x00, 0x00}, "0"},
		{[]byte{0x01, 0x28}, "50"},  // 128 → round(50.196) = 50
		{[]byte{0x02, 0x55}, "100"}, // 255 → 100
	} {
		line := append([]byte{0xFE, 0xFE, 0xE0, 0x94, 0x14, 0x0A}, tc.bcd...)
		st, err := Decode(def, line)
		if err != nil {
			t.Fatalf("Decode power %v: %v", tc.bcd, err)
		}
		if st["TXPWR"] != tc.watts {
			t.Errorf("level %v: TXPWR = %q, want %q", tc.bcd, st["TXPWR"], tc.watts)
		}
	}
}

// TestEmbeddedIC7300_PowerOverRangeRejected guards review 2026-06-19 M2: watts
// above the rig's ScaleMaxWatts must be rejected (rig_invalid_value, no write),
// not silently clamped to full-scale level 255 — clamping would transmit at max
// power for an invalid request. The exact-max boundary (100 W) still encodes.
func TestEmbeddedIC7300_PowerOverRangeRejected(t *testing.T) {
	def, ok := Lookup("icom-ic7300")
	if !ok {
		t.Fatal("icom-ic7300 not embedded")
	}
	// Boundary: exactly ScaleMaxWatts encodes fine (→ level 255).
	if _, err := EncodeCommand(def, "set_power", "100"); err != nil {
		t.Fatalf("set_power 100W (max) should encode: %v", err)
	}
	// Over range: rejected as an invalid value, never coerced to 255.
	if _, err := EncodeCommand(def, "set_power", "150"); !stderr.Is(err, ErrInvalidPaddedValue) {
		t.Errorf("set_power 150W (over max) = %v, want ErrInvalidPaddedValue", err)
	}
}

func TestEncodeCommandCIV_Errors(t *testing.T) {
	def := civTestDef()

	if _, err := EncodeCommand(def, "READ", ""); !stderr.Is(err, ErrCommandNotExposed) {
		t.Errorf("unexposed: err = %v, want ErrCommandNotExposed", err)
	}
	if _, err := EncodeCommand(def, "nope", "x"); !stderr.Is(err, ErrUnknownCommand) {
		t.Errorf("unknown: err = %v, want ErrUnknownCommand", err)
	}
	if _, err := EncodeCommand(def, "set_mode", "FM"); !stderr.Is(err, ErrUnmappedValue) {
		t.Errorf("unmapped mode: err = %v, want ErrUnmappedValue", err)
	}
	if _, err := EncodeCommand(def, "set_freq", ""); !stderr.Is(err, ErrMissingValue) {
		t.Errorf("missing freq: err = %v, want ErrMissingValue", err)
	}
	if _, err := EncodeCommand(def, "set_freq", "abc"); !stderr.Is(err, ErrInvalidPaddedValue) {
		t.Errorf("bad freq: err = %v, want ErrInvalidPaddedValue", err)
	}
}

func TestDecodeCIV_FreqBroadcast(t *testing.T) {
	// FE FE 00 94 00 <5 BCD> — Transceive broadcast to 0x00 from the rig.
	line := []byte{0xFE, 0xFE, 0x00, 0x94, 0x00, 0x00, 0x40, 0x07, 0x14, 0x00}
	st, err := Decode(civTestDef(), line)
	if err != nil {
		t.Fatalf("Decode freq broadcast: %v", err)
	}
	if st["VFOAFREQ"] != "14074000" {
		t.Errorf("VFOAFREQ = %q, want 14074000", st["VFOAFREQ"])
	}
}

func TestDecodeCIV_FreqReply(t *testing.T) {
	// FE FE E0 94 03 <5 BCD> — transponded reply to the controller from the rig.
	line := []byte{0xFE, 0xFE, 0xE0, 0x94, 0x03, 0x00, 0x40, 0x07, 0x14, 0x00}
	st, err := Decode(civTestDef(), line)
	if err != nil {
		t.Fatalf("Decode freq reply: %v", err)
	}
	if st["VFOAFREQ"] != "14074000" {
		t.Errorf("VFOAFREQ = %q, want 14074000", st["VFOAFREQ"])
	}
}

func TestDecodeCIV_ModeBroadcast(t *testing.T) {
	// FE FE 00 94 01 01 01 — mode broadcast: base mode 01 (USB), filter 01.
	line := []byte{0xFE, 0xFE, 0x00, 0x94, 0x01, 0x01, 0x01}
	st, err := Decode(civTestDef(), line)
	if err != nil {
		t.Fatalf("Decode mode broadcast: %v", err)
	}
	if st["MAINMODE"] != "USB" {
		t.Errorf("MAINMODE = %q, want USB", st["MAINMODE"])
	}
}

func TestDecodeCIV_EchoFiltered(t *testing.T) {
	// from == controller (E0): our own echoed send — must be dropped.
	line := []byte{0xFE, 0xFE, 0x94, 0xE0, 0x05, 0x00, 0x40, 0x07, 0x14, 0x00}
	_, err := Decode(civTestDef(), line)
	if !stderr.Is(err, ErrNoMatch) {
		t.Errorf("echo: err = %v, want ErrNoMatch", err)
	}
}

func TestDecodeCIV_UnknownCommandByte(t *testing.T) {
	// Command byte 0x27 (scope) — not in the state table; dropped as no-match.
	line := []byte{0xFE, 0xFE, 0x00, 0x94, 0x27, 0x00}
	_, err := Decode(civTestDef(), line)
	if !stderr.Is(err, ErrNoMatch) {
		t.Errorf("unknown cmd: err = %v, want ErrNoMatch", err)
	}
}

func TestDecodeCIV_ShortAndMalformed(t *testing.T) {
	def := civTestDef()
	for _, line := range [][]byte{
		nil,
		{0xFE},
		{0xFE, 0xFE, 0x00, 0x94},       // no command byte
		{0x01, 0x02, 0x00, 0x94, 0x00}, // no FE FE preamble
	} {
		if _, err := Decode(def, line); !stderr.Is(err, ErrNoMatch) {
			t.Errorf("Decode(% X): err = %v, want ErrNoMatch", line, err)
		}
	}
}

func TestRigModes_CIV(t *testing.T) {
	got := RigModes(civTestDef())
	want := []string{"LSB", "USB", "CW", "USB-D"}
	if len(got) != len(want) {
		t.Fatalf("RigModes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("RigModes[%d] = %q, want %q (full %v)", i, got[i], want[i], got)
		}
	}
}

func TestValidateCIV_OK(t *testing.T) {
	if err := ValidateRigDefinition(civTestDef()); err != nil {
		t.Fatalf("ValidateRigDefinition(valid CI-V): %v", err)
	}
}

func TestValidateCIV_Rejects(t *testing.T) {
	cases := map[string]func(*RigDefinition){
		"bad civ_address":        func(d *RigDefinition) { d.CivAddress = "ZZ" },
		"empty civ_address":      func(d *RigDefinition) { d.CivAddress = "" },
		"multi-byte civ_address": func(d *RigDefinition) { d.CivAddress = "94E0" },
		"unknown encoding":       func(d *RigDefinition) { d.Commands[0].Encoding = "weird" },
		"bad hex cmd":            func(d *RigDefinition) { d.Commands[0].Cmd = "XY" },
		"empty cmd":              func(d *RigDefinition) { d.Commands[0].Cmd = "" },
		"duplicate command": func(d *RigDefinition) {
			d.Commands = append(d.Commands, Command{Name: "READ", Cmd: "03", Encoding: EncodingNone})
		},
		"exposed tx_on": func(d *RigDefinition) {
			for i := range d.Commands {
				if d.Commands[i].Name == "tx_on" {
					d.Commands[i].Exposed = true
				}
			}
		},
		"mode_seq no entries": func(d *RigDefinition) {
			for i := range d.Commands {
				if d.Commands[i].Name == "set_mode" {
					d.Commands[i].ModeSeq = nil
				}
			}
		},
		"mode_seq duplicate mode": func(d *RigDefinition) {
			for i := range d.Commands {
				if d.Commands[i].Name == "set_mode" {
					d.Commands[i].ModeSeq = append(d.Commands[i].ModeSeq,
						ModeSequence{Mode: "USB", Frames: []string{"0601:01"}})
				}
			}
		},
		"mode_seq bad frame hex": func(d *RigDefinition) {
			for i := range d.Commands {
				if d.Commands[i].Name == "set_mode" {
					d.Commands[i].ModeSeq[0].Frames = []string{"ZZ"}
				}
			}
		},
		"unknown marker kind": func(d *RigDefinition) { d.States[0].Markers[0].Kind = "nope" },
		"bad state prefix":    func(d *RigDefinition) { d.States[0].Prefix = "ZZ" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			def := civTestDef()
			mutate(&def)
			if err := ValidateRigDefinition(def); err == nil {
				t.Errorf("ValidateRigDefinition(%s) = nil, want error", name)
			}
		})
	}
}

// TestEmbeddedIC7300 pins the shipped icom-ic7300.json rigdef end-to-end
// through the public codec surface — the same functions the bridge calls. The
// embedded loader already ran ValidateRigDefinition at init (a fault would
// panic the package), so this asserts the wire bytes are what the bench
// validated, not just that the file parses.
func TestEmbeddedIC7300(t *testing.T) {
	def, ok := Lookup("icom-ic7300")
	if !ok {
		t.Fatal("icom-ic7300 not in the embedded rig DB")
	}
	if def.Protocol != ProtocolIcomCIV {
		t.Fatalf("Protocol = %q, want %q", def.Protocol, ProtocolIcomCIV)
	}
	if def.Ft8Mode != "USB-D" {
		t.Errorf("Ft8Mode = %q, want USB-D", def.Ft8Mode)
	}

	// INIT primes comms with a single (decodable) VFO-A freq read. READ is the
	// full connect snapshot; POLL is the steady-state mirror gaps (ADR 0035).
	initBytes, err := Encode(def, "INIT")
	if err != nil {
		t.Fatalf("Encode INIT: %v", err)
	}
	eqBytes(t, initBytes, []byte{0xFE, 0xFE, 0x94, 0xE0, 0x25, 0x00, 0xFD})

	read, err := Encode(def, "READ")
	if err != nil {
		t.Fatalf("Encode READ: %v", err)
	}
	eqBytes(t, read, []byte{
		0xFE, 0xFE, 0x94, 0xE0, 0x25, 0x00, 0xFD, // VFO-A freq
		0xFE, 0xFE, 0x94, 0xE0, 0x25, 0x01, 0xFD, // VFO-B freq
		0xFE, 0xFE, 0x94, 0xE0, 0x26, 0x00, 0xFD, // mode + data flag
		0xFE, 0xFE, 0x94, 0xE0, 0x0F, 0xFD, // split
		0xFE, 0xFE, 0x94, 0xE0, 0x14, 0x0A, 0xFD, // power level (14 0A)
	})

	poll, err := Encode(def, "POLL")
	if err != nil {
		t.Fatalf("Encode POLL: %v", err)
	}
	eqBytes(t, poll, []byte{
		0xFE, 0xFE, 0x94, 0xE0, 0x25, 0x00, 0xFD, // VFO-A operating freq — polled every
		// cycle, not push-only: Transceive pushes VFO-A on a freq CHANGE, but at
		// steady state (parked) it never re-sends, so a fresh subscriber or a stale
		// mirror would otherwise miss the operating frequency (the wrong-band logging
		// bug). Mirrors READ; see ADR 0035 revision.
		0xFE, 0xFE, 0x94, 0xE0, 0x25, 0x01, 0xFD, // VFO-B
		0xFE, 0xFE, 0x94, 0xE0, 0x26, 0x00, 0xFD, // mode + data flag
		0xFE, 0xFE, 0x94, 0xE0, 0x0F, 0xFD, // split
		0xFE, 0xFE, 0x94, 0xE0, 0x14, 0x0A, 0xFD, // power level (14 0A)
	})

	// FT8 mode set: base USB then data ON (the bench-validated two-frame form).
	usbD, err := EncodeCommand(def, "set_mode", "USB-D")
	if err != nil {
		t.Fatalf("EncodeCommand set_mode USB-D: %v", err)
	}
	eqBytes(t, usbD, []byte{
		0xFE, 0xFE, 0x94, 0xE0, 0x06, 0x01, 0x01, 0xFD,
		0xFE, 0xFE, 0x94, 0xE0, 0x1A, 0x06, 0x01, 0x01, 0xFD,
	})

	// Inbound freq op.
	freq, err := EncodeCommand(def, "set_freq", "14074000")
	if err != nil {
		t.Fatalf("EncodeCommand set_freq: %v", err)
	}
	eqBytes(t, freq, []byte{0xFE, 0xFE, 0x94, 0xE0, 0x05, 0x00, 0x40, 0x07, 0x14, 0x00, 0xFD})

	// Decode the Transceive freq push (00) — the fast operating-freq path.
	st, err := Decode(def, []byte{0xFE, 0xFE, 0x00, 0x94, 0x00, 0x00, 0x40, 0x07, 0x14, 0x00})
	if err != nil || st["VFOAFREQ"] != "14074000" {
		t.Errorf("Decode freq push: st=%v err=%v", st, err)
	}
	// Decode the Transceive base-mode push (01) — the INSTANT mode path on a
	// front-panel mode change; the 26 00 poll refines the data flag a beat later.
	st, err = Decode(def, []byte{0xFE, 0xFE, 0x00, 0x94, 0x01, 0x03, 0x01})
	if err != nil || st["MAINMODE"] != "CW" {
		t.Errorf("Decode mode push (01): st=%v err=%v", st, err)
	}
	// Decode the polled VFO-A / VFO-B freq reads (25 00 / 25 01).
	st, err = Decode(def, []byte{0xFE, 0xFE, 0xE0, 0x94, 0x25, 0x00, 0x20, 0x68, 0x13, 0x10, 0x00})
	if err != nil || st["VFOAFREQ"] != "10136820" {
		t.Errorf("Decode 25 00 (VFO-A): st=%v err=%v", st, err)
	}
	st, err = Decode(def, []byte{0xFE, 0xFE, 0xE0, 0x94, 0x25, 0x01, 0x60, 0x32, 0x07, 0x14, 0x00})
	if err != nil || st["VFOBFREQ"] != "14073260" {
		t.Errorf("Decode 25 01 (VFO-B): st=%v err=%v", st, err)
	}
	// Decode the polled mode+data read (26 00): mode=01 USB, data=01 ON → USB-D.
	st, err = Decode(def, []byte{0xFE, 0xFE, 0xE0, 0x94, 0x26, 0x00, 0x01, 0x01, 0x01})
	if err != nil || st["MAINMODE"] != "USB-D" {
		t.Errorf("Decode 26 00 USB-D: st=%v err=%v", st, err)
	}
	st, err = Decode(def, []byte{0xFE, 0xFE, 0xE0, 0x94, 0x26, 0x00, 0x01, 0x00, 0x01})
	if err != nil || st["MAINMODE"] != "USB" {
		t.Errorf("Decode 26 00 USB (data off): st=%v err=%v", st, err)
	}
	// Decode split (0F): 01 = ON.
	st, err = Decode(def, []byte{0xFE, 0xFE, 0xE0, 0x94, 0x0F, 0x01})
	if err != nil || st["SPLIT"] != "ON" {
		t.Errorf("Decode 0F split: st=%v err=%v", st, err)
	}

	// swap_vfo exchanges VFO A/B via CI-V 07 B0 (a valueless `none` command).
	swap, err := EncodeCommand(def, "swap_vfo", "")
	if err != nil {
		t.Fatalf("EncodeCommand swap_vfo: %v", err)
	}
	eqBytes(t, swap, []byte{0xFE, 0xFE, 0x94, 0xE0, 0x07, 0xB0, 0xFD})

	// TX keying (CI-V 1C 00 01 / 1C 00 00) — controller-only, via low-level
	// Encode (bypasses the exposed gate, like tune/FT8-TX). The exposed-ops
	// assertion below pins that these are NEVER reachable via the command path.
	txOn, err := Encode(def, "tx_on")
	if err != nil {
		t.Fatalf("Encode tx_on: %v", err)
	}
	eqBytes(t, txOn, []byte{0xFE, 0xFE, 0x94, 0xE0, 0x1C, 0x00, 0x01, 0xFD})
	txOff, err := Encode(def, "tx_off")
	if err != nil {
		t.Fatalf("Encode tx_off: %v", err)
	}
	eqBytes(t, txOff, []byte{0xFE, 0xFE, 0x94, 0xE0, 0x1C, 0x00, 0x00, 0xFD})

	// TX *keying* must not be reachable: the rigdef ships no exposed tx_on/tx_off
	// (controller-only, ADR 0027/0030). The exposed inbound ops are freq + mode +
	// swap_vfo + set_power (set_power is a power-level set, not TX keying, and is
	// exposed on the Yaesu rigdefs too). swap_vfo declares no sets_state — it has
	// no commanded value to adopt, so the bridge reads state back after the ACK.
	wantOps := map[string]bool{"set_freq": true, "set_mode": true, "swap_vfo": true, "set_power": true}
	ops := ExposedCommands(def)
	if len(ops) != len(wantOps) {
		t.Errorf("ExposedCommands = %v, want exactly %v", ops, wantOps)
	}
	for _, op := range ops {
		if !wantOps[op] {
			t.Errorf("ExposedCommands includes unexpected %q", op)
		}
		if op == "tx_on" || op == "tx_off" {
			t.Errorf("ExposedCommands includes %q — TX keying must never be exposed", op)
		}
	}

	// Settable modes include the data mode (superset of broadcast base modes).
	modes := RigModes(def)
	var hasUSBD bool
	for _, m := range modes {
		if m == "USB-D" {
			hasUSBD = true
		}
	}
	if !hasUSBD {
		t.Errorf("RigModes = %v, want it to include USB-D", modes)
	}
}

func TestValidate_UnknownProtocol(t *testing.T) {
	def := civTestDef()
	def.Protocol = "martian"
	if err := ValidateRigDefinition(def); err == nil {
		t.Errorf("ValidateRigDefinition(unknown protocol) = nil, want error")
	}
}

// TestCIVAck classifies the FB/FA command acknowledgement (ADR 0034
// wait-for-ACK). Bench-validated frame shapes: FE FE E0 94 FB FD (OK) / …FA FD
// (NG). Everything else — broadcasts, polled replies, our own echo — is not an
// ACK, so the read loop decodes it normally.
func TestCIVAck(t *testing.T) {
	def := civTestDef()
	cases := []struct {
		name            string
		frame           []byte
		wantAck, wantOK bool
	}{
		{"FB ok", []byte{0xFE, 0xFE, 0xE0, 0x94, 0xFB}, true, true},
		{"FA ng", []byte{0xFE, 0xFE, 0xE0, 0x94, 0xFA}, true, false},
		{"freq broadcast", []byte{0xFE, 0xFE, 0x00, 0x94, 0x00, 0x00, 0x40, 0x07, 0x14, 0x00}, false, false},
		{"mode reply", []byte{0xFE, 0xFE, 0xE0, 0x94, 0x04, 0x01, 0x01}, false, false},
		{"our echo (from controller E0)", []byte{0xFE, 0xFE, 0x94, 0xE0, 0x05, 0x00, 0x40, 0x07, 0x14, 0x00}, false, false},
		{"too short", []byte{0xFE, 0xFE, 0xE0, 0x94}, false, false},
		{"not a CI-V preamble", []byte{0x01, 0x02, 0xE0, 0x94, 0xFB}, false, false},
		// review 2026-06-19 M1: ACK-looking frames not addressed to us, or with
		// payload bytes, must NOT be classified as our command's ACK.
		{"FB to another controller (E1)", []byte{0xFE, 0xFE, 0xE1, 0x94, 0xFB}, false, false},
		{"FB broadcast (to 00)", []byte{0xFE, 0xFE, 0x00, 0x94, 0xFB}, false, false},
		{"overlong ACK-looking (payload byte)", []byte{0xFE, 0xFE, 0xE0, 0x94, 0xFB, 0x00}, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isAck, ok := CIVAck(def, c.frame)
			if isAck != c.wantAck || (isAck && ok != c.wantOK) {
				t.Errorf("CIVAck = (isAck %v, accepted %v), want (%v, %v)", isAck, ok, c.wantAck, c.wantOK)
			}
		})
	}
}

// TestCIVAck_NonCIVRig: a Kenwood rig never classifies a CI-V frame as an ACK —
// the wait-for-ACK path is icom_civ-only and CIVAck guards on the protocol.
func TestCIVAck_NonCIVRig(t *testing.T) {
	def := civTestDef()
	def.Protocol = ProtocolKenwood
	if isAck, _ := CIVAck(def, []byte{0xFE, 0xFE, 0xE0, 0x94, 0xFB}); isAck {
		t.Error("CIVAck classified an ACK for a non-CI-V rig")
	}
}

// TestCommandSetsState reads the op→state-marker mapping the wait-for-ACK path
// uses to synthesize a push from a commanded value.
func TestCommandSetsState(t *testing.T) {
	def, ok := Lookup("icom-ic7300")
	if !ok {
		t.Fatal("embedded icom-ic7300 rigdef not found")
	}
	cases := []struct{ op, want string }{
		{"set_freq", "VFOAFREQ"},
		{"set_mode", "MAINMODE"},
		{"INIT", ""},     // no sets_state
		{"nonesuch", ""}, // unknown command
	}
	for _, c := range cases {
		if got := CommandSetsState(def, c.op); got != c.want {
			t.Errorf("CommandSetsState(%q) = %q, want %q", c.op, got, c.want)
		}
	}
}

// TestValidate_SetsStateUnknownTag: a command whose sets_state names a tag no
// State marker carries fails validation — otherwise the command path would
// synthesize a push nothing maps and the change would silently never display.
func TestValidate_SetsStateUnknownTag(t *testing.T) {
	def := civTestDef()
	for i := range def.Commands {
		if def.Commands[i].Name == "set_freq" {
			def.Commands[i].SetsState = "NOSUCHTAG"
		}
	}
	if err := ValidateRigDefinition(def); err == nil {
		t.Error("ValidateRigDefinition(bad sets_state) = nil, want error")
	}

	// A sets_state that DOES name a real marker validates fine.
	for i := range def.Commands {
		if def.Commands[i].Name == "set_freq" {
			def.Commands[i].SetsState = "VFOAFREQ"
		}
	}
	if err := ValidateRigDefinition(def); err != nil {
		t.Errorf("ValidateRigDefinition(valid sets_state) = %v, want nil", err)
	}
}
